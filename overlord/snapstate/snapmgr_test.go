// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"

	// So it registers Configure.
	_ "github.com/snapcore/snapd/overlord/configstate"
)

func TestSnapManager(t *testing.T) { TestingT(t) }

type snapmgrTestSuite struct {
	state   *state.State
	snapmgr *snapstate.SnapManager

	fakeBackend *fakeSnappyBackend
	fakeStore   *fakeStore

	user *auth.UserState

	reset func()
}

func (s *snapmgrTestSuite) settle() {
	// FIXME: use the real settle here
	for i := 0; i < 50; i++ {
		s.snapmgr.Ensure()
		s.snapmgr.Wait()
	}
}

var _ = Suite(&snapmgrTestSuite{})

func (s *snapmgrTestSuite) SetUpTest(c *C) {
	s.fakeBackend = &fakeSnappyBackend{}
	s.state = state.New(nil)
	s.fakeStore = &fakeStore{
		fakeCurrentProgress: 75,
		fakeTotalProgress:   100,
		fakeBackend:         s.fakeBackend,
		state:               s.state,
	}

	var err error
	s.snapmgr, err = snapstate.Manager(s.state)
	c.Assert(err, IsNil)
	s.snapmgr.AddForeignTaskHandlers(s.fakeBackend)

	snapstate.SetSnapManagerBackend(s.snapmgr, s.fakeBackend)

	restore1 := snapstate.MockReadInfo(s.fakeBackend.ReadInfo)
	restore2 := snapstate.MockOpenSnapFile(s.fakeBackend.OpenSnapFile)

	s.reset = func() {
		restore2()
		restore1()
	}

	s.state.Lock()
	snapstate.ReplaceStore(s.state, s.fakeStore)
	s.user, err = auth.NewUser(s.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	c.Assert(err, IsNil)
	s.state.Unlock()

	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
}

func (s *snapmgrTestSuite) TearDownTest(c *C) {
	snapstate.ValidateRefreshes = nil
	snapstate.AutoAliases = nil
	snapstate.CanAutoRefresh = nil
	s.reset()
}

func (s *snapmgrTestSuite) TestStore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	sto := &store.Store{}
	snapstate.ReplaceStore(s.state, sto)
	store1 := snapstate.Store(s.state)
	c.Check(store1, Equals, sto)

	// cached
	store2 := snapstate.Store(s.state)
	c.Check(store2, Equals, sto)
}

const (
	unlinkBefore = 1 << iota
	cleanupAfter
	maybeCore
)

func taskKinds(tasks []*state.Task) []string {
	kinds := make([]string, len(tasks))
	for i, task := range tasks {
		kinds[i] = task.Kind()
	}
	return kinds
}

func verifyInstallUpdateTasks(c *C, opts, discards int, ts *state.TaskSet, st *state.State) {
	kinds := taskKinds(ts.Tasks())

	expected := []string{
		"download-snap",
		"validate-snap",
		"mount-snap",
	}
	if opts&unlinkBefore != 0 {
		expected = append(expected,
			"stop-snap-services",
			"remove-aliases",
			"unlink-current-snap",
		)
	}
	expected = append(expected,
		"copy-snap-data",
		"setup-profiles",
		"link-snap",
	)
	if opts&maybeCore != 0 {
		expected = append(expected, "setup-profiles")
	}
	expected = append(expected,
		"set-auto-aliases",
		"setup-aliases",
		"start-snap-services",
	)
	for i := 0; i < discards; i++ {
		expected = append(expected,
			"clear-snap",
			"discard-snap",
		)
	}
	if opts&cleanupAfter != 0 {
		expected = append(expected,
			"cleanup",
		)
	}
	expected = append(expected,
		"run-hook",
	)

	c.Assert(kinds, DeepEquals, expected)
}

func verifyRemoveTasks(c *C, ts *state.TaskSet) {
	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
		"stop-snap-services",
		"remove-aliases",
		"unlink-snap",
		"remove-profiles",
		"clear-snap",
		"discard-snap",
		"clear-aliases",
		"discard-conns",
	})
}

func (s *snapmgrTestSuite) TestLastIndexFindsLast(c *C) {
	snapst := &snapstate.SnapState{Sequence: []*snap.SideInfo{
		{Revision: snap.R(7)},
		{Revision: snap.R(11)},
		{Revision: snap.R(11)},
	}}
	c.Check(snapst.LastIndex(snap.R(11)), Equals, 2)
}

func (s *snapmgrTestSuite) TestInstallDevModeConfinementFiltering(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// if a snap is devmode, you can't install it without --devmode
	_, err := snapstate.Install(s.state, "some-snap", "channel-for-devmode", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `.* requires devmode or confinement override`)

	// if a snap is devmode, you *can* install it with --devmode
	_, err = snapstate.Install(s.state, "some-snap", "channel-for-devmode", snap.R(0), s.user.ID, snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)

	// if a snap is *not* devmode, you can still install it with --devmode
	_, err = snapstate.Install(s.state, "some-snap", "channel-for-strict", snap.R(0), s.user.ID, snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestInstallClassicConfinementFiltering(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// if a snap is classic, you can't install it without --classic
	_, err := snapstate.Install(s.state, "some-snap", "channel-for-classic", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `.* requires classic or confinement override`)

	// if a snap is classic, you *can* install it with --classic
	_, err = snapstate.Install(s.state, "some-snap", "channel-for-classic", snap.R(0), s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)

	// if a snap is *not* classic, you can still install it with --classic
	_, err = snapstate.Install(s.state, "some-snap", "channel-for-strict", snap.R(0), s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestInstallTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyInstallUpdateTasks(c, 0, 0, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
}

func (s *snapmgrTestSuite) TestCoreInstallTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(s.state, "some-core", "some-channel", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyInstallUpdateTasks(c, maybeCore, 0, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	var phase2 *state.Task
	for _, t := range ts.Tasks() {
		if t.Kind() == "setup-profiles" {
			phase2 = t
		}
	}
	c.Assert(phase2, NotNil)
	var flag bool
	err = phase2.Get("core-phase-2", &flag)
	c.Assert(err, IsNil)
	c.Check(flag, Equals, true)
}

func (s *snapmgrTestSuite) testRevertTasks(flags snapstate.Flags, c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(7)},
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current:  snap.R(11),
		SnapType: "app",
	})

	ts, err := snapstate.Revert(s.state, "some-snap", flags)
	c.Assert(err, IsNil)

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
		"prepare-snap",
		"stop-snap-services",
		"remove-aliases",
		"unlink-current-snap",
		"setup-profiles",
		"link-snap",
		"set-auto-aliases",
		"setup-aliases",
		"start-snap-services",
		"run-hook",
	})

	chg := s.state.NewChange("revert", "revert snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Flags, Equals, flags)
}

func (s *snapmgrTestSuite) TestRevertTasks(c *C) {
	s.testRevertTasks(snapstate.Flags{}, c)
}

func (s *snapmgrTestSuite) TestRevertTasksDevMode(c *C) {
	s.testRevertTasks(snapstate.Flags{DevMode: true}, c)
}

func (s *snapmgrTestSuite) TestRevertTasksJailMode(c *C) {
	s.testRevertTasks(snapstate.Flags{JailMode: true}, c)
}

func (s *snapmgrTestSuite) TestRevertTasksClassic(c *C) {
	s.testRevertTasks(snapstate.Flags{Classic: true}, c)
}

func (s *snapmgrTestSuite) TestUpdateCreatesGCTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current:  snap.R(4),
		SnapType: "app",
	})

	ts, err := snapstate.Update(s.state, "some-snap", "", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyInstallUpdateTasks(c, unlinkBefore|cleanupAfter, 2, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
}

func (s *snapmgrTestSuite) TestUpdateCreatesDiscardAfterCurrentTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	ts, err := snapstate.Update(s.state, "some-snap", "", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyInstallUpdateTasks(c, unlinkBefore|cleanupAfter, 3, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
}

func (s *snapmgrTestSuite) TestUpdateMany(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	updates, tts, err := snapstate.UpdateMany(s.state, nil, 0)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 1)
	c.Check(updates, DeepEquals, []string{"some-snap"})

	ts := tts[0]
	verifyInstallUpdateTasks(c, unlinkBefore|cleanupAfter, 3, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
}

func (s *snapmgrTestSuite) TestUpdateManyDevModeConfinementFiltering(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "channel-for-devmode",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	// updated snap is devmode, updatemany doesn't update it
	_, tts, _ := snapstate.UpdateMany(s.state, []string{"some-snap"}, s.user.ID)
	// FIXME: UpdateMany will not error out in this case (daemon catches this case, with a weird error)
	c.Assert(tts, HasLen, 0)
}

func (s *snapmgrTestSuite) TestUpdateManyClassicConfinementFiltering(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "channel-for-classic",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	// if a snap installed without --classic gets a classic update it isn't installed
	_, tts, _ := snapstate.UpdateMany(s.state, []string{"some-snap"}, s.user.ID)
	// FIXME: UpdateMany will not error out in this case (daemon catches this case, with a weird error)
	c.Assert(tts, HasLen, 0)
}

func (s *snapmgrTestSuite) TestUpdateManyClassic(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "channel-for-classic",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
		Flags:    snapstate.Flags{Classic: true},
	})

	// snap installed with classic: refresh gets classic
	_, tts, err := snapstate.UpdateMany(s.state, []string{"some-snap"}, s.user.ID)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 1)
}

func (s *snapmgrTestSuite) TestUpdateManyDevMode(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Flags:  snapstate.Flags{DevMode: true},
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	updates, _, err := snapstate.UpdateMany(s.state, []string{"some-snap"}, 0)
	c.Assert(err, IsNil)
	c.Check(updates, HasLen, 1)
}

func (s *snapmgrTestSuite) TestUpdateAllDevMode(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Flags:  snapstate.Flags{DevMode: true},
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	updates, _, err := snapstate.UpdateMany(s.state, nil, 0)
	c.Assert(err, IsNil)
	c.Check(updates, HasLen, 0)
}

func (s *snapmgrTestSuite) TestUpdateManyValidateRefreshes(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	validateCalled := false
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, userID int) ([]*snap.Info, error) {
		validateCalled = true
		c.Check(refreshes, HasLen, 1)
		c.Check(refreshes[0].SnapID, Equals, "some-snap-id")
		c.Check(refreshes[0].Revision, Equals, snap.R(11))
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes

	updates, tts, err := snapstate.UpdateMany(s.state, nil, 0)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 1)
	c.Check(updates, DeepEquals, []string{"some-snap"})
	verifyInstallUpdateTasks(c, unlinkBefore|cleanupAfter, 0, tts[0], s.state)

	c.Check(validateCalled, Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateManyValidateRefreshesUnhappy(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current: snap.R(1),
	})

	validateErr := errors.New("refresh control error")
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, userID int) ([]*snap.Info, error) {
		c.Check(refreshes, HasLen, 1)
		c.Check(refreshes[0].SnapID, Equals, "some-snap-id")
		c.Check(refreshes[0].Revision, Equals, snap.R(11))
		return nil, validateErr
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes

	// refresh all => no error
	updates, tts, err := snapstate.UpdateMany(s.state, nil, 0)
	c.Assert(err, IsNil)
	c.Check(tts, HasLen, 0)
	c.Check(updates, HasLen, 0)

	// refresh some-snap => report error
	updates, tts, err = snapstate.UpdateMany(s.state, []string{"some-snap"}, 0)
	c.Assert(err, Equals, validateErr)
	c.Check(tts, HasLen, 0)
	c.Check(updates, HasLen, 0)

}

func (s *snapmgrTestSuite) TestRevertCreatesNoGCTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(1)},
			{RealName: "some-snap", Revision: snap.R(2)},
			{RealName: "some-snap", Revision: snap.R(3)},
			{RealName: "some-snap", Revision: snap.R(4)},
		},
		Current: snap.R(2),
	})

	ts, err := snapstate.RevertToRevision(s.state, "some-snap", snap.R(4), snapstate.Flags{})
	c.Assert(err, IsNil)

	// ensure that we do not run any form of garbage-collection
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
		"prepare-snap",
		"stop-snap-services",
		"remove-aliases",
		"unlink-current-snap",
		"setup-profiles",
		"link-snap",
		"set-auto-aliases",
		"setup-aliases",
		"start-snap-services",
		"run-hook",
	})
}

func (s *snapmgrTestSuite) TestEnableTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  false,
	})

	ts, err := snapstate.Enable(s.state, "some-snap")
	c.Assert(err, IsNil)

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
		"prepare-snap",
		"setup-profiles",
		"link-snap",
		"setup-aliases",
		"start-snap-services",
	})
}

func (s *snapmgrTestSuite) TestDisableTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	ts, err := snapstate.Disable(s.state, "some-snap")
	c.Assert(err, IsNil)

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
		"stop-snap-services",
		"remove-aliases",
		"unlink-snap",
		"remove-profiles",
	})
}

func (s *snapmgrTestSuite) TestEnableConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  false,
	})

	ts, err := snapstate.Enable(s.state, "some-snap")
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("enable", "...").AddAll(ts)

	_, err = snapstate.Enable(s.state, "some-snap")
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
}

func (s *snapmgrTestSuite) TestDisableConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	ts, err := snapstate.Disable(s.state, "some-snap")
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("install", "...").AddAll(ts)

	_, err = snapstate.Disable(s.state, "some-snap")
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
}

func (s *snapmgrTestSuite) TestDoInstallChannelDefault(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(s.state, "some-snap", "", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	c.Check(snapsup.Channel, Equals, "stable")
}

func (s *snapmgrTestSuite) TestInstallRevision(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(s.state, "some-snap", "", snap.R(7), 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	c.Check(snapsup.Revision(), Equals, snap.R(7))
}

func (s *snapmgrTestSuite) TestInstallConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("install", "...").AddAll(ts)

	_, err = snapstate.Install(s.state, "some-snap", "some-channel", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
}

func (s *snapmgrTestSuite) TestInstallAliasConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("aliases", map[string]map[string]string{
		"otherfoosnap": {
			"foo.bar": "enabled",
		},
	})

	_, err := snapstate.Install(s.state, "foo", "some-channel", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "foo" command namespace conflicts with enabled alias "foo\.bar" for "otherfoosnap"`)
}

// A sneakyStore changes the state when called
type sneakyStore struct {
	*fakeStore
	state *state.State
}

func (s sneakyStore) SnapInfo(spec store.SnapSpec, user *auth.UserState) (*snap.Info, error) {
	s.state.Lock()
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
	})
	s.state.Unlock()
	return s.fakeStore.SnapInfo(spec, user)
}

func (s *snapmgrTestSuite) TestInstallStateConflict(c *C) {

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, sneakyStore{fakeStore: s.fakeStore, state: s.state})

	_, err := snapstate.Install(s.state, "some-snap", "some-channel", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "some-snap" state changed during install preparations`)
}

func (s *snapmgrTestSuite) TestInstallPathConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("install", "...").AddAll(ts)

	mockSnap := makeTestSnap(c, "name: some-snap\nversion: 1.0")
	_, err = snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap"}, mockSnap, "", snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
}

func (s *snapmgrTestSuite) TestInstallPathMissingName(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	mockSnap := makeTestSnap(c, "name: some-snap\nversion: 1.0")
	_, err := snapstate.InstallPath(s.state, &snap.SideInfo{}, mockSnap, "", snapstate.Flags{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`internal error: snap name to install %q not provided`, mockSnap))
}

func (s *snapmgrTestSuite) TestInstallPathSnapIDRevisionUnset(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	mockSnap := makeTestSnap(c, "name: some-snap\nversion: 1.0")
	_, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap", SnapID: "snapididid"}, mockSnap, "", snapstate.Flags{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`internal error: snap id set to install %q but revision is unset`, mockSnap))
}

func (s *snapmgrTestSuite) TestUpdateTasksPropagatesErrors(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "fakestore-please-error-on-refresh", Revision: snap.R(7)}},
		Current:  snap.R(7),
	})

	_, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `cannot get refresh information for snap "some-snap": failing as requested`)
}

func (s *snapmgrTestSuite) TestUpdateTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	validateCalled := false
	happyValidateRefreshes := func(st *state.State, refreshes []*snap.Info, userID int) ([]*snap.Info, error) {
		validateCalled = true
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = happyValidateRefreshes

	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	verifyInstallUpdateTasks(c, unlinkBefore|cleanupAfter, 0, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))

	c.Check(validateCalled, Equals, true)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	c.Check(snapsup.Channel, Equals, "some-channel")
}

func (s *snapmgrTestSuite) TestUpdateDevModeConfinementFiltering(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "channel-for-devmode",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	// updated snap is devmode, refresh without --devmode, do nothing
	// TODO: better error message here
	_, err := snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `.* requires devmode or confinement override`)

	// updated snap is devmode, refresh with --devmode
	_, err = snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateClassicConfinementFiltering(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "channel-for-classic",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	// updated snap is classic, refresh without --classic, do nothing
	// TODO: better error message here
	_, err := snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `.* requires classic or confinement override`)

	// updated snap is classic, refresh with --classic
	ts, err := snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("refresh", "refresh snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// verify snap is in classic
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Classic, Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateClassicFromClassic(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "channel-for-classic",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
		Flags:    snapstate.Flags{Classic: true},
	})

	// snap installed with --classic, update needs classic, refresh with --classic works
	ts, err := snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)
	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Classic, Equals, true)

	// devmode overrides the snapsetup classic flag
	ts, err = snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)
	snapsup, err = snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Classic, Equals, false)

	// jailmode overrides it too (you need to provide both)
	ts, err = snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{JailMode: true})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)
	snapsup, err = snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Classic, Equals, false)

	// jailmode and classic together gets you both
	ts, err = snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{JailMode: true, Classic: true})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)
	snapsup, err = snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Classic, Equals, true)

	// snap installed with --classic, update needs classic, refresh without --classic works
	ts, err = snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)
	snapsup, err = snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Classic, Equals, true)

	chg := s.state.NewChange("refresh", "refresh snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// verify snap is in classic
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Classic, Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateStrictFromClassic(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "channel",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
		Flags:    snapstate.Flags{Classic: true},
	})

	// snap installed with --classic, update does not need classic, refresh works without --classic
	_, err := snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// snap installed with --classic, update does not need classic, refresh works with --classic
	_, err = snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateChannelFallback(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	ts, err := snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	c.Check(snapsup.Channel, Equals, "edge")
}

func (s *snapmgrTestSuite) TestUpdateConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("refresh", "...").AddAll(ts)

	_, err = snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
}

func (s *snapmgrTestSuite) TestRemoveTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(11)},
		},
		Current: snap.R(11),
	})

	ts, err := snapstate.Remove(s.state, "foo", snap.R(0))
	c.Assert(err, IsNil)

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	verifyRemoveTasks(c, ts)
}

func (s *snapmgrTestSuite) TestRemoveConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", Revision: snap.R(11)}},
		Current:  snap.R(11),
	})

	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(0))
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("remove", "...").AddAll(ts)

	_, err = snapstate.Remove(s.state, "some-snap", snap.R(0))
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
}

func (s *snapmgrTestSuite) TestInstallRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", snap.R(42), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "some-snap",
	}})
	expected := fakeOps{
		{
			op:    "storesvc-snap",
			name:  "some-snap",
			revno: snap.R(42),
		},
		{
			op:   "storesvc-download",
			name: "some-snap",
		},
		{
			op:    "validate-snap:Doing",
			name:  "some-snap",
			revno: snap.R(42),
		},
		{
			op:  "current",
			old: "<no-current>",
		},
		{
			op:   "open-snap-file",
			name: "/var/lib/snapd/snaps/some-snap_42.snap",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				Channel:  "some-channel",
				SnapID:   "snapIDsnapidsnapidsnapidsnapidsn",
				Revision: snap.R(42),
			},
		},
		{
			op:    "setup-snap",
			name:  "/var/lib/snapd/snaps/some-snap_42.snap",
			revno: snap.R(42),
		},
		{
			op:   "copy-data",
			name: "/snap/some-snap/42",
			old:  "<no-old>",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(42),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				Channel:  "some-channel",
				SnapID:   "snapIDsnapidsnapidsnapidsnapidsn",
				Revision: snap.R(42),
			},
		},
		{
			op:   "link-snap",
			name: "/snap/some-snap/42",
		},
		{
			op: "update-aliases",
		},
		{
			op:   "start-snap-services",
			name: "/snap/some-snap/42",
		},
		{
			op:    "cleanup-trash",
			name:  "some-snap",
			revno: snap.R(42),
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// check progress
	ta := ts.Tasks()
	task := ta[0]
	_, cur, total := task.Progress()
	c.Assert(cur, Equals, s.fakeStore.fakeCurrentProgress)
	c.Assert(total, Equals, s.fakeStore.fakeTotalProgress)
	c.Check(task.Summary(), Equals, `Download snap "some-snap" (42) from channel "some-channel"`)

	// check link/start snap summary
	linkTask := ta[len(ta)-5]
	c.Check(linkTask.Summary(), Equals, `Make snap "some-snap" (42) available to the system`)
	startTask := ta[len(ta)-2]
	c.Check(startTask.Summary(), Equals, `Start snap "some-snap" (42) services`)

	// verify snap-setup in the task state
	var snapsup snapstate.SnapSetup
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		Channel:  "some-channel",
		UserID:   s.user.ID,
		SnapPath: "/var/lib/snapd/snaps/some-snap_42.snap",
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
		},
		SideInfo: snapsup.SideInfo,
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(42),
		Channel:  "some-channel",
		SnapID:   "snapIDsnapidsnapidsnapidsnapidsn",
	})

	// verify snaps in the system state
	var snaps map[string]*snapstate.SnapState
	err = s.state.Get("snaps", &snaps)
	c.Assert(err, IsNil)

	snapst := snaps["some-snap"]
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Channel, Equals, "some-channel")
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Channel:  "some-channel",
		SnapID:   "snapIDsnapidsnapidsnapidsnapidsn",
		Revision: snap.R(42),
	})
	c.Assert(snapst.Required, Equals, false)
}

func (s *snapmgrTestSuite) TestUpdateRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
		SnapID:   "some-snap-id",
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	expected := fakeOps{
		{
			op: "storesvc-list-refresh",
			cand: store.RefreshCandidate{
				Channel:  "some-channel",
				SnapID:   "some-snap-id",
				Revision: snap.R(7),
				Epoch:    "",
			},
			revno: snap.R(11),
		},
		{
			op:   "storesvc-download",
			name: "some-snap",
		},
		{
			op:    "validate-snap:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:  "current",
			old: "/snap/some-snap/7",
		},
		{
			op:   "open-snap-file",
			name: "/var/lib/snapd/snaps/some-snap_11.snap",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  "/var/lib/snapd/snaps/some-snap_11.snap",
			revno: snap.R(11),
		},
		{
			op:   "stop-snap-services",
			name: "/snap/some-snap/7",
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
		{
			op:   "copy-data",
			name: "/snap/some-snap/11",
			old:  "/snap/some-snap/7",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:   "link-snap",
			name: "/snap/some-snap/11",
		},
		{
			op: "update-aliases",
		},
		{
			op:   "start-snap-services",
			name: "/snap/some-snap/11",
		},
		{
			op:    "cleanup-trash",
			name:  "some-snap",
			revno: snap.R(11),
		},
	}

	// ensure all our tasks ran
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "some-snap",
	}})
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// check progress
	task := ts.Tasks()[0]
	_, cur, total := task.Progress()
	c.Assert(cur, Equals, s.fakeStore.fakeCurrentProgress)
	c.Assert(total, Equals, s.fakeStore.fakeTotalProgress)

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		Channel: "some-channel",
		UserID:  s.user.ID,

		SnapPath: "/var/lib/snapd/snaps/some-snap_11.snap",
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
		},
		SideInfo: snapsup.SideInfo,
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(11),
		Channel:  "some-channel",
		SnapID:   "some-snap-id",
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "",
		Revision: snap.R(7),
	})
	c.Assert(snapst.Sequence[1], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Channel:  "some-channel",
		SnapID:   "some-snap-id",
		Revision: snap.R(11),
	})
}

func (s *snapmgrTestSuite) TestUpdateUndoRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.fakeBackend.linkSnapFailTrigger = "/snap/some-snap/11"

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	expected := fakeOps{
		{
			op: "storesvc-list-refresh",
			cand: store.RefreshCandidate{
				Channel:  "some-channel",
				SnapID:   "some-snap-id",
				Revision: snap.R(7),
				Epoch:    "",
			},
			revno: snap.R(11),
		},
		{
			op:   "storesvc-download",
			name: "some-snap",
		},
		{
			op:    "validate-snap:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:  "current",
			old: "/snap/some-snap/7",
		},
		{
			op:   "open-snap-file",
			name: "/var/lib/snapd/snaps/some-snap_11.snap",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  "/var/lib/snapd/snaps/some-snap_11.snap",
			revno: snap.R(11),
		},
		{
			op:   "stop-snap-services",
			name: "/snap/some-snap/7",
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
		{
			op:   "copy-data",
			name: "/snap/some-snap/11",
			old:  "/snap/some-snap/7",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:   "link-snap.failed",
			name: "/snap/some-snap/11",
		},
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/11",
		},
		{
			op:    "setup-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:   "undo-copy-snap-data",
			name: "/snap/some-snap/11",
			old:  "/snap/some-snap/7",
		},
		{
			op:   "link-snap",
			name: "/snap/some-snap/7",
		},
		{
			op: "update-aliases",
		},
		{
			op:   "start-snap-services",
			name: "/snap/some-snap/7",
		},
		{
			op:    "undo-setup-snap",
			name:  "/snap/some-snap/11",
			stype: "app",
		},
	}

	// ensure all our tasks ran
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "some-snap",
	}})
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 1)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "",
		Revision: snap.R(7),
	})
}

func (s *snapmgrTestSuite) TestUpdateTotalUndoRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Channel:  "stable",
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	tasks := ts.Tasks()
	last := tasks[len(tasks)-1]
	// sanity
	c.Assert(last.Lanes(), HasLen, 1)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(last)
	terr.JoinLane(last.Lanes()[0])
	chg.AddTask(terr)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	expected := fakeOps{
		{
			op: "storesvc-list-refresh",
			cand: store.RefreshCandidate{
				Channel:  "some-channel",
				SnapID:   "some-snap-id",
				Revision: snap.R(7),
				Epoch:    "",
			},
			revno: snap.R(11),
		},
		{
			op:   "storesvc-download",
			name: "some-snap",
		},
		{
			op:    "validate-snap:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:  "current",
			old: "/snap/some-snap/7",
		},
		{
			op:   "open-snap-file",
			name: "/var/lib/snapd/snaps/some-snap_11.snap",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  "/var/lib/snapd/snaps/some-snap_11.snap",
			revno: snap.R(11),
		},
		{
			op:   "stop-snap-services",
			name: "/snap/some-snap/7",
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
		{
			op:   "copy-data",
			name: "/snap/some-snap/11",
			old:  "/snap/some-snap/7",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:   "link-snap",
			name: "/snap/some-snap/11",
		},
		{
			op: "update-aliases",
		},

		{
			op:   "start-snap-services",
			name: "/snap/some-snap/11",
		},
		// undoing everything from here down...
		{
			op:   "stop-snap-services",
			name: "/snap/some-snap/11",
		},
		{
			op: "matching-aliases",
		},
		{
			op: "update-aliases",
		},
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/11",
		},
		{
			op:    "setup-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:   "undo-copy-snap-data",
			name: "/snap/some-snap/11",
			old:  "/snap/some-snap/7",
		},
		{
			op:   "link-snap",
			name: "/snap/some-snap/7",
		},
		{
			op: "update-aliases",
		},
		{
			op:   "start-snap-services",
			name: "/snap/some-snap/7",
		},
		{
			op:    "undo-setup-snap",
			name:  "/snap/some-snap/11",
			stype: "app",
		},
	}

	// ensure all our tasks ran
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "some-snap",
	}})
	// friendlier failure first
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Channel, Equals, "stable")
	c.Assert(snapst.Sequence, HasLen, 1)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "",
		Revision: snap.R(7),
	})
}

func (s *snapmgrTestSuite) TestUpdateSameRevision(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Channel:  "channel-for-7",
		Current:  si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", "channel-for-7", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "some-snap" has no updates available`)
}

func (s *snapmgrTestSuite) TestUpdateSameRevisionSwitchesChannel(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Channel:  "other-chanenl",
		Current:  si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", "channel-for-7", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(ts.Tasks(), HasLen, 1)
	c.Check(ts.Tasks()[0].Kind(), Equals, "switch-snap-channel")
}

func (s *snapmgrTestSuite) TestUpdateSameRevisionSwitchChannelRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "other-channel",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Channel:  "other-channel",
		Current:  si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", "channel-for-7", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := s.state.NewChange("refresh", "refresh a snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	expected := fakeOps{
		// we just expect the "storesvc-list-refresh" op, we
		// don't have a fakeOp for switchChannel because it has
		// not a backend method, it just manipulates the state
		{
			op: "storesvc-list-refresh",
			cand: store.RefreshCandidate{
				Channel:  "channel-for-7",
				SnapID:   "some-snap-id",
				Revision: snap.R(7),
				Epoch:    "",
			},
		},
	}

	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	task := ts.Tasks()[0]
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		Channel:  "channel-for-7",
		SideInfo: snapsup.SideInfo,
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
		Channel:  "channel-for-7",
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 1)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "channel-for-7",
		Revision: snap.R(7),
	})
}

func (s *snapmgrTestSuite) TestUpdateValidateRefreshesSaysNo(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
	})

	validateErr := errors.New("refresh control error")
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, userID int) ([]*snap.Info, error) {
		c.Check(refreshes, HasLen, 1)
		c.Check(refreshes[0].SnapID, Equals, "some-snap-id")
		c.Check(refreshes[0].Revision, Equals, snap.R(11))
		return nil, validateErr
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes

	_, err := snapstate.Update(s.state, "some-snap", "stable", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, Equals, validateErr)
}

func (s *snapmgrTestSuite) TestUpdateValidateRefreshesSaysNoButIgnoreValidationIsSet(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	validateErr := errors.New("refresh control error")
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, userID int) ([]*snap.Info, error) {
		return nil, validateErr
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes

	flags := snapstate.Flags{JailMode: true, IgnoreValidation: true}
	ts, err := snapstate.Update(s.state, "some-snap", "stable", snap.R(0), s.user.ID, flags)
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags, DeepEquals, flags.ForSnapSetup())
}

func (s *snapmgrTestSuite) TestSingleUpdateBlockedRevision(c *C) {
	// single updates should *not* set the block list
	si7 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	si11 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(11),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si7, &si11},
		Current:  si7.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	c.Assert(s.fakeBackend.ops, HasLen, 1)
	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op:    "storesvc-list-refresh",
		revno: snap.R(11),
		cand: store.RefreshCandidate{
			SnapID:   "some-snap-id",
			Revision: snap.R(7),
			Epoch:    "",
			Channel:  "some-channel",
		},
	})

}

func (s *snapmgrTestSuite) TestMultiUpdateBlockedRevision(c *C) {
	// multi-updates should *not* set the block list
	si7 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	si11 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(11),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si7, &si11},
		Current:  si7.Revision,
	})

	updates, _, err := snapstate.UpdateMany(s.state, []string{"some-snap"}, s.user.ID)
	c.Assert(err, IsNil)
	c.Check(updates, DeepEquals, []string{"some-snap"})

	c.Assert(s.fakeBackend.ops, HasLen, 1)
	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op:    "storesvc-list-refresh",
		revno: snap.R(11),
		cand: store.RefreshCandidate{
			SnapID:   "some-snap-id",
			Revision: snap.R(7),
		},
	})

}

func (s *snapmgrTestSuite) TestAllUpdateBlockedRevision(c *C) {
	//  update-all *should* set the block list
	si7 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	si11 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(11),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si7, &si11},
		Current:  si7.Revision,
	})

	updates, _, err := snapstate.UpdateMany(s.state, nil, s.user.ID)
	c.Check(err, IsNil)
	c.Check(updates, HasLen, 0)

	c.Assert(s.fakeBackend.ops, HasLen, 1)
	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-list-refresh",
		cand: store.RefreshCandidate{
			SnapID:   "some-snap-id",
			Revision: snap.R(7),
			Block:    []snap.Revision{snap.R(11)},
		},
	})

}

var orthogonalAutoAliasesScenarios = []struct {
	aliasesBefore map[string][]string
	names         []string
	retire        []string
	update        bool
	new           bool
}{
	{nil, nil, nil, true, true},
	{nil, []string{"some-snap"}, nil, true, false},
	{nil, []string{"other-snap"}, nil, false, true},
	{map[string][]string{"some-snap": {"aliasA", "aliasC"}}, []string{"some-snap"}, nil, true, false},
	{map[string][]string{"other-snap": {"aliasB", "aliasC"}}, []string{"other-snap"}, []string{"other-snap"}, false, false},
	{map[string][]string{"other-snap": {"aliasB", "aliasC"}}, nil, []string{"other-snap"}, true, false},
	{map[string][]string{"other-snap": {"aliasB", "aliasC"}}, []string{"some-snap"}, nil, true, false},
	{map[string][]string{"other-snap": {"aliasC"}}, []string{"other-snap"}, []string{"other-snap"}, false, true},
	{map[string][]string{"other-snap": {"aliasC"}}, nil, []string{"other-snap"}, true, true},
	{map[string][]string{"other-snap": {"aliasC"}}, []string{"some-snap"}, nil, true, false},
	{map[string][]string{"some-snap": {"aliasB"}, "other-snap": {"aliasA"}}, []string{"some-snap"}, []string{"other-snap"}, true, false},
	{map[string][]string{"some-snap": {"aliasB"}, "other-snap": {"aliasA"}}, nil, []string{"other-snap", "some-snap"}, true, true},
	{map[string][]string{"some-snap": {"aliasB"}, "other-snap": {"aliasA"}}, []string{"other-snap"}, []string{"other-snap", "some-snap"}, false, true},
	{map[string][]string{"some-snap": {"aliasB"}}, nil, []string{"some-snap"}, true, true},
	{map[string][]string{"some-snap": {"aliasB"}}, []string{"other-snap"}, []string{"some-snap"}, false, true},
	{map[string][]string{"some-snap": {"aliasB"}}, []string{"some-snap"}, nil, true, false},
	{map[string][]string{"other-snap": {"aliasA"}}, nil, []string{"other-snap"}, true, true},
	{map[string][]string{"other-snap": {"aliasA"}}, []string{"other-snap"}, []string{"other-snap"}, false, true},
	{map[string][]string{"other-snap": {"aliasA"}}, []string{"some-snap"}, []string{"other-snap"}, true, false},
}

func (s *snapmgrTestSuite) TestUpdateManyAutoAliasesScenarios(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "other-snap", SnapID: "other-snap-id", Revision: snap.R(2)},
		},
		Current:  snap.R(2),
		SnapType: "app",
	})

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		switch info.Name() {
		case "some-snap":
			return map[string]string{"aliasA": "cmdA"}, nil
		case "other-snap":
			return map[string]string{"aliasB": "cmdB"}, nil
		}
		return nil, nil
	}

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current:  snap.R(4),
		SnapType: "app",
	})

	expectedAuto := func(aliases []string) map[string]string {
		res := make(map[string]string, len(aliases))
		for _, alias := range aliases {
			res[alias] = "auto"
		}
		return res
	}

	for _, scenario := range orthogonalAutoAliasesScenarios {
		aliases := make(map[string]map[string]string)
		for snapName, autoAliases := range scenario.aliasesBefore {
			statuses := make(map[string]string)
			for _, alias := range autoAliases {
				statuses[alias] = "auto"
			}
			aliases[snapName] = statuses
		}
		s.state.Set("aliases", aliases)

		updates, tts, err := snapstate.UpdateMany(s.state, scenario.names, s.user.ID)
		c.Check(err, IsNil)

		new, retiring, err := snapstate.AutoAliasesDelta(s.state, []string{"some-snap", "other-snap"})
		c.Assert(err, IsNil)

		j := 0
		expectedUpdatesSet := make(map[string]bool)
		var expectedRetiring map[string]map[string]string
		var retireTs *state.TaskSet
		if len(scenario.retire) != 0 {
			retireTs = tts[0]
			j++
			taskAliases := make(map[string]map[string]string)
			for _, aliasTask := range retireTs.Tasks() {
				c.Check(aliasTask.Kind(), Equals, "alias")
				var aliases map[string]string
				err := aliasTask.Get("aliases", &aliases)
				c.Assert(err, IsNil)
				snapsup, err := snapstate.TaskSnapSetup(aliasTask)
				c.Assert(err, IsNil)
				taskAliases[snapsup.Name()] = aliases
			}
			expectedRetiring = make(map[string]map[string]string)
			for _, snapName := range scenario.retire {
				expectedRetiring[snapName] = expectedAuto(retiring[snapName])
				if snapName == "other-snap" && !scenario.new && !scenario.update {
					expectedUpdatesSet["other-snap"] = true
				}
			}
			c.Check(taskAliases, DeepEquals, expectedRetiring)
		}
		if scenario.update {
			updateTs := tts[j]
			j++
			expectedUpdatesSet["some-snap"] = true
			first := updateTs.Tasks()[0]
			c.Check(first.Kind(), Equals, "download-snap")
			wait := false
			if expectedRetiring["other-snap"]["aliasA"] != "" {
				wait = true
			} else if expectedRetiring["some-snap"] != nil {
				wait = true
			}
			if wait {
				c.Check(first.WaitTasks(), DeepEquals, retireTs.Tasks())
			} else {
				c.Check(first.WaitTasks(), HasLen, 0)
			}
		}
		if scenario.new {
			newTs := tts[j]
			j++
			expectedUpdatesSet["other-snap"] = true
			tasks := newTs.Tasks()
			c.Check(tasks, HasLen, 1)
			aliasTask := tasks[0]
			c.Check(aliasTask.Kind(), Equals, "alias")
			var aliases map[string]string
			err := aliasTask.Get("aliases", &aliases)
			c.Assert(err, IsNil)
			c.Check(aliases, DeepEquals, expectedAuto(new["other-snap"]))
			wait := false
			if expectedRetiring["some-snap"]["aliasB"] != "" {
				wait = true
			} else if expectedRetiring["other-snap"] != nil {
				wait = true
			}
			if wait {
				c.Check(aliasTask.WaitTasks(), DeepEquals, retireTs.Tasks())
			} else {
				c.Check(aliasTask.WaitTasks(), HasLen, 0)
			}
		}
		c.Assert(j, Equals, len(tts))

		// check reported updated names
		c.Check(len(updates) > 0, Equals, true)
		sort.Strings(updates)
		expectedUpdates := make([]string, 0, len(expectedUpdatesSet))
		for x := range expectedUpdatesSet {
			expectedUpdates = append(expectedUpdates, x)
		}
		sort.Strings(expectedUpdates)
		c.Check(updates, DeepEquals, expectedUpdates)
	}
}

func (s *snapmgrTestSuite) TestUpdateOneAutoAliasesScenarios(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "other-snap", SnapID: "other-snap-id", Revision: snap.R(2)},
		},
		Current:  snap.R(2),
		SnapType: "app",
	})

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		switch info.Name() {
		case "some-snap":
			return map[string]string{"aliasA": "cmdA"}, nil
		case "other-snap":
			return map[string]string{"aliasB": "cmdB"}, nil
		}
		return nil, nil
	}

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current:  snap.R(4),
		SnapType: "app",
	})

	expectedAuto := func(aliases []string) map[string]string {
		res := make(map[string]string, len(aliases))
		for _, alias := range aliases {
			res[alias] = "auto"
		}
		return res
	}

	for _, scenario := range orthogonalAutoAliasesScenarios {
		if len(scenario.names) != 1 {
			continue
		}

		aliases := make(map[string]map[string]string)
		for snapName, autoAliases := range scenario.aliasesBefore {
			statuses := make(map[string]string)
			for _, alias := range autoAliases {
				statuses[alias] = "auto"
			}
			aliases[snapName] = statuses
		}
		s.state.Set("aliases", aliases)

		ts, err := snapstate.Update(s.state, scenario.names[0], "", snap.R(0), s.user.ID, snapstate.Flags{})
		c.Assert(err, IsNil)
		new, retiring, err := snapstate.AutoAliasesDelta(s.state, []string{"some-snap", "other-snap"})
		c.Assert(err, IsNil)

		j := 0
		tasks := ts.Tasks()
		var expectedRetiring map[string]map[string]string
		var retireTasks []*state.Task
		if len(scenario.retire) != 0 {
			nretire := len(scenario.retire)
			retireTasks = tasks[:nretire]
			j += nretire
			taskAliases := make(map[string]map[string]string)
			for _, aliasTask := range retireTasks {
				c.Check(aliasTask.Kind(), Equals, "alias")
				var aliases map[string]string
				err := aliasTask.Get("aliases", &aliases)
				c.Assert(err, IsNil)
				snapsup, err := snapstate.TaskSnapSetup(aliasTask)
				c.Assert(err, IsNil)
				taskAliases[snapsup.Name()] = aliases
			}
			expectedRetiring = make(map[string]map[string]string)
			for _, snapName := range scenario.retire {
				expectedRetiring[snapName] = expectedAuto(retiring[snapName])
			}
			c.Check(taskAliases, DeepEquals, expectedRetiring)
		}
		if scenario.update {
			first := tasks[j]
			j += 14
			c.Check(first.Kind(), Equals, "download-snap")
			wait := false
			if expectedRetiring["other-snap"]["aliasA"] != "" {
				wait = true
			} else if expectedRetiring["some-snap"] != nil {
				wait = true
			}
			if wait {
				c.Check(first.WaitTasks(), DeepEquals, retireTasks)
			} else {
				c.Check(first.WaitTasks(), HasLen, 0)
			}
		}
		if scenario.new {
			aliasTask := tasks[j]
			j++
			c.Check(aliasTask.Kind(), Equals, "alias")
			var aliases map[string]string
			err := aliasTask.Get("aliases", &aliases)
			c.Assert(err, IsNil)
			c.Check(aliases, DeepEquals, expectedAuto(new["other-snap"]))
			wait := false
			if expectedRetiring["some-snap"]["aliasB"] != "" {
				wait = true
			} else if expectedRetiring["other-snap"] != nil {
				wait = true
			}
			if wait {
				c.Check(aliasTask.WaitTasks(), DeepEquals, retireTasks)
			} else {
				c.Check(aliasTask.WaitTasks(), HasLen, 0)
			}
		}
		c.Assert(j, Equals, len(tasks))
	}
}

func (s *snapmgrTestSuite) TestUpdateLocalSnapFails(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `cannot refresh local snap "some-snap"`)
}

func (s *snapmgrTestSuite) TestUpdateDisabledUnsupported(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   false,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `refreshing disabled snap "some-snap" not supported`)
}

func makeTestSnap(c *C, snapYamlContent string) (snapFilePath string) {
	return snaptest.MakeTestSnapWithFiles(c, snapYamlContent, nil)
}

func (s *snapmgrTestSuite) TestInstallFirstLocalRunThrough(c *C) {
	// use the real thing for this one
	snapstate.MockOpenSnapFile(backend.OpenSnapFile)

	s.state.Lock()
	defer s.state.Unlock()

	mockSnap := makeTestSnap(c, `name: mock
version: 1.0`)
	chg := s.state.NewChange("install", "install a local snap")
	ts, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "mock"}, mockSnap, "", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// ensure only local install was run, i.e. first actions are pseudo-action current
	c.Assert(s.fakeBackend.ops.Ops(), HasLen, 10)
	c.Check(s.fakeBackend.ops[0].op, Equals, "current")
	c.Check(s.fakeBackend.ops[0].old, Equals, "<no-current>")
	// and setup-snap
	c.Check(s.fakeBackend.ops[1].op, Equals, "setup-snap")
	c.Check(s.fakeBackend.ops[1].name, Matches, `.*/mock_1.0_all.snap`)
	c.Check(s.fakeBackend.ops[1].revno, Equals, snap.R("x1"))

	c.Check(s.fakeBackend.ops[4].op, Equals, "candidate")
	c.Check(s.fakeBackend.ops[4].sinfo, DeepEquals, snap.SideInfo{
		RealName: "mock",
		Revision: snap.R(-1),
	})
	c.Check(s.fakeBackend.ops[5].op, Equals, "link-snap")
	c.Check(s.fakeBackend.ops[5].name, Equals, "/snap/mock/x1")
	c.Check(s.fakeBackend.ops[6].op, Equals, "setup-profiles:Doing") // core phase 2
	c.Check(s.fakeBackend.ops[8].op, Equals, "start-snap-services")
	c.Check(s.fakeBackend.ops[8].name, Equals, "/snap/mock/x1")

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	task := ts.Tasks()[0]
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		SnapPath: mockSnap,
		SideInfo: snapsup.SideInfo,
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "mock",
		Revision: snap.R(-1),
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "mock", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "mock",
		Channel:  "",
		Revision: snap.R(-1),
	})
	c.Assert(snapst.LocalRevision(), Equals, snap.R(-1))
}

func (s *snapmgrTestSuite) TestInstallSubsequentLocalRunThrough(c *C) {
	// use the real thing for this one
	snapstate.MockOpenSnapFile(backend.OpenSnapFile)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "mock", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "mock", Revision: snap.R(-2)},
		},
		Current: snap.R(-2),
	})

	mockSnap := makeTestSnap(c, `name: mock
version: 1.0`)
	chg := s.state.NewChange("install", "install a local snap")
	ts, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "mock"}, mockSnap, "", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	ops := s.fakeBackend.ops
	// ensure only local install was run, i.e. first action is pseudo-action current
	c.Assert(ops, HasLen, 13)
	c.Check(ops[0].op, Equals, "current")
	c.Check(ops[0].old, Equals, "/snap/mock/x2")
	// and setup-snap
	c.Check(ops[1].op, Equals, "setup-snap")
	c.Check(ops[1].name, Matches, `.*/mock_1.0_all.snap`)
	c.Check(ops[1].revno, Equals, snap.R("x3"))
	// and cleanup
	c.Check(ops[len(ops)-1], DeepEquals, fakeOp{
		op:    "cleanup-trash",
		name:  "mock",
		revno: snap.R("x3"),
	})

	c.Check(ops[2].op, Equals, "stop-snap-services")
	c.Check(ops[2].name, Equals, "/snap/mock/x2")

	c.Check(ops[4].op, Equals, "unlink-snap")
	c.Check(ops[4].name, Equals, "/snap/mock/x2")

	c.Check(ops[5].op, Equals, "copy-data")
	c.Check(ops[5].name, Equals, "/snap/mock/x3")
	c.Check(ops[5].old, Equals, "/snap/mock/x2")

	c.Check(ops[6].op, Equals, "setup-profiles:Doing")
	c.Check(ops[6].name, Equals, "mock")
	c.Check(ops[6].revno, Equals, snap.R(-3))

	c.Check(ops[7].op, Equals, "candidate")
	c.Check(ops[7].sinfo, DeepEquals, snap.SideInfo{
		RealName: "mock",
		Revision: snap.R(-3),
	})
	c.Check(ops[8].op, Equals, "link-snap")
	c.Check(ops[8].name, Equals, "/snap/mock/x3")
	c.Check(ops[9].op, Equals, "setup-profiles:Doing") // core phase 2
	c.Check(ops[11].op, Equals, "start-snap-services")
	c.Check(ops[11].name, Equals, "/snap/mock/x3")

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	task := ts.Tasks()[0]
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		SnapPath: mockSnap,
		SideInfo: snapsup.SideInfo,
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "mock",
		Revision: snap.R(-3),
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "mock", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.CurrentSideInfo(), DeepEquals, &snap.SideInfo{
		RealName: "mock",
		Channel:  "",
		Revision: snap.R(-3),
	})
	c.Assert(snapst.LocalRevision(), Equals, snap.R(-3))
}

func (s *snapmgrTestSuite) TestInstallOldSubsequentLocalRunThrough(c *C) {
	// use the real thing for this one
	snapstate.MockOpenSnapFile(backend.OpenSnapFile)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "mock", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "mock", Revision: snap.R(100001)},
		},
		Current: snap.R(100001),
	})

	mockSnap := makeTestSnap(c, `name: mock
version: 1.0`)
	chg := s.state.NewChange("install", "install a local snap")
	ts, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "mock"}, mockSnap, "", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// ensure only local install was run, i.e. first action is pseudo-action current
	ops := s.fakeBackend.ops
	c.Assert(ops, HasLen, 13)
	c.Check(ops[0].op, Equals, "current")
	c.Check(ops[0].old, Equals, "/snap/mock/100001")
	// and setup-snap
	c.Check(ops[1].op, Equals, "setup-snap")
	c.Check(ops[1].name, Matches, `.*/mock_1.0_all.snap`)
	c.Check(ops[1].revno, Equals, snap.R("x1"))
	// and cleanup
	c.Check(ops[len(ops)-1], DeepEquals, fakeOp{
		op:    "cleanup-trash",
		name:  "mock",
		revno: snap.R("x1"),
	})

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "mock", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.CurrentSideInfo(), DeepEquals, &snap.SideInfo{
		RealName: "mock",
		Channel:  "",
		Revision: snap.R(-1),
	})
	c.Assert(snapst.LocalRevision(), Equals, snap.R(-1))
}

func (s *snapmgrTestSuite) TestInstallPathWithMetadataRunThrough(c *C) {
	// use the real thing for this one
	snapstate.MockOpenSnapFile(backend.OpenSnapFile)

	s.state.Lock()
	defer s.state.Unlock()

	someSnap := makeTestSnap(c, `name: orig-name
version: 1.0`)
	chg := s.state.NewChange("install", "install a local snap")

	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "snapIDsnapidsnapidsnapidsnapidsn",
		Revision: snap.R(42),
	}
	ts, err := snapstate.InstallPath(s.state, si, someSnap, "", snapstate.Flags{Required: true})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// ensure only local install was run, i.e. first actions are pseudo-action current
	c.Assert(s.fakeBackend.ops.Ops(), HasLen, 10)
	c.Check(s.fakeBackend.ops[0].op, Equals, "current")
	c.Check(s.fakeBackend.ops[0].old, Equals, "<no-current>")
	// and setup-snap
	c.Check(s.fakeBackend.ops[1].op, Equals, "setup-snap")
	c.Check(s.fakeBackend.ops[1].name, Matches, `.*/orig-name_1.0_all.snap`)
	c.Check(s.fakeBackend.ops[1].revno, Equals, snap.R(42))

	c.Check(s.fakeBackend.ops[4].op, Equals, "candidate")
	c.Check(s.fakeBackend.ops[4].sinfo, DeepEquals, *si)
	c.Check(s.fakeBackend.ops[5].op, Equals, "link-snap")
	c.Check(s.fakeBackend.ops[5].name, Equals, "/snap/some-snap/42")
	c.Check(s.fakeBackend.ops[8].op, Equals, "start-snap-services")
	c.Check(s.fakeBackend.ops[8].name, Equals, "/snap/some-snap/42")

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	task := ts.Tasks()[0]
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		SnapPath: someSnap,
		SideInfo: snapsup.SideInfo,
		Flags: snapstate.Flags{
			Required: true,
		},
	})
	c.Assert(snapsup.SideInfo, DeepEquals, si)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Channel, Equals, "")
	c.Assert(snapst.Sequence[0], DeepEquals, si)
	c.Assert(snapst.LocalRevision().Unset(), Equals, true)
	c.Assert(snapst.Required, Equals, true)
}

func (s *snapmgrTestSuite) TestRemoveRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(0))
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	c.Check(len(s.fakeBackend.ops), Equals, 9)
	expected := fakeOps{
		{
			op:   "stop-snap-services",
			name: "/snap/some-snap/7",
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
		{
			op:    "remove-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-data",
			name: "/snap/some-snap/7",
		},
		{
			op:   "remove-snap-common-data",
			name: "/snap/some-snap/7",
		},
		{
			op:    "remove-snap-files",
			name:  "/snap/some-snap/7",
			stype: "app",
		},
		{
			op:   "discard-namespace",
			name: "some-snap",
		},
		{
			op:   "discard-conns:Doing",
			name: "some-snap",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Check(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	tasks := ts.Tasks()
	for _, t := range tasks {
		snapsup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)

		var expSnapSetup *snapstate.SnapSetup
		if t.Kind() == "discard-conns" || t.Kind() == "clear-aliases" {
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
				},
			}
		} else {
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
					Revision: snap.R(7),
				},
			}
		}
		c.Check(snapsup, DeepEquals, expSnapSetup, Commentf(t.Kind()))
	}

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestRemoveWithManyRevisionsRunThrough(c *C) {
	si3 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(3),
	}

	si5 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(5),
	}

	si7 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si5, &si3, &si7},
		Current:  si7.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(0))
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "stop-snap-services",
			name: "/snap/some-snap/7",
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
		{
			op:    "remove-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-data",
			name: "/snap/some-snap/7",
		},
		{
			op:    "remove-snap-files",
			name:  "/snap/some-snap/7",
			stype: "app",
		},
		{
			op:   "remove-snap-data",
			name: "/snap/some-snap/3",
		},
		{
			op:    "remove-snap-files",
			name:  "/snap/some-snap/3",
			stype: "app",
		},
		{
			op:   "remove-snap-data",
			name: "/snap/some-snap/5",
		},
		{
			op:   "remove-snap-common-data",
			name: "/snap/some-snap/5",
		},
		{
			op:    "remove-snap-files",
			name:  "/snap/some-snap/5",
			stype: "app",
		},
		{
			op:   "discard-namespace",
			name: "some-snap",
		},
		{
			op:   "discard-conns:Doing",
			name: "some-snap",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	tasks := ts.Tasks()
	revnos := []snap.Revision{{N: 7}, {N: 3}, {N: 5}}
	whichRevno := 0
	for _, t := range tasks {
		snapsup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)

		var expSnapSetup *snapstate.SnapSetup
		if t.Kind() == "discard-conns" || t.Kind() == "clear-aliases" {
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
				},
			}
		} else {
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
					Revision: revnos[whichRevno],
				},
			}
		}

		c.Check(snapsup, DeepEquals, expSnapSetup, Commentf(t.Kind()))

		if t.Kind() == "discard-snap" {
			whichRevno++
		}
	}

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestRemoveOneRevisionRunThrough(c *C) {
	si3 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(3),
	}

	si5 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(5),
	}

	si7 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si5, &si3, &si7},
		Current:  si7.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(3))
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	c.Check(len(s.fakeBackend.ops), Equals, 2)
	expected := fakeOps{
		{
			op:   "remove-snap-data",
			name: "/snap/some-snap/3",
		},
		{
			op:    "remove-snap-files",
			name:  "/snap/some-snap/3",
			stype: "app",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	tasks := ts.Tasks()
	for _, t := range tasks {
		snapsup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)

		expSnapSetup := &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: "some-snap",
				Revision: snap.R(3),
			},
		}

		c.Check(snapsup, DeepEquals, expSnapSetup, Commentf(t.Kind()))
	}

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Sequence, HasLen, 2)
}

func (s *snapmgrTestSuite) TestRemoveLastRevisionRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   false,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(2))
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	c.Check(len(s.fakeBackend.ops), Equals, 5)
	expected := fakeOps{
		{
			op:   "remove-snap-data",
			name: "/snap/some-snap/2",
		},
		{
			op:   "remove-snap-common-data",
			name: "/snap/some-snap/2",
		},
		{
			op:    "remove-snap-files",
			name:  "/snap/some-snap/2",
			stype: "app",
		},
		{
			op:   "discard-namespace",
			name: "some-snap",
		},
		{
			op:   "discard-conns:Doing",
			name: "some-snap",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	tasks := ts.Tasks()
	for _, t := range tasks {
		snapsup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)

		expSnapSetup := &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: "some-snap",
			},
		}
		if t.Kind() != "discard-conns" && t.Kind() != "clear-aliases" {
			expSnapSetup.SideInfo.Revision = snap.R(2)
		}

		c.Check(snapsup, DeepEquals, expSnapSetup, Commentf(t.Kind()))
	}

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestRemoveCurrentActiveRevisionRefused(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	_, err := snapstate.Remove(s.state, "some-snap", snap.R(2))

	c.Check(err, ErrorMatches, `cannot remove active revision 2 of snap "some-snap"`)
}

func (s *snapmgrTestSuite) TestRemoveCurrentRevisionOfSeveralRefused(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si, &si},
		Current:  si.Revision,
		SnapType: "app",
	})

	_, err := snapstate.Remove(s.state, "some-snap", snap.R(2))
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `cannot remove active revision 2 of snap "some-snap" (revert first?)`)
}

func (s *snapmgrTestSuite) TestRemoveMissingRevisionRefused(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	_, err := snapstate.Remove(s.state, "some-snap", snap.R(1))

	c.Check(err, ErrorMatches, `revision 1 of snap "some-snap" is not installed`)
}

func (s *snapmgrTestSuite) TestRemoveRefused(c *C) {
	si := snap.SideInfo{
		RealName: "gadget",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "gadget", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	_, err := snapstate.Remove(s.state, "gadget", snap.R(0))

	c.Check(err, ErrorMatches, `snap "gadget" is not removable`)
}

func (s *snapmgrTestSuite) TestRemoveRefusedLastRevision(c *C) {
	si := snap.SideInfo{
		RealName: "gadget",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "gadget", &snapstate.SnapState{
		Active:   false,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	_, err := snapstate.Remove(s.state, "gadget", snap.R(7))

	c.Check(err, ErrorMatches, `snap "gadget" is not removable`)
}

func (s *snapmgrTestSuite) TestRemoveDeletesConfigOnLastRevision(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	tr := config.NewTransaction(s.state)
	tr.Set("some-snap", "foo", "bar")
	tr.Commit()

	var res string
	tr = config.NewTransaction(s.state)
	c.Assert(tr.Get("some-snap", "foo", &res), IsNil)

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(0))
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, Equals, state.ErrNoState)

	tr = config.NewTransaction(s.state)
	err = tr.Get("some-snap", "foo", &res)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `snap "some-snap" has no "foo" configuration option`)
}

func (s *snapmgrTestSuite) TestRemoveDoesntDeleteConfigIfNotLastRevision(c *C) {
	si1 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(8),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si1, &si2},
		Current:  si2.Revision,
		SnapType: "app",
	})

	tr := config.NewTransaction(s.state)
	tr.Set("some-snap", "foo", "bar")
	tr.Commit()

	var res string
	tr = config.NewTransaction(s.state)
	c.Assert(tr.Get("some-snap", "foo", &res), IsNil)

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", si1.Revision)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	tr = config.NewTransaction(s.state)
	c.Assert(tr.Get("some-snap", "foo", &res), IsNil)
	c.Assert(res, Equals, "bar")
}

func (s *snapmgrTestSuite) TestUpdateMakesConfigSnapshot(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	tr := config.NewTransaction(s.state)
	tr.Set("some-snap", "foo", "bar")
	tr.Commit()

	var cfgs map[string]interface{}
	// we don't have config snapshots yet
	c.Assert(s.state.Get("config-snapshots", &cfgs), Equals, state.ErrNoState)

	chg := s.state.NewChange("update", "update a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(2), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()

	s.state.Lock()
	cfgs = nil
	// config snapshots of rev. 1 has been made
	c.Assert(s.state.Get("config-snapshots", &cfgs), IsNil)
	c.Assert(cfgs["some-snap"], DeepEquals, map[string]interface{}{
		"1": map[string]interface{}{
			"foo": "bar",
		},
	})
}

func (s *snapmgrTestSuite) TestRevertRestoresConfigSnapshot(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", Revision: snap.R(2)},
		},
		Current:  snap.R(2),
		SnapType: "app",
	})

	// set configuration for current snap
	tr := config.NewTransaction(s.state)
	tr.Set("some-snap", "foo", "100")
	tr.Commit()

	// make config snapshot for rev.1
	config.StoreConfigurationSnapshotMaybe(s.state, "some-snap", snap.R(1))

	// modify for rev. 2
	tr = config.NewTransaction(s.state)
	tr.Set("some-snap", "foo", "200")
	tr.Commit()

	chg := s.state.NewChange("revert", "revert snap")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()

	s.state.Lock()
	// config snapshot of rev. 2 has been made by 'revert'
	var cfgs map[string]interface{}
	c.Assert(s.state.Get("config-snapshots", &cfgs), IsNil)
	c.Assert(cfgs["some-snap"], DeepEquals, map[string]interface{}{
		"1": map[string]interface{}{
			"foo": "100",
		},
		"2": map[string]interface{}{
			"foo": "200",
		},
	})

	// current snap configuration has been restored from rev. 1 config snapshot
	tr = config.NewTransaction(s.state)
	var res string
	c.Assert(tr.Get("some-snap", "foo", &res), IsNil)
	c.Assert(res, Equals, "100")
}

func (s *snapmgrTestSuite) TestUpdateDoesGC(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current:  snap.R(4),
		SnapType: "app",
	})

	chg := s.state.NewChange("update", "update a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// ensure garbage collection runs as the last tasks
	ops := s.fakeBackend.ops
	c.Assert(ops[len(ops)-8], DeepEquals, fakeOp{
		op:   "link-snap",
		name: "/snap/some-snap/11",
	})
	c.Assert(ops[len(ops)-6], DeepEquals, fakeOp{
		op:   "start-snap-services",
		name: "/snap/some-snap/11",
	})
	c.Assert(ops[len(ops)-5], DeepEquals, fakeOp{
		op:   "remove-snap-data",
		name: "/snap/some-snap/1",
	})
	c.Assert(ops[len(ops)-4], DeepEquals, fakeOp{
		op:    "remove-snap-files",
		name:  "/snap/some-snap/1",
		stype: "app",
	})
	c.Assert(ops[len(ops)-3], DeepEquals, fakeOp{
		op:   "remove-snap-data",
		name: "/snap/some-snap/2",
	})
	c.Assert(ops[len(ops)-2], DeepEquals, fakeOp{
		op:    "remove-snap-files",
		name:  "/snap/some-snap/2",
		stype: "app",
	})
	c.Assert(ops[len(ops)-1], DeepEquals, fakeOp{
		op:    "cleanup-trash",
		name:  "some-snap",
		revno: snap.R(11),
	})

}

func (s *snapmgrTestSuite) TestRevertNoRevertAgain(c *C) {
	siNew := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(77),
	}

	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si, &siNew},
		Current:  snap.R(7),
	})

	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{})
	c.Assert(err, ErrorMatches, "no revision to revert to")
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestRevertNothingToRevertTo(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
	})

	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{})
	c.Assert(err, ErrorMatches, "no revision to revert to")
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestRevertToRevisionNoValidVersion(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(77),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si, &si2},
		Current:  snap.R(77),
	})

	ts, err := snapstate.RevertToRevision(s.state, "some-snap", snap.R("99"), snapstate.Flags{})
	c.Assert(err, ErrorMatches, `cannot find revision 99 for snap "some-snap"`)
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestRevertToRevisionAlreadyCurrent(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(77),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si, &si2},
		Current:  snap.R(77),
	})

	ts, err := snapstate.RevertToRevision(s.state, "some-snap", snap.R("77"), snapstate.Flags{})
	c.Assert(err, ErrorMatches, `already on requested revision`)
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestRevertRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}
	siOld := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: []*snap.SideInfo{&siOld, &si},
		Current:  si.Revision,
	})

	chg := s.state.NewChange("revert", "revert a snap backwards")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "stop-snap-services",
			name: "/snap/some-snap/7",
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(2),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				Revision: snap.R(2),
			},
		},
		{
			op:   "link-snap",
			name: "/snap/some-snap/2",
		},
		{
			op: "update-aliases",
		},
		{
			op:   "start-snap-services",
			name: "/snap/some-snap/2",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify that the R(2) version is active now and R(7) is still there
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Current, Equals, snap.R(2))
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Channel:  "",
		Revision: snap.R(2),
	})
	c.Assert(snapst.Sequence[1], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Channel:  "",
		Revision: snap.R(7),
	})
	c.Assert(snapst.Block(), DeepEquals, []snap.Revision{snap.R(7)})
}

func (s *snapmgrTestSuite) TestRevertWithLocalRevisionRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(-7),
	}
	siOld := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(-2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: []*snap.SideInfo{&siOld, &si},
		Current:  si.Revision,
	})

	chg := s.state.NewChange("revert", "revert a snap backwards")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	c.Assert(s.fakeBackend.ops, HasLen, 8)

	// verify that LocalRevision is still -7
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.LocalRevision(), Equals, snap.R(-7))
}

func (s *snapmgrTestSuite) TestRevertToRevisionNewVersion(c *C) {
	siNew := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
		SnapID:   "october",
	}

	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
		SnapID:   "october",
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: []*snap.SideInfo{&si, &siNew},
		Current:  snap.R(2),
		Channel:  "edge",
	})

	chg := s.state.NewChange("revert", "revert a snap forward")
	ts, err := snapstate.RevertToRevision(s.state, "some-snap", snap.R(7), snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "stop-snap-services",
			name: "/snap/some-snap/2",
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/2",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:    "candidate",
			sinfo: siNew,
		},
		{
			op:   "link-snap",
			name: "/snap/some-snap/7",
		},
		{
			op: "update-aliases",
		},
		{
			op:   "start-snap-services",
			name: "/snap/some-snap/7",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify that the R(7) version is active now
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.Active, Equals, true)
	c.Check(snapst.Current, Equals, snap.R(7))
	c.Check(snapst.Sequence, HasLen, 2)
	c.Check(snapst.Channel, Equals, "edge")
	c.Check(snapst.CurrentSideInfo(), DeepEquals, &siNew)

	c.Check(snapst.Block(), HasLen, 0)
}

func (s *snapmgrTestSuite) TestRevertTotalUndoRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: []*snap.SideInfo{&si, &si2},
		Current:  si2.Revision,
	})

	chg := s.state.NewChange("revert", "revert a snap")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	tasks := ts.Tasks()
	last := tasks[len(tasks)-1]

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(last)
	chg.AddTask(terr)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "stop-snap-services",
			name: "/snap/some-snap/2",
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/2",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				Revision: snap.R(1),
			},
		},
		{
			op:   "link-snap",
			name: "/snap/some-snap/1",
		},
		{
			op: "update-aliases",
		},
		{
			op:   "start-snap-services",
			name: "/snap/some-snap/1",
		},
		// undoing everything from here down...
		{
			op:   "stop-snap-services",
			name: "/snap/some-snap/1",
		},
		{
			op: "matching-aliases",
		},
		{
			op: "update-aliases",
		},
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/1",
		},
		{
			op:    "setup-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op:   "link-snap",
			name: "/snap/some-snap/2",
		},
		{
			op: "update-aliases",
		},
		{
			op:   "start-snap-services",
			name: "/snap/some-snap/2",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Check(s.fakeBackend.ops, DeepEquals, expected)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Current, Equals, si2.Revision)
}

func (s *snapmgrTestSuite) TestRevertUndoRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: []*snap.SideInfo{&si, &si2},
		Current:  si2.Revision,
	})

	chg := s.state.NewChange("revert", "install a revert")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.fakeBackend.linkSnapFailTrigger = "/snap/some-snap/1"

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "stop-snap-services",
			name: "/snap/some-snap/2",
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/2",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				Revision: snap.R(1),
			},
		},
		{
			op:   "link-snap.failed",
			name: "/snap/some-snap/1",
		},
		// undo stuff here
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/1",
		},
		{
			op:    "setup-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op:   "link-snap",
			name: "/snap/some-snap/2",
		},
		{
			op: "update-aliases",
		},
		{
			op:   "start-snap-services",
			name: "/snap/some-snap/2",
		},
	}

	// ensure all our tasks ran
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Current, Equals, snap.R(2))
}

func (s *snapmgrTestSuite) TestEnableDoesNotEnableAgain(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{&si},
		Current:  snap.R(7),
		Active:   true,
	})

	ts, err := snapstate.Enable(s.state, "some-snap")
	c.Assert(err, ErrorMatches, `snap "some-snap" already enabled`)
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestEnableRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
		Channel:  "edge",
		SnapID:   "foo",
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		Active:   false,
		Channel:  "edge",
	})

	chg := s.state.NewChange("enable", "enable a snap")
	ts, err := snapstate.Enable(s.state, "some-snap")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	expected := fakeOps{
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:    "candidate",
			sinfo: si,
		},
		{
			op:   "link-snap",
			name: "/snap/some-snap/7",
		},
		{
			op: "update-aliases",
		},
		{
			op:   "start-snap-services",
			name: "/snap/some-snap/7",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	info, err := snapst.CurrentInfo()
	c.Assert(err, IsNil)
	c.Assert(info.Channel, Equals, "edge")
	c.Assert(info.SnapID, Equals, "foo")
}

func (s *snapmgrTestSuite) TestDisableRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		Active:   true,
	})

	chg := s.state.NewChange("disable", "disable a snap")
	ts, err := snapstate.Disable(s.state, "some-snap")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "stop-snap-services",
			name: "/snap/some-snap/7",
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
		{
			op:    "remove-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, false)
}

func (s *snapmgrTestSuite) TestDisableDoesNotEnableAgain(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{&si},
		Current:  snap.R(7),
		Active:   false,
	})

	ts, err := snapstate.Disable(s.state, "some-snap")
	c.Assert(err, ErrorMatches, `snap "some-snap" already disabled`)
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestUndoMountSnapFailsInCopyData(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.fakeBackend.copySnapDataFailTrigger = "/snap/some-snap/11"

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	expected := fakeOps{
		{
			op:    "storesvc-snap",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:   "storesvc-download",
			name: "some-snap",
		},
		{
			op:    "validate-snap:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:  "current",
			old: "<no-current>",
		},
		{
			op:   "open-snap-file",
			name: "/var/lib/snapd/snaps/some-snap_11.snap",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "snapIDsnapidsnapidsnapidsnapidsn",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  "/var/lib/snapd/snaps/some-snap_11.snap",
			revno: snap.R(11),
		},
		{
			op:   "copy-data.failed",
			name: "/snap/some-snap/11",
			old:  "<no-old>",
		},
		{
			op:    "undo-setup-snap",
			name:  "/snap/some-snap/11",
			stype: "app",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)
}

func (s *snapmgrTestSuite) TestRefreshFailureCausesErrorReport(c *C) {
	var errSnap, errMsg, errSig string
	var errExtra map[string]string
	var n int
	restore := snapstate.MockErrtrackerReport(func(aSnap, aErrMsg, aDupSig string, extra map[string]string) (string, error) {
		errSnap = aSnap
		errMsg = aErrMsg
		errSig = aDupSig
		errExtra = extra
		n += 1
		return "oopsid", nil
	})
	defer restore()

	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("ubuntu-core-transition-retry", 7)
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.fakeBackend.linkSnapFailTrigger = "/snap/some-snap/11"

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// verify we generated a failure report
	c.Check(n, Equals, 1)
	c.Check(errSnap, Equals, "some-snap")
	c.Check(errExtra, DeepEquals, map[string]string{
		"UbuntuCoreTransitionCount": "7",
		"Channel":                   "some-channel",
		"Revision":                  "11",
	})
	c.Check(errMsg, Matches, `(?sm)change "install": "install a snap"
download-snap: Undoing
 snap-setup: "some-snap" \(11\) "some-channel"
validate-snap: Done
.*
link-snap: Error
 INFO unlink
 ERROR fail
set-auto-aliases: Hold
setup-aliases: Hold
start-snap-services: Hold
cleanup: Hold
run-hook: Hold`)
	c.Check(errSig, Matches, `(?sm)snap-install:
download-snap: Undoing
 snap-setup: "some-snap"
validate-snap: Done
.*
link-snap: Error
 INFO unlink
 ERROR fail
set-auto-aliases: Hold
setup-aliases: Hold
start-snap-services: Hold
cleanup: Hold
run-hook: Hold`)

	// run again with empty "ubuntu-core-transition-retry"
	s.state.Set("ubuntu-core-transition-retry", 0)
	chg = s.state.NewChange("install", "install a snap")
	ts, err = snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)
	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()
	// verify that we excluded this field from the bugreport
	c.Check(n, Equals, 2)
	c.Check(errExtra, DeepEquals, map[string]string{
		"Channel":  "some-channel",
		"Revision": "11",
	})

}

func (s *snapmgrTestSuite) verifyRefreshLast(c *C) {
	var lastRefresh time.Time

	tr := config.NewTransaction(s.state)
	tr.Get("core", "refresh.last", &lastRefresh)
	c.Check(time.Now().Year(), Equals, lastRefresh.Year())
}

func (s *snapmgrTestSuite) TestEnsureRefreshesNoUpdate(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }

	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.last", time.Time{})
	tr.Commit()

	// Ensure() also runs ensureRefreshes()
	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()

	// nothing needs to be done, but refresh.last got updated
	c.Check(s.state.Changes(), HasLen, 0)
	s.verifyRefreshLast(c)
}

func (s *snapmgrTestSuite) TestEnsureRefreshesWithUpdate(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }

	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.last", time.Time{})
	tr.Commit()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	// Ensure() also runs ensureRefreshes() and our test setup has an
	// update for the "some-snap" in our fake store
	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()

	// verify we have an auto-refresh change scheduled now
	c.Check(s.state.Changes(), HasLen, 1)
	chg := s.state.Changes()[0]
	c.Check(chg.Kind(), Equals, "auto-refresh")
	c.Check(chg.IsReady(), Equals, false)
	s.verifyRefreshLast(c)
}

func (s *snapmgrTestSuite) TestEnsureRefreshDisabled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }

	// can only be disabled in debug mode
	oldEnv := os.Getenv("SNAPD_DEBUG")
	defer func() { os.Setenv("SNAPD_DEBUG", oldEnv) }()
	os.Setenv("SNAPD_DEBUG", "1")

	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.last", time.Time{})
	tr.Set("core", "refresh.disabled", true)
	tr.Commit()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	// Ensure() also runs ensureRefreshes() and our test setup has an
	// update for the "some-snap" in our fake store
	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()

	// verify that the disable works
	c.Check(s.state.Changes(), HasLen, 0)

	// and no last refresh got updated
	var lastRefresh time.Time
	tr = config.NewTransaction(s.state)
	tr.Get("core", "refresh.last", &lastRefresh)
	c.Check(lastRefresh.IsZero(), Equals, true)

}

func (s *snapmgrTestSuite) TestEnsureRefreshesWithUpdateError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }

	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.last", time.Time{})
	tr.Commit()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	// Ensure() also runs ensureRefreshes() and our test setup has an
	// update for the "some-snap" in our fake store
	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()

	c.Check(s.state.Changes(), HasLen, 1)
	chg := s.state.Changes()[0]
	terr := s.state.NewTask("error-trigger", "simulate an error")
	tasks := chg.Tasks()
	for _, t := range tasks[:len(tasks)-2] {
		terr.WaitFor(t)
	}
	chg.AddTask(terr)

	// run the changes
	s.state.Unlock()
	s.settle()
	s.state.Lock()

	s.verifyRefreshLast(c)
}

func (s *snapmgrTestSuite) TestEnsureRefreshesInFlight(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }

	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.last", time.Time{})
	tr.Commit()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	// simulate an in-flight change
	chg := s.state.NewChange("auto-refresh", "...")
	chg.SetStatus(state.DoStatus)
	c.Check(s.state.Changes(), HasLen, 1)

	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()

	// verify no additional change got generated
	c.Check(s.state.Changes(), HasLen, 1)
}

func (s *snapmgrTestSuite) TestEnsureRefreshesWithUpdateStoreError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }

	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.last", time.Time{})
	tr.Commit()

	origAutoRefreshAssertions := snapstate.AutoRefreshAssertions
	defer func() { snapstate.AutoRefreshAssertions = origAutoRefreshAssertions }()

	// simulate failure in snapstate.AutoRefresh()
	autoRefreshAssertionsCalled := 0
	snapstate.AutoRefreshAssertions = func(st *state.State, userID int) error {
		autoRefreshAssertionsCalled++
		return fmt.Errorf("simulate store error")
	}

	// check that no change got created and that autoRefreshAssertins
	// got called once
	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()
	c.Check(s.state.Changes(), HasLen, 0)
	c.Check(autoRefreshAssertionsCalled, Equals, 1)

	// run Ensure() again and check that AutoRefresh() did not run
	// again because to test that lastRefreshAttempt backoff is working
	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()
	c.Check(s.state.Changes(), HasLen, 0)
	c.Check(autoRefreshAssertionsCalled, Equals, 1)
}

type snapmgrQuerySuite struct {
	st *state.State
}

var _ = Suite(&snapmgrQuerySuite{})

func (s *snapmgrQuerySuite) SetUpTest(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	s.st = st

	dirs.SetRootDir(c.MkDir())

	// Write a snap.yaml with fake name
	sideInfo11 := &snap.SideInfo{RealName: "name1", Revision: snap.R(11), EditedSummary: "s11"}
	sideInfo12 := &snap.SideInfo{RealName: "name1", Revision: snap.R(12), EditedSummary: "s12"}
	snaptest.MockSnap(c, `
name: name0
version: 1.1
description: |
    Lots of text`, "", sideInfo11)
	snaptest.MockSnap(c, `
name: name0
version: 1.2
description: |
    Lots of text`, "", sideInfo12)
	snapstate.Set(st, "name1", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfo11, sideInfo12},
		Current:  sideInfo12.Revision,
		SnapType: "app",
	})

	// have also a snap being installed
	/*
		snapstate.Set(st, "installing", &snapstate.SnapState{
			Candidate: &snap.SideInfo{RealName: "installing", Revision: snap.R(1)},
		})
	*/
}

func (s *snapmgrQuerySuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *snapmgrQuerySuite) TestInfo(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	info, err := snapstate.Info(st, "name1", snap.R(11))
	c.Assert(err, IsNil)

	c.Check(info.Name(), Equals, "name1")
	c.Check(info.Revision, Equals, snap.R(11))
	c.Check(info.Summary(), Equals, "s11")
	c.Check(info.Version, Equals, "1.1")
	c.Check(info.Description(), Equals, "Lots of text")
}

func (s *snapmgrQuerySuite) TestSnapStateCurrentInfo(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(st, "name1", &snapst)
	c.Assert(err, IsNil)

	info, err := snapst.CurrentInfo()
	c.Assert(err, IsNil)

	c.Check(info.Name(), Equals, "name1")
	c.Check(info.Revision, Equals, snap.R(12))
	c.Check(info.Summary(), Equals, "s12")
	c.Check(info.Version, Equals, "1.2")
	c.Check(info.Description(), Equals, "Lots of text")
}

func (s *snapmgrQuerySuite) TestSnapStateCurrentInfoErrNoCurrent(c *C) {
	snapst := new(snapstate.SnapState)
	_, err := snapst.CurrentInfo()
	c.Assert(err, Equals, snapstate.ErrNoCurrent)

}

func (s *snapmgrQuerySuite) TestCurrentInfo(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	info, err := snapstate.CurrentInfo(st, "name1")
	c.Assert(err, IsNil)

	c.Check(info.Name(), Equals, "name1")
	c.Check(info.Revision, Equals, snap.R(12))
}

func (s *snapmgrQuerySuite) TestCurrentInfoAbsent(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	_, err := snapstate.CurrentInfo(st, "absent")
	c.Assert(err, ErrorMatches, `cannot find snap "absent"`)
}

func (s *snapmgrQuerySuite) TestActiveInfos(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	infos, err := snapstate.ActiveInfos(st)
	c.Assert(err, IsNil)

	c.Check(infos, HasLen, 1)

	c.Check(infos[0].Name(), Equals, "name1")
	c.Check(infos[0].Revision, Equals, snap.R(12))
	c.Check(infos[0].Summary(), Equals, "s12")
	c.Check(infos[0].Version, Equals, "1.2")
	c.Check(infos[0].Description(), Equals, "Lots of text")
}

func (s *snapmgrQuerySuite) TestTypeInfo(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	for _, x := range []struct {
		snapName string
		snapType snap.Type
		getInfo  func(*state.State) (*snap.Info, error)
	}{
		{
			snapName: "gadget",
			snapType: snap.TypeGadget,
			getInfo:  snapstate.GadgetInfo,
		},
		{
			snapName: "core",
			snapType: snap.TypeOS,
			getInfo:  snapstate.CoreInfo,
		},
		{
			snapName: "kernel",
			snapType: snap.TypeKernel,
			getInfo:  snapstate.KernelInfo,
		},
	} {
		_, err := x.getInfo(st)
		c.Assert(err, Equals, state.ErrNoState)

		sideInfo := &snap.SideInfo{
			RealName: x.snapName,
			Revision: snap.R(2),
		}
		snaptest.MockSnap(c, fmt.Sprintf("name: %q\ntype: %q\nversion: %q\n", x.snapName, x.snapType, x.snapName), "", sideInfo)
		snapstate.Set(st, x.snapName, &snapstate.SnapState{
			SnapType: string(x.snapType),
			Active:   true,
			Sequence: []*snap.SideInfo{sideInfo},
			Current:  sideInfo.Revision,
		})

		info, err := x.getInfo(st)
		c.Assert(err, IsNil)

		c.Check(info.Name(), Equals, x.snapName)
		c.Check(info.Revision, Equals, snap.R(2))
		c.Check(info.Version, Equals, x.snapName)
		c.Check(info.Type, Equals, x.snapType)
	}
}

func (s *snapmgrQuerySuite) TestTypeInfoCore(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	for testNr, t := range []struct {
		expectedSnap string
		snapNames    []string
		errMatcher   string
	}{
		// nothing
		{"", []string{}, state.ErrNoState.Error()},
		// single
		{"core", []string{"core"}, ""},
		{"ubuntu-core", []string{"ubuntu-core"}, ""},
		{"hard-core", []string{"hard-core"}, ""},
		// unrolled loop to ensure we don't pass because
		// the order is randomly right
		{"core", []string{"core", "ubuntu-core"}, ""},
		{"core", []string{"core", "ubuntu-core"}, ""},
		{"core", []string{"core", "ubuntu-core"}, ""},
		{"core", []string{"core", "ubuntu-core"}, ""},
		{"core", []string{"core", "ubuntu-core"}, ""},
		{"core", []string{"core", "ubuntu-core"}, ""},
		{"core", []string{"core", "ubuntu-core"}, ""},
		{"core", []string{"core", "ubuntu-core"}, ""},
		// unknown combination
		{"", []string{"duo-core", "single-core"}, `unexpected cores.*`},
		// multi-core is not supported
		{"", []string{"core", "ubuntu-core", "multi-core"}, `unexpected number of cores, got 3`},
	} {
		// clear snapstate
		st.Set("snaps", map[string]*json.RawMessage{})

		for _, snapName := range t.snapNames {
			sideInfo := &snap.SideInfo{
				RealName: snapName,
				Revision: snap.R(1),
			}
			snaptest.MockSnap(c, fmt.Sprintf("name: %q\ntype: os\nversion: %q\n", snapName, snapName), "", sideInfo)
			snapstate.Set(st, snapName, &snapstate.SnapState{
				SnapType: string(snap.TypeOS),
				Active:   true,
				Sequence: []*snap.SideInfo{sideInfo},
				Current:  sideInfo.Revision,
			})
		}

		info, err := snapstate.CoreInfo(st)
		if t.errMatcher != "" {
			c.Assert(err, ErrorMatches, t.errMatcher)
		} else {
			c.Assert(info, NotNil)
			c.Check(info.Name(), Equals, t.expectedSnap, Commentf("(%d) test %q %v", testNr, t.expectedSnap, t.snapNames))
			c.Check(info.Type, Equals, snap.TypeOS)
		}
	}
}

func (s *snapmgrQuerySuite) TestPreviousSideInfo(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(st, "name1", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.CurrentSideInfo(), NotNil)
	c.Assert(snapst.CurrentSideInfo().Revision, Equals, snap.R(12))
	c.Assert(snapstate.PreviousSideInfo(&snapst), NotNil)
	c.Assert(snapstate.PreviousSideInfo(&snapst).Revision, Equals, snap.R(11))
}

func (s *snapmgrQuerySuite) TestPreviousSideInfoNoCurrent(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	snapst := &snapstate.SnapState{}
	c.Assert(snapstate.PreviousSideInfo(snapst), IsNil)
}

func (s *snapmgrQuerySuite) TestAll(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	snapStates, err := snapstate.All(st)
	c.Assert(err, IsNil)
	c.Assert(snapStates, HasLen, 1)

	snapst := snapStates["name1"]
	c.Assert(snapst, NotNil)

	c.Check(snapst.Active, Equals, true)
	c.Check(snapst.CurrentSideInfo(), NotNil)

	info12, err := snap.ReadInfo("name1", snapst.CurrentSideInfo())
	c.Assert(err, IsNil)

	c.Check(info12.Name(), Equals, "name1")
	c.Check(info12.Revision, Equals, snap.R(12))
	c.Check(info12.Summary(), Equals, "s12")
	c.Check(info12.Version, Equals, "1.2")
	c.Check(info12.Description(), Equals, "Lots of text")

	info11, err := snap.ReadInfo("name1", snapst.Sequence[0])
	c.Assert(err, IsNil)

	c.Check(info11.Name(), Equals, "name1")
	c.Check(info11.Revision, Equals, snap.R(11))
	c.Check(info11.Version, Equals, "1.1")
}

func (s *snapmgrQuerySuite) TestAllEmptyAndEmptyNormalisation(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	snapStates, err := snapstate.All(st)
	c.Assert(err, IsNil)
	c.Check(snapStates, HasLen, 0)

	snapstate.Set(st, "foo", nil)

	snapStates, err = snapstate.All(st)
	c.Assert(err, IsNil)
	c.Check(snapStates, HasLen, 0)

	snapstate.Set(st, "foo", &snapstate.SnapState{})

	snapStates, err = snapstate.All(st)
	c.Assert(err, IsNil)
	c.Check(snapStates, HasLen, 0)
}

func (s *snapmgrTestSuite) TestTrySetsTryMode(c *C) {
	s.testTrySetsTryMode(snapstate.Flags{}, c)
}

func (s *snapmgrTestSuite) TestTrySetsTryModeDevMode(c *C) {
	s.testTrySetsTryMode(snapstate.Flags{DevMode: true}, c)
}
func (s *snapmgrTestSuite) TestTrySetsTryModeJailMode(c *C) {
	s.testTrySetsTryMode(snapstate.Flags{JailMode: true}, c)
}
func (s *snapmgrTestSuite) TestTrySetsTryModeClassic(c *C) {
	s.testTrySetsTryMode(snapstate.Flags{Classic: true}, c)
}

func (s *snapmgrTestSuite) testTrySetsTryMode(flags snapstate.Flags, c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// make mock try dir
	tryYaml := filepath.Join(c.MkDir(), "meta", "snap.yaml")
	err := os.MkdirAll(filepath.Dir(tryYaml), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(tryYaml, []byte("name: foo\nversion: 1.0"), 0644)
	c.Assert(err, IsNil)

	chg := s.state.NewChange("try", "try snap")
	ts, err := snapstate.TryPath(s.state, "foo", filepath.Dir(filepath.Dir(tryYaml)), flags)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// verify snap is in TryMode
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, IsNil)

	flags.TryMode = true
	c.Check(snapst.Flags, DeepEquals, flags)

	c.Check(s.state.TaskCount(), Equals, len(ts.Tasks()))
	c.Check(taskKinds(ts.Tasks()), DeepEquals, []string{
		"prepare-snap",
		"mount-snap",
		"copy-snap-data",
		"setup-profiles",
		"link-snap",
		"setup-profiles",
		"set-auto-aliases",
		"setup-aliases",
		"start-snap-services",
		"run-hook",
	})

}

func (s *snapmgrTestSuite) TestTryUndoRemovesTryFlag(c *C) {
	s.testTrySetsTryMode(snapstate.Flags{}, c)
}

func (s *snapmgrTestSuite) TestTryUndoRemovesTryFlagLeavesDevMode(c *C) {
	s.testTrySetsTryMode(snapstate.Flags{DevMode: true}, c)
}
func (s *snapmgrTestSuite) TestTryUndoRemovesTryFlagLeavesJailMode(c *C) {
	s.testTrySetsTryMode(snapstate.Flags{JailMode: true}, c)
}
func (s *snapmgrTestSuite) TestTryUndoRemovesTryFlagLeavesClassic(c *C) {
	s.testTrySetsTryMode(snapstate.Flags{Classic: true}, c)
}

func (s *snapmgrTestSuite) testTryUndoRemovesTryFlag(flags snapstate.Flags, c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// simulate existing state for foo
	var snapst snapstate.SnapState
	snapst.Sequence = []*snap.SideInfo{
		{
			RealName: "foo",
			Revision: snap.R(23),
		},
	}
	snapst.Flags = flags
	snapst.Current = snap.R(23)
	snapstate.Set(s.state, "foo", &snapst)
	c.Check(snapst.TryMode, Equals, false)

	chg := s.state.NewChange("try", "try snap")
	ts, err := snapstate.TryPath(s.state, "foo", c.MkDir(), flags)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	last := ts.Tasks()[len(ts.Tasks())-1]
	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(last)
	chg.AddTask(terr)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// verify snap is not in try mode, the state got undone
	err = snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Flags, DeepEquals, flags)
}

type snapStateSuite struct{}

var _ = Suite(&snapStateSuite{})

func (s *snapStateSuite) TestSnapStateDevMode(c *C) {
	snapst := &snapstate.SnapState{}
	c.Check(snapst.DevMode, Equals, false)
	snapst.Flags.DevMode = true
	c.Check(snapst.DevMode, Equals, true)
}

func (s *snapStateSuite) TestSnapStateType(c *C) {
	snapst := &snapstate.SnapState{}
	_, err := snapst.Type()
	c.Check(err, ErrorMatches, "snap type unset")

	snapst.SetType(snap.TypeKernel)
	typ, err := snapst.Type()
	c.Assert(err, IsNil)
	c.Check(typ, Equals, snap.TypeKernel)
}

func (s *snapStateSuite) TestCurrentSideInfoEmpty(c *C) {
	var snapst snapstate.SnapState
	c.Check(snapst.CurrentSideInfo(), IsNil)
	c.Check(snapst.Current.Unset(), Equals, true)
}

func (s *snapStateSuite) TestCurrentSideInfoSimple(c *C) {
	si1 := &snap.SideInfo{Revision: snap.R(1)}
	snapst := snapstate.SnapState{
		Sequence: []*snap.SideInfo{si1},
		Current:  snap.R(1),
	}
	c.Check(snapst.CurrentSideInfo(), DeepEquals, si1)
}

func (s *snapStateSuite) TestCurrentSideInfoInOrder(c *C) {
	si1 := &snap.SideInfo{Revision: snap.R(1)}
	si2 := &snap.SideInfo{Revision: snap.R(2)}
	snapst := snapstate.SnapState{
		Sequence: []*snap.SideInfo{si1, si2},
		Current:  snap.R(2),
	}
	c.Check(snapst.CurrentSideInfo(), DeepEquals, si2)
}

func (s *snapStateSuite) TestCurrentSideInfoOutOfOrder(c *C) {
	si1 := &snap.SideInfo{Revision: snap.R(1)}
	si2 := &snap.SideInfo{Revision: snap.R(2)}
	snapst := snapstate.SnapState{
		Sequence: []*snap.SideInfo{si1, si2},
		Current:  snap.R(1),
	}
	c.Check(snapst.CurrentSideInfo(), DeepEquals, si1)
}

func (s *snapStateSuite) TestCurrentSideInfoInconsistent(c *C) {
	snapst := snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{Revision: snap.R(1)},
		},
	}
	c.Check(func() { snapst.CurrentSideInfo() }, PanicMatches, `snapst.Current and snapst.Sequence out of sync:.*`)
}

func (s *snapStateSuite) TestCurrentSideInfoInconsistentWithCurrent(c *C) {
	snapst := snapstate.SnapState{Current: snap.R(17)}
	c.Check(func() { snapst.CurrentSideInfo() }, PanicMatches, `cannot find snapst.Current in the snapst.Sequence`)
}

type snapSetupSuite struct{}

var _ = Suite(&snapSetupSuite{})

type canRemoveSuite struct{}

var _ = Suite(&canRemoveSuite{})

func (s *canRemoveSuite) TestAppAreAlwaysOKToRemove(c *C) {
	info := &snap.Info{
		Type: snap.TypeApp,
	}
	info.RealName = "foo"

	c.Check(snapstate.CanRemove(info, &snapstate.SnapState{Active: true}, false), Equals, true)
	c.Check(snapstate.CanRemove(info, &snapstate.SnapState{Active: true}, true), Equals, true)
}

func (s *canRemoveSuite) TestLastGadgetsAreNotOK(c *C) {
	info := &snap.Info{
		Type: snap.TypeGadget,
	}
	info.RealName = "foo"

	c.Check(snapstate.CanRemove(info, &snapstate.SnapState{}, true), Equals, false)
}

func (s *canRemoveSuite) TestLastOSAndKernelAreNotOK(c *C) {
	os := &snap.Info{
		Type: snap.TypeOS,
	}
	os.RealName = "os"
	kernel := &snap.Info{
		Type: snap.TypeKernel,
	}
	kernel.RealName = "krnl"

	c.Check(snapstate.CanRemove(os, &snapstate.SnapState{}, true), Equals, false)

	c.Check(snapstate.CanRemove(kernel, &snapstate.SnapState{}, true), Equals, false)
}

func (s *canRemoveSuite) TestOneRevisionIsOK(c *C) {
	info := &snap.Info{
		Type: snap.TypeGadget,
	}
	info.RealName = "foo"

	c.Check(snapstate.CanRemove(info, &snapstate.SnapState{Active: true}, false), Equals, true)
}

func (s *canRemoveSuite) TestRequiredIsNotOK(c *C) {
	info := &snap.Info{
		Type: snap.TypeApp,
	}
	info.RealName = "foo"

	c.Check(snapstate.CanRemove(info, &snapstate.SnapState{Active: false, Flags: snapstate.Flags{Required: true}}, true), Equals, false)
	c.Check(snapstate.CanRemove(info, &snapstate.SnapState{Active: true, Flags: snapstate.Flags{Required: true}}, true), Equals, false)
	c.Check(snapstate.CanRemove(info, &snapstate.SnapState{Active: true, Flags: snapstate.Flags{Required: true}}, false), Equals, true)
}

func revs(seq []*snap.SideInfo) []int {
	revs := make([]int, len(seq))
	for i, si := range seq {
		revs[i] = si.Revision.N
	}

	return revs
}

type opSeqOpts struct {
	revert  bool
	fail    bool
	before  []int
	current int
	via     int
	after   []int
}

// build a SnapState with a revision sequence given by `before` and a
// current revision of `current`. Then refresh --revision via. Then
// check the revision sequence is as in `after`.
func (s *snapmgrTestSuite) testOpSequence(c *C, opts *opSeqOpts) (*snapstate.SnapState, *state.TaskSet) {
	s.state.Lock()
	defer s.state.Unlock()

	seq := make([]*snap.SideInfo, len(opts.before))
	for i, n := range opts.before {
		seq[i] = &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(n)}
	}

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: seq,
		Current:  snap.R(opts.current),
		SnapType: "app",
	})

	var chg *state.Change
	var ts *state.TaskSet
	var err error
	if opts.revert {
		chg = s.state.NewChange("revert", "revert a snap")
		ts, err = snapstate.RevertToRevision(s.state, "some-snap", snap.R(opts.via), snapstate.Flags{})
	} else {
		chg = s.state.NewChange("refresh", "refresh a snap")
		ts, err = snapstate.Update(s.state, "some-snap", "", snap.R(opts.via), s.user.ID, snapstate.Flags{})
	}
	c.Assert(err, IsNil)
	if opts.fail {
		tasks := ts.Tasks()
		last := tasks[len(tasks)-1]
		terr := s.state.NewTask("error-trigger", "provoking total undo")
		terr.WaitFor(last)
		if len(last.Lanes()) > 0 {
			lanes := last.Lanes()
			// sanity
			c.Assert(lanes, HasLen, 1)
			terr.JoinLane(lanes[0])
		}
		chg.AddTask(terr)
	}
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(revs(snapst.Sequence), DeepEquals, opts.after)

	return &snapst, ts
}

func (s *snapmgrTestSuite) testUpdateSequence(c *C, opts *opSeqOpts) *state.TaskSet {
	opts.revert = false
	snapst, ts := s.testOpSequence(c, opts)
	// update always ends with current==seq[-1]==via:
	c.Check(snapst.Current.N, Equals, opts.after[len(opts.after)-1])
	c.Check(snapst.Current.N, Equals, opts.via)

	c.Check(s.fakeBackend.ops.Count("copy-data"), Equals, 1)
	c.Check(s.fakeBackend.ops.First("copy-data"), DeepEquals, &fakeOp{
		op:   "copy-data",
		name: fmt.Sprintf("/snap/some-snap/%d", opts.via),
		old:  fmt.Sprintf("/snap/some-snap/%d", opts.current),
	})

	return ts
}

func (s *snapmgrTestSuite) testUpdateFailureSequence(c *C, opts *opSeqOpts) *state.TaskSet {
	opts.revert = false
	opts.after = opts.before
	s.fakeBackend.linkSnapFailTrigger = fmt.Sprintf("/snap/some-snap/%d", opts.via)
	snapst, ts := s.testOpSequence(c, opts)
	// a failed update will always end with current unchanged
	c.Check(snapst.Current.N, Equals, opts.current)

	ops := s.fakeBackend.ops
	c.Check(ops.Count("copy-data"), Equals, 1)
	do := ops.First("copy-data")

	c.Check(ops.Count("undo-copy-snap-data"), Equals, 1)
	undo := ops.First("undo-copy-snap-data")

	do.op = undo.op
	c.Check(do, DeepEquals, undo) // i.e. they only differed in the op

	return ts
}

// testTotal*Failure fails *after* link-snap
func (s *snapmgrTestSuite) testTotalUpdateFailureSequence(c *C, opts *opSeqOpts) *state.TaskSet {
	opts.revert = false
	opts.fail = true
	snapst, ts := s.testOpSequence(c, opts)
	// a failed update will always end with current unchanged
	c.Check(snapst.Current.N, Equals, opts.current)

	ops := s.fakeBackend.ops
	c.Check(ops.Count("copy-data"), Equals, 1)
	do := ops.First("copy-data")

	c.Check(ops.Count("undo-copy-snap-data"), Equals, 1)
	undo := ops.First("undo-copy-snap-data")

	do.op = undo.op
	c.Check(do, DeepEquals, undo) // i.e. they only differed in the op

	return ts
}

func (s *snapmgrTestSuite) testRevertSequence(c *C, opts *opSeqOpts) *state.TaskSet {
	opts.revert = true
	opts.after = opts.before
	snapst, ts := s.testOpSequence(c, opts)
	// successful revert leaves current == via
	c.Check(snapst.Current.N, Equals, opts.via)

	c.Check(s.fakeBackend.ops.Count("copy-data"), Equals, 0)

	return ts
}

func (s *snapmgrTestSuite) testRevertFailureSequence(c *C, opts *opSeqOpts) *state.TaskSet {
	opts.revert = true
	opts.after = opts.before
	s.fakeBackend.linkSnapFailTrigger = fmt.Sprintf("/snap/some-snap/%d", opts.via)
	snapst, ts := s.testOpSequence(c, opts)
	// a failed revert will always end with current unchanged
	c.Check(snapst.Current.N, Equals, opts.current)

	c.Check(s.fakeBackend.ops.Count("copy-data"), Equals, 0)
	c.Check(s.fakeBackend.ops.Count("undo-copy-snap-data"), Equals, 0)

	return ts
}

func (s *snapmgrTestSuite) testTotalRevertFailureSequence(c *C, opts *opSeqOpts) *state.TaskSet {
	opts.revert = true
	opts.fail = true
	opts.after = opts.before
	snapst, ts := s.testOpSequence(c, opts)
	// a failed revert will always end with current unchanged
	c.Check(snapst.Current.N, Equals, opts.current)

	c.Check(s.fakeBackend.ops.Count("copy-data"), Equals, 0)
	c.Check(s.fakeBackend.ops.Count("undo-copy-snap-data"), Equals, 0)

	return ts
}

// *** sequence tests ***

// 1. a boring update
// 1a. ... that works
func (s *snapmgrTestSuite) TestSeqNormal(c *C) {
	s.testUpdateSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 3, via: 4, after: []int{2, 3, 4}})
}

// 1b. that fails during link
func (s *snapmgrTestSuite) TestSeqNormalFailure(c *C) {
	s.testUpdateFailureSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 3, via: 4})
}

// 1c. that fails after link
func (s *snapmgrTestSuite) TestSeqTotalNormalFailure(c *C) {
	// total updates are failures after sequence trimming => we lose a rev
	s.testTotalUpdateFailureSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 3, via: 4, after: []int{2, 3}})
}

// 2. a boring revert
// 2a. that works
func (s *snapmgrTestSuite) TestSeqRevert(c *C) {
	s.testRevertSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 3, via: 2})
}

// 2b. that fails during link
func (s *snapmgrTestSuite) TestSeqRevertFailure(c *C) {
	s.testRevertFailureSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 3, via: 2})
}

// 2c. that fails after link
func (s *snapmgrTestSuite) TestSeqTotalRevertFailure(c *C) {
	s.testTotalRevertFailureSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 3, via: 2})
}

// 3. a post-revert update
// 3a. that works
func (s *snapmgrTestSuite) TestSeqPostRevert(c *C) {
	s.testUpdateSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 2, via: 4, after: []int{1, 2, 4}})
}

// 3b. that fails during link
func (s *snapmgrTestSuite) TestSeqPostRevertFailure(c *C) {
	s.testUpdateFailureSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 2, via: 4})
}

// 3c. that fails after link
func (s *snapmgrTestSuite) TestSeqTotalPostRevertFailure(c *C) {
	// lose a rev here as well
	s.testTotalUpdateFailureSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 2, via: 4, after: []int{1, 2}})
}

// 3d. manually requesting the one reverted away from
func (s *snapmgrTestSuite) TestSeqRefreshPostRevertSameRevno(c *C) {
	s.testUpdateSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 2, via: 3, after: []int{1, 2, 3}})
}

// 4. a post-revert revert
// 4a. that works
func (s *snapmgrTestSuite) TestSeqRevertPostRevert(c *C) {
	s.testRevertSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 2, via: 1})
}

// 4b. that fails during link
func (s *snapmgrTestSuite) TestSeqRevertPostRevertFailure(c *C) {
	s.testRevertFailureSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 2, via: 1})
}

// 4c. that fails after link
func (s *snapmgrTestSuite) TestSeqTotalRevertPostRevertFailure(c *C) {
	s.testTotalRevertFailureSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 2, via: 1})
}

// 5. an update that missed a rev
// 5a. that works
func (s *snapmgrTestSuite) TestSeqMissedOne(c *C) {
	s.testUpdateSequence(c, &opSeqOpts{before: []int{1, 2}, current: 2, via: 4, after: []int{1, 2, 4}})
}

// 5b. that fails during link
func (s *snapmgrTestSuite) TestSeqMissedOneFailure(c *C) {
	s.testUpdateFailureSequence(c, &opSeqOpts{before: []int{1, 2}, current: 2, via: 4})
}

// 5c. that fails after link
func (s *snapmgrTestSuite) TestSeqTotalMissedOneFailure(c *C) {
	// we don't lose a rev here because len(Seq) < 3 going in
	s.testTotalUpdateFailureSequence(c, &opSeqOpts{before: []int{1, 2}, current: 2, via: 4, after: []int{1, 2}})
}

// 6. an update that updates to a revision we already have ("ABA update")
// 6a. that works
func (s *snapmgrTestSuite) TestSeqABA(c *C) {
	s.testUpdateSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 3, via: 2, after: []int{1, 3, 2}})
	c.Check(s.fakeBackend.ops[len(s.fakeBackend.ops)-1], DeepEquals, fakeOp{
		op:    "cleanup-trash",
		name:  "some-snap",
		revno: snap.R(2),
	})
}

// 6b. that fails during link
func (s *snapmgrTestSuite) TestSeqABAFailure(c *C) {
	s.testUpdateFailureSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 3, via: 2})
	c.Check(s.fakeBackend.ops.First("cleanup-trash"), IsNil)
}

// 6c that fails after link
func (s *snapmgrTestSuite) TestSeqTotalABAFailure(c *C) {
	// we don't lose a rev here because ABA
	s.testTotalUpdateFailureSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 3, via: 2, after: []int{1, 2, 3}})
	// XXX: TODO: NOTE!! WARNING!! etc
	//
	// if this happens in real life, things will be weird. revno 2 will
	// have data that has been copied from 3, instead of old 2's data,
	// because the failure occurred *after* nuking the trash. This can
	// happen when things are chained. Because of this, if it were to
	// *actually* happen the correct end sequence would be [1, 3] and not
	// [1, 2, 3]. IRL this scenario can happen if an update that works is
	// chained to an update that fails. Detecting this case is rather hard,
	// and the end result is not nice, and we want to move cleanup to a
	// separate handler & status that will cope with this better (so trash
	// gets nuked after all tasks succeeded).
}

func (s *snapmgrTestSuite) TestUpdateTasksWithOldCurrent(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si1 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}
	si2 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)}
	si3 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)}
	si4 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{si1, si2, si3, si4},
		Current:  snap.R(2),
		SnapType: "app",
	})

	// run the update
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyInstallUpdateTasks(c, unlinkBefore|cleanupAfter, 2, ts, s.state)

	// and ensure that it will remove the revisions after "current"
	// (si3, si4)
	var snapsup snapstate.SnapSetup
	tasks := ts.Tasks()

	i := len(tasks) - 6
	c.Check(tasks[i].Kind(), Equals, "clear-snap")
	err = tasks[i].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.Revision(), Equals, si3.Revision)

	i = len(tasks) - 4
	c.Check(tasks[i].Kind(), Equals, "clear-snap")
	err = tasks[i].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.Revision(), Equals, si4.Revision)
}

func (s *snapmgrTestSuite) TestUpdateCanDoBackwards(c *C) {
	si7 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	si11 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(11),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si7, &si11},
		Current:  si11.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "", snap.R(7), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()
	expected := fakeOps{
		{
			op:   "stop-snap-services",
			name: "/snap/some-snap/11",
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/11",
		},
		{
			op:   "copy-data",
			name: "/snap/some-snap/7",
			old:  "/snap/some-snap/11",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "",
				Revision: snap.R(7),
			},
		},
		{
			op:   "link-snap",
			name: "/snap/some-snap/7",
		},
		{
			op: "update-aliases",
		},
		{
			op:   "start-snap-services",
			name: "/snap/some-snap/7",
		},
		{
			op:    "cleanup-trash",
			name:  "some-snap",
			revno: snap.R(7),
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)
}

func (s *snapmgrTestSuite) TestSnapStateNoLocalRevision(c *C) {
	si7 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(-7),
	}
	si11 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(-11),
	}
	snapst := &snapstate.SnapState{
		Sequence: []*snap.SideInfo{&si7, &si11},
		Current:  si7.Revision,
	}
	c.Assert(snapst.LocalRevision(), Equals, snap.R(-11))
}

func (s *snapmgrTestSuite) TestSnapStateLocalRevision(c *C) {
	si7 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}
	snapst := &snapstate.SnapState{
		Sequence: []*snap.SideInfo{&si7},
		Current:  si7.Revision,
	}
	c.Assert(snapst.LocalRevision().Unset(), Equals, true)
}

func (s *snapmgrTestSuite) TestInstallMany(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	installed, tts, err := snapstate.InstallMany(s.state, []string{"one", "two"}, 0)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 2)
	c.Check(installed, DeepEquals, []string{"one", "two"})

	for _, ts := range tts {
		verifyInstallUpdateTasks(c, 0, 0, ts, s.state)
	}
}

func (s *snapmgrTestSuite) TestRemoveMany(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "one", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "one", SnapID: "one-id", Revision: snap.R(1)},
		},
		Current: snap.R(1),
	})
	snapstate.Set(s.state, "two", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "two", SnapID: "two-id", Revision: snap.R(1)},
		},
		Current: snap.R(1),
	})

	removed, tts, err := snapstate.RemoveMany(s.state, []string{"one", "two"})
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 2)
	c.Check(removed, DeepEquals, []string{"one", "two"})

	c.Assert(s.state.TaskCount(), Equals, 8*2)
	for _, ts := range tts {
		c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
			"stop-snap-services",
			"remove-aliases",
			"unlink-snap",
			"remove-profiles",
			"clear-snap",
			"discard-snap",
			"clear-aliases",
			"discard-conns",
		})
	}
}

func taskWithKind(ts *state.TaskSet, kind string) *state.Task {
	for _, task := range ts.Tasks() {
		if task.Kind() == kind {
			return task
		}
	}
	return nil
}

var gadgetYaml = `
defaults:
    some-snap-id:
        key: value

volumes:
    volume-id:
        bootloader: grub
`

func (s *snapmgrTestSuite) prepareGadget(c *C) {
	gadgetSideInfo := &snap.SideInfo{RealName: "the-gadget", SnapID: "the-gadget-id", Revision: snap.R(1)}
	gadgetInfo := snaptest.MockSnap(c, `
name: the-gadget
type: gadget
version: 1.0
`, "", gadgetSideInfo)

	err := ioutil.WriteFile(filepath.Join(gadgetInfo.MountDir(), "meta/gadget.yaml"), []byte(gadgetYaml), 0600)
	c.Assert(err, IsNil)

	snapstate.Set(s.state, "the-gadget", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&gadgetInfo.SideInfo},
		Current:  snap.R(1),
		SnapType: "gadget",
	})
}

func (s *snapmgrTestSuite) TestGadgetDefaults(c *C) {
	r := release.MockOnClassic(false)
	defer r()
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// using MockSnap, we want to read the bits on disk
	snapstate.MockReadInfo(snap.ReadInfo)

	s.state.Lock()
	defer s.state.Unlock()

	s.prepareGadget(c)

	snapPath := makeTestSnap(c, "name: some-snap\nversion: 1.0")

	ts, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}, snapPath, "edge", snapstate.Flags{})
	c.Assert(err, IsNil)

	var m map[string]interface{}
	runHook := taskWithKind(ts, "run-hook")
	c.Assert(runHook.Kind(), Equals, "run-hook")
	err = runHook.Get("hook-context", &m)
	c.Assert(err, IsNil)
	c.Assert(m["patch"], DeepEquals, map[string]interface{}{"key": "value"})
}

func (s *snapmgrTestSuite) TestGadgetDefaultsInstalled(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// using MockSnap, we want to read the bits on disk
	snapstate.MockReadInfo(snap.ReadInfo)

	s.state.Lock()
	defer s.state.Unlock()

	s.prepareGadget(c)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "app",
	})

	snapPath := makeTestSnap(c, "name: some-snap\nversion: 1.0")

	ts, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)}, snapPath, "edge", snapstate.Flags{})
	c.Assert(err, IsNil)

	var m map[string]interface{}
	runHook := taskWithKind(ts, "run-hook")
	c.Assert(runHook.Kind(), Equals, "run-hook")
	err = runHook.Get("hook-context", &m)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestTransitionCoreTasksNoUbuntuCore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "corecore", SnapID: "core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
	})

	_, err := snapstate.TransitionCore(s.state, "ubuntu-core", "core")
	c.Assert(err, ErrorMatches, `cannot transition snap "ubuntu-core": not installed`)
}

func verifyTransitionConnectionsTasks(c *C, ts *state.TaskSet) {
	c.Check(taskKinds(ts.Tasks()), DeepEquals, []string{
		"transition-ubuntu-core",
	})

	transIf := ts.Tasks()[0]
	var oldName, newName string
	err := transIf.Get("old-name", &oldName)
	c.Assert(err, IsNil)
	c.Check(oldName, Equals, "ubuntu-core")

	err = transIf.Get("new-name", &newName)
	c.Assert(err, IsNil)
	c.Check(newName, Equals, "core")
}

func (s *snapmgrTestSuite) TestTransitionCoreTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "ubuntu-core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "ubuntu-core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
	})

	tsl, err := snapstate.TransitionCore(s.state, "ubuntu-core", "core")
	c.Assert(err, IsNil)

	c.Assert(tsl, HasLen, 3)
	// 1. install core
	verifyInstallUpdateTasks(c, maybeCore, 0, tsl[0], s.state)
	// 2 transition-connections
	verifyTransitionConnectionsTasks(c, tsl[1])
	// 3 remove-ubuntu-core
	verifyRemoveTasks(c, tsl[2])
}

func (s *snapmgrTestSuite) TestTransitionCoreTasksWithUbuntuCoreAndCore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "ubuntu-core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "ubuntu-core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
	})
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "ubuntu-core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
	})

	tsl, err := snapstate.TransitionCore(s.state, "ubuntu-core", "core")
	c.Assert(err, IsNil)

	c.Assert(tsl, HasLen, 2)
	// 1. transition connections
	verifyTransitionConnectionsTasks(c, tsl[0])
	// 2. remove ubuntu-core
	verifyRemoveTasks(c, tsl[1])
}

func (s *snapmgrTestSuite) TestTransitionCoreRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "ubuntu-core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "ubuntu-core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
	})

	chg := s.state.NewChange("transition-ubuntu-core", "...")
	tsl, err := snapstate.TransitionCore(s.state, "ubuntu-core", "core")
	c.Assert(err, IsNil)
	for _, ts := range tsl {
		chg.AddAll(ts)
	}

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		name: "core",
		// the transition has no user associcated with it
		macaroon: "",
	}})
	expected := fakeOps{
		{
			op:    "storesvc-snap",
			name:  "core",
			revno: snap.R(11),
		},
		{
			op:   "storesvc-download",
			name: "core",
		},
		{
			op:    "validate-snap:Doing",
			name:  "core",
			revno: snap.R(11),
		},
		{
			op:  "current",
			old: "<no-current>",
		},
		{
			op:   "open-snap-file",
			name: "/var/lib/snapd/snaps/core_11.snap",
			sinfo: snap.SideInfo{
				RealName: "core",
				SnapID:   "snapIDsnapidsnapidsnapidsnapidsn",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  "/var/lib/snapd/snaps/core_11.snap",
			revno: snap.R(11),
		},
		{
			op:   "copy-data",
			name: "/snap/core/11",
			old:  "<no-old>",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "core",
			revno: snap.R(11),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "core",
				SnapID:   "snapIDsnapidsnapidsnapidsnapidsn",
				Revision: snap.R(11),
			},
		},
		{
			op:   "link-snap",
			name: "/snap/core/11",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "core",
			revno: snap.R(11),
		},
		{
			op: "update-aliases",
		},
		{
			op:   "start-snap-services",
			name: "/snap/core/11",
		},
		{
			op:   "transition-ubuntu-core:Doing",
			name: "ubuntu-core",
		},
		{
			op:   "stop-snap-services",
			name: "/snap/ubuntu-core/1",
		},
		{
			op:   "remove-snap-aliases",
			name: "ubuntu-core",
		},
		{
			op:   "unlink-snap",
			name: "/snap/ubuntu-core/1",
		},
		{
			op:    "remove-profiles:Doing",
			name:  "ubuntu-core",
			revno: snap.R(1),
		},
		{
			op:   "remove-snap-data",
			name: "/snap/ubuntu-core/1",
		},
		{
			op:   "remove-snap-common-data",
			name: "/snap/ubuntu-core/1",
		},
		{
			op:    "remove-snap-files",
			name:  "/snap/ubuntu-core/1",
			stype: "os",
		},
		{
			op:   "discard-namespace",
			name: "ubuntu-core",
		},
		{
			op:   "discard-conns:Doing",
			name: "ubuntu-core",
		},
		{
			op:    "cleanup-trash",
			name:  "core",
			revno: snap.R(11),
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)
}
func (s *snapmgrTestSuite) TestTransitionCoreRunThroughWithCore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "ubuntu-core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "ubuntu-core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
	})
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "core", SnapID: "core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
	})

	chg := s.state.NewChange("transition-ubuntu-core", "...")
	tsl, err := snapstate.TransitionCore(s.state, "ubuntu-core", "core")
	c.Assert(err, IsNil)
	for _, ts := range tsl {
		chg.AddAll(ts)
	}

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(s.fakeStore.downloads, HasLen, 0)
	expected := fakeOps{
		{
			op:    "storesvc-snap",
			name:  "core",
			revno: snap.R(11),
		},
		{
			op:   "transition-ubuntu-core:Doing",
			name: "ubuntu-core",
		},
		{
			op:   "stop-snap-services",
			name: "/snap/ubuntu-core/1",
		},
		{
			op:   "remove-snap-aliases",
			name: "ubuntu-core",
		},
		{
			op:   "unlink-snap",
			name: "/snap/ubuntu-core/1",
		},
		{
			op:    "remove-profiles:Doing",
			name:  "ubuntu-core",
			revno: snap.R(1),
		},
		{
			op:   "remove-snap-data",
			name: "/snap/ubuntu-core/1",
		},
		{
			op:   "remove-snap-common-data",
			name: "/snap/ubuntu-core/1",
		},
		{
			op:    "remove-snap-files",
			name:  "/snap/ubuntu-core/1",
			stype: "os",
		},
		{
			op:   "discard-namespace",
			name: "ubuntu-core",
		},
		{
			op:   "discard-conns:Doing",
			name: "ubuntu-core",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

}

func (s *snapmgrTestSuite) TestTransitionCoreStartsAutomatically(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "ubuntu-core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "corecore", SnapID: "core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
	})

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	c.Check(s.state.Changes(), HasLen, 1)
	c.Check(s.state.Changes()[0].Kind(), Equals, "transition-ubuntu-core")
}

func (s *snapmgrTestSuite) TestTransitionCoreTimeLimitWorks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "ubuntu-core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "corecore", SnapID: "core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
	})

	// tried 3h ago, no retry
	s.state.Set("ubuntu-core-transition-last-retry-time", time.Now().Add(-3*time.Hour))

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	c.Check(s.state.Changes(), HasLen, 0)

	// tried 7h ago, retry
	s.state.Set("ubuntu-core-transition-last-retry-time", time.Now().Add(-7*time.Hour))

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()
	c.Check(s.state.Changes(), HasLen, 1)

	var t time.Time
	s.state.Get("ubuntu-core-transition-last-retry-time", &t)
	c.Assert(time.Now().Sub(t) < 2*time.Minute, Equals, true)
}

func (s *snapmgrTestSuite) TestTransitionCoreNoOtherChanges(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "ubuntu-core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "corecore", SnapID: "core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
	})
	chg := s.state.NewChange("unrelated-change", "unfinished change blocks core transition")
	chg.SetStatus(state.DoStatus)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	c.Check(s.state.Changes(), HasLen, 1)
	c.Check(s.state.Changes()[0].Kind(), Equals, "unrelated-change")
}

func (s *snapmgrTestSuite) TestTransitionCoreBlocksOtherChanges(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// if we have a ubuntu-core -> core transition
	chg := s.state.NewChange("transition-ubuntu-core", "...")
	chg.SetStatus(state.DoStatus)

	// other tasks block until the transition is done
	_, err := snapstate.Install(s.state, "some-snap", "stable", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Check(err, ErrorMatches, "ubuntu-core to core transition in progress, no other changes allowed until this is done")

	// and when the transition is done, other tasks run
	chg.SetStatus(state.DoneStatus)
	ts, err := snapstate.Install(s.state, "some-snap", "stable", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Check(err, IsNil)
	c.Check(ts, NotNil)
}

func (s *snapmgrTestSuite) TestForceDevModeCleanupRunsForUbuntuCore(c *C) {
	s.checkForceDevModeCleanupRuns(c, "ubuntu-core", true)
}

func (s *snapmgrTestSuite) TestForceDevModeCleanupRunsForCore(c *C) {
	s.checkForceDevModeCleanupRuns(c, "core", true)
}

func (s *snapmgrTestSuite) TestForceDevModeCleanupSkipsRando(c *C) {
	s.checkForceDevModeCleanupRuns(c, "rando", false)
}

func (s *snapmgrTestSuite) checkForceDevModeCleanupRuns(c *C, name string, shouldBeReset bool) {
	r := release.MockForcedDevmode(true)
	defer r()
	c.Assert(release.ReleaseInfo.ForceDevMode(), Equals, true)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, name, &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{{
			RealName: name,
			SnapID:   "id-id-id",
			Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
		Flags:    snapstate.Flags{DevMode: true},
	})

	var snapst1 snapstate.SnapState
	// sanity check
	snapstate.Get(s.state, name, &snapst1)
	c.Assert(snapst1.DevMode, Equals, true)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	var snapst2 snapstate.SnapState
	snapstate.Get(s.state, name, &snapst2)

	c.Check(snapst2.DevMode, Equals, !shouldBeReset)

	var n int
	s.state.Get("fix-forced-devmode", &n)
	c.Check(n, Equals, 1)
}

func (s *snapmgrTestSuite) TestForceDevModeCleanupRunsNoSnaps(c *C) {
	r := release.MockForcedDevmode(true)
	defer r()
	c.Assert(release.ReleaseInfo.ForceDevMode(), Equals, true)

	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()
	defer s.state.Unlock()

	var n int
	s.state.Get("fix-forced-devmode", &n)
	c.Check(n, Equals, 1)
}

func (s *snapmgrTestSuite) TestForceDevModeCleanupSkipsNonForcedOS(c *C) {
	r := release.MockForcedDevmode(false)
	defer r()
	c.Assert(release.ReleaseInfo.ForceDevMode(), Equals, false)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{{
			RealName: "core",
			SnapID:   "id-id-id",
			Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
		Flags:    snapstate.Flags{DevMode: true},
	})

	var snapst1 snapstate.SnapState
	// sanity check
	snapstate.Get(s.state, "core", &snapst1)
	c.Assert(snapst1.DevMode, Equals, true)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	var snapst2 snapstate.SnapState
	snapstate.Get(s.state, "core", &snapst2)

	// no change
	c.Check(snapst2.DevMode, Equals, true)

	// not really run at all in fact
	var n int
	s.state.Get("fix-forced-devmode", &n)
	c.Check(n, Equals, 0)
}

type canDisableSuite struct{}

var _ = Suite(&canDisableSuite{})

func (s *canDisableSuite) TestCanDisable(c *C) {
	for _, tt := range []struct {
		typ        snap.Type
		canDisable bool
	}{
		{snap.TypeApp, true},
		{snap.TypeGadget, false},
		{snap.TypeKernel, false},
		{snap.TypeOS, false},
	} {
		info := &snap.Info{Type: tt.typ}
		c.Check(snapstate.CanDisable(info), Equals, tt.canDisable)
	}
}
