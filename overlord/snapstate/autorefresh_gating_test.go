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
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
)

type autorefreshGatingSuite struct {
	testutil.BaseTest
	state *state.State
	repo  *interfaces.Repository
}

var _ = Suite(&autorefreshGatingSuite{})

func (s *autorefreshGatingSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() {
		dirs.SetRootDir("/")
	})
	s.state = state.New(nil)

	s.repo = interfaces.NewRepository()
	for _, iface := range builtin.Interfaces() {
		c.Assert(s.repo.AddInterface(iface), IsNil)
	}

	s.state.Lock()
	defer s.state.Unlock()
	ifacerepo.Replace(s.state, s.repo)
}

func (s *autorefreshGatingSuite) mockInstalledSnap(c *C, snapYaml string, hasHook bool) *snap.Info {
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

const baseSnapAyaml = `name: base-snap-a
type: base
`

const snapAyaml = `name: snap-a
type: app
base: base-snap-a
`

const baseSnapByaml = `name: base-snap-b
type: base
`

const snapByaml = `name: snap-b
type: app
base: base-snap-b
version: 1
`

const kernelYaml = `name: kernel
type: kernel
version: 1
`

const gadget1Yaml = `name: gadget
type: gadget
version: 1
`

const snapCyaml = `name: snap-c
type: app
version: 1
`

const snapDyaml = `name: snap-d
type: app
version: 1
slots:
    slot: desktop
`

const snapEyaml = `name: snap-e
type: app
version: 1
base: other-base
plugs:
    plug: desktop
`

const snapFyaml = `name: snap-f
type: app
version: 1
plugs:
    plug: desktop
`

const snapGyaml = `name: snap-g
type: app
version: 1
base: other-base
plugs:
    desktop:
    mir:
`

const coreYaml = `name: core
type: os
version: 1
slots:
    desktop:
    mir:
`

const core18Yaml = `name: core18
type: os
version: 1
`

const snapdYaml = `name: snapd
version: 1
type: snapd
slots:
    desktop:
`

const useHook = true
const noHook = false

func (s *autorefreshGatingSuite) TestAffectedByBase(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := s.state

	st.Lock()
	defer st.Unlock()
	s.mockInstalledSnap(c, snapAyaml, useHook)
	baseSnapA := s.mockInstalledSnap(c, baseSnapAyaml, noHook)
	// unrelated snaps
	snapB := s.mockInstalledSnap(c, snapByaml, useHook)
	s.mockInstalledSnap(c, baseSnapByaml, noHook)

	c.Assert(s.repo.AddSnap(snapB), IsNil)

	updates := []*snap.Info{baseSnapA}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]*snapstate.AffectedSnapInfo{
		"snap-a": {
			Base: true,
			AffectingSnaps: map[string]bool{
				"base-snap-a": true,
			}}})
}

func (s *autorefreshGatingSuite) TestAffectedByCore(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := s.state

	st.Lock()
	defer st.Unlock()
	snapC := s.mockInstalledSnap(c, snapCyaml, useHook)
	core := s.mockInstalledSnap(c, coreYaml, noHook)
	snapB := s.mockInstalledSnap(c, snapByaml, useHook)

	c.Assert(s.repo.AddSnap(core), IsNil)
	c.Assert(s.repo.AddSnap(snapB), IsNil)
	c.Assert(s.repo.AddSnap(snapC), IsNil)

	updates := []*snap.Info{core}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]*snapstate.AffectedSnapInfo{
		"snap-c": {
			Base: true,
			AffectingSnaps: map[string]bool{
				"core": true,
			}}})
}

func (s *autorefreshGatingSuite) TestAffectedByKernel(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := s.state

	st.Lock()
	defer st.Unlock()
	kernel := s.mockInstalledSnap(c, kernelYaml, noHook)
	s.mockInstalledSnap(c, snapCyaml, useHook)
	s.mockInstalledSnap(c, snapByaml, noHook)

	updates := []*snap.Info{kernel}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]*snapstate.AffectedSnapInfo{
		"snap-c": {
			Restart: true,
			AffectingSnaps: map[string]bool{
				"kernel": true,
			}}})
}

func (s *autorefreshGatingSuite) TestAffectedByGadget(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := s.state

	st.Lock()
	defer st.Unlock()
	kernel := s.mockInstalledSnap(c, gadget1Yaml, noHook)
	s.mockInstalledSnap(c, snapCyaml, useHook)
	s.mockInstalledSnap(c, snapByaml, noHook)

	updates := []*snap.Info{kernel}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]*snapstate.AffectedSnapInfo{
		"snap-c": {
			Restart: true,
			AffectingSnaps: map[string]bool{
				"gadget": true,
			}}})
}

func (s *autorefreshGatingSuite) TestAffectedBySlot(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := s.state

	st.Lock()
	defer st.Unlock()

	snapD := s.mockInstalledSnap(c, snapDyaml, useHook)
	snapE := s.mockInstalledSnap(c, snapEyaml, useHook)
	// unrelated snap
	snapF := s.mockInstalledSnap(c, snapFyaml, useHook)

	c.Assert(s.repo.AddSnap(snapF), IsNil)
	c.Assert(s.repo.AddSnap(snapD), IsNil)
	c.Assert(s.repo.AddSnap(snapE), IsNil)
	cref := &interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "snap-e", Name: "plug"}, SlotRef: interfaces.SlotRef{Snap: "snap-d", Name: "slot"}}
	_, err := s.repo.Connect(cref, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	updates := []*snap.Info{snapD}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]*snapstate.AffectedSnapInfo{
		"snap-e": {
			Restart: true,
			AffectingSnaps: map[string]bool{
				"snap-d": true,
			}}})
}

func (s *autorefreshGatingSuite) TestNotAffectedByCoreOrSnapdSlot(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := s.state

	st.Lock()
	defer st.Unlock()

	snapG := s.mockInstalledSnap(c, snapGyaml, useHook)
	core := s.mockInstalledSnap(c, coreYaml, noHook)
	snapB := s.mockInstalledSnap(c, snapByaml, useHook)

	c.Assert(s.repo.AddSnap(snapG), IsNil)
	c.Assert(s.repo.AddSnap(core), IsNil)
	c.Assert(s.repo.AddSnap(snapB), IsNil)

	cref := &interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "snap-g", Name: "mir"}, SlotRef: interfaces.SlotRef{Snap: "core", Name: "mir"}}
	_, err := s.repo.Connect(cref, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	updates := []*snap.Info{core}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, HasLen, 0)
}

func (s *autorefreshGatingSuite) TestAffectedByPlugWithMountBackend(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := s.state

	st.Lock()
	defer st.Unlock()

	snapD := s.mockInstalledSnap(c, snapDyaml, useHook)
	snapE := s.mockInstalledSnap(c, snapEyaml, useHook)
	// unrelated snap
	snapF := s.mockInstalledSnap(c, snapFyaml, useHook)

	c.Assert(s.repo.AddSnap(snapF), IsNil)
	c.Assert(s.repo.AddSnap(snapD), IsNil)
	c.Assert(s.repo.AddSnap(snapE), IsNil)
	cref := &interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "snap-e", Name: "plug"}, SlotRef: interfaces.SlotRef{Snap: "snap-d", Name: "slot"}}
	_, err := s.repo.Connect(cref, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	// snapE has a plug using mount backend and is refreshed, this affects slot of snap-d.
	updates := []*snap.Info{snapE}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]*snapstate.AffectedSnapInfo{
		"snap-d": {
			Restart: true,
			AffectingSnaps: map[string]bool{
				"snap-e": true,
			}}})
}

func (s *autorefreshGatingSuite) TestAffectedByPlugWithMountBackendSnapdSlot(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := s.state

	st.Lock()
	defer st.Unlock()

	snapdSnap := s.mockInstalledSnap(c, snapdYaml, useHook)
	snapG := s.mockInstalledSnap(c, snapGyaml, useHook)
	// unrelated snap
	snapF := s.mockInstalledSnap(c, snapFyaml, useHook)

	c.Assert(s.repo.AddSnap(snapF), IsNil)
	c.Assert(s.repo.AddSnap(snapdSnap), IsNil)
	c.Assert(s.repo.AddSnap(snapG), IsNil)
	cref := &interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "snap-g", Name: "desktop"}, SlotRef: interfaces.SlotRef{Snap: "snapd", Name: "desktop"}}
	_, err := s.repo.Connect(cref, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	// snapE has a plug using mount backend, refreshing snapd affects snapE.
	updates := []*snap.Info{snapdSnap}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]*snapstate.AffectedSnapInfo{
		"snap-g": {
			Restart: true,
			AffectingSnaps: map[string]bool{
				"snapd": true,
			}}})
}

func (s *autorefreshGatingSuite) TestAffectedByPlugWithMountBackendCoreSlot(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := s.state

	st.Lock()
	defer st.Unlock()

	coreSnap := s.mockInstalledSnap(c, coreYaml, noHook)
	snapG := s.mockInstalledSnap(c, snapGyaml, useHook)

	c.Assert(s.repo.AddSnap(coreSnap), IsNil)
	c.Assert(s.repo.AddSnap(snapG), IsNil)
	cref := &interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "snap-g", Name: "desktop"}, SlotRef: interfaces.SlotRef{Snap: "core", Name: "desktop"}}
	_, err := s.repo.Connect(cref, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	// snapG has a plug using mount backend, refreshing core affects snapE.
	updates := []*snap.Info{coreSnap}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]*snapstate.AffectedSnapInfo{
		"snap-g": {
			Restart: true,
			AffectingSnaps: map[string]bool{
				"core": true,
			}}})
}

func (s *autorefreshGatingSuite) TestAffectedByBootBase(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	st := s.state

	r := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer r()

	st.Lock()
	defer st.Unlock()
	s.mockInstalledSnap(c, snapAyaml, useHook)
	s.mockInstalledSnap(c, snapByaml, useHook)
	s.mockInstalledSnap(c, snapDyaml, useHook)
	s.mockInstalledSnap(c, snapEyaml, useHook)
	core18 := s.mockInstalledSnap(c, core18Yaml, noHook)

	updates := []*snap.Info{core18}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]*snapstate.AffectedSnapInfo{
		"snap-a": {
			Base:    false,
			Restart: true,
			AffectingSnaps: map[string]bool{
				"core18": true,
			},
		},
		"snap-b": {
			Base:    false,
			Restart: true,
			AffectingSnaps: map[string]bool{
				"core18": true,
			},
		},
		"snap-d": {
			Base:    false,
			Restart: true,
			AffectingSnaps: map[string]bool{
				"core18": true,
			},
		},
		"snap-e": {
			Base:    false,
			Restart: true,
			AffectingSnaps: map[string]bool{
				"core18": true,
			}}})
}

func (s *autorefreshGatingSuite) TestCreateAutoRefreshGateHooks(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	affected := map[string]*snapstate.AffectedSnapInfo{
		"snap-a": {
			Base:    true,
			Restart: true,
		},
		"snap-b": {},
	}

	seenSnaps := make(map[string]bool)

	ts := snapstate.CreateGateAutoRefreshHooks(st, affected)
	c.Assert(ts.Tasks(), HasLen, 2)

	checkHook := func(t *state.Task) {
		c.Assert(t.Kind(), Equals, "run-hook")
		var hs hookstate.HookSetup
		c.Assert(t.Get("hook-setup", &hs), IsNil)
		c.Check(hs.Hook, Equals, "gate-auto-refresh")
		c.Check(hs.Optional, Equals, true)
		seenSnaps[hs.Snap] = true

		var data interface{}
		c.Assert(t.Get("hook-context", &data), IsNil)

		// the order of hook tasks is not deterministic
		if hs.Snap == "snap-a" {
			c.Check(data, DeepEquals, map[string]interface{}{"base": true, "restart": true})
		} else {
			c.Assert(hs.Snap, Equals, "snap-b")
			c.Check(data, DeepEquals, map[string]interface{}{"base": false, "restart": false})
		}
	}

	checkHook(ts.Tasks()[0])
	checkHook(ts.Tasks()[1])

	c.Check(seenSnaps, DeepEquals, map[string]bool{"snap-a": true, "snap-b": true})
}
