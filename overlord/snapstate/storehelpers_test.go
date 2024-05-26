// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
)

const snapYaml1 = `
name: some-snap
version: 1.0
`

const snapYaml2 = `
name: some-snap
version: 1.0
base: none
`

const snapYamlWithBase1 = `
name: some-snap1
version: 1.0
base: some-base
`

const snapYamlWithBase2 = `
name: some-snap2
version: 1.0
base: some-base
`

const snapYamlWithBase3 = `
name: some-snap3
version: 2.0
base: other-base
`

const snapYamlWithBase4 = `
name: some-snap4
version: 1.0
base: yet-another-base
`

const snapYamlWithContentPlug1 = `
name: some-snap
version: 1.0
base: some-base
plugs:
  some-plug:
    interface: content
    content: shared-content
    default-provider: snap-content-slot
`

const snapYamlWithContentPlug2 = `
name: some-snap2
version: 1.0
base: some-base
plugs:
  some-plug:
    interface: content
    content: shared-content
    default-provider: snap-content-slot
`

const snapYamlWithContentPlug3 = `
name: some-snap
version: 1.0
base: some-base
plugs:
  some-plug:
    interface: content
    content: shared-content
    default-provider: snap-content-slot-other
`

const (
	// use sizes that make it easier to spot unexpected dependencies in the
	// total sum.
	someBaseSize             = 1
	otherBaseSize            = 100
	snap1Size                = 1000
	snap2Size                = 10000
	snap3Size                = 100000
	snap4Size                = 1000000
	snapContentSlotSize      = 10000000
	snapOtherContentSlotSize = 100000000
	someOtherBaseSize        = 1000000000
)

type installSizeTestStore struct {
	*fakeStore
}

func (f installSizeTestStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	sizes := map[string]int64{
		"some-base":               someBaseSize,
		"other-base":              otherBaseSize,
		"snap-content-slot":       snapContentSlotSize,
		"snap-content-slot-other": snapOtherContentSlotSize,
		"some-other-base":         someOtherBaseSize,
	}
	for _, sa := range actions {
		if sa.Action != "install" {
			panic(fmt.Sprintf("unexpected action: %s", sa.Action))
		}
		if sa.Channel != "stable" {
			panic(fmt.Sprintf("unexpected channel: %s", sa.Channel))
		}
		if _, ok := sizes[sa.InstanceName]; !ok {
			panic(fmt.Sprintf("unexpected snap: %q", sa.InstanceName))
		}
	}
	sars, _ := mylog.Check3(f.fakeStore.SnapAction(ctx, currentSnaps, actions, assertQuery, user, opts))

	for _, sr := range sars {
		if sz, ok := sizes[sr.Info.InstanceName()]; ok {
			sr.Info.Size = sz
		} else {
			panic(fmt.Sprintf("unexpected snap: %q", sr.Info.InstanceName()))
		}
		if sr.Info.InstanceName() == "snap-content-slot-other" {
			sr.Info.Base = "some-other-base"
		}
	}
	return sars, nil, nil
}

func (s *snapmgrTestSuite) mockCoreSnap(c *C) {
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "core", SnapID: "core-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "os",
	})
	// mock the yaml
	makeInstalledMockCoreSnap(c)
}

func (s *snapmgrTestSuite) setupInstallSizeStore() {
	fakestore := installSizeTestStore{fakeStore: s.fakeStore}
	snapstate.ReplaceStore(s.state, fakestore)
}

func (s *snapmgrTestSuite) TestInstallSizeSimple(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupInstallSizeStore()
	s.mockCoreSnap(c)

	snap1 := snaptest.MockSnap(c, snapYaml1, &snap.SideInfo{
		RealName: "some-snap1",
		Revision: snap.R(1),
	})
	snap1.Size = snap1Size
	snap2 := snaptest.MockSnap(c, snapYaml2, &snap.SideInfo{
		RealName: "some-snap2",
		Revision: snap.R(2),
	})
	snap2.Size = snap2Size

	sz := mylog.Check2(snapstate.InstallSize(st, []snapstate.MinimalInstallInfo{snapstate.InstallSnapInfo{Info: snap1}, snapstate.InstallSnapInfo{Info: snap2}}, 0, nil))

	c.Check(sz, Equals, uint64(snap1Size+snap2Size))
}

func (s *snapmgrTestSuite) TestInstallSizeWithBases(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupInstallSizeStore()

	snap1 := snaptest.MockSnap(c, snapYamlWithBase1, &snap.SideInfo{
		RealName: "some-snap1",
		Revision: snap.R(1),
	})
	snap1.Size = snap1Size
	snap2 := snaptest.MockSnap(c, snapYamlWithBase2, &snap.SideInfo{
		RealName: "some-snap2",
		Revision: snap.R(2),
	})
	snap2.Size = snap2Size
	snap3 := snaptest.MockSnap(c, snapYamlWithBase3, &snap.SideInfo{
		RealName: "some-snap3",
		Revision: snap.R(4),
	})
	snap3.Size = snap3Size
	snap4 := snaptest.MockSnap(c, snapYamlWithBase4, &snap.SideInfo{
		RealName: "some-snap4",
		Revision: snap.R(1),
	})
	snap4.Size = snap4Size

	// base of some-snap4 is already installed
	snapstate.Set(st, "yet-another-base", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "yet-another-base", Revision: snap.R(1), SnapID: "yet-another-base-id"},
		}),
		Current: snap.R(1),
	})

	sz := mylog.Check2(snapstate.InstallSize(st, []snapstate.MinimalInstallInfo{
		snapstate.InstallSnapInfo{Info: snap1},
		snapstate.InstallSnapInfo{Info: snap2},
		snapstate.InstallSnapInfo{Info: snap3},
		snapstate.InstallSnapInfo{Info: snap4},
	}, 0, nil))

	c.Check(sz, Equals, uint64(snap1Size+snap2Size+snap3Size+snap4Size+someBaseSize+otherBaseSize))
}

func (s *snapmgrTestSuite) TestInstallSizeWithContentProviders(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	repo := interfaces.NewRepository()
	ifacerepo.Replace(st, repo)

	s.setupInstallSizeStore()

	snap1 := snaptest.MockSnap(c, snapYamlWithContentPlug1, &snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	})
	snap1.Size = snap1Size

	snap2 := snaptest.MockSnap(c, snapYamlWithContentPlug2, &snap.SideInfo{
		RealName: "some-snap2",
		Revision: snap.R(1),
	})
	snap2.Size = snap2Size

	s.mockCoreSnap(c)

	// both snaps have same content providers and base
	sz := mylog.Check2(snapstate.InstallSize(st, []snapstate.MinimalInstallInfo{
		snapstate.InstallSnapInfo{Info: snap1}, snapstate.InstallSnapInfo{Info: snap2},
	}, 0, nil))

	c.Check(sz, Equals, uint64(snap1Size+snap2Size+someBaseSize+snapContentSlotSize))
}

func (s *snapmgrTestSuite) TestInstallSizeWithNestedDependencies(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	repo := interfaces.NewRepository()
	ifacerepo.Replace(st, repo)

	s.setupInstallSizeStore()
	snap1 := snaptest.MockSnap(c, snapYamlWithContentPlug3, &snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	})
	snap1.Size = snap1Size

	s.mockCoreSnap(c)

	sz := mylog.Check2(snapstate.InstallSize(st, []snapstate.MinimalInstallInfo{snapstate.InstallSnapInfo{Info: snap1}}, 0, nil))

	c.Check(sz, Equals, uint64(snap1Size+someBaseSize+snapOtherContentSlotSize+someOtherBaseSize))
}

func (s *snapmgrTestSuite) TestInstallSizeWithOtherChangeAffectingSameSnaps(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	var currentSnapsCalled int
	restore := snapstate.MockCurrentSnaps(func(st *state.State) ([]*store.CurrentSnap, error) {
		currentSnapsCalled++
		// call original implementation of currentSnaps
		curr := mylog.Check2(snapstate.CurrentSnaps(st))
		if currentSnapsCalled == 1 {
			return curr, err
		}
		// simulate other change that installed some-snap3 and other-base while
		// we release the lock inside InstallSize.
		curr = append(curr, &store.CurrentSnap{InstanceName: "some-snap3"})
		curr = append(curr, &store.CurrentSnap{InstanceName: "other-base"})
		return curr, nil
	})
	defer restore()

	s.setupInstallSizeStore()

	snap1 := snaptest.MockSnap(c, snapYamlWithBase1, &snap.SideInfo{
		RealName: "some-snap1",
		Revision: snap.R(1),
	})
	snap1.Size = snap1Size
	snap3 := snaptest.MockSnap(c, snapYamlWithBase3, &snap.SideInfo{
		RealName: "some-snap3",
		Revision: snap.R(2),
	})
	snap3.Size = snap3Size

	sz := mylog.Check2(snapstate.InstallSize(st, []snapstate.MinimalInstallInfo{
		snapstate.InstallSnapInfo{Info: snap1}, snapstate.InstallSnapInfo{Info: snap3},
	}, 0, nil))

	// snap3 and its base installed by another change, not counted here
	c.Check(sz, Equals, uint64(snap1Size+someBaseSize))

	// validity
	c.Check(currentSnapsCalled, Equals, 2)
}

func (s *snapmgrTestSuite) TestInstallSizeErrorNoDownloadInfo(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	snap1 := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "snap",
		},
	}

	_ := mylog.Check2(snapstate.InstallSize(st, []snapstate.MinimalInstallInfo{snapstate.InstallSnapInfo{Info: snap1}}, 0, nil))
	c.Assert(err, ErrorMatches, `internal error: download info missing.*`)
}

func (s *snapmgrTestSuite) TestInstallSizeWithPrereqNoStore(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	repo := interfaces.NewRepository()
	ifacerepo.Replace(st, repo)

	s.setupInstallSizeStore()

	snap1 := snaptest.MockSnap(c, `name: some-snap
version: 1.0
epoch: 1
base: core
plugs:
  myplug:
    interface: content
    content: mycontent
    content-provider: some-snap2`, &snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	})
	snap1.Size = snap1Size

	snap2 := snaptest.MockSnap(c, `name: some-snap2
version: 1.0
epoch: 1
base: core
slots:
  myslot:
    interface: content
    content: mycontent`, &snap.SideInfo{
		RealName: "some-snap2",
		Revision: snap.R(1),
	})
	snap2.Size = snap2Size

	// core is already installed
	s.mockCoreSnap(c)

	sz := mylog.Check2(snapstate.InstallSize(st, []snapstate.MinimalInstallInfo{
		snapstate.InstallSnapInfo{Info: snap1}, snapstate.InstallSnapInfo{Info: snap2},
	}, 0, nil))

	c.Check(sz, Equals, uint64(snap1Size+snap2Size))

	// no call to the store is made
	c.Assert(s.fakeStore.fakeBackend.ops, HasLen, 0)
}

func (s *snapmgrTestSuite) TestInstallSizeWithPrereqAndCoreNoStore(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	repo := interfaces.NewRepository()
	ifacerepo.Replace(st, repo)

	s.setupInstallSizeStore()

	snap1 := snaptest.MockSnap(c, `name: some-snap
version: 1.0
epoch: 1
base: core
plugs:
  myplug:
    interface: content
    content: mycontent
    content-provider: some-snap2`, &snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	})
	snap1.Size = snap1Size

	snap2 := snaptest.MockSnap(c, `name: some-snap2
version: 1.0
epoch: 1
base: core
slots:
  myslot:
    interface: content
    content: mycontent`, &snap.SideInfo{
		RealName: "some-snap2",
		Revision: snap.R(1),
	})
	snap2.Size = snap2Size

	core := snaptest.MockSnap(c, `name: core
version: 1.0
epoch: 1
type: os`, &snap.SideInfo{
		RealName: "core",
		Revision: snap.R(1),
	})
	core.Size = someBaseSize

	sz := mylog.Check2(snapstate.InstallSize(st, []snapstate.MinimalInstallInfo{
		snapstate.InstallSnapInfo{Info: snap1}, snapstate.InstallSnapInfo{Info: snap2}, snapstate.InstallSnapInfo{Info: core},
	}, 0, nil))

	c.Check(sz, Equals, uint64(snap1Size+snap2Size+someBaseSize))

	// no call to the store is made
	c.Assert(s.fakeStore.fakeBackend.ops, HasLen, 0)
}

func (s *snapmgrTestSuite) TestInstallSizeRemotePrereq(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	repo := interfaces.NewRepository()
	ifacerepo.Replace(st, repo)

	s.setupInstallSizeStore()

	snap1 := snaptest.MockSnap(c, snapYamlWithContentPlug1, &snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	})
	snap1.Size = snap1Size

	s.mockCoreSnap(c)

	sz := mylog.Check2(snapstate.InstallSize(st, []snapstate.MinimalInstallInfo{
		snapstate.InstallSnapInfo{Info: snap1},
	}, 0, nil))

	c.Check(sz, Equals, uint64(snap1Size+snapContentSlotSize+someBaseSize))

	// the prereq's size info is fetched from the store
	op := s.fakeStore.fakeBackend.ops.MustFindOp(c, "storesvc-snap-action:action")
	c.Assert(op.action.InstanceName, Equals, "snap-content-slot")
}

func (s *snapmgrTestSuite) TestSnapHoldsSnapsOnly(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, st, snapAyaml, false)
	mockInstalledSnap(c, st, snapByaml, false)
	mockInstalledSnap(c, st, snapCyaml, false)

	_ := mylog.Check2(snapstate.HoldRefresh(st, snapstate.HoldGeneral, "snap-c", 24*time.Hour, "snap-a", "snap-b"))


	snapHolds := mylog.Check2(snapstate.SnapHolds(st, []string{"snap-a", "snap-b", "snap-c"}))

	c.Check(snapHolds, DeepEquals, map[string][]string{
		"snap-a": {"snap-c"},
		"snap-b": {"snap-c"},
	})
}

func (s *snapmgrTestSuite) TestSnapHoldsSystemOnly(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, st, snapAyaml, false)
	mockInstalledSnap(c, st, snapByaml, false)

	mockLastRefreshed(c, st, "2021-05-09T10:00:00Z", "snap-a", "snap-b")

	now := mylog.Check2(time.Parse(time.RFC3339, "2021-05-10T10:00:00Z"))

	restore := snapstate.MockTimeNow(func() time.Time {
		return now
	})
	defer restore()

	tr := config.NewTransaction(st)
	mylog.Check(tr.Set("core", "refresh.hold", "2021-05-10T11:00:00Z"))

	tr.Commit()

	snapHolds := mylog.Check2(snapstate.SnapHolds(st, []string{"snap-a", "snap-b"}))

	c.Check(snapHolds, DeepEquals, map[string][]string{
		"snap-a": {"system"},
		"snap-b": {"system"},
	})

	tr = config.NewTransaction(st)
	mylog.Check(tr.Set("core", "refresh.hold", "forever"))

	tr.Commit()

	snapHolds = mylog.Check2(snapstate.SnapHolds(st, []string{"snap-a", "snap-b"}))

	c.Check(snapHolds, DeepEquals, map[string][]string{
		"snap-a": {"system"},
		"snap-b": {"system"},
	})
}
