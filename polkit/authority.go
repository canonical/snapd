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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"
)

type CheckFlags uint32

const (
	CheckNone             CheckFlags = 0x00
	CheckAllowInteraction CheckFlags = 0x01
)

var (
	ErrDismissed   = errors.New("Authorization request dismissed")
	ErrInteraction = errors.New("Authorization requires interaction")
)

func checkAuthorization(subject authSubject, actionId string, details map[string]string, flags CheckFlags) (bool, error) {
	bus := mylog.Check2(dbus.SystemBus())

	authority := bus.Object("org.freedesktop.PolicyKit1",
		"/org/freedesktop/PolicyKit1/Authority")

	var result authResult
	mylog.Check(authority.Call(
		"org.freedesktop.PolicyKit1.Authority.CheckAuthorization", 0,
		subject, actionId, details, flags, "").Store(&result))

	if !result.IsAuthorized {
		if result.IsChallenge {
			err = ErrInteraction
		} else if result.Details["polkit.dismissed"] != "" {
			err = ErrDismissed
		}
	}
	return result.IsAuthorized, err
}

// CheckAuthorization queries polkit to determine whether a process is
// authorized to perform an action.
func CheckAuthorization(pid int32, uid uint32, actionId string, details map[string]string, flags CheckFlags) (bool, error) {
	subject := authSubject{
		Kind:    "unix-process",
		Details: make(map[string]dbus.Variant),
	}
	subject.Details["pid"] = dbus.MakeVariant(uint32(pid)) // polkit is *wrong*!
	startTime := mylog.Check2(getStartTimeForPid(pid))

	// While discovering the pid's start time is racy, it isn't security
	// relevant since it only impacts expiring the permission after
	// process exit.
	subject.Details["start-time"] = dbus.MakeVariant(startTime)
	subject.Details["uid"] = dbus.MakeVariant(uid)
	return checkAuthorization(subject, actionId, details, flags)
}

type authSubject struct {
	Kind    string
	Details map[string]dbus.Variant
}

type authResult struct {
	IsAuthorized bool
	IsChallenge  bool
	Details      map[string]string
}
