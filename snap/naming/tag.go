// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package naming

import (
	"errors"
	"fmt"
	"strings"
)

var errInvalidSecurityTag = errors.New("invalid security tag")

// SecurityTag exposes details of a validated snap security tag.
type SecurityTag interface {
	// String returns the entire security tag.
	String() string

	// InstanceName returns the snap name and instance key.
	InstanceName() string
}

// AppSecurityTag exposes details of a validated snap application security tag.
type AppSecurityTag interface {
	SecurityTag
	// AppName returns the name of the application.
	AppName() string
}

type appSecurityTag struct {
	instanceName string
	appName      string
}

func (t appSecurityTag) String() string {
	return fmt.Sprintf("snap.%s.%s", t.instanceName, t.appName)
}

func (t appSecurityTag) InstanceName() string {
	return t.instanceName
}

func (t appSecurityTag) AppName() string {
	return t.appName
}

// HookSecurityTag exposes details of a validated snap hook security tag.
type HookSecurityTag interface {
	SecurityTag
	// HookName returns the name of the hook.
	HookName() string

	// ComponentName returns the name of the component that this hook is
	// associated with, if there is one. If this hook isn't a component hook,
	// this will return an empty string.
	ComponentName() string
}

type hookSecurityTag struct {
	instanceName  string
	hookName      string
	componentName string
}

func (t hookSecurityTag) String() string {
	return fmt.Sprintf("snap.%s.hook.%s", t.instanceName, t.hookName)
}

func (t hookSecurityTag) InstanceName() string {
	return t.instanceName
}

func (t hookSecurityTag) HookName() string {
	return t.hookName
}

func (t hookSecurityTag) ComponentName() string {
	return t.componentName
}

// ParseSecurityTag parses a snap security tag and returns a parsed representation or an error.
//
// Further type assertions can be used to described the particular form, either
// describing an application or a hook specific security tag.
func ParseSecurityTag(tag string) (SecurityTag, error) {
	// We expect at most four parts. Split with up to five parts so that the
	// len(parts) test catches invalid format tags very early.
	parts := strings.SplitN(tag, ".", 5)
	// We expect either three or four components.
	if len(parts) != 3 && len(parts) != 4 {
		return nil, errInvalidSecurityTag
	}
	// We expect "snap" and the snap instance name as first two fields.
	snapLiteral, snapName := parts[0], parts[1]
	if snapLiteral != "snap" {
		return nil, errInvalidSecurityTag
	}
	// Depending on the type of the tag we either expect application name or
	// the "hook" literal and the hook name.
	if len(parts) == 3 {
		// TODO: if components get apps, we can lift this check out of the if
		// statement and move the strings.Cut to earlier in the function
		if err := ValidateInstance(snapName); err != nil {
			return nil, errInvalidSecurityTag
		}

		appName := parts[2]
		if err := ValidateApp(appName); err != nil {
			return nil, errInvalidSecurityTag
		}
		return &appSecurityTag{instanceName: snapName, appName: appName}, nil
	}

	hookLiteral, hookName := parts[2], parts[3]
	if hookLiteral != "hook" {
		return nil, errInvalidSecurityTag
	}
	if err := ValidateHook(hookName); err != nil {
		return nil, errInvalidSecurityTag
	}

	snapName, componentName, isComponent := strings.Cut(snapName, "+")
	if err := ValidateInstance(snapName); err != nil {
		return nil, errInvalidSecurityTag
	}

	if isComponent {
		if err := ValidateSnap(componentName); err != nil {
			return nil, errInvalidSecurityTag
		}
	}

	return &hookSecurityTag{
		instanceName:  snapName,
		hookName:      hookName,
		componentName: componentName,
	}, nil
}

// ParseAppSecurityTag parses an app security tag.
func ParseAppSecurityTag(tag string) (AppSecurityTag, error) {
	parsedTag, err := ParseSecurityTag(tag)
	if err != nil {
		return nil, err
	}
	if parsedAppTag, ok := parsedTag.(AppSecurityTag); ok {
		return parsedAppTag, nil
	}
	return nil, fmt.Errorf("%q is not an app security tag", tag)
}

// ParseHookSecurityTag parses a hook security tag.
func ParseHookSecurityTag(tag string) (HookSecurityTag, error) {
	parsedTag, err := ParseSecurityTag(tag)
	if err != nil {
		return nil, err
	}
	if parsedHookTag, ok := parsedTag.(HookSecurityTag); ok {
		return parsedHookTag, nil
	}
	return nil, fmt.Errorf("%q is not a hook security tag", tag)
}
