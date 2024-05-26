// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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

package snapstate_test

import (
	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

func (s *snapmgrTestSuite) TestConfigDefaults(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	// using MockSnap, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	s.state.Lock()
	defer s.state.Unlock()

	s.prepareGadget(c)

	deviceCtx := deviceWithGadgetContext("the-gadget")

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11), SnapID: "somesnapidididididididididididid"},
		}),
		Current:  snap.R(11),
		SnapType: "app",
	})
	makeInstalledMockCoreSnap(c)

	defls := mylog.Check2(snapstate.ConfigDefaults(s.state, deviceCtx, "some-snap"))

	c.Assert(defls, DeepEquals, map[string]interface{}{"key": "value"})

	snapstate.Set(s.state, "local-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "local-snap", Revision: snap.R(5)},
		}),
		Current:  snap.R(5),
		SnapType: "app",
	})
	_ = mylog.Check2(snapstate.ConfigDefaults(s.state, deviceCtx, "local-snap"))
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestConfigDefaultsSmokeUC20(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	// using MockSnap, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	s.state.Lock()
	defer s.state.Unlock()

	// provide a uc20 gadget structure
	s.prepareGadget(c, `
        bootloader: grub
        structure:
        - name: ubuntu-seed
          role: system-seed
          filesystem: vfat
          type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
          size: 1200M
        - name: ubuntu-boot
          role: system-boot
          filesystem: ext4
          type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
          # whats the appropriate size?
          size: 750M
        - name: ubuntu-data
          role: system-data
          filesystem: ext4
          type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
          size: 1G
`)
	// use a UC20 model context
	deviceCtx := deviceWithGadgetContext20("the-gadget")

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11), SnapID: "somesnapidididididididididididid"},
		}),
		Current:  snap.R(11),
		SnapType: "app",
	})
	makeInstalledMockCoreSnap(c)

	defls := mylog.Check2(snapstate.ConfigDefaults(s.state, deviceCtx, "some-snap"))

	c.Assert(defls, DeepEquals, map[string]interface{}{"key": "value"})
}

func (s *snapmgrTestSuite) TestConfigDefaultsNoGadget(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	// using MockSnap, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	s.state.Lock()
	defer s.state.Unlock()

	deviceCtxNoGadget := deviceWithoutGadgetContext()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11), SnapID: "somesnapidididididididididididid"},
		}),
		Current:  snap.R(11),
		SnapType: "app",
	})
	makeInstalledMockCoreSnap(c)

	_ := mylog.Check2(snapstate.ConfigDefaults(s.state, deviceCtxNoGadget, "some-snap"))
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestConfigDefaultsSystemWithCore(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	// using MockSnapReadInfo, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	s.state.Lock()
	defer s.state.Unlock()

	s.prepareGadget(c, `
defaults:
    system:
        foo: bar
`)

	deviceCtx := deviceWithGadgetContext("the-gadget")

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11), SnapID: "the-core-ididididididididididid"},
		}),
		Current:  snap.R(11),
		SnapType: "os",
	})

	makeInstalledMockCoreSnap(c)

	defls := mylog.Check2(snapstate.ConfigDefaults(s.state, deviceCtx, "core"))

	c.Assert(defls, DeepEquals, map[string]interface{}{"foo": "bar"})
}

var snapdSnapYaml = `name: snapd
version: 1.0
type: snapd
`

func (s *snapmgrTestSuite) TestConfigDefaultsSystemWithSnapdNoCore(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	// using MockSnapReadInfo, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	s.state.Lock()
	defer s.state.Unlock()

	s.prepareGadget(c, `
defaults:
    system:
        foo: bar
`)

	deviceCtx := &snapstatetest.TrivialDeviceContext{
		DeviceModel: MakeModel(map[string]interface{}{
			"gadget": "the-gadget",
			"base":   "the-base",
		}),
	}

	snapstate.Set(s.state, "core", nil)
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "snapd", SnapID: "the-snapd-snapidididididididididi", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "snapd",
	})

	snaptest.MockSnap(c, snapdSnapYaml, &snap.SideInfo{
		RealName: "snapd",
		Revision: snap.R(1),
	})

	defls := mylog.Check2(snapstate.ConfigDefaults(s.state, deviceCtx, "core"))

	c.Assert(defls, DeepEquals, map[string]interface{}{"foo": "bar"})
}

func (s *snapmgrTestSuite) TestConfigDefaultsSystemConflictsCoreSnapId(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	// using MockSnapReadInfo, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	s.state.Lock()
	defer s.state.Unlock()

	s.prepareGadget(c, `
defaults:
    system:
        foo: bar
    thecoresnapididididididididididi:
        foo: other-bar
        other-key: other-key-default
`)

	deviceCtx := deviceWithGadgetContext("the-gadget")

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "core", SnapID: "thecoresnapididididididididididi", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "os",
	})

	makeInstalledMockCoreSnap(c)

	// 'system' key defaults take precedence over snap-id ones
	defls := mylog.Check2(snapstate.ConfigDefaults(s.state, deviceCtx, "core"))

	c.Assert(defls, DeepEquals, map[string]interface{}{"foo": "bar"})
}

func (s *snapmgrTestSuite) TestTransitionCoreTasksNoUbuntuCore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "core", SnapID: "core-snap-id", Revision: snap.R(1)}}),
		Current:  snap.R(1),
		SnapType: "os",
	})

	_ := mylog.Check2(snapstate.TransitionCore(s.state, "ubuntu-core", "core"))
	c.Assert(err, ErrorMatches, `cannot transition snap "ubuntu-core": not installed`)
}
