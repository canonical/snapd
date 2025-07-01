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
	"sort"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/state"
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
			expectedDeletes: []string{
				"/dev/disk/by-uuid/data:tmp-default-recovery",
				"/dev/disk/by-uuid/save:tmp-default-recovery",
			},
			expectedErr: `recovery key has expired`,
		},
		{
			keyslots: []fdestate.KeyslotRef{
				{ContainerRole: "system-data", Name: "tmp-default-recovery"},
				{ContainerRole: "system-save", Name: "tmp-default-recovery"},
			},
			badRecoveryKeyID: true,
			expectedDeletes: []string{
				"/dev/disk/by-uuid/data:tmp-default-recovery",
				"/dev/disk/by-uuid/save:tmp-default-recovery",
			},
			expectedErr: `cannot find recovery key with id "bad-id": no recovery key entry for key-id`,
		},
		{
			keyslots: []fdestate.KeyslotRef{
				{ContainerRole: "system-data", Name: "tmp-default-recovery"},
				{ContainerRole: "system-save", Name: "tmp-default-recovery"},
			},
			badRecoveryKeyID: true,
			errOn:            []string{"delete:/dev/disk/by-uuid/data:tmp-default-recovery"},
			// best effort deletion for clean up
			expectedDeletes: []string{"/dev/disk/by-uuid/save:tmp-default-recovery"},
			expectedErr:     `cannot find recovery key with id "bad-id": no recovery key entry for key-id`,
		},
		{
			keyslots: []fdestate.KeyslotRef{
				{ContainerRole: "system-data", Name: "tmp-default-recovery"},
				{ContainerRole: "system-save", Name: "tmp-default-recovery"},
			},
			errOn:        []string{"add:/dev/disk/by-uuid/save:tmp-default-recovery"},
			expectedAdds: []string{"/dev/disk/by-uuid/data:tmp-default-recovery"},
			expectedDeletes: []string{
				"/dev/disk/by-uuid/data:tmp-default-recovery",
				"/dev/disk/by-uuid/save:tmp-default-recovery",
			},
			expectedErr: `cannot add recovery key slot \(container-role: "system-save", name: "tmp-default-recovery"\): add error on /dev/disk/by-uuid/save:tmp-default-recovery`,
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
		c.Check(tc.expectedAdds, DeepEquals, added, cmt)

		sort.Strings(deleted)
		c.Check(tc.expectedDeletes, DeepEquals, deleted, cmt)
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
