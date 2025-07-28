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
	"sort"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/strutil"
)

func (s *fdeMgrSuite) mockCurrentKeys(c *C, rkeys, unlockKeys []fdestate.KeyslotRef) {
	var dataUnlockKeyNames, saveUnlockKeyNames []string
	if len(unlockKeys) == 0 {
		dataUnlockKeyNames = []string{"default", "default-fallback"}
		saveUnlockKeyNames = []string{"default", "default-fallback"}
	} else {
		for _, ref := range unlockKeys {
			switch ref.ContainerRole {
			case "system-data":
				dataUnlockKeyNames = append(dataUnlockKeyNames, ref.Name)
			case "system-save":
				saveUnlockKeyNames = append(saveUnlockKeyNames, ref.Name)
			default:
				c.Errorf("unexpected unlock key slot reference: %s", ref.String())
			}
		}
	}

	var dataRecoveryKeyNames, saveRecoveryKeyNames []string
	if len(rkeys) == 0 {
		dataRecoveryKeyNames = []string{"default-recovery"}
		saveRecoveryKeyNames = []string{"default-recovery"}
	} else {
		for _, ref := range rkeys {
			switch ref.ContainerRole {
			case "system-data":
				dataRecoveryKeyNames = append(dataRecoveryKeyNames, ref.Name)
			case "system-save":
				saveRecoveryKeyNames = append(saveRecoveryKeyNames, ref.Name)
			default:
				c.Errorf("unexpected recovery key slot reference: %s", ref.String())
			}
		}
	}

	s.AddCleanup(fdestate.MockDisksDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		switch mountpoint {
		case filepath.Join(dirs.GlobalRootDir, "run/mnt/data"):
			return "data", nil
		case dirs.SnapSaveDir:
			return "save", nil
		}
		panic(fmt.Sprintf("missing mocked mount point %q", mountpoint))
	}))

	s.AddCleanup(fdestate.MockSecbootListContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		switch devicePath {
		case "/dev/disk/by-uuid/data":
			return dataUnlockKeyNames, nil
		case "/dev/disk/by-uuid/save":
			return saveUnlockKeyNames, nil
		default:
			return nil, fmt.Errorf("unexpected devicePath %q", devicePath)
		}
	}))

	s.AddCleanup(fdestate.MockSecbootListContainerRecoveryKeyNames(func(devicePath string) ([]string, error) {
		switch devicePath {
		case "/dev/disk/by-uuid/data":
			return dataRecoveryKeyNames, nil
		case "/dev/disk/by-uuid/save":
			return saveRecoveryKeyNames, nil
		default:
			return nil, fmt.Errorf("unexpected devicePath %q", devicePath)
		}
	}))
}

func (s *fdeMgrSuite) TestDoAddRecoveryKeys(c *C) {
	const onClassic = true
	manager := s.startedManager(c, onClassic)
	s.mockCurrentKeys(c, nil, nil)

	s.st.Lock()
	defer s.st.Unlock()

	type testcase struct {
		keyslots                      []fdestate.KeyslotRef
		expectedAdds, expectedDeletes []string
		badRecoveryKeyID              bool
		expiredRecoveryKeyID          bool
		errOn                         []string
		expectedErr                   string
	}
	tcs := []testcase{
		{
			keyslots:     []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "tmp-default-recovery"}},
			expectedAdds: []string{"/dev/disk/by-uuid/data:tmp-default-recovery"},
		},
		{
			keyslots: []fdestate.KeyslotRef{
				{ContainerRole: "system-data", Name: "default-recovery"}, // already exists
				{ContainerRole: "system-data", Name: "tmp-default-recovery"},
			},
			expectedAdds: []string{"/dev/disk/by-uuid/data:tmp-default-recovery"},
		},
		{
			keyslots: []fdestate.KeyslotRef{
				{ContainerRole: "system-data", Name: "tmp-default-recovery"},
				{ContainerRole: "system-save", Name: "tmp-default-recovery"},
			},
			expiredRecoveryKeyID: true,
			expectedErr:          `recovery key has expired`,
		},
		{
			keyslots: []fdestate.KeyslotRef{
				{ContainerRole: "system-data", Name: "tmp-default-recovery"},
				{ContainerRole: "system-save", Name: "tmp-default-recovery"},
			},
			badRecoveryKeyID: true,
			expectedErr:      `cannot find recovery key with id "bad-id": no recovery key entry for key-id`,
		},
		{
			keyslots: []fdestate.KeyslotRef{
				{ContainerRole: "system-data", Name: "tmp-default-recovery-1"},
				{ContainerRole: "system-data", Name: "tmp-default-recovery-2"},
				{ContainerRole: "system-data", Name: "tmp-default-recovery-3"},
			},
			errOn: []string{
				"add:/dev/disk/by-uuid/data:tmp-default-recovery-3",    // to trigger clean up
				"delete:/dev/disk/by-uuid/data:tmp-default-recovery-2", // to test best effort deletion
			},
			// best effort deletion for clean up
			expectedDeletes: []string{"/dev/disk/by-uuid/data:tmp-default-recovery-1"},
			expectedAdds: []string{
				"/dev/disk/by-uuid/data:tmp-default-recovery-1",
				"/dev/disk/by-uuid/data:tmp-default-recovery-2",
			},
			expectedErr: `cannot add recovery key slot \(container-role: "system-data", name: "tmp-default-recovery-3"\): add error on /dev/disk/by-uuid/data:tmp-default-recovery-3`,
		},
		{
			keyslots: []fdestate.KeyslotRef{
				{ContainerRole: "system-data", Name: "tmp-default-recovery"},
				{ContainerRole: "system-save", Name: "tmp-default-recovery"},
			},
			errOn:           []string{"add:/dev/disk/by-uuid/save:tmp-default-recovery"},
			expectedAdds:    []string{"/dev/disk/by-uuid/data:tmp-default-recovery"},
			expectedDeletes: []string{"/dev/disk/by-uuid/data:tmp-default-recovery"},
			expectedErr:     `cannot add recovery key slot \(container-role: "system-save", name: "tmp-default-recovery"\): add error on /dev/disk/by-uuid/save:tmp-default-recovery`,
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("tcs[%d] failed", i)

		var expectedRecoveryKey keys.RecoveryKey
		var added, deleted []string

		defer fdestate.MockSecbootAddContainerRecoveryKey(func(devicePath, slotName string, rkey keys.RecoveryKey) error {
			c.Check(rkey, DeepEquals, expectedRecoveryKey, cmt)
			entry := fmt.Sprintf("%s:%s", devicePath, slotName)
			if strutil.ListContains(tc.errOn, fmt.Sprintf("add:%s", entry)) {
				return fmt.Errorf("add error on %s", entry)
			}
			added = append(added, entry)
			return nil
		})()

		defer fdestate.MockSecbootDeleteContainerKey(func(devicePath, slotName string) error {
			entry := fmt.Sprintf("%s:%s", devicePath, slotName)
			if strutil.ListContains(tc.errOn, fmt.Sprintf("delete:%s", entry)) {
				return fmt.Errorf("delete error on %s", entry)
			}
			deleted = append(deleted, entry)
			return nil
		})()

		task := s.st.NewTask("fde-add-recovery-keys", "test")
		task.Set("keyslots", tc.keyslots)

		var rkeyID string
		var err error
		if tc.badRecoveryKeyID {
			rkeyID = "bad-id"
		} else if tc.expiredRecoveryKeyID {
			restore := fdestate.MockTimeNow(func() time.Time { return time.Now().Add(-100000 * time.Hour) })
			expectedRecoveryKey, rkeyID, err = manager.GenerateRecoveryKey()
			restore()
			c.Assert(err, IsNil, cmt)
		} else {
			expectedRecoveryKey, rkeyID, err = manager.GenerateRecoveryKey()
			c.Assert(err, IsNil, cmt)
		}
		task.Set("recovery-key-id", rkeyID)

		chg := s.st.NewChange("sample", "...")
		chg.AddTask(task)

		s.settle(c)

		if tc.expectedErr == "" {
			c.Check(chg.Status(), Equals, state.DoneStatus, cmt)
		} else {
			c.Check(chg.Err(), ErrorMatches, fmt.Sprintf(`cannot perform the following tasks:
- test \(%s\)`, tc.expectedErr), cmt)
		}

		sort.Strings(added)
		c.Check(added, DeepEquals, tc.expectedAdds, cmt)

		sort.Strings(deleted)
		c.Check(deleted, DeepEquals, tc.expectedDeletes, cmt)
	}
}

func (s *fdeMgrSuite) TestDoAddRecoveryKeysIdempotence(c *C) {
	const onClassic = true
	manager := s.startedManager(c, onClassic)

	currentKeys := []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "some-key"}}
	s.mockCurrentKeys(c, currentKeys, nil)

	ops := make([]string, 0)
	defer fdestate.MockSecbootAddContainerRecoveryKey(func(devicePath, slotName string, rkey keys.RecoveryKey) error {
		var containerRole string
		switch filepath.Base(devicePath) {
		case "data":
			containerRole = "system-data"
		case "save":
			containerRole = "system-save"
		default:
			panic("unexpected device path")
		}
		newKeys := []fdestate.KeyslotRef{{ContainerRole: containerRole, Name: slotName}}
		found := false
		for _, ref := range currentKeys {
			if ref.ContainerRole == containerRole && ref.Name == slotName {
				found = true
				continue
			}
			newKeys = append(newKeys, ref)
		}
		c.Assert(found, Equals, false, Commentf("%s:%s already exists", containerRole, slotName))

		currentKeys = newKeys
		s.mockCurrentKeys(c, currentKeys, nil)
		ops = append(ops, fmt.Sprintf("add:%s:%s", containerRole, slotName))
		return nil
	})()

	s.st.Lock()
	defer s.st.Unlock()

	keyslotRefs := []fdestate.KeyslotRef{
		{ContainerRole: "system-data", Name: "default-0"},
		{ContainerRole: "system-data", Name: "default-1"},
		{ContainerRole: "system-data", Name: "default-2"},
		{ContainerRole: "system-data", Name: "default-3"},
	}
	_, rkeyID, err := manager.GenerateRecoveryKey()
	c.Assert(err, IsNil)

	chg := s.st.NewChange("sample", "...")

	for i := 1; i <= 3; i++ {
		t := s.st.NewTask("fde-add-recovery-keys", fmt.Sprintf("test add recovery %d", i))
		t.Set("keyslots", keyslotRefs)
		// notice that the same (already consumed key-id is used).
		t.Set("recovery-key-id", rkeyID)
		chg.AddTask(t)
	}

	s.settle(c)

	sort.Strings(ops)

	c.Check(chg.Status(), Equals, state.DoneStatus)
	// task is idempotent
	c.Check(ops, DeepEquals, []string{
		"add:system-data:default-0",
		"add:system-data:default-1",
		"add:system-data:default-2",
		"add:system-data:default-3",
	})
}

func (s *fdeMgrSuite) TestDoRemoveKeys(c *C) {
	const onClassic = true
	s.startedManager(c, onClassic)
	s.mockCurrentKeys(c, nil, nil)

	var deleted []string
	defer fdestate.MockSecbootDeleteContainerKey(func(devicePath, slotName string) error {
		c.Check(devicePath, Equals, "/dev/disk/by-uuid/data")
		deleted = append(deleted, slotName)
		return nil
	})()

	s.st.Lock()
	defer s.st.Unlock()

	task := s.st.NewTask("fde-remove-keys", "test")
	task.Set("keyslots", []fdestate.KeyslotRef{
		{ContainerRole: "system-data", Name: "default"},
		{ContainerRole: "system-data", Name: "default-recovery"},
		{ContainerRole: "system-data", Name: "already-deleted"},
	})
	chg := s.st.NewChange("sample", "...")
	chg.AddTask(task)

	s.settle(c)

	sort.Strings(deleted)

	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Check(deleted, DeepEquals, []string{"default", "default-recovery"})
}

func (s *fdeMgrSuite) TestDoRemoveKeysIdempotence(c *C) {
	const onClassic = true
	s.startedManager(c, onClassic)

	currentKeys := []fdestate.KeyslotRef{
		{ContainerRole: "system-data", Name: "default-0"},
		{ContainerRole: "system-data", Name: "default-1"},
		{ContainerRole: "system-data", Name: "default-2"},
		{ContainerRole: "system-data", Name: "default-3"},
	}
	s.mockCurrentKeys(c, nil, currentKeys)

	ops := make([]string, 0)
	defer fdestate.MockSecbootDeleteContainerKey(func(devicePath, slotName string) error {
		var containerRole string
		switch filepath.Base(devicePath) {
		case "data":
			containerRole = "system-data"
		case "save":
			containerRole = "system-save"
		default:
			panic("unexpected device path")
		}
		newKeys := []fdestate.KeyslotRef{}
		found := false
		for _, ref := range currentKeys {
			if ref.ContainerRole == containerRole && ref.Name == slotName {
				found = true
				continue
			}
			newKeys = append(newKeys, ref)
		}
		c.Assert(found, Equals, true, Commentf("%s:%s not found", containerRole, slotName))

		currentKeys = newKeys
		s.mockCurrentKeys(c, nil, currentKeys)
		ops = append(ops, fmt.Sprintf("remove:%s:%s", containerRole, slotName))
		return nil
	})()

	s.st.Lock()
	defer s.st.Unlock()

	keyslotRefs := []fdestate.KeyslotRef{
		{ContainerRole: "system-data", Name: "default-0"},
		{ContainerRole: "system-data", Name: "default-1"},
		{ContainerRole: "system-data", Name: "default-2"},
		{ContainerRole: "system-data", Name: "default-3"},
	}

	chg := s.st.NewChange("sample", "...")

	for i := 1; i <= 3; i++ {
		t := s.st.NewTask("fde-remove-keys", fmt.Sprintf("test remove %d", i))
		t.Set("keyslots", keyslotRefs)
		chg.AddTask(t)
	}

	s.settle(c)

	sort.Strings(ops)

	c.Check(chg.Status(), Equals, state.DoneStatus)
	// task is idempotent
	c.Check(ops, DeepEquals, []string{
		"remove:system-data:default-0",
		"remove:system-data:default-1",
		"remove:system-data:default-2",
		"remove:system-data:default-3",
	})
}

func (s *fdeMgrSuite) TestDoRemoveKeysGetKeyslotsError(c *C) {
	const onClassic = true
	s.startedManager(c, onClassic)

	defer fdestate.MockDisksDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		return "", errors.New("boom!")
	})()

	called := 0
	defer fdestate.MockSecbootDeleteContainerKey(func(devicePath, slotName string) error {
		called++
		return nil
	})()

	s.st.Lock()
	defer s.st.Unlock()

	task := s.st.NewTask("fde-remove-keys", "test")
	task.Set("keyslots", []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "default"}})
	chg := s.st.NewChange("sample", "...")
	chg.AddTask(task)

	s.settle(c)

	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:
- test \(cannot get key slots: cannot find UUID for mount .*/run/mnt/data: boom!\)`)
}

func (s *fdeMgrSuite) TestDoRemoveKeysRemoveError(c *C) {
	const onClassic = true
	s.startedManager(c, onClassic)
	s.mockCurrentKeys(c, nil, nil)

	defer fdestate.MockSecbootDeleteContainerKey(func(devicePath, slotName string) error {
		return errors.New("boom!")
	})()

	s.st.Lock()
	defer s.st.Unlock()

	task := s.st.NewTask("fde-remove-keys", "test")
	task.Set("keyslots", []fdestate.KeyslotRef{})
	chg := s.st.NewChange("sample", "...")
	chg.AddTask(task)

	s.settle(c)

	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:
- test \(cannot remove key slot \(container-role: "system-data", name: "default-recovery"\): boom!\)`)
}

func (s *fdeMgrSuite) TestDoRenameKeys(c *C) {
	const onClassic = true
	s.startedManager(c, onClassic)
	s.mockCurrentKeys(c, nil, nil)

	renames := make(map[string]string)
	defer fdestate.MockSecbootRenameContainerKey(func(devicePath, oldName, newName string) error {
		renames[fmt.Sprintf("%s:%s", devicePath, oldName)] = newName
		return nil
	})()

	s.st.Lock()
	defer s.st.Unlock()

	task := s.st.NewTask("fde-rename-keys", "test")
	task.Set("keyslots", []fdestate.KeyslotRef{
		{ContainerRole: "system-data", Name: "default"},
		{ContainerRole: "system-save", Name: "default-recovery"},
		{ContainerRole: "system-data", Name: "already-renamed"},
	})
	task.Set("renames", map[string]string{
		`(container-role: "system-data", name: "default")`:          "new-default",
		`(container-role: "system-save", name: "default-recovery")`: "new-default-recovery",
		`(container-role: "system-data", name: "already-renamed")`:  "already-renamed",
	})
	chg := s.st.NewChange("sample", "...")
	chg.AddTask(task)

	s.settle(c)

	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Check(renames, DeepEquals, map[string]string{
		"/dev/disk/by-uuid/data:default":          "new-default",
		"/dev/disk/by-uuid/save:default-recovery": "new-default-recovery",
	})
}

func (s *fdeMgrSuite) TestDoRenameKeysIdempotence(c *C) {
	const onClassic = true
	s.startedManager(c, onClassic)

	currentKeys := []fdestate.KeyslotRef{
		{ContainerRole: "system-data", Name: "default-0"},
		{ContainerRole: "system-data", Name: "default-1"},
		{ContainerRole: "system-data", Name: "default-2"},
		{ContainerRole: "system-data", Name: "default-3"},
	}
	s.mockCurrentKeys(c, nil, currentKeys)

	ops := make([]string, 0)
	defer fdestate.MockSecbootRenameContainerKey(func(devicePath, oldName, newName string) error {
		var containerRole string
		switch filepath.Base(devicePath) {
		case "data":
			containerRole = "system-data"
		case "save":
			containerRole = "system-save"
		default:
			panic("unexpected device path")
		}
		newKeys := []fdestate.KeyslotRef{{ContainerRole: containerRole, Name: newName}}
		found := false
		for _, ref := range currentKeys {
			if ref.ContainerRole == containerRole && ref.Name == oldName {
				found = true
				continue
			}
			newKeys = append(newKeys, ref)
		}
		c.Assert(found, Equals, true, Commentf("%s:%s not found", containerRole, oldName))

		currentKeys = newKeys
		s.mockCurrentKeys(c, nil, currentKeys)
		ops = append(ops, fmt.Sprintf("rename:%s:%s:%s", containerRole, oldName, newName))
		return nil
	})()

	s.st.Lock()
	defer s.st.Unlock()

	keyslotRefs := []fdestate.KeyslotRef{
		{ContainerRole: "system-data", Name: "default-0"},
		{ContainerRole: "system-data", Name: "default-1"},
		{ContainerRole: "system-data", Name: "default-2"},
		{ContainerRole: "system-data", Name: "default-3"},
	}
	renames := map[string]string{
		`(container-role: "system-data", name: "default-0")`: "new-default-0",
		`(container-role: "system-data", name: "default-1")`: "new-default-1",
		`(container-role: "system-data", name: "default-2")`: "new-default-2",
		`(container-role: "system-data", name: "default-3")`: "new-default-3",
	}

	chg := s.st.NewChange("sample", "...")

	for i := 1; i <= 3; i++ {
		t := s.st.NewTask("fde-rename-keys", fmt.Sprintf("test rename %d", i))
		t.Set("keyslots", keyslotRefs)
		t.Set("renames", renames)
		chg.AddTask(t)
	}

	s.settle(c)

	sort.Strings(ops)

	c.Check(chg.Status(), Equals, state.DoneStatus)
	// task is idempotent
	c.Check(ops, DeepEquals, []string{
		"rename:system-data:default-0:new-default-0",
		"rename:system-data:default-1:new-default-1",
		"rename:system-data:default-2:new-default-2",
		"rename:system-data:default-3:new-default-3",
	})
}

func (s *fdeMgrSuite) TestDoRenameKeysMissingMapping(c *C) {
	const onClassic = true
	s.startedManager(c, onClassic)

	s.st.Lock()
	defer s.st.Unlock()

	task := s.st.NewTask("fde-rename-keys", "test")
	task.Set("keyslots", []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "default"}})
	task.Set("renames", map[string]string{`(container-role: "system-data", name: "some-slot")`: "some-other-slot"})
	chg := s.st.NewChange("sample", "...")
	chg.AddTask(task)

	s.settle(c)

	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:
- test \(internal error: cannot find mapping for \(container-role: "system-data", name: "default"\)\)`)
}

func (s *fdeMgrSuite) TestDoRenameKeysRenameError(c *C) {
	const onClassic = true
	s.startedManager(c, onClassic)
	s.mockCurrentKeys(c, nil, nil)

	defer fdestate.MockSecbootRenameContainerKey(func(devicePath, oldName, newName string) error {
		return errors.New("boom!")
	})()

	s.st.Lock()
	defer s.st.Unlock()

	task := s.st.NewTask("fde-rename-keys", "test")
	task.Set("keyslots", []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "default"}})
	task.Set("renames", map[string]string{`(container-role: "system-data", name: "default")`: "new-default"})
	chg := s.st.NewChange("sample", "...")
	chg.AddTask(task)

	s.settle(c)

	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:
- test \(cannot rename key slot \(container-role: "system-data", name: "default"\) to "new-default": boom!\)`)
}

func (s *fdeMgrSuite) TestDoRenameKeysRenameAlreadyExists(c *C) {
	const onClassic = true
	s.startedManager(c, onClassic)
	s.mockCurrentKeys(c, nil, nil)

	s.st.Lock()
	defer s.st.Unlock()

	task := s.st.NewTask("fde-rename-keys", "test")
	task.Set("keyslots", []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "default"}})
	// default-fallback already exists on system-data
	task.Set("renames", map[string]string{`(container-role: "system-data", name: "default")`: "default-fallback"})
	chg := s.st.NewChange("sample", "...")
	chg.AddTask(task)

	s.settle(c)

	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:
- test \(key slot \(container-role: "system-data", name: "default-fallback"\) already exists\)`)
}

func (s *fdeMgrSuite) TestDoChangeAuthKeys(c *C) {
	const onClassic = true
	s.startedManager(c, onClassic)
	s.mockCurrentKeys(c, []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "default-recovery"}}, nil)

	s.st.Lock()
	defer s.st.Unlock()

	type testcase struct {
		keyslots        []fdestate.KeyslotRef
		authMode        device.AuthMode
		noOpt           bool
		errOn           []string
		expectedChanges []string
		expectedUndos   []string
		expectedErr     string
	}

	// Note: key slots are evaluated in the following order:
	//   1. container-role: "system-data", name: "default",
	//   2. container-role: "system-data", name: "default-fallback",
	//   3. container-role: "system-save", name: "default-fallback",
	//
	// This matters when determining which key slots are relevant for undo
	tcs := []testcase{
		{
			keyslots: []fdestate.KeyslotRef{
				{ContainerRole: "system-data", Name: "default"},
				{ContainerRole: "system-data", Name: "default-fallback"},
				{ContainerRole: "system-save", Name: "default-fallback"},
			},
			expectedChanges: []string{
				"/dev/disk/by-uuid/data:default",
				"/dev/disk/by-uuid/data:default-fallback",
				"/dev/disk/by-uuid/save:default-fallback",
			},
			authMode: device.AuthModePassphrase,
		},
		{
			keyslots:    []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "default"}},
			noOpt:       true,
			expectedErr: "cannot find authentication options in memory: unexpected snapd restart",
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
			authMode:      device.AuthModePassphrase,
			errOn:         []string{"read:/dev/disk/by-uuid/data:default-fallback"},
			expectedUndos: []string{"/dev/disk/by-uuid/data:default"}, // based on operations order explained above
			expectedErr:   `cannot read key data for \(container-role: "system-data", name: "default-fallback"\): cannot read key data for "default-fallback" from "/dev/disk/by-uuid/data": read error on /dev/disk/by-uuid/data:default-fallback`,
		},
		{
			keyslots:    []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "default"}},
			authMode:    device.AuthModePassphrase,
			errOn:       []string{"change:/dev/disk/by-uuid/data:default"},
			expectedErr: `cannot change passphrase for \(container-role: "system-data", name: "default"\): change error on /dev/disk/by-uuid/data:default`,
		},
		{
			keyslots:    []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "default"}},
			authMode:    device.AuthModePassphrase,
			errOn:       []string{"write:/dev/disk/by-uuid/data:default"},
			expectedErr: `cannot write key data for \(container-role: "system-data", name: "default"\): write error on /dev/disk/by-uuid/data:default`,
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
	for idx, tc := range tcs {
		task := s.st.NewTask("fde-change-auth", "test")
		task.Set("keyslots", tc.keyslots)
		task.Set("auth-mode", tc.authMode)

		if !tc.noOpt {
			s.st.Unlock()
			defer fdestate.MockChangeAuthOptionsInCache(s.st, "old", "new")()
			s.st.Lock()
		}

		changeCalls := make(map[string]int, 0)

		defer fdestate.MockSecbootReadContainerKeyData(func(devicePath, slotName string) (secboot.KeyData, error) {
			entry := fmt.Sprintf("%s:%s", devicePath, slotName)
			if strutil.ListContains(tc.errOn, fmt.Sprintf("read:%s:%s", devicePath, slotName)) {
				return nil, fmt.Errorf("read error on %s", entry)
			}
			return &mockKeyData{
				changePassphrase: func(oldPassphrase, newPassphrase string) error {
					switch changeCalls[entry] {
					case 0:
						c.Check(oldPassphrase, Equals, "old")
						c.Check(newPassphrase, Equals, "new")
					case 1:
						c.Check(oldPassphrase, Equals, "new")
						c.Check(newPassphrase, Equals, "old")
					default:
						panic("unexpected number of change calls")
					}
					if strutil.ListContains(tc.errOn, fmt.Sprintf("change:%s", entry)) {
						return fmt.Errorf("change error on %s", entry)
					}
					return nil
				},
				writeTokenAtomic: func(devicePath, slotName string) error {
					if strutil.ListContains(tc.errOn, fmt.Sprintf("write:%s", entry)) {
						return fmt.Errorf("write error on %s", entry)
					}
					changeCalls[entry]++
					return nil
				},
			}, nil
		})()

		chg := s.st.NewChange("sample", "...")
		chg.AddTask(task)

		s.settle(c)

		for _, entry := range tc.expectedChanges {
			c.Check(changeCalls[entry], Equals, 1)
		}

		for _, entry := range tc.expectedUndos {
			c.Check(changeCalls[entry], Equals, 2)
		}

		// check that no entry in changeCalls is not already covered to
		// catch development errors
		for entry, cnt := range changeCalls {
			switch cnt {
			case 1:
				c.Assert(strutil.ListContains(tc.expectedChanges, entry), Equals, true, Commentf("tcs[%d]", idx))
			case 2:
				c.Assert(strutil.ListContains(tc.expectedUndos, entry), Equals, true, Commentf("tcs[%d]", idx))
			default:
				panic("unexpected number of change calls")
			}
		}

		if tc.expectedErr == "" {
			c.Check(chg.Status(), Equals, state.DoneStatus)
			if !tc.noOpt {
				// Auth options are removed on completion
				c.Assert(fdestate.GetChangeAuthOptionsFromCache(s.st), IsNil)
			}
		} else {
			c.Check(chg.Err(), ErrorMatches, fmt.Sprintf(`cannot perform the following tasks:
- test \(%s\)`, tc.expectedErr))
			if !tc.noOpt {
				// Auth options are kept to account for re-runs
				opts := fdestate.GetChangeAuthOptionsFromCache(s.st)
				c.Assert(opts.New(), Equals, "new")
				c.Assert(opts.Old(), Equals, "old")
			}
		}
	}
}

func (s *fdeMgrSuite) TestDoChangeAuthKeysNoop(c *C) {
	const onClassic = true
	s.startedManager(c, onClassic)

	defer fdestate.MockSecbootReadContainerKeyData(func(devicePath, slotName string) (secboot.KeyData, error) {
		panic("unexpected")
	})()

	s.st.Lock()
	defer s.st.Unlock()

	task := s.st.NewTask("fde-change-auth", "test")
	task.Set("keyslots", []fdestate.KeyslotRef{})
	task.Set("auth-mode", device.AuthModePassphrase)

	s.st.Unlock()
	defer fdestate.MockChangeAuthOptionsInCache(s.st, "old", "old")()
	s.st.Lock()

	chg := s.st.NewChange("sample", "...")
	chg.AddTask(task)

	s.settle(c)

	c.Check(chg.Status(), Equals, state.DoneStatus)
}

func (s *fdeMgrSuite) TestDoAddProtectedKeys(c *C) {
	const onClassic = true
	s.startedManager(c, onClassic)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model, "run")

	s.mockCurrentKeys(c, nil, nil)

	defaultKeyslots := []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "tmp-default"}}
	defaultRoles := [][]string{{"run"}}

	s.st.Lock()
	defer s.st.Unlock()

	type testcase struct {
		keyslots                      []fdestate.KeyslotRef
		authMode                      device.AuthMode
		noVolumesAuth                 bool
		recoveryMode                  bool
		noop                          bool
		roles                         [][]string
		expectedAdds, expectedDeletes []string
		errOn                         []string
		expectedErr                   string
	}
	tcs := []testcase{
		{
			authMode: device.AuthModeNone, noVolumesAuth: true,
			expectedAdds: []string{"/dev/disk/by-uuid/data:tmp-default"},
		},
		{
			authMode:     device.AuthModePassphrase,
			expectedAdds: []string{"/dev/disk/by-uuid/data:tmp-default"},
		},
		{
			authMode:    device.AuthModePIN,
			expectedErr: `internal error: invalid authentication options: "pin" authentication mode is not implemented`,
		},
		{
			authMode: device.AuthModePIN, noVolumesAuth: true,
			expectedErr: "cannot find authentication options in memory: unexpected snapd restart",
		},
		{
			authMode: device.AuthModePassphrase, noVolumesAuth: true,
			expectedErr: "cannot find authentication options in memory: unexpected snapd restart",
		},
		{
			authMode: device.AuthModePassphrase, recoveryMode: true,
			expectedErr: "cannot add protected keys if the system was unlocked with a recovery key during boot",
		},
		{
			authMode:    device.AuthModePassphrase,
			roles:       [][]string{{"run", "recover"}},
			expectedErr: `internal error: expected one key role, found \[run recover\]`,
		},
		{
			authMode:    device.AuthModePassphrase,
			roles:       [][]string{{"run+recover"}},
			expectedErr: `internal error: cannot find parameters \(key role: run\+recover, container role: system-data\)`,
		},
		{
			keyslots: []fdestate.KeyslotRef{
				{ContainerRole: "system-data", Name: "tmp-default-1"},
				{ContainerRole: "system-data", Name: "tmp-default-2"},
				{ContainerRole: "system-data", Name: "tmp-default-3"},
			},
			authMode: device.AuthModePassphrase,
			roles:    [][]string{{"run"}, {"run"}, {"run"}},
			errOn: []string{
				"add:/dev/disk/by-uuid/data:tmp-default-3",    // to trigger clean up
				"delete:/dev/disk/by-uuid/data:tmp-default-2", // to test best effort deletion
			},
			// best effort deletion for clean up
			expectedDeletes: []string{"/dev/disk/by-uuid/data:tmp-default-1"},
			expectedAdds: []string{
				"/dev/disk/by-uuid/data:tmp-default-1",
				"/dev/disk/by-uuid/data:tmp-default-2",
			},
			expectedErr: `cannot add protected key slot \(container-role: "system-data", name: "tmp-default-3"\): add error on /dev/disk/by-uuid/data:tmp-default-3`,
		},
		{
			keyslots: []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "default"}},
			authMode: device.AuthModePassphrase,
			noop:     true,
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("tcs[%d] failed", i)

		var added, deleted []string

		if len(tc.keyslots) == 0 {
			tc.keyslots = defaultKeyslots
		}

		roles := make(map[string][]string)
		if len(tc.roles) == 0 {
			tc.roles = defaultRoles
		}

		c.Assert(tc.roles, HasLen, len(tc.keyslots))
		for idx, ref := range tc.keyslots {
			roles[ref.String()] = tc.roles[idx]
		}

		if tc.recoveryMode {
			unlockState := boot.DiskUnlockState{
				UbuntuData: boot.PartitionState{UnlockKey: "recovery"},
			}
			c.Assert(unlockState.WriteTo("unlocked.json"), IsNil)
		}

		var volumesAuth *device.VolumesAuthOptions
		if !tc.noVolumesAuth {
			volumesAuth = &device.VolumesAuthOptions{Mode: tc.authMode}
			if tc.authMode == device.AuthModePassphrase {
				volumesAuth.Passphrase = "password"
			}
			s.st.Cache(fdestate.VolumesAuthOptionsKey(), volumesAuth)
		}

		defer fdestate.MockSecbootAddContainerTPMProtectedKey(func(devicePath string, slotName string, params *secboot.ProtectKeyParams) error {
			c.Check(params.PCRProfile, DeepEquals, secboot.SerializedPCRProfile("serialized-pcr-profile"), cmt)
			c.Check(params.PCRPolicyCounterHandle, Equals, uint32(41), cmt)
			c.Check(params.VolumesAuth, DeepEquals, volumesAuth, cmt)

			entry := fmt.Sprintf("%s:%s", devicePath, slotName)
			if strutil.ListContains(tc.errOn, fmt.Sprintf("add:%s", entry)) {
				return fmt.Errorf("add error on %s", entry)
			}
			added = append(added, entry)
			return nil
		})()

		defer fdestate.MockSecbootDeleteContainerKey(func(devicePath, slotName string) error {
			entry := fmt.Sprintf("%s:%s", devicePath, slotName)
			if strutil.ListContains(tc.errOn, fmt.Sprintf("delete:%s", entry)) {
				return fmt.Errorf("delete error on %s", entry)
			}
			deleted = append(deleted, entry)
			return nil
		})()

		loadParameterCalls := 0
		defer fdestate.MockBackendLoadParametersForBootChains(func(manager backend.FDEStateManager, method device.SealingMethod, rootdir string, bootChains boot.BootChains) error {
			loadParameterCalls++
			params := backend.SealingParameters{TpmPCRProfile: []byte("serialized-pcr-profile")}
			c.Assert(manager.Update("run", "system-data", &params), IsNil)
			return nil
		})()

		c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

		task := s.st.NewTask("fde-add-protected-keys", "test")
		task.Set("keyslots", tc.keyslots)
		task.Set("auth-mode", tc.authMode)
		task.Set("roles", roles)

		chg := s.st.NewChange("sample", "...")
		chg.AddTask(task)

		s.settle(c)

		if tc.expectedErr == "" {
			c.Check(chg.Status(), Equals, state.DoneStatus, cmt)
			c.Assert(chg.Err(), IsNil, cmt)
			if !tc.noop {
				// only called once
				c.Check(loadParameterCalls, Equals, 1, cmt)
			}
			// volumes auth is removed
			c.Assert(s.st.Cached(fdestate.VolumesAuthOptionsKey()), IsNil)
		} else {
			c.Check(chg.Err(), ErrorMatches, fmt.Sprintf(`cannot perform the following tasks:
- test \(%s\)`, tc.expectedErr), cmt)
			if !tc.noVolumesAuth {
				// volumes auth is kept to account for re-runs
				c.Assert(s.st.Cached(fdestate.VolumesAuthOptionsKey()), Equals, volumesAuth)
			}
		}

		if tc.noop {
			c.Check(loadParameterCalls, Equals, 0, cmt)
			c.Check(added, HasLen, 0)
			c.Check(deleted, HasLen, 0)
		}

		sort.Strings(added)
		c.Check(added, DeepEquals, tc.expectedAdds, cmt)

		sort.Strings(deleted)
		c.Check(deleted, DeepEquals, tc.expectedDeletes, cmt)

		// clean up
		c.Assert(os.RemoveAll(filepath.Join(dirs.SnapBootstrapRunDir, "unlocked.json")), IsNil, cmt)
		s.st.Cache(fdestate.VolumesAuthOptionsKey(), nil)
		var fdeState fdestate.FdeState
		c.Assert(s.st.Get("fde", &fdeState), IsNil)
		delete(fdeState.KeyslotRoles["run"].Parameters, "system-data")
		s.st.Set("fde", fdeState)
	}
}

func (s *fdeMgrSuite) TestDoAddProtectedKeysIdempotence(c *C) {
	const onClassic = true
	s.startedManager(c, onClassic)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model, "run")

	currentKeys := []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "some-key"}}
	s.mockCurrentKeys(c, currentKeys, nil)

	defer fdestate.MockBackendLoadParametersForBootChains(func(manager backend.FDEStateManager, method device.SealingMethod, rootdir string, bootChains boot.BootChains) error {
		c.Assert(manager.Update("run", "system-data", &backend.SealingParameters{TpmPCRProfile: []byte("serialized-pcr-profile")}), IsNil)
		return nil
	})()

	ops := make([]string, 0)
	defer fdestate.MockSecbootAddContainerTPMProtectedKey(func(devicePath string, slotName string, params *secboot.ProtectKeyParams) error {
		var containerRole string
		switch filepath.Base(devicePath) {
		case "data":
			containerRole = "system-data"
		case "save":
			containerRole = "system-save"
		default:
			panic("unexpected device path")
		}
		newKeys := []fdestate.KeyslotRef{{ContainerRole: containerRole, Name: slotName}}
		found := false
		for _, ref := range currentKeys {
			if ref.ContainerRole == containerRole && ref.Name == slotName {
				found = true
				continue
			}
			newKeys = append(newKeys, ref)
		}
		c.Assert(found, Equals, false, Commentf("%s:%s already exists", containerRole, slotName))

		currentKeys = newKeys
		s.mockCurrentKeys(c, currentKeys, nil)
		ops = append(ops, fmt.Sprintf("add:%s:%s", containerRole, slotName))
		return nil
	})()

	s.st.Lock()
	defer s.st.Unlock()

	keyslotRefs := []fdestate.KeyslotRef{
		{ContainerRole: "system-data", Name: "default-0"},
		{ContainerRole: "system-data", Name: "default-1"},
		{ContainerRole: "system-data", Name: "default-2"},
		{ContainerRole: "system-data", Name: "default-3"},
	}
	roles := make(map[string][]string, 4)
	for _, ref := range keyslotRefs {
		roles[ref.String()] = []string{"run"}
	}

	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	chg := s.st.NewChange("sample", "...")

	for i := 1; i <= 3; i++ {
		t := s.st.NewTask("fde-add-protected-keys", "test")
		t.Set("keyslots", keyslotRefs)
		t.Set("auth-mode", device.AuthModeNone)
		t.Set("roles", roles)
		chg.AddTask(t)
	}

	s.settle(c)

	sort.Strings(ops)

	c.Check(chg.Err(), IsNil)
	c.Check(chg.Status(), Equals, state.DoneStatus)
	// task is idempotent
	c.Check(ops, DeepEquals, []string{
		"add:system-data:default-0",
		"add:system-data:default-1",
		"add:system-data:default-2",
		"add:system-data:default-3",
	})
}
