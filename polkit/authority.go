// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package polkit

import (
	"errors"

	"github.com/godbus/dbus"
)

type CheckAuthorizationFlags uint32

const (
	CheckAuthorizationNone                 CheckAuthorizationFlags = 0x00
	CheckAuthorizationAllowUserInteraction CheckAuthorizationFlags = 0x01
)

var (
	ErrDismissed           = errors.New("Authorization request dismissed")
	ErrInteractionRequired = errors.New("Authorization requires interaction")
)

func checkAuthorization(subject subject, actionId string, details map[string]string, flags CheckAuthorizationFlags) (bool, error) {
	bus, err := dbus.SystemBus()
	if err != nil {
		return false, err
	}
	authority := bus.Object("org.freedesktop.PolicyKit1",
		"/org/freedesktop/PolicyKit1/Authority")

	var result authorizationResult
	err = authority.Call(
		"org.freedesktop.PolicyKit1.Authority.CheckAuthorization", 0,
		subject, actionId, details, flags, "").Store(&result)
	if err != nil && !result.IsAuthorized {
		if result.IsChallenge {
			err = ErrInteractionRequired
		} else if result.Details["polkit.dismissed"] != "" {
			err = ErrDismissed
		}
	}
	return result.IsAuthorized, err
}

// CheckAuthorizationForPid queries polkit to determine whether a process is
// authorized to perform an action.
func CheckAuthorizationForPid(pid uint32, actionId string, details map[string]string, flags CheckAuthorizationFlags) (bool, error) {
	subject := subject{
		Kind:    "unix-process",
		Details: make(map[string]dbus.Variant),
	}
	subject.Details["pid"] = dbus.MakeVariant(pid)
	if startTime, err := getStartTimeForPid(pid); err == nil {
		subject.Details["start-time"] = dbus.MakeVariant(startTime)
	} else {
		return false, err
	}
	return checkAuthorization(subject, actionId, details, flags)
}

type subject struct {
	Kind    string
	Details map[string]dbus.Variant
}

type authorizationResult struct {
	IsAuthorized bool
	IsChallenge  bool
	Details      map[string]string
}
