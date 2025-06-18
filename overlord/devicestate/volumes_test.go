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

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/fdestate"
)

type volumesSuite struct{}

var _ = Suite(&volumesSuite{})

func (s *volumesSuite) TestGetGadgetVolumeStructuresWithKeyslots(c *C) {
	defer devicestate.MockFDEManagerGetKeyslots(func(m *fdestate.FDEManager, keyslotRefs []fdestate.KeyslotRef) (keyslots []fdestate.Keyslot, missingRefs []fdestate.KeyslotRef, err error) {
		c.Check(keyslotRefs, IsNil)
		keyslots = []fdestate.Keyslot{
			{Name: "default", ContainerRole: "system-data", Type: fdestate.KeyslotTypePlatform},
			{Name: "default-recovery", ContainerRole: "system-data", Type: fdestate.KeyslotTypeRecovery},
			{Name: "default-fallback", ContainerRole: "system-save", Type: fdestate.KeyslotTypePlatform},
		}
		return keyslots, nil, nil
	})()

	gadgetInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"pc": {
				Structure: []gadget.VolumeStructure{
					{Role: "system-data"},
					{Role: "system-save"},
					{Role: "system-seed"},
					{Role: "system-boot"},
				},
			},
		},
	}

	structures, err := devicestate.GetGadgetVolumeStructuresWithKeyslots(nil, gadgetInfo)
	sort.Slice(structures, func(i, j int) bool {
		return structures[i].Role < structures[j].Role
	})
	c.Assert(err, IsNil)
	c.Check(structures, DeepEquals, []devicestate.VolumeStructureWithKeyslots{
		{
			VolumeStructure: gadget.VolumeStructure{Role: "system-boot"},
		},
		{
			VolumeStructure: gadget.VolumeStructure{Role: "system-data"},
			Keyslots: []fdestate.Keyslot{
				{Name: "default", ContainerRole: "system-data", Type: fdestate.KeyslotTypePlatform},
				{Name: "default-recovery", ContainerRole: "system-data", Type: fdestate.KeyslotTypeRecovery},
			},
		},
		{
			VolumeStructure: gadget.VolumeStructure{Role: "system-save"},
			Keyslots: []fdestate.Keyslot{
				{Name: "default-fallback", ContainerRole: "system-save", Type: fdestate.KeyslotTypePlatform},
			},
		},
		{
			VolumeStructure: gadget.VolumeStructure{Role: "system-seed"},
		},
	})
}

func (s *volumesSuite) TestGetGadgetVolumeStructuresWithKeyslotsError(c *C) {
	defer devicestate.MockFDEManagerGetKeyslots(func(m *fdestate.FDEManager, keyslotRefs []fdestate.KeyslotRef) (keyslots []fdestate.Keyslot, missingRefs []fdestate.KeyslotRef, err error) {
		return nil, nil, errors.New("boom!")
	})()

	_, err := devicestate.GetGadgetVolumeStructuresWithKeyslots(nil, nil)
	c.Assert(err, ErrorMatches, "failed to get key slots: boom!")
}
