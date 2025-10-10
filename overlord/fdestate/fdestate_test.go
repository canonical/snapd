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
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	sb "github.com/snapcore/secboot"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
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
	c.Check(tsks[0].Kind(), Equals, "fde-add-recovery-keys")
	// check recovery key ID is passed to task
	var tskRecoveryKeyID string
	c.Assert(tsks[0].Get("recovery-key-id", &tskRecoveryKeyID), IsNil)
	c.Check(tskRecoveryKeyID, Equals, recoveryKeyID)
	// check tmp key slots are passed to task
	var tskKeyslots []fdestate.KeyslotRef
	c.Assert(tsks[0].Get("keyslots", &tskKeyslots), IsNil)
	c.Check(tskKeyslots, DeepEquals, tmpKeyslots)

	c.Check(tsks[1].Summary(), Matches, "Remove old recovery key slots")
	c.Check(tsks[1].Kind(), Equals, "fde-remove-keys")
	// check target key slots are passed to task
	c.Assert(tsks[1].Get("keyslots", &tskKeyslots), IsNil)
	c.Check(tskKeyslots, DeepEquals, keyslots)

	c.Check(tsks[2].Summary(), Matches, "Rename temporary recovery key slots")
	c.Check(tsks[2].Kind(), Equals, "fde-rename-keys")
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

	// mock no existing key slots, except one non-recovery keyslot
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
		return nil, nil
	})()
	defer fdestate.MockSecbootListContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		if devicePath == "/dev/disk/by-uuid/aaa" {
			return []string{"default-recovery"}, nil
		}
		return nil, nil
	})()

	// initialize fde manager
	onClassic := true
	s.startedManager(c, onClassic)

	keyslots := []fdestate.KeyslotRef{{ContainerRole: "system-save", Name: "default-recovery"}}

	s.st.Lock()
	defer s.st.Unlock()

	// invalid recovery key id
	_, err := fdestate.ReplaceRecoveryKey(s.st, "bad-key-id", keyslots)
	c.Assert(err, ErrorMatches, "invalid recovery key: not found")
	var rkeyErr *fdestate.InvalidRecoveryKeyError
	c.Assert(errors.As(err, &rkeyErr), Equals, true)
	c.Assert(rkeyErr.Reason, Equals, fdestate.InvalidRecoveryKeyReasonNotFound)

	// expired recovery key id
	_, err = fdestate.ReplaceRecoveryKey(s.st, "expired-key-id", keyslots)
	c.Assert(err, ErrorMatches, "invalid recovery key: expired")
	c.Assert(errors.As(err, &rkeyErr), Equals, true)
	c.Assert(rkeyErr.Reason, Equals, fdestate.InvalidRecoveryKeyReasonExpired)

	// invalid keyslot
	badKeyslot := fdestate.KeyslotRef{ContainerRole: "", Name: "some-name"}
	_, err = fdestate.ReplaceRecoveryKey(s.st, "good-key-id", []fdestate.KeyslotRef{badKeyslot})
	c.Assert(err, ErrorMatches, `invalid key slot reference \(container-role: "", name: "some-name"\): container role cannot be empty`)

	// invalid keyslot
	badKeyslot = fdestate.KeyslotRef{ContainerRole: "system-data", Name: "default-fallback"}
	_, err = fdestate.ReplaceRecoveryKey(s.st, "good-key-id", []fdestate.KeyslotRef{badKeyslot})
	c.Assert(err, ErrorMatches, `invalid key slot reference \(container-role: "system-data", name: "default-fallback"\): unsupported name, expected "default-recovery"`)

	// invalid keyslot
	badKeyslot = fdestate.KeyslotRef{ContainerRole: "system-data", Name: "default-recovery"}
	_, err = fdestate.ReplaceRecoveryKey(s.st, "good-key-id", []fdestate.KeyslotRef{badKeyslot})
	c.Assert(err, ErrorMatches, `invalid key slot reference \(container-role: "system-data", name: "default-recovery"\): unsupported type "platform", expected "recovery"`)

	// missing keyslots
	_, err = fdestate.ReplaceRecoveryKey(s.st, "good-key-id", keyslots)
	c.Assert(err, ErrorMatches, `key slot reference \(container-role: "system-save", name: "default-recovery"\) not found`)
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

func (s *fdeMgrSuite) testChangeAuth(c *C, authMode device.AuthMode, withWarning, defaultKeyslots bool) {
	keyslots := []fdestate.KeyslotRef{
		{ContainerRole: "system-data", Name: "default"},
	}
	if defaultKeyslots {
		keyslots = append(keyslots,
			fdestate.KeyslotRef{ContainerRole: "system-data", Name: "default-fallback"},
			fdestate.KeyslotRef{ContainerRole: "system-save", Name: "default-fallback"},
		)
	}

	s.mockCurrentKeys(c, nil, keyslots)

	defer fdestate.MockSecbootReadContainerKeyData(func(devicePath, slotName string) (secboot.KeyData, error) {
		switch fmt.Sprintf("%s:%s", devicePath, slotName) {
		case "/dev/disk/by-uuid/data:default",
			"/dev/disk/by-uuid/data:default-fallback",
			"/dev/disk/by-uuid/save:default-fallback":
			return &mockKeyData{authMode: device.AuthModePassphrase}, nil
		default:
			panic("unexpected container")
		}
	})()

	// initialize fde manager
	onClassic := true
	s.startedManager(c, onClassic)

	s.st.Lock()
	defer s.st.Unlock()

	logBuf, restore := logger.MockLogger()
	defer restore()
	if withWarning {
		s.st.Unlock()
		defer fdestate.MockChangeAuthOptionsInCache(s.st, "old-stale", "new-stale")()
		s.st.Lock()
	}

	var ts *state.TaskSet
	var err error
	if defaultKeyslots {
		ts, err = fdestate.ChangeAuth(s.st, authMode, "old", "new", nil)
	} else {
		ts, err = fdestate.ChangeAuth(s.st, authMode, "old", "new", keyslots)
	}
	c.Assert(err, IsNil)
	c.Assert(ts, NotNil)
	tsks := ts.Tasks()
	c.Check(tsks, HasLen, 1)

	c.Check(tsks[0].Kind(), Equals, "fde-change-auth")
	switch authMode {
	case device.AuthModePassphrase:
		c.Check(tsks[0].Summary(), Matches, "Change passphrase protected key slots")
	case device.AuthModePIN:
		c.Check(tsks[0].Summary(), Matches, "Change PIN protected key slots")
	default:
		c.Errorf("unexpected authMode %q", authMode)
	}
	// check target key slots are passed to task
	var tskKeyslots []fdestate.KeyslotRef
	c.Assert(tsks[0].Get("keyslots", &tskKeyslots), IsNil)
	c.Check(tskKeyslots, DeepEquals, keyslots)
	var tskAuthMode device.AuthMode
	c.Assert(tsks[0].Get("auth-mode", &tskAuthMode), IsNil)
	c.Check(tskAuthMode, Equals, authMode)

	authOptions := fdestate.GetChangeAuthOptionsFromCache(s.st)
	c.Check(authOptions.Old(), Equals, "old")
	c.Check(authOptions.New(), Equals, "new")

	if withWarning {
		c.Check(logBuf.String(), Matches, ".*WARNING: authentication change options already exists in memory\n")
	} else {
		c.Check(logBuf.Len(), Equals, 0)
	}
}

func (s *fdeMgrSuite) TestChangeAuthModePassphrase(c *C) {
	const authMode = device.AuthModePassphrase
	const withWarning = false
	const defaultKeyslots = false
	s.testChangeAuth(c, authMode, withWarning, defaultKeyslots)
}

func (s *fdeMgrSuite) TestChangeAuthWithCachedAuthOptionsWarning(c *C) {
	const authMode = device.AuthModePassphrase
	const withWarning = true
	const defaultKeyslots = false
	s.testChangeAuth(c, authMode, withWarning, defaultKeyslots)
}

func (s *fdeMgrSuite) TestChangeAuthModePassphraseDefaultKeyslots(c *C) {
	const authMode = device.AuthModePassphrase
	const withWarning = false
	const defaultKeyslots = true
	s.testChangeAuth(c, authMode, withWarning, defaultKeyslots)
}

func (s *fdeMgrSuite) TestChangeAuthModePIN(c *C) {
	// this should break when changing PIN support lands
	_, err := fdestate.ChangeAuth(s.st, device.AuthModePIN, "old", "new", []fdestate.KeyslotRef{})
	c.Assert(err, ErrorMatches, `internal error: unexpected authentication mode "pin"`)
}

func (s *fdeMgrSuite) TestChangeAuthErrors(c *C) {
	defer fdestate.MockSecbootReadContainerKeyData(func(devicePath, slotName string) (secboot.KeyData, error) {
		switch fmt.Sprintf("%s:%s", devicePath, slotName) {
		case "/dev/disk/by-uuid/data:default":
			return &mockKeyData{authMode: device.AuthModeNone}, nil
		case "/dev/disk/by-uuid/data:default-fallback":
			return nil, fmt.Errorf("boom!")
		default:
			panic("unexpected container")
		}
	})()

	s.mockCurrentKeys(c, nil, []fdestate.KeyslotRef{
		{ContainerRole: "system-data", Name: "default"},
		{ContainerRole: "system-data", Name: "default-fallback"},
	})

	// initialize fde manager
	onClassic := true
	s.startedManager(c, onClassic)

	s.st.Lock()
	defer s.st.Unlock()

	// unsupported auth mode
	_, err := fdestate.ChangeAuth(s.st, device.AuthModeNone, "old", "new", []fdestate.KeyslotRef{})
	c.Assert(err, ErrorMatches, `internal error: unexpected authentication mode "none"`)

	// invalid keyslot reference
	badKeyslot := fdestate.KeyslotRef{ContainerRole: "", Name: "some-name"}
	_, err = fdestate.ChangeAuth(s.st, device.AuthModePassphrase, "old", "new", []fdestate.KeyslotRef{badKeyslot})
	c.Assert(err, ErrorMatches, `invalid key slot reference \(container-role: "", name: "some-name"\): container role cannot be empty`)

	// invalid keyslot reference
	badKeyslot = fdestate.KeyslotRef{ContainerRole: "system-data", Name: "default-recovery"}
	_, err = fdestate.ChangeAuth(s.st, device.AuthModePassphrase, "old", "new", []fdestate.KeyslotRef{badKeyslot})
	c.Assert(err, ErrorMatches, `invalid key slot reference \(container-role: "system-data", name: "default-recovery"\): unsupported name, expected "default" or "default-fallback"`)

	// missing keyslot
	badKeyslot = fdestate.KeyslotRef{ContainerRole: "system-save", Name: "default-fallback"}
	_, err = fdestate.ChangeAuth(s.st, device.AuthModePassphrase, "old", "new", []fdestate.KeyslotRef{badKeyslot})
	c.Assert(err, ErrorMatches, `key slot reference \(container-role: "system-save", name: "default-fallback"\) not found`)
	var notFoundErr *fdestate.KeyslotRefsNotFoundError
	c.Assert(errors.As(err, &notFoundErr), Equals, true)
	c.Check(notFoundErr.KeyslotRefs, DeepEquals, []fdestate.KeyslotRef{badKeyslot})

	// bad keyslot auth mode
	badKeyslot = fdestate.KeyslotRef{ContainerRole: "system-data", Name: "default"}
	_, err = fdestate.ChangeAuth(s.st, device.AuthModePassphrase, "old", "new", []fdestate.KeyslotRef{badKeyslot})
	c.Assert(err, ErrorMatches, `invalid key slot reference \(container-role: "system-data", name: "default"\): unsupported authentication mode "none", expected "passphrase"`)

	// keyslot key data loading error
	badKeyslot = fdestate.KeyslotRef{ContainerRole: "system-data", Name: "default-fallback"}
	_, err = fdestate.ChangeAuth(s.st, device.AuthModePassphrase, "old", "new", []fdestate.KeyslotRef{badKeyslot})
	c.Assert(err, ErrorMatches, `cannot read key data for \(container-role: "system-data", name: "default-fallback"\): cannot read key data for "default-fallback" from "/dev/disk/by-uuid/data": boom!`)

	// bad keyslot type (recovery instead of platform)
	s.st.Unlock()
	s.mockCurrentKeys(c, []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "default"}}, nil)
	s.st.Lock()
	badKeyslot = fdestate.KeyslotRef{ContainerRole: "system-data", Name: "default"}
	_, err = fdestate.ChangeAuth(s.st, device.AuthModePassphrase, "old", "new", []fdestate.KeyslotRef{badKeyslot})
	c.Assert(err, ErrorMatches, `invalid key slot reference \(container-role: "system-data", name: "default"\): unsupported type "recovery", expected "platform"`)

	// change conflict
	chg := s.st.NewChange("fde-efi-secureboot-db-update", "")
	task := s.st.NewTask("some-fde-task", "")
	chg.AddTask(task)
	_, err = fdestate.ChangeAuth(s.st, device.AuthModePassphrase, "old", "new", []fdestate.KeyslotRef{})
	c.Assert(err, ErrorMatches, `external EFI DBX update in progress, no other FDE changes allowed until this is done`)
	c.Check(err, testutil.ErrorIs, &snapstate.ChangeConflictError{})
}

func (s *fdeMgrSuite) testSystemEncryptedFromState(c *C, hasEncryptedDisks bool) {
	onClassic := true
	if hasEncryptedDisks {
		s.startedManager(c, onClassic)
	} else {
		s.startedManagerNoEncryptedDisks(c, onClassic)
	}

	st := s.st
	st.Lock()
	defer st.Unlock()

	encrypted, err := fdestate.SystemEncryptedFromState(st)
	c.Assert(err, IsNil)
	c.Assert(encrypted, Equals, hasEncryptedDisks)
}

func (s *fdeMgrSuite) TestSystemEncryptedFromStateHasEncryptedDisks(c *C) {
	hasEncryptedDisks := true
	s.testSystemEncryptedFromState(c, hasEncryptedDisks)
}

func (s *fdeMgrSuite) TestSystemEncryptedFromStateNoEncryptedDisks(c *C) {
	hasEncryptedDisks := false
	s.testSystemEncryptedFromState(c, hasEncryptedDisks)
}

func (s *fdeMgrSuite) testReplacePlatformKey(c *C, authMode device.AuthMode, defaultKeyslots bool) {
	keyslots := []fdestate.KeyslotRef{
		{ContainerRole: "system-data", Name: "default"},
	}
	tmpKeyslots := []fdestate.KeyslotRef{
		{ContainerRole: "system-data", Name: "snapd-tmp:default"},
	}
	if defaultKeyslots {
		keyslots = append(keyslots, fdestate.KeyslotRef{ContainerRole: "system-data", Name: "default-fallback"})
		keyslots = append(keyslots, fdestate.KeyslotRef{ContainerRole: "system-save", Name: "default-fallback"})
		tmpKeyslots = append(tmpKeyslots, fdestate.KeyslotRef{ContainerRole: "system-data", Name: "snapd-tmp:default-fallback"})
		tmpKeyslots = append(tmpKeyslots, fdestate.KeyslotRef{ContainerRole: "system-save", Name: "snapd-tmp:default-fallback"})
	}

	keyType := "platform"
	var volumesAuth *device.VolumesAuthOptions
	switch authMode {
	case device.AuthModePassphrase:
		volumesAuth = &device.VolumesAuthOptions{Mode: device.AuthModePassphrase, Passphrase: "password"}
		keyType = "passphrase"
	case device.AuthModePIN:
		volumesAuth = &device.VolumesAuthOptions{Mode: device.AuthModePIN, PIN: "1234"}
		keyType = "pin"
	}

	s.mockDeviceInState(&asserts.Model{}, "run")
	s.mockCurrentKeys(c, nil, nil)

	defer fdestate.MockSecbootReadContainerKeyData(func(devicePath, slotName string) (secboot.KeyData, error) {
		switch fmt.Sprintf("%s:%s", devicePath, slotName) {
		case "/dev/disk/by-uuid/data:default",
			"/dev/disk/by-uuid/data:default-fallback",
			"/dev/disk/by-uuid/save:default-fallback":
			return &mockKeyData{authMode: device.AuthModePassphrase, roles: []string{"run"}, platformName: "tpm2"}, nil
		default:
			panic("unexpected container")
		}
	})()

	// initialize fde manager
	onClassic := true
	s.startedManager(c, onClassic)

	s.st.Lock()
	defer s.st.Unlock()

	var ts *state.TaskSet
	var err error
	if defaultKeyslots {
		ts, err = fdestate.ReplacePlatformKey(s.st, volumesAuth, nil)
	} else {
		ts, err = fdestate.ReplacePlatformKey(s.st, volumesAuth, keyslots)
	}
	if authMode == device.AuthModePIN {
		// this is expected to break when PIN support lands
		c.Assert(err, ErrorMatches, `"pin" authentication mode is not implemented`)
		c.Assert(ts, IsNil)
		return
	}
	c.Assert(err, IsNil)
	c.Assert(ts, NotNil)
	tsks := ts.Tasks()
	c.Check(tsks, HasLen, 3)

	c.Check(tsks[0].Summary(), Matches, fmt.Sprintf("Add temporary %s key slots", keyType))
	c.Check(tsks[0].Kind(), Equals, "fde-add-platform-keys")
	// check tmp key slots are passed to task
	var tskKeyslots []fdestate.KeyslotRef
	c.Assert(tsks[0].Get("keyslots", &tskKeyslots), IsNil)
	c.Check(tskKeyslots, DeepEquals, tmpKeyslots)
	var tskAuthMode device.AuthMode
	c.Assert(tsks[0].Get("auth-mode", &tskAuthMode), IsNil)
	c.Check(tskAuthMode, Equals, authMode)
	var tskRoles map[string][]string
	c.Assert(tsks[0].Get("roles", &tskRoles), IsNil)
	if defaultKeyslots {
		c.Check(tskRoles, DeepEquals, map[string][]string{
			`(container-role: "system-data", name: "snapd-tmp:default")`:          {"run"},
			`(container-role: "system-data", name: "snapd-tmp:default-fallback")`: {"run"},
			`(container-role: "system-save", name: "snapd-tmp:default-fallback")`: {"run"},
		})
	} else {
		c.Check(tskRoles, DeepEquals, map[string][]string{
			`(container-role: "system-data", name: "snapd-tmp:default")`: {"run"},
		})
	}

	c.Check(tsks[1].Summary(), Matches, fmt.Sprintf("Remove old %s key slots", keyType))
	c.Check(tsks[1].Kind(), Equals, "fde-remove-keys")
	// check target key slots are passed to task
	c.Assert(tsks[1].Get("keyslots", &tskKeyslots), IsNil)
	c.Check(tskKeyslots, DeepEquals, keyslots)

	c.Check(tsks[2].Summary(), Matches, fmt.Sprintf("Rename temporary %s key slots", keyType))
	c.Check(tsks[2].Kind(), Equals, "fde-rename-keys")
	// check tmp key slots are passed to task
	c.Assert(tsks[2].Get("keyslots", &tskKeyslots), IsNil)
	c.Check(tskKeyslots, DeepEquals, tmpKeyslots)
	// and renames are also passed
	var renames map[string]string
	c.Assert(tsks[2].Get("renames", &renames), IsNil)
	if defaultKeyslots {
		c.Check(renames, DeepEquals, map[string]string{
			`(container-role: "system-data", name: "snapd-tmp:default")`:          "default",
			`(container-role: "system-data", name: "snapd-tmp:default-fallback")`: "default-fallback",
			`(container-role: "system-save", name: "snapd-tmp:default-fallback")`: "default-fallback",
		})
	} else {
		c.Check(renames, DeepEquals, map[string]string{
			`(container-role: "system-data", name: "snapd-tmp:default")`: "default",
		})
	}

	if authMode == device.AuthModeNone {
		c.Check(s.st.Cached(fdestate.VolumesAuthOptionsKey()), IsNil)
	} else {
		c.Check(s.st.Cached(fdestate.VolumesAuthOptionsKey()), Equals, volumesAuth)
	}
}

func (s *fdeMgrSuite) TestReplacePlatformKeyAuthModeNone(c *C) {
	const defaultKeyslots = false
	const authMode = device.AuthModeNone
	s.testReplacePlatformKey(c, authMode, defaultKeyslots)
}

func (s *fdeMgrSuite) TestReplacePlatformKeyAuthModePassphrase(c *C) {
	const defaultKeyslots = false
	const authMode = device.AuthModePassphrase
	s.testReplacePlatformKey(c, authMode, defaultKeyslots)
}

func (s *fdeMgrSuite) TestReplacePlatformKeyAuthModePIN(c *C) {
	const defaultKeyslots = false
	const authMode = device.AuthModePIN
	s.testReplacePlatformKey(c, authMode, defaultKeyslots)
}

func (s *fdeMgrSuite) TestReplacePlatformKeyDefaultKeyslots(c *C) {
	const defaultKeyslots = true
	const authMode = device.AuthModeNone
	s.testReplacePlatformKey(c, authMode, defaultKeyslots)
}

func (s *fdeMgrSuite) TestReplacePlatformKeyErrors(c *C) {
	defer fdestate.MockSecbootReadContainerKeyData(func(devicePath, slotName string) (secboot.KeyData, error) {
		switch fmt.Sprintf("%s:%s", devicePath, slotName) {
		case "/dev/disk/by-uuid/data:default":
			return &mockKeyData{authMode: device.AuthModeNone}, nil
		case "/dev/disk/by-uuid/data:default-fallback":
			return nil, fmt.Errorf("boom!")
		default:
			panic("unexpected container")
		}
	})()

	s.mockCurrentKeys(c, nil, []fdestate.KeyslotRef{
		{ContainerRole: "system-data", Name: "default"},
		{ContainerRole: "system-data", Name: "default-fallback"},
	})

	// initialize fde manager
	onClassic := true
	s.startedManager(c, onClassic)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model, "run")

	s.st.Lock()
	defer s.st.Unlock()

	// unsupported auth mode
	_, err := fdestate.ReplacePlatformKey(s.st, &device.VolumesAuthOptions{Mode: "unknown"}, nil)
	c.Assert(err, ErrorMatches, `invalid authentication mode "unknown", only "passphrase" and "pin" modes are supported`)

	// invalid key slot reference
	badKeyslot := fdestate.KeyslotRef{ContainerRole: "", Name: "some-name"}
	_, err = fdestate.ReplacePlatformKey(s.st, nil, []fdestate.KeyslotRef{badKeyslot})
	c.Assert(err, ErrorMatches, `invalid key slot reference \(container-role: "", name: "some-name"\): container role cannot be empty`)

	// invalid key slot reference
	badKeyslot = fdestate.KeyslotRef{ContainerRole: "system-data", Name: "default-recovery"}
	_, err = fdestate.ReplacePlatformKey(s.st, nil, []fdestate.KeyslotRef{badKeyslot})
	c.Assert(err, ErrorMatches, `invalid key slot reference \(container-role: "system-data", name: "default-recovery"\): unsupported name, expected "default" or "default-fallback"`)

	// missing keyslot
	badKeyslot = fdestate.KeyslotRef{ContainerRole: "system-save", Name: "default-fallback"}
	_, err = fdestate.ReplacePlatformKey(s.st, nil, []fdestate.KeyslotRef{badKeyslot})
	c.Assert(err, ErrorMatches, `key slot reference \(container-role: "system-save", name: "default-fallback"\) not found`)
	var notFoundErr *fdestate.KeyslotRefsNotFoundError
	c.Assert(errors.As(err, &notFoundErr), Equals, true)
	c.Check(notFoundErr.KeyslotRefs, DeepEquals, []fdestate.KeyslotRef{badKeyslot})

	// keyslot key data loading error
	badKeyslot = fdestate.KeyslotRef{ContainerRole: "system-data", Name: "default-fallback"}
	_, err = fdestate.ReplacePlatformKey(s.st, nil, []fdestate.KeyslotRef{badKeyslot})
	c.Assert(err, ErrorMatches, `cannot read key data for \(container-role: "system-data", name: "default-fallback"\): cannot read key data for "default-fallback" from "/dev/disk/by-uuid/data": boom!`)

	// bad keyslot type (recovery instead of platform)
	s.st.Unlock()
	s.mockCurrentKeys(c, []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "default"}}, nil)
	s.st.Lock()
	badKeyslot = fdestate.KeyslotRef{ContainerRole: "system-data", Name: "default"}
	_, err = fdestate.ReplacePlatformKey(s.st, nil, []fdestate.KeyslotRef{badKeyslot})
	c.Assert(err, ErrorMatches, `invalid key slot reference \(container-role: "system-data", name: "default"\): unsupported type "recovery", expected "platform"`)

	// recovery mode
	unlockState := boot.DiskUnlockState{
		UbuntuData: boot.PartitionState{UnlockKey: "recovery"},
	}
	c.Assert(unlockState.WriteTo("unlocked.json"), IsNil)
	_, err = fdestate.ReplacePlatformKey(s.st, nil, nil)
	c.Assert(err, ErrorMatches, "system was unlocked with a recovery key during boot: reboot required")
	// cleanup
	c.Assert(os.RemoveAll(filepath.Join(dirs.SnapBootstrapRunDir, "unlocked.json")), IsNil)

	// change conflict with fde changes
	chg := s.st.NewChange("fde-change-passphrase", "")
	task := s.st.NewTask("some-fde-task", "")
	chg.AddTask(task)
	_, err = fdestate.ReplacePlatformKey(s.st, nil, nil)
	c.Assert(err, ErrorMatches, `changing passphrase in progress, no other FDE changes allowed until this is done`)
	c.Check(err, testutil.ErrorIs, &snapstate.ChangeConflictError{})
	// cleanup
	chg.Abort()

	// change conflict with snaps that cause a reseal
	gadgetSnapYamlContent := fmt.Sprintf(`
name: %s
version: "1.0"
type: gadget
`[1:], model.Gadget())
	kernelSnapYamlContent := fmt.Sprintf(`
name: %s
version: "1.0"
type: kernel
`[1:], model.Kernel())
	baseSnapYamlContent := fmt.Sprintf(`
name: %s
version: "1.0"
type: base
`[1:], model.Base())

	for _, sn := range []struct {
		snapYaml string
		name     string
	}{
		{snapYaml: gadgetSnapYamlContent, name: model.Gadget()},
		{snapYaml: kernelSnapYamlContent, name: model.Kernel()},
		{snapYaml: baseSnapYamlContent, name: model.Base()},
	} {
		path := snaptest.MakeTestSnapWithFiles(c, sn.snapYaml, nil)
		s.st.Set("seeded", true)
		ts, _, err := snapstate.InstallPath(s.st, &snap.SideInfo{
			RealName: sn.name,
		}, path, "", "", snapstate.Flags{}, nil)
		c.Assert(err, IsNil)
		chg = s.st.NewChange("install-essential-snap", "")
		chg.AddAll(ts)
		_, err = fdestate.ReplacePlatformKey(s.st, nil, nil)
		c.Check(err, ErrorMatches, fmt.Sprintf(`snap %q has "install-essential-snap" change in progress`, sn.name))
		// cleanup
		chg.Abort()
	}
}

func (s *fdeMgrSuite) TestReplacePlatformKeyConflictSnaps(c *C) {
	defer fdestate.MockSecbootReadContainerKeyData(func(devicePath, slotName string) (secboot.KeyData, error) {
		return &mockKeyData{authMode: device.AuthModeNone, platformName: "tpm2"}, nil
	})()

	// initialize fde manager
	onClassic := true
	s.startedManager(c, onClassic)

	s.mockCurrentKeys(c, nil, nil)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model, "run")

	s.st.Lock()
	defer s.st.Unlock()

	s.st.Set("seeded", true)

	// mock change in progress
	ts, err := fdestate.ReplacePlatformKey(s.st, nil, nil)
	c.Assert(err, IsNil)
	chg := s.st.NewChange("fde-change", "")
	chg.AddAll(ts)
	c.Assert(err, IsNil)

	gadgetSnapYamlContent := fmt.Sprintf(`
name: %s
version: "1.0"
type: gadget
`[1:], model.Gadget())
	kernelSnapYamlContent := fmt.Sprintf(`
name: %s
version: "1.0"
type: kernel
`[1:], model.Kernel())
	baseSnapYamlContent := fmt.Sprintf(`
name: %s
version: "1.0"
type: base
`[1:], model.Base())
	appSnapYamlContent := `
name: apps
version: "1.0"
type: app
`[1:]

	for _, sn := range []struct {
		snapYaml   string
		name       string
		noConflict bool
	}{
		{snapYaml: gadgetSnapYamlContent, name: model.Gadget()},
		{snapYaml: kernelSnapYamlContent, name: model.Kernel()},
		{snapYaml: baseSnapYamlContent, name: model.Base()},
		{snapYaml: appSnapYamlContent, name: "apps", noConflict: true},
	} {
		c.Logf("checking snap %s:\n%s", sn.name, sn.snapYaml)
		path := snaptest.MakeTestSnapWithFiles(c, sn.snapYaml, nil)

		_, _, err = snapstate.InstallPath(s.st, &snap.SideInfo{
			RealName: sn.name,
		}, path, "", "", snapstate.Flags{}, nil)

		if !sn.noConflict {
			c.Check(err, ErrorMatches, fmt.Sprintf(`snap %q has "fde-change" change in progress`, sn.name))
		} else {
			c.Check(err, IsNil)
		}
	}
}

func (s *fdeMgrSuite) TestReplacePlatformKeySecbootPlatforms(c *C) {
	// initialize fde manager
	onClassic := true
	s.startedManager(c, onClassic)

	s.mockCurrentKeys(c, nil, nil)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model, "run")

	s.st.Lock()
	defer s.st.Unlock()

	s.st.Set("seeded", true)

	for _, platform := range sb.ListRegisteredKeyDataPlatforms() {
		defer fdestate.MockSecbootReadContainerKeyData(func(devicePath, slotName string) (secboot.KeyData, error) {
			return &mockKeyData{authMode: device.AuthModeNone, platformName: platform}, nil
		})()

		ts, err := fdestate.ReplacePlatformKey(s.st, nil, []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "default"}})
		switch platform {
		case "tpm2":
			// only supported platform currently is "tpm2"
			c.Assert(err, IsNil)
			c.Assert(ts.Tasks(), HasLen, 3)
		default:
			errMsg := fmt.Sprintf(`invalid key slot reference \(container-role: "system-data", name: "default"\): unsupported platform "%s", expected "tpm2"`, platform)
			c.Assert(err, ErrorMatches, errMsg)
			c.Assert(ts, IsNil)
		}
	}
}
