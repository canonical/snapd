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
	"errors"
	"fmt"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

// state must be locked
func (s *fdeMgrSuite) settle(c *C) {
	s.st.Unlock()
	defer s.st.Lock()
	err := s.o.Settle(testutil.HostScaledTimeout(10 * time.Second))
	c.Assert(err, IsNil)
}

func (s *fdeMgrSuite) TestKeyslotRefValidate(c *C) {
	k := fdestate.KeyslotRef{ContainerRole: "system-data", Name: "some-keyslot"}
	c.Assert(k.Validate(), IsNil)

	k = fdestate.KeyslotRef{ContainerRole: "system-save", Name: "some-other-keyslot"}
	c.Assert(k.Validate(), IsNil)

	k = fdestate.KeyslotRef{ContainerRole: "some-container", Name: "some-keyslot"}
	c.Assert(k.Validate(), ErrorMatches, `unsupported container role "some-container", expected "system-data" or "system-save"`)

	k = fdestate.KeyslotRef{Name: "some-keyslot"}
	c.Assert(k.Validate(), ErrorMatches, "container role cannot be empty")

	k = fdestate.KeyslotRef{ContainerRole: "system-save", Name: ""}
	c.Assert(k.Validate(), ErrorMatches, "name cannot be empty")
}

func (s *fdeMgrSuite) testReplaceRecoveryKey(c *C, defaultKeyslots bool) {
	keyslots := []fdestate.KeyslotRef{
		{ContainerRole: "system-data", Name: "default-recovery"},
	}
	tmpKeyslots := []fdestate.KeyslotRef{
		{ContainerRole: "system-data", Name: "snapd-tmp:default-recovery"},
	}
	if defaultKeyslots {
		// system-save also
		keyslots = append(keyslots, fdestate.KeyslotRef{ContainerRole: "system-save", Name: "default-recovery"})
		tmpKeyslots = append(tmpKeyslots, fdestate.KeyslotRef{ContainerRole: "system-save", Name: "snapd-tmp:default-recovery"})
	}

	s.mockDeviceInState(&asserts.Model{}, "run")

	defer fdestate.MockDisksDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		switch mountpoint {
		case filepath.Join(dirs.GlobalRootDir, "run/mnt/data"):
			return "aaa", nil
		case dirs.SnapSaveDir:
			return "bbb", nil
		}
		panic(fmt.Sprintf("missing mocked mount point %q", mountpoint))
	})()

	defer fdestate.MockSecbootListContainerRecoveryKeyNames(func(devicePath string) ([]string, error) {
		switch devicePath {
		case "/dev/disk/by-uuid/aaa":
			return []string{"default-recovery"}, nil
		case "/dev/disk/by-uuid/bbb":
			return []string{"default-recovery"}, nil
		default:
			return nil, fmt.Errorf("unexpected devicePath %q", devicePath)
		}
	})()

	defer fdestate.MockSecbootListContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		switch devicePath {
		case "/dev/disk/by-uuid/aaa":
			return []string{}, nil
		case "/dev/disk/by-uuid/bbb":
			return []string{}, nil
		default:
			return nil, fmt.Errorf("unexpected devicePath %q", devicePath)
		}
	})()

	// initialize fde manager
	onClassic := true
	manager := s.startedManager(c, onClassic)

	_, recoveryKeyID, err := manager.GenerateRecoveryKey()
	c.Assert(err, IsNil)

	s.st.Lock()
	defer s.st.Unlock()

	var ts *state.TaskSet
	if defaultKeyslots {
		ts, err = fdestate.ReplaceRecoveryKey(s.st, recoveryKeyID, nil)
	} else {
		ts, err = fdestate.ReplaceRecoveryKey(s.st, recoveryKeyID, keyslots)
	}
	c.Assert(err, IsNil)
	c.Assert(ts, NotNil)
	tsks := ts.Tasks()
	c.Check(tsks, HasLen, 3)

	c.Check(tsks[0].Summary(), Matches, "Add temporary recovery key slots")
	c.Check(tsks[0].Kind(), Equals, "add-recovery-keys")
	// check recovery key ID is passed to task
	var tskRecoveryKeyID string
	c.Assert(tsks[0].Get("recovery-key-id", &tskRecoveryKeyID), IsNil)
	c.Check(tskRecoveryKeyID, Equals, recoveryKeyID)
	// check tmp key slots are passed to task
	var tskKeyslots []fdestate.KeyslotRef
	c.Assert(tsks[0].Get("keyslots", &tskKeyslots), IsNil)
	c.Check(tskKeyslots, DeepEquals, tmpKeyslots)

	c.Check(tsks[1].Summary(), Matches, "Remove old recovery key slots")
	c.Check(tsks[1].Kind(), Equals, "remove-keys")
	// check target key slots are passed to task
	c.Assert(tsks[1].Get("keyslots", &tskKeyslots), IsNil)
	c.Check(tskKeyslots, DeepEquals, keyslots)

	c.Check(tsks[2].Summary(), Matches, "Rename temporary recovery key slots")
	c.Check(tsks[2].Kind(), Equals, "rename-keys")
	// check tmp key slots are passed to task
	c.Assert(tsks[2].Get("keyslots", &tskKeyslots), IsNil)
	c.Check(tskKeyslots, DeepEquals, tmpKeyslots)
	// and renames are also passed
	var renames map[string]string
	c.Assert(tsks[2].Get("renames", &renames), IsNil)
	if defaultKeyslots {
		c.Check(renames, DeepEquals, map[string]string{
			`(container-role: "system-data", name: "snapd-tmp:default-recovery")`: "default-recovery",
			`(container-role: "system-save", name: "snapd-tmp:default-recovery")`: "default-recovery",
		})
	} else {
		c.Check(renames, DeepEquals, map[string]string{
			`(container-role: "system-data", name: "snapd-tmp:default-recovery")`: "default-recovery",
		})
	}
}

func (s *fdeMgrSuite) TestReplaceRecoveryKey(c *C) {
	const defaultKeyslots = false
	s.testReplaceRecoveryKey(c, defaultKeyslots)
}

func (s *fdeMgrSuite) TestReplaceRecoveryKeyDefaultKeyslots(c *C) {
	const defaultKeyslots = true
	s.testReplaceRecoveryKey(c, defaultKeyslots)
}

func (s *fdeMgrSuite) TestReplaceRecoveryKeyErrors(c *C) {
	mockStore := &mockRecoveryKeyCache{
		getRecoveryKey: func(keyID string) (rkeyInfo backend.CachedRecoverKey, err error) {
			switch keyID {
			case "good-key-id":
				return backend.CachedRecoverKey{Expiration: time.Now().Add(100 * time.Hour)}, nil
			case "expired-key-id":
				return backend.CachedRecoverKey{Expiration: time.Now().Add(-100 * time.Hour)}, nil
			default:
				return backend.CachedRecoverKey{}, backend.ErrNoRecoveryKey
			}
		},
	}
	defer fdestate.MockBackendNewInMemoryRecoveryKeyCache(func() backend.RecoveryKeyCache {
		return mockStore
	})()

	// mock no existing key slots
	s.mockDeviceInState(&asserts.Model{}, "run")
	defer fdestate.MockDisksDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		return "", nil
	})()
	defer fdestate.MockSecbootListContainerRecoveryKeyNames(func(devicePath string) ([]string, error) {
		return nil, nil
	})()
	defer fdestate.MockSecbootListContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		return nil, nil
	})()

	// initialize fde manager
	onClassic := true
	s.startedManager(c, onClassic)

	keyslots := []fdestate.KeyslotRef{
		{ContainerRole: "system-data", Name: "default-recovery"},
		{ContainerRole: "system-save", Name: "default-recovery"},
	}

	s.st.Lock()
	defer s.st.Unlock()

	// invalid recovery key id
	_, err := fdestate.ReplaceRecoveryKey(s.st, "bad-key-id", keyslots)
	c.Assert(err, ErrorMatches, "invalid recovery key ID: no recovery key entry for key-id")

	// expired recovery key id
	_, err = fdestate.ReplaceRecoveryKey(s.st, "expired-key-id", keyslots)
	c.Assert(err, ErrorMatches, "invalid recovery key ID: recovery key has expired")

	// invalid keyslot
	badKeyslot := fdestate.KeyslotRef{ContainerRole: "", Name: "some-name"}
	_, err = fdestate.ReplaceRecoveryKey(s.st, "good-key-id", []fdestate.KeyslotRef{badKeyslot})
	c.Assert(err, ErrorMatches, `invalid key slot reference \(container-role: "", name: "some-name"\): container role cannot be empty`)

	// invalid keyslot
	badKeyslot = fdestate.KeyslotRef{ContainerRole: "system-data", Name: "default-fallback"}
	_, err = fdestate.ReplaceRecoveryKey(s.st, "good-key-id", []fdestate.KeyslotRef{badKeyslot})
	c.Assert(err, ErrorMatches, `invalid key slot reference \(container-role: "system-data", name: "default-fallback"\): unsupported name, expected "default-recovery"`)

	// missing keyslots
	_, err = fdestate.ReplaceRecoveryKey(s.st, "good-key-id", keyslots)
	c.Assert(err, ErrorMatches, `key slot references \[\(container-role: "system-data", name: "default-recovery"\), \(container-role: "system-save", name: "default-recovery"\)\] not found`)
	var notFoundErr *fdestate.KeyslotRefsNotFoundError
	c.Assert(errors.As(err, &notFoundErr), Equals, true)
	c.Check(notFoundErr.KeyslotRefs, DeepEquals, keyslots)

	// change conflict
	chg := s.st.NewChange("fde-replace-recovery-key", "")
	task := s.st.NewTask("some-fde-task", "")
	chg.AddTask(task)
	_, err = fdestate.ReplaceRecoveryKey(s.st, "good-key-id", keyslots)
	c.Assert(err, ErrorMatches, `replacing recovery key in progress, no other FDE changes allowed until this is done`)
	c.Check(err, testutil.ErrorIs, &snapstate.ChangeConflictError{})
}

func (s *fdeMgrSuite) TestEnsureLoopLogging(c *C) {
	testutil.CheckEnsureLoopLogging("fdemgr.go", c, false)
}
