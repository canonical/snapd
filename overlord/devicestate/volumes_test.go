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

package devicestate_test

import (
	"errors"
	"sort"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type volumesSuite struct {
	deviceMgrBaseSuite
}

var _ = Suite(&volumesSuite{})

func (s *volumesSuite) SetUpTest(c *C) {
	const classic = true
	s.setupBaseTest(c, classic)

	const snapYaml = `
name: canonical-pc
type: gadget
version: 0.1
`
	var gadgetYaml = `
volumes:
  pc:
    schema: gpt
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
      - name: BIOS Boot
        type: 21686148-6449-6E6F-744E-656564454649
        size: 1M
      - name: ubuntu-seed
        role: system-seed
        type: 0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1M
      - name: ubuntu-boot
        role: system-boot
        type: 0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1M
      - name: ubuntu-save
        role: system-save
        type: 0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1M
      - name: ubuntu-data
        role: system-data
        type: 0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1M
`
	si := &snap.SideInfo{
		RealName: "canonical-pc",
		Revision: snap.R(14),
		SnapID:   "ididid",
	}
	snapInfo := snaptest.MockSnapWithFiles(c, snapYaml, si, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})

	s.AddCleanup(devicestate.MockSnapstateGadgetInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		return snapInfo, nil
	}))

	s.state.Lock()
	defer s.state.Unlock()
	// mock model for DeviceCtx to work
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]any{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "canonical-pc",
		"base":         "core24",
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})
}

func (s *volumesSuite) TestGetVolumeStructuresWithKeyslots(c *C) {
	defer devicestate.MockFdestateGetKeyslots(func(st *state.State, keyslotRefs []fdestate.KeyslotRef) (keyslots []fdestate.Keyslot, missingRefs []fdestate.KeyslotRef, err error) {
		c.Check(keyslotRefs, IsNil)
		keyslots = []fdestate.Keyslot{
			{Name: "default", ContainerRole: "system-data", Type: fdestate.KeyslotTypePlatform},
			{Name: "default-recovery", ContainerRole: "system-data", Type: fdestate.KeyslotTypeRecovery},
			{Name: "default-fallback", ContainerRole: "system-save", Type: fdestate.KeyslotTypePlatform},
		}
		return keyslots, nil, nil
	})()

	s.state.Lock()
	defer s.state.Unlock()

	structures, err := devicestate.GetVolumeStructuresWithKeyslots(s.state)
	sort.Slice(structures, func(i, j int) bool {
		return structures[i].Role < structures[j].Role
	})
	c.Assert(err, IsNil)

	c.Check(structures[0].Name, Equals, "BIOS Boot")
	c.Check(structures[0].Keyslots, IsNil)
	c.Check(structures[1].Name, Equals, "mbr")
	c.Check(structures[1].Keyslots, IsNil)
	c.Check(structures[2].Name, Equals, "ubuntu-boot")
	c.Check(structures[2].Keyslots, IsNil)
	c.Check(structures[3].Name, Equals, "ubuntu-data")
	c.Check(structures[3].Keyslots, DeepEquals, []fdestate.Keyslot{
		{Name: "default", ContainerRole: "system-data", Type: fdestate.KeyslotTypePlatform},
		{Name: "default-recovery", ContainerRole: "system-data", Type: fdestate.KeyslotTypeRecovery},
	})
	c.Check(structures[4].Name, Equals, "ubuntu-save")
	c.Check(structures[4].Keyslots, DeepEquals, []fdestate.Keyslot{
		{Name: "default-fallback", ContainerRole: "system-save", Type: fdestate.KeyslotTypePlatform},
	})
	c.Check(structures[5].Name, Equals, "ubuntu-seed")
	c.Check(structures[5].Keyslots, IsNil)
}

func (s *volumesSuite) TestGetVolumeStructuresWithKeyslotsGadgetInfoError(c *C) {
	defer devicestate.MockSnapstateGadgetInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		return nil, errors.New("boom!")
	})()

	s.state.Lock()
	defer s.state.Unlock()

	_, err := devicestate.GetVolumeStructuresWithKeyslots(s.state)
	c.Assert(err, ErrorMatches, "cannot get gadget snap info: boom!")
}

func (s *volumesSuite) TestGetVolumeStructuresWithKeyslotsGetKeyslotsError(c *C) {
	defer devicestate.MockFdestateGetKeyslots(func(st *state.State, keyslotRefs []fdestate.KeyslotRef) (keyslots []fdestate.Keyslot, missingRefs []fdestate.KeyslotRef, err error) {
		return nil, nil, errors.New("boom!")
	})()

	s.state.Lock()
	defer s.state.Unlock()

	_, err := devicestate.GetVolumeStructuresWithKeyslots(s.state)
	c.Assert(err, ErrorMatches, "failed to get key slots: boom!")
}
