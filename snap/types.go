// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snap

import (
	"encoding/json"
	"fmt"
)

// Type represents the kind of snap (app, core, gadget, os, kernel, snapd)
type Type string

// The various types of snap parts we support
const (
	TypeApp    Type = "app"
	TypeGadget Type = "gadget"
	TypeKernel Type = "kernel"
	TypeBase   Type = "base"
	TypeSnapd  Type = "snapd"

	// FIXME: this really should be TypeCore
	TypeOS Type = "os"
)

// This is the sort order from least important to most important for
// types. On e.g. firstboot this will be used to order the snaps this
// way.
var typeOrder = map[Type]int{
	TypeApp:    50,
	TypeGadget: 40,
	TypeBase:   30,
	TypeKernel: 20,
	TypeOS:     10,
	TypeSnapd:  0,
}

func (m Type) SortsBefore(other Type) bool {
	return typeOrder[m] < typeOrder[other]
}

// UnmarshalJSON sets *m to a copy of data.
func (m *Type) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}

	return m.fromString(str)
}

// UnmarshalYAML so Type implements yaml's Unmarshaler interface
func (m *Type) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var str string
	if err := unmarshal(&str); err != nil {
		return err
	}

	return m.fromString(str)
}

// fromString converts str to Type and sets *m to it if validations pass
func (m *Type) fromString(str string) error {
	t := Type(str)

	// this is a workaround as the store sends "application" but snappy uses
	// "app" for TypeApp
	if str == "application" {
		t = TypeApp
	}

	if t != TypeApp && t != TypeGadget && t != TypeOS && t != TypeKernel && t != TypeBase && t != TypeSnapd {
		return fmt.Errorf("invalid snap type: %q", str)
	}

	*m = t

	return nil
}

// ConfinementType represents the kind of confinement supported by the snap
// (devmode only, or strict confinement)
type ConfinementType string

// The various confinement types we support
const (
	DevModeConfinement ConfinementType = "devmode"
	ClassicConfinement ConfinementType = "classic"
	StrictConfinement  ConfinementType = "strict"
)

// UnmarshalJSON sets *confinementType to a copy of data, assuming validation passes
func (confinementType *ConfinementType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	return confinementType.fromString(s)
}

// UnmarshalYAML so ConfinementType implements yaml's Unmarshaler interface
func (confinementType *ConfinementType) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}

	return confinementType.fromString(s)
}

func (confinementType *ConfinementType) fromString(str string) error {
	c := ConfinementType(str)
	if c != DevModeConfinement && c != ClassicConfinement && c != StrictConfinement {
		return fmt.Errorf("invalid confinement type: %q", str)
	}

	*confinementType = c

	return nil
}

type ServiceStopReason string

const (
	StopReasonRefresh ServiceStopReason = "refresh"
	StopReasonRemove  ServiceStopReason = "remove"
	StopReasonDisable ServiceStopReason = "disable"
	StopReasonOther   ServiceStopReason = ""
)

// DaemonScope represents the scope of the daemon running under systemd
type DaemonScope string

const (
	SystemDaemon DaemonScope = "system"
	UserDaemon   DaemonScope = "user"
)

// UnmarshalJSON sets *daemonScope to a copy of data, assuming validation passes
func (daemonScope *DaemonScope) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	return daemonScope.fromString(s)
}

// UnmarshalYAML so DaemonScope implements yaml's Unmarshaler interface
func (daemonScope *DaemonScope) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}

	return daemonScope.fromString(s)
}

func (daemonScope *DaemonScope) fromString(str string) error {
	d := DaemonScope(str)
	if d != SystemDaemon && d != UserDaemon {
		return fmt.Errorf("invalid daemon scope: %q", str)
	}

	*daemonScope = d
	return nil
}

// ComponentType is a type to represent the possible types of snap components.
type ComponentType string

const (
	// TestComponent is just for testing purposes.
	TestComponent ComponentType = "test"
	// KernelModulesComponent is for components containing modules/firmware
	KernelModulesComponent ComponentType = "kernel-modules"
)

var validComponentTypes = [...]ComponentType{TestComponent, KernelModulesComponent}

// ComponentTypeFromString converts a string to a ComponentType. An error is
// returned if the string is not a valid ComponentType.
func ComponentTypeFromString(t string) (ComponentType, error) {
	for _, valid := range validComponentTypes {
		if t == string(valid) {
			return valid, nil
		}
	}
	return "", fmt.Errorf("invalid component type %q", t)
}

// Component represents a snap component.
type Component struct {
	Type          ComponentType
	Summary       string
	Description   string
	Name          string
	ExplicitHooks map[string]*HookInfo
}

func (ct *ComponentType) UnmarshalYAML(unmarshall func(interface{}) error) error {
	typeStr := ""
	if err := unmarshall(&typeStr); err != nil {
		return err
	}

	for _, t := range validComponentTypes {
		if string(t) == typeStr {
			*ct = t
			return nil
		}
	}

	return fmt.Errorf("unknown component type %q", typeStr)
}
