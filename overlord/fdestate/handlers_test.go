// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

/*
 * Copyright (C) 2025 Canonical Ltd
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

package fdestate_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/strutil"
)

func (s *fdeMgrSuite) TestDoChangeAuthKeys(c *C) {
	const onClassic = true
	s.startedManager(c, onClassic)
	s.mockCurrentKeys(c, []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "default-recovery"}}, nil)

	s.st.Lock()
	defer s.st.Unlock()

	type testcase struct {
		keyslots    []fdestate.KeyslotRef
		authMode    device.AuthMode
		noOpt       bool
		errOn       []string
		expectedErr string
	}
	tcs := []testcase{
		{
			keyslots: []fdestate.KeyslotRef{
				{ContainerRole: "system-data", Name: "default"},
				{ContainerRole: "system-data", Name: "default-fallback"},
				{ContainerRole: "system-save", Name: "default-fallback"},
			},
			authMode: device.AuthModePassphrase,
		},
		{
			keyslots:    []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "default"}},
			noOpt:       true,
			expectedErr: "failed to find cached authentication options: no entry found in cache",
		},
		{
			keyslots:    []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "not-found"}},
			expectedErr: `key slot reference \(container-role: "system-data", name: "not-found"\) not found`,
		},
		{
			keyslots: []fdestate.KeyslotRef{
				{ContainerRole: "system-data", Name: "default"},
				{ContainerRole: "system-data", Name: "default-fallback"},
				{ContainerRole: "system-save", Name: "default-fallback"},
			},
			authMode:    device.AuthModePassphrase,
			errOn:       []string{"read:/dev/disk/by-uuid/data:default-fallback"},
			expectedErr: `failed to read key data for \(container-role: "system-data", name: "default-fallback"\): failed to read key data for "default-fallback" from "/dev/disk/by-uuid/data": read error on /dev/disk/by-uuid/data:default-fallback`,
		},
		{
			keyslots:    []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "default"}},
			authMode:    device.AuthModePassphrase,
			errOn:       []string{"change:/dev/disk/by-uuid/data:default"},
			expectedErr: `failed to change passphrase for \(container-role: "system-data", name: "default"\): change error on /dev/disk/by-uuid/data:default`,
		},
		{
			keyslots:    []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "default"}},
			authMode:    device.AuthModePassphrase,
			errOn:       []string{"write:/dev/disk/by-uuid/data:default"},
			expectedErr: `failed to write key data for \(container-role: "system-data", name: "default"\): write error on /dev/disk/by-uuid/data:default`,
		},
		{
			keyslots:    []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "default"}},
			authMode:    device.AuthModePIN,
			expectedErr: "internal error: changing PINs is not implemented",
		},
		{
			keyslots:    []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "default"}},
			authMode:    device.AuthModeNone,
			expectedErr: `internal error: unexpected auth-mode "none"`,
		},
	}
	for _, tc := range tcs {
		task := s.st.NewTask("change-auth-keys", "test")
		task.Set("keyslots", tc.keyslots)
		task.Set("auth-mode", tc.authMode)

		if !tc.noOpt {
			s.st.Unlock()
			defer fdestate.MockChangeAuthOptionsInCache(s.st, "old", "new")()
			s.st.Lock()
		}

		defer fdestate.MockSecbootReadContainerKeyData(func(devicePath, slotName string) (secboot.KeyData, error) {
			entry := fmt.Sprintf("%s:%s", devicePath, slotName)
			if strutil.ListContains(tc.errOn, fmt.Sprintf("read:%s:%s", devicePath, slotName)) {
				return nil, fmt.Errorf("read error on %s", entry)
			}
			return &mockKeyData{
				changePassphrase: func(oldPassphrase, newPassphrase string) error {
					c.Check(oldPassphrase, Equals, "old")
					c.Check(newPassphrase, Equals, "new")
					if strutil.ListContains(tc.errOn, fmt.Sprintf("change:%s:%s", devicePath, slotName)) {
						return fmt.Errorf("change error on %s", entry)
					}
					return nil
				},
				writeTokenAtomic: func(devicePath, slotName string) error {
					if strutil.ListContains(tc.errOn, fmt.Sprintf("write:%s:%s", devicePath, slotName)) {
						return fmt.Errorf("write error on %s", entry)
					}
					return nil
				},
			}, nil
		})()

		chg := s.st.NewChange("sample", "...")
		chg.AddTask(task)

		s.settle(c)

		// auth options are removed
		c.Assert(fdestate.GetChangeAuthOptionsFromCache(s.st), IsNil)

		if tc.expectedErr == "" {
			c.Check(chg.Status(), Equals, state.DoneStatus)
		} else {
			c.Check(chg.Err(), ErrorMatches, fmt.Sprintf(`cannot perform the following tasks:
- test \(%s\)`, tc.expectedErr))
		}
	}
}
