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
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
)

type autoRefreshGatingStore struct {
	*fakeStore
	refreshedSnaps []*snap.Info
}

type autorefreshGatingSuite struct {
	testutil.BaseTest
	state *state.State
	repo  *interfaces.Repository
	store *autoRefreshGatingStore
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

	s.store = &autoRefreshGatingStore{fakeStore: &fakeStore{}}
	snapstate.ReplaceStore(s.state, s.store)
	s.state.Set("refresh-privacy-key", "privacy-key")
}

func (r *autoRefreshGatingStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	if assertQuery != nil {
		panic("no assertion query support")
	}
	if len(currentSnaps) != len(actions) || len(currentSnaps) == 0 {
		panic("expected in test one action for each current snaps, and at least one snap")
	}
	for _, a := range actions {
		if a.Action != "refresh" {
			panic("expected refresh actions")
		}
	}

	res := []store.SnapActionResult{}
	for _, rs := range r.refreshedSnaps {
		res = append(res, store.SnapActionResult{Info: rs})
	}

	return res, nil, nil
}

func mockInstalledSnap(c *C, st *state.State, snapYaml string, hasHook bool) *snap.Info {
	snapInfo := snaptest.MockSnap(c, string(snapYaml), &snap.SideInfo{
		Revision: snap.R(1),
	})

	snapName := snapInfo.SnapName()
	si := &snap.SideInfo{RealName: snapName, SnapID: "id", Revision: snap.R(1)}
	snapstate.Set(st, snapName, &snapstate.SnapState{
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

func checkGatingTask(c *C, task *state.Task, expected map[string]*snapstate.RefreshCandidate) {
	c.Assert(task.Kind(), Equals, "conditional-auto-refresh")
	var snaps map[string]*snapstate.RefreshCandidate
	c.Assert(task.Get("snaps", &snaps), IsNil)
	c.Check(snaps, DeepEquals, expected)
}

func (s *autorefreshGatingSuite) TestAffectedByBase(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := s.state

	st.Lock()
	defer st.Unlock()
	mockInstalledSnap(c, s.state, snapAyaml, useHook)
	baseSnapA := mockInstalledSnap(c, s.state, baseSnapAyaml, noHook)
	// unrelated snaps
	snapB := mockInstalledSnap(c, s.state, snapByaml, useHook)
	mockInstalledSnap(c, s.state, baseSnapByaml, noHook)

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
	snapC := mockInstalledSnap(c, s.state, snapCyaml, useHook)
	core := mockInstalledSnap(c, s.state, coreYaml, noHook)
	snapB := mockInstalledSnap(c, s.state, snapByaml, useHook)

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
	kernel := mockInstalledSnap(c, s.state, kernelYaml, noHook)
	mockInstalledSnap(c, s.state, snapCyaml, useHook)
	mockInstalledSnap(c, s.state, snapByaml, noHook)

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
	kernel := mockInstalledSnap(c, s.state, gadget1Yaml, noHook)
	mockInstalledSnap(c, s.state, snapCyaml, useHook)
	mockInstalledSnap(c, s.state, snapByaml, noHook)

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

	snapD := mockInstalledSnap(c, s.state, snapDyaml, useHook)
	snapE := mockInstalledSnap(c, s.state, snapEyaml, useHook)
	// unrelated snap
	snapF := mockInstalledSnap(c, s.state, snapFyaml, useHook)

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

	snapG := mockInstalledSnap(c, s.state, snapGyaml, useHook)
	core := mockInstalledSnap(c, s.state, coreYaml, noHook)
	snapB := mockInstalledSnap(c, s.state, snapByaml, useHook)

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

	snapD := mockInstalledSnap(c, s.state, snapDyaml, useHook)
	snapE := mockInstalledSnap(c, s.state, snapEyaml, useHook)
	// unrelated snap
	snapF := mockInstalledSnap(c, s.state, snapFyaml, useHook)

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

	snapdSnap := mockInstalledSnap(c, s.state, snapdYaml, useHook)
	snapG := mockInstalledSnap(c, s.state, snapGyaml, useHook)
	// unrelated snap
	snapF := mockInstalledSnap(c, s.state, snapFyaml, useHook)

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

	coreSnap := mockInstalledSnap(c, s.state, coreYaml, noHook)
	snapG := mockInstalledSnap(c, s.state, snapGyaml, useHook)

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
	mockInstalledSnap(c, s.state, snapAyaml, useHook)
	mockInstalledSnap(c, s.state, snapByaml, useHook)
	mockInstalledSnap(c, s.state, snapDyaml, useHook)
	mockInstalledSnap(c, s.state, snapEyaml, useHook)
	core18 := mockInstalledSnap(c, s.state, core18Yaml, noHook)

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

func (s *autorefreshGatingSuite) TestAutorefreshPhase1FeatureFlag(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	st.Set("seeded", true)

	restore := snapstatetest.MockDeviceModel(DefaultModel())
	defer restore()

	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	defer func() { snapstate.AutoAliases = nil }()

	s.store.refreshedSnaps = []*snap.Info{{
		Architectures: []string{"all"},
		SnapType:      snap.TypeApp,
		SideInfo: snap.SideInfo{
			RealName: "snap-a",
			Revision: snap.R(8),
		},
	}}
	mockInstalledSnap(c, s.state, snapAyaml, useHook)

	// gate-auto-refresh-hook feature not enabled, expect old-style refresh.
	_, tss, err := snapstate.AutoRefresh(context.TODO(), st)
	c.Check(err, IsNil)
	c.Assert(tss, HasLen, 2)
	c.Check(tss[0].Tasks()[0].Kind(), Equals, "prerequisites")
	c.Check(tss[0].Tasks()[1].Kind(), Equals, "download-snap")
	c.Check(tss[1].Tasks()[0].Kind(), Equals, "check-rerefresh")

	// enable gate-auto-refresh-hook feature
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.gate-auto-refresh-hook", true)
	tr.Commit()

	_, tss, err = snapstate.AutoRefresh(context.TODO(), st)
	c.Check(err, IsNil)
	c.Assert(tss, HasLen, 2)
	// TODO: verify conditional-auto-refresh task data
	c.Check(tss[0].Tasks()[0].Kind(), Equals, "conditional-auto-refresh")
	c.Check(tss[1].Tasks()[0].Kind(), Equals, "run-hook")
}

func (s *autorefreshGatingSuite) TestAutoRefreshPhase1(c *C) {
	s.store.refreshedSnaps = []*snap.Info{{
		Architectures: []string{"all"},
		SnapType:      snap.TypeApp,
		SideInfo: snap.SideInfo{
			RealName: "snap-a",
			Revision: snap.R(8),
		},
	}, {
		Architectures: []string{"all"},
		SnapType:      snap.TypeBase,
		SideInfo: snap.SideInfo{
			RealName: "base-snap-b",
			Revision: snap.R(3),
		},
	}, {
		Architectures: []string{"all"},
		SnapType:      snap.TypeApp,
		SideInfo: snap.SideInfo{
			RealName: "snap-c",
			Revision: snap.R(5),
		},
	}}

	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, s.state, snapAyaml, useHook)
	mockInstalledSnap(c, s.state, snapByaml, useHook)
	mockInstalledSnap(c, s.state, snapCyaml, noHook)
	mockInstalledSnap(c, s.state, baseSnapByaml, noHook)

	restore := snapstatetest.MockDeviceModel(DefaultModel())
	defer restore()

	names, tss, err := snapstate.AutoRefreshPhase1(context.TODO(), st)
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"base-snap-b", "snap-a", "snap-c"})
	c.Assert(tss, HasLen, 2)

	c.Assert(tss[0].Tasks(), HasLen, 1)
	checkGatingTask(c, tss[0].Tasks()[0], map[string]*snapstate.RefreshCandidate{
		"snap-a": {
			SnapSetup: snapstate.SnapSetup{
				Type:      "app",
				PlugsOnly: true,
				Flags: snapstate.Flags{
					IsAutoRefresh: true,
				},
				SideInfo: &snap.SideInfo{
					RealName: "snap-a",
					Revision: snap.R(8),
				},
				DownloadInfo: &snap.DownloadInfo{},
			},
		},
		"base-snap-b": {
			SnapSetup: snapstate.SnapSetup{
				Type:      "base",
				PlugsOnly: true,
				Flags: snapstate.Flags{
					IsAutoRefresh: true,
				},
				SideInfo: &snap.SideInfo{
					RealName: "base-snap-b",
					Revision: snap.R(3),
				},
				DownloadInfo: &snap.DownloadInfo{},
			},
		},
		"snap-c": {
			SnapSetup: snapstate.SnapSetup{
				Type:      "app",
				PlugsOnly: true,
				Flags: snapstate.Flags{
					IsAutoRefresh: true,
				},
				SideInfo: &snap.SideInfo{
					RealName: "snap-c",
					Revision: snap.R(5),
				},
				DownloadInfo: &snap.DownloadInfo{},
			},
		},
	})

	c.Assert(tss[1].Tasks(), HasLen, 2)

	// check hooks for affected snaps
	seenSnaps := make(map[string]bool)
	var hs hookstate.HookSetup
	c.Assert(tss[1].Tasks()[0].Get("hook-setup", &hs), IsNil)
	c.Check(hs.Hook, Equals, "gate-auto-refresh")
	seenSnaps[hs.Snap] = true

	c.Assert(tss[1].Tasks()[1].Get("hook-setup", &hs), IsNil)
	c.Check(hs.Hook, Equals, "gate-auto-refresh")
	seenSnaps[hs.Snap] = true

	// hook for snap-a because it gets refreshed, for snap-b because its base
	// gets refreshed. snap-c is refreshed but doesn't have the hook.
	c.Check(seenSnaps, DeepEquals, map[string]bool{"snap-a": true, "snap-b": true})

	// check that refresh-candidates in the state were updated
	var candidates map[string]*snapstate.RefreshCandidate
	c.Assert(st.Get("refresh-candidates", &candidates), IsNil)
	c.Assert(candidates, HasLen, 3)
	c.Check(candidates["snap-a"], NotNil)
	c.Check(candidates["base-snap-b"], NotNil)
	c.Check(candidates["snap-c"], NotNil)
}

func (s *autorefreshGatingSuite) TestAutoRefreshPhase1ConflictsFilteredOut(c *C) {
	s.store.refreshedSnaps = []*snap.Info{{
		Architectures: []string{"all"},
		SnapType:      snap.TypeApp,
		SideInfo: snap.SideInfo{
			RealName: "snap-a",
			Revision: snap.R(8),
		},
	}, {
		Architectures: []string{"all"},
		SnapType:      snap.TypeBase,
		SideInfo: snap.SideInfo{
			RealName: "snap-c",
			Revision: snap.R(5),
		},
	}}

	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, s.state, snapAyaml, useHook)
	mockInstalledSnap(c, s.state, snapCyaml, noHook)

	conflictChange := st.NewChange("conflicting change", "")
	conflictTask := st.NewTask("conflicting task", "")
	si := &snap.SideInfo{
		RealName: "snap-c",
		Revision: snap.R(1),
	}
	sup := snapstate.SnapSetup{SideInfo: si}
	conflictTask.Set("snap-setup", sup)
	conflictChange.AddTask(conflictTask)

	restore := snapstatetest.MockDeviceModel(DefaultModel())
	defer restore()

	logbuf, restoreLogger := logger.MockLogger()
	defer restoreLogger()

	names, tss, err := snapstate.AutoRefreshPhase1(context.TODO(), st)
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"snap-a"})
	c.Assert(tss, HasLen, 2)

	c.Assert(tss[0].Tasks(), HasLen, 1)
	checkGatingTask(c, tss[0].Tasks()[0], map[string]*snapstate.RefreshCandidate{
		"snap-a": {
			SnapSetup: snapstate.SnapSetup{
				Type:      "app",
				PlugsOnly: true,
				Flags: snapstate.Flags{
					IsAutoRefresh: true,
				},
				SideInfo: &snap.SideInfo{
					RealName: "snap-a",
					Revision: snap.R(8),
				},
				DownloadInfo: &snap.DownloadInfo{},
			}}})

	c.Assert(tss[1].Tasks(), HasLen, 1)

	c.Assert(logbuf.String(), testutil.Contains, `cannot refresh snap "snap-c": snap "snap-c" has "conflicting change" change in progress`)

	seenSnaps := make(map[string]bool)
	var hs hookstate.HookSetup
	c.Assert(tss[1].Tasks()[0].Get("hook-setup", &hs), IsNil)
	c.Check(hs.Hook, Equals, "gate-auto-refresh")
	seenSnaps[hs.Snap] = true

	c.Check(seenSnaps, DeepEquals, map[string]bool{"snap-a": true})

	// check that refresh-candidates in the state were updated
	var candidates map[string]*snapstate.RefreshCandidate
	c.Assert(st.Get("refresh-candidates", &candidates), IsNil)
	c.Assert(candidates, HasLen, 2)
	c.Check(candidates["snap-a"], NotNil)
	c.Check(candidates["snap-c"], NotNil)
}

func (s *autorefreshGatingSuite) TestAutoRefreshPhase1NoHooks(c *C) {
	s.store.refreshedSnaps = []*snap.Info{{
		Architectures: []string{"all"},
		SnapType:      snap.TypeBase,
		SideInfo: snap.SideInfo{
			RealName: "base-snap-b",
			Revision: snap.R(3),
		},
	}, {
		Architectures: []string{"all"},
		SnapType:      snap.TypeBase,
		SideInfo: snap.SideInfo{
			RealName: "snap-c",
			Revision: snap.R(5),
		},
	}}

	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, s.state, snapByaml, noHook)
	mockInstalledSnap(c, s.state, snapCyaml, noHook)
	mockInstalledSnap(c, s.state, baseSnapByaml, noHook)

	restore := snapstatetest.MockDeviceModel(DefaultModel())
	defer restore()

	names, tss, err := snapstate.AutoRefreshPhase1(context.TODO(), st)
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"base-snap-b", "snap-c"})
	c.Assert(tss, HasLen, 1)

	c.Assert(tss[0].Tasks(), HasLen, 1)
	c.Check(tss[0].Tasks()[0].Kind(), Equals, "conditional-auto-refresh")
}

func fakeReadInfo(name string, si *snap.SideInfo) (*snap.Info, error) {
	info := &snap.Info{
		SuggestedName: name,
		SideInfo:      *si,
		Architectures: []string{"all"},
		SnapType:      snap.TypeApp,
		Epoch:         snap.Epoch{},
	}
	switch name {
	case "base-snap-b":
		info.SnapType = snap.TypeBase
	case "snap-a", "snap-b":
		info.Hooks = map[string]*snap.HookInfo{
			"gate-auto-refresh": {
				Name: "gate-auto-refresh",
				Snap: info,
			},
		}
		if name == "snap-b" {
			info.Base = "base-snap-b"
		}
	}
	return info, nil
}

func (s *snapmgrTestSuite) TestAutoRefreshPhase2(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	snapstate.ReplaceStore(s.state, &autoRefreshGatingStore{
		fakeStore: s.fakeStore,
		refreshedSnaps: []*snap.Info{{
			Architectures: []string{"all"},
			SnapType:      snap.TypeApp,
			SideInfo: snap.SideInfo{
				RealName: "snap-a",
				Revision: snap.R(8),
			},
		}, {
			Architectures: []string{"all"},
			SnapType:      snap.TypeBase,
			SideInfo: snap.SideInfo{
				RealName: "base-snap-b",
				Revision: snap.R(3),
			},
		}}})

	mockInstalledSnap(c, s.state, snapAyaml, useHook)
	mockInstalledSnap(c, s.state, snapByaml, useHook)
	mockInstalledSnap(c, s.state, baseSnapByaml, noHook)

	snapstate.MockSnapReadInfo(fakeReadInfo)

	restore := snapstatetest.MockDeviceModel(DefaultModel())
	defer restore()

	names, tss, err := snapstate.AutoRefreshPhase1(context.TODO(), st)
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"base-snap-b", "snap-a"})

	chg := s.state.NewChange("refresh", "...")
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Check(chg.Err(), IsNil)

	expected := []string{
		"conditional-auto-refresh",
		"run-hook [snap-a;gate-auto-refresh]",
		// snap-b hook is triggered because of base-snap-b refresh
		"run-hook [snap-b;gate-auto-refresh]",
		"prerequisites",
		"download-snap",
		"validate-snap",
		"mount-snap",
		"run-hook [base-snap-b;pre-refresh]",
		"stop-snap-services",
		"remove-aliases",
		"unlink-current-snap",
		"copy-snap-data",
		"setup-profiles",
		"link-snap",
		"auto-connect",
		"set-auto-aliases",
		"setup-aliases",
		"run-hook [base-snap-b;post-refresh]",
		"start-snap-services",
		"cleanup",
		"run-hook [base-snap-b;check-health]",
		"prerequisites",
		"download-snap",
		"validate-snap",
		"mount-snap",
		"run-hook [snap-a;pre-refresh]",
		"stop-snap-services",
		"remove-aliases",
		"unlink-current-snap",
		"copy-snap-data",
		"setup-profiles",
		"link-snap",
		"auto-connect",
		"set-auto-aliases",
		"setup-aliases",
		"run-hook [snap-a;post-refresh]",
		"start-snap-services",
		"cleanup",
		"run-hook [snap-a;configure]",
		"run-hook [snap-a;check-health]",
		"check-rerefresh",
	}
	verifyPhasedAutorefreshTasks(c, chg.Tasks(), expected)
}

// XXX: this case is probably artifical; with proper conflict prevention
// we shouldn't get conflicts from doInstall in phase2.
func (s *snapmgrTestSuite) TestAutoRefreshPhase2Conflict(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	snapstate.ReplaceStore(s.state, &autoRefreshGatingStore{
		fakeStore: s.fakeStore,
		refreshedSnaps: []*snap.Info{{
			Architectures: []string{"all"},
			SnapType:      snap.TypeApp,
			SideInfo: snap.SideInfo{
				RealName: "snap-a",
				Revision: snap.R(8),
			},
		}, {
			Architectures: []string{"all"},
			SnapType:      snap.TypeBase,
			SideInfo: snap.SideInfo{
				RealName: "base-snap-b",
				Revision: snap.R(3),
			},
		}}})

	mockInstalledSnap(c, s.state, snapAyaml, useHook)
	mockInstalledSnap(c, s.state, snapByaml, useHook)
	mockInstalledSnap(c, s.state, baseSnapByaml, noHook)

	snapstate.MockSnapReadInfo(fakeReadInfo)

	restore := snapstatetest.MockDeviceModel(DefaultModel())
	defer restore()

	names, tss, err := snapstate.AutoRefreshPhase1(context.TODO(), st)
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"base-snap-b", "snap-a"})

	chg := s.state.NewChange("refresh", "...")
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	conflictChange := st.NewChange("conflicting change", "")
	conflictTask := st.NewTask("conflicting task", "")
	si := &snap.SideInfo{
		RealName: "snap-a",
		Revision: snap.R(1),
	}
	sup := snapstate.SnapSetup{SideInfo: si}
	conflictTask.Set("snap-setup", sup)
	conflictChange.AddTask(conflictTask)
	conflictTask.WaitFor(tss[0].Tasks()[0])

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Check(chg.Err(), IsNil)

	expected := []string{
		"conditional-auto-refresh",
		"run-hook [snap-a;gate-auto-refresh]",
		// snap-b hook is triggered because of base-snap-b refresh
		"run-hook [snap-b;gate-auto-refresh]",
		"prerequisites",
		"download-snap",
		"validate-snap",
		"mount-snap",
		"run-hook [base-snap-b;pre-refresh]",
		"stop-snap-services",
		"remove-aliases",
		"unlink-current-snap",
		"copy-snap-data",
		"setup-profiles",
		"link-snap",
		"auto-connect",
		"set-auto-aliases",
		"setup-aliases",
		"run-hook [base-snap-b;post-refresh]",
		"start-snap-services",
		"cleanup",
		"run-hook [base-snap-b;check-health]",
		"check-rerefresh",
	}
	verifyPhasedAutorefreshTasks(c, chg.Tasks(), expected)
}

func (s *snapmgrTestSuite) TestAutoRefreshPhase2GatedSnaps(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	restore := snapstate.MockSnapsToRefresh(func(gatingTask *state.Task) ([]*snapstate.RefreshCandidate, error) {
		c.Assert(gatingTask.Kind(), Equals, "conditional-auto-refresh")
		var candidates map[string]*snapstate.RefreshCandidate
		c.Assert(gatingTask.Get("snaps", &candidates), IsNil)
		seenSnaps := make(map[string]bool)
		var filteredByGatingHooks []*snapstate.RefreshCandidate
		for _, cand := range candidates {
			seenSnaps[cand.InstanceName()] = true
			if cand.InstanceName() == "snap-a" {
				continue
			}
			filteredByGatingHooks = append(filteredByGatingHooks, cand)
		}
		c.Check(seenSnaps, DeepEquals, map[string]bool{
			"snap-a":      true,
			"base-snap-b": true,
		})
		return filteredByGatingHooks, nil
	})
	defer restore()

	snapstate.ReplaceStore(s.state, &autoRefreshGatingStore{
		fakeStore: s.fakeStore,
		refreshedSnaps: []*snap.Info{
			{
				Architectures: []string{"all"},
				SnapType:      snap.TypeApp,
				SideInfo: snap.SideInfo{
					RealName: "snap-a",
					Revision: snap.R(8),
				},
			}, {
				Architectures: []string{"all"},
				SnapType:      snap.TypeBase,
				SideInfo: snap.SideInfo{
					RealName: "base-snap-b",
					Revision: snap.R(3),
				},
			},
		}})

	mockInstalledSnap(c, s.state, snapAyaml, useHook)
	mockInstalledSnap(c, s.state, snapByaml, useHook)
	mockInstalledSnap(c, s.state, baseSnapByaml, noHook)

	snapstate.MockSnapReadInfo(fakeReadInfo)

	restoreModel := snapstatetest.MockDeviceModel(DefaultModel())
	defer restoreModel()

	names, tss, err := snapstate.AutoRefreshPhase1(context.TODO(), st)
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"base-snap-b", "snap-a"})

	chg := s.state.NewChange("refresh", "...")
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Check(chg.Err(), IsNil)

	expected := []string{
		"conditional-auto-refresh",
		"run-hook [snap-a;gate-auto-refresh]",
		// snap-b hook is triggered because of base-snap-b refresh
		"run-hook [snap-b;gate-auto-refresh]",
		"prerequisites",
		"download-snap",
		"validate-snap",
		"mount-snap",
		"run-hook [base-snap-b;pre-refresh]",
		"stop-snap-services",
		"remove-aliases",
		"unlink-current-snap",
		"copy-snap-data",
		"setup-profiles",
		"link-snap",
		"auto-connect",
		"set-auto-aliases",
		"setup-aliases",
		"run-hook [base-snap-b;post-refresh]",
		"start-snap-services",
		"cleanup",
		"run-hook [base-snap-b;check-health]",
		"check-rerefresh",
	}
	verifyPhasedAutorefreshTasks(c, chg.Tasks(), expected)
}

func verifyPhasedAutorefreshTasks(c *C, tasks []*state.Task, expected []string) {
	for i, t := range tasks {
		var got string
		if t.Kind() == "run-hook" {
			var hsup hookstate.HookSetup
			c.Assert(t.Get("hook-setup", &hsup), IsNil)
			got = fmt.Sprintf("%s [%s;%s]", t.Kind(), hsup.Snap, hsup.Hook)
		} else {
			got = t.Kind()
		}
		c.Assert(got, Equals, expected[i])
	}
}
