// -*- Mode: Go; indent-tabs-mode: t -*-

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

package boot_test

import (
	"encoding/json"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/testutil"
)

var _ = Suite(&diskUnlockStateSuite{})

type diskUnlockStateSuite struct {
	testutil.BaseTest
	rootDir string
}

func (s *diskUnlockStateSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.rootDir = c.MkDir()
	dirs.SetRootDir(s.rootDir)
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *diskUnlockStateSuite) TestUnlockedStateWriteTo(c *C) {
	state := boot.DiskUnlockState{
		UbuntuData: boot.PartitionState{
			MountState: boot.PartitionMounted,
		},
		State: &secboot.ActivateState{},
	}

	state.WriteTo("test.json")

	jsonData, err := os.ReadFile(filepath.Join(s.rootDir, "run/snapd/snap-bootstrap/test.json"))
	c.Assert(err, IsNil)
	var data map[string]any
	err = json.Unmarshal(jsonData, &data)
	c.Assert(err, IsNil)

	var expectedState map[string]any
	if secboot.WithSecbootSupport {
		expectedState = map[string]any{
			"activations":    nil,
			"primary-key-id": 0.,
		}
	} else {
		expectedState = map[string]any{}
	}
	expected := map[string]any{
		"ubuntu-boot": map[string]any{},
		"ubuntu-data": map[string]any{
			"mount-state": "mounted",
		},
		"ubuntu-save": map[string]any{},
		"state":       expectedState,
	}
	c.Check(data, DeepEquals, expected)
}

func (s *diskUnlockStateSuite) TestUnlockedStateLoad(c *C) {
	data := map[string]any{
		"ubuntu-data": map[string]any{
			"mount-state": "mounted",
		},
	}
	jsonData, err := json.Marshal(data)
	c.Assert(err, IsNil)

	err = os.MkdirAll(filepath.Join(s.rootDir, "run/snapd/snap-bootstrap"), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(s.rootDir, "run/snapd/snap-bootstrap/test.json"), jsonData, 0644)
	c.Assert(err, IsNil)

	us, err := boot.LoadDiskUnlockState("test.json")
	c.Assert(err, IsNil)
	c.Check(*us, DeepEquals, boot.DiskUnlockState{
		UbuntuData: boot.PartitionState{
			MountState: boot.PartitionMounted,
		},
	})
}
