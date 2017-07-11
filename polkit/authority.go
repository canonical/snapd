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
	"github.com/godbus/dbus"
)

type CheckAuthorizationFlags uint32

const (
	CheckAuthorizationNone                 CheckAuthorizationFlags = 0x00
	CheckAuthorizationAllowUserInteraction CheckAuthorizationFlags = 0x01
)

// CheckAuthorization queries polkit to determine whether a subject is
// authorized to perform an action.
func CheckAuthorization(subject Subject, actionId string, details map[string]string, flags CheckAuthorizationFlags) (result AuthorizationResult, err error) {
	s, err := subject.serialize()
	if err != nil {
		return
	}
	bus, err := dbus.SystemBus()
	if err != nil {
		return
	}
	authority := bus.Object("org.freedesktop.PolicyKit1",
		"/org/freedesktop/PolicyKit1/Authority")

	err = authority.Call(
		"org.freedesktop.PolicyKit1.Authority.CheckAuthorization", 0,
		s, actionId, details, flags, "").Store(&result)
	return
}

type serializedSubject struct {
	Kind    string
	Details map[string]dbus.Variant
}

type Subject interface {
	serialize() (serializedSubject, error)
}

type ProcessSubject struct {
	Pid       int
	StartTime uint64
}

func (s ProcessSubject) serialize() (serializedSubject, error) {
	details := make(map[string]dbus.Variant)
	details["pid"] = dbus.MakeVariant(uint32(s.Pid))
	if s.StartTime == 0 {
		var err error
		if s.StartTime, err = getStartTimeForPid(s.Pid); err != nil {
			return serializedSubject{}, err
		}
	}
	details["start-time"] = dbus.MakeVariant(s.StartTime)
	return serializedSubject{"unix-process", details}, nil
}

type AuthorizationResult struct {
	IsAuthorized bool
	IsChallenge  bool
	Details      map[string]string
}
