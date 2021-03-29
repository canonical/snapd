// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
)

type refreshControlSuite struct {
	testutil.BaseTest
	state *state.State
	repo  *interfaces.Repository
}

var _ = Suite(&refreshControlSuite{})

func (s *refreshControlSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.BaseTest.AddCleanup(func() {
		dirs.SetRootDir("/")
	})
	s.state = state.New(nil)
	s.repo = interfaces.NewRepository()

	iface1 := &ifacetest.TestInterface{InterfaceName: "iface1"}
	c.Assert(s.repo.AddInterface(iface1), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	ifacerepo.Replace(s.state, s.repo)
}

func (s *refreshControlSuite) mockInstalledSnap(c *C, snapYaml []byte, hasHook bool) *snap.Info {
	snapInfo := snaptest.MockSnap(c, string(snapYaml), &snap.SideInfo{
		Revision: snap.R(1),
	})

	snapName := snapInfo.SnapName()
	si := &snap.SideInfo{RealName: snapName, SnapID: "id", Revision: snap.R(1)}
	snapstate.Set(s.state, snapName, &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
		SnapType: string(snapInfo.Type()),
	})

	if hasHook {
		c.Assert(os.MkdirAll(snapInfo.HooksDir(), 0775), IsNil)
		err := ioutil.WriteFile(filepath.Join(snapInfo.HooksDir(), "gate-auto-refresh"), nil, 0755)
		c.Assert(err, IsNil)
	}
	return snapInfo
}

var baseSnapAyaml = []byte(`name: base-snap-a
type: base
`)

var snapAyaml = []byte(`name: snap-a
type: app
base: base-snap-a
`)

var baseSnapByaml = []byte(`name: base-snap-b
type: base
`)

var snapByaml = []byte(`name: snap-b
type: app
base: base-snap-b
`)

var kernelYaml = []byte(`name: kernel
type: kernel
`)

var gadget1Yaml = []byte(`name: gadget
type: gadget
`)

var snapCyaml = []byte(`name: snap-c
type: app
`)

var snapDyaml = []byte(`name: snap-d
type: app
version: 1
slots:
    slot: iface1
`)

var snapEyaml = []byte(`name: snap-e
type: app
version: 1
plugs:
    plug: iface1
`)

var snapFyaml = []byte(`name: snap-e
type: app
version: 1
plugs:
    plug: iface1
`)

var coreYaml = []byte(`name: core
type: os
`)

func (s *refreshControlSuite) TestAffectedByBase(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()
	s.mockInstalledSnap(c, snapAyaml, true)
	baseSnapA := s.mockInstalledSnap(c, baseSnapAyaml, false)
	// unrelated snaps
	s.mockInstalledSnap(c, snapByaml, true)
	s.mockInstalledSnap(c, baseSnapByaml, false)

	updates := []*snap.Info{baseSnapA}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]map[string]bool{"snap-a": {"base-snap-a": true}})
}

func (s *refreshControlSuite) TestAffectedByCore(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()
	s.mockInstalledSnap(c, snapCyaml, true)
	core := s.mockInstalledSnap(c, coreYaml, false)
	s.mockInstalledSnap(c, snapByaml, true)

	updates := []*snap.Info{core}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]map[string]bool{"snap-c": {"core": true}})
}

func (s *refreshControlSuite) TestAffectedByKernel(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()
	kernel := s.mockInstalledSnap(c, kernelYaml, false)
	s.mockInstalledSnap(c, snapCyaml, true)
	s.mockInstalledSnap(c, snapByaml, false)

	updates := []*snap.Info{kernel}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]map[string]bool{"snap-c": {"kernel": true}})
}

func (s *refreshControlSuite) TestAffectedByGadget(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()
	kernel := s.mockInstalledSnap(c, gadget1Yaml, false)
	s.mockInstalledSnap(c, snapCyaml, true)
	s.mockInstalledSnap(c, snapByaml, false)

	updates := []*snap.Info{kernel}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]map[string]bool{"snap-c": {"gadget": true}})
}

func (s *refreshControlSuite) TestAffectedBySlot(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	snapD := s.mockInstalledSnap(c, snapDyaml, true)
	snapE := s.mockInstalledSnap(c, snapEyaml, true)
	// unrelated snap
	s.mockInstalledSnap(c, snapFyaml, true)

	c.Assert(s.repo.AddSnap(snapD), IsNil)
	c.Assert(s.repo.AddSnap(snapE), IsNil)
	cref := &interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "snap-e", Name: "plug"}, SlotRef: interfaces.SlotRef{Snap: "snap-d", Name: "slot"}}
	_, err := s.repo.Connect(cref, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	updates := []*snap.Info{snapD}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]map[string]bool{"snap-e": {"snap-d": true}})
}

func (s *refreshControlSuite) TestAffectedByPlugWithMountBackend(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	snapD := s.mockInstalledSnap(c, snapDyaml, true)
	snapE := s.mockInstalledSnap(c, snapEyaml, true)
	// unrelated snap
	s.mockInstalledSnap(c, snapFyaml, true)

	c.Assert(s.repo.AddSnap(snapD), IsNil)
	c.Assert(s.repo.AddSnap(snapE), IsNil)
	cref := &interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "snap-e", Name: "plug"}, SlotRef: interfaces.SlotRef{Snap: "snap-d", Name: "slot"}}
	_, err := s.repo.Connect(cref, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	// snapE has a plug using mount backend and is refreshed, this affects slot of snap-d.
	updates := []*snap.Info{snapE}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]map[string]bool{"snap-d": {"snap-e": true}})
}
