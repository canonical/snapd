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
	"fmt"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type linkSnapSuite struct {
	baseHandlerSuite

	stateBackend *witnessRestartReqStateBackend
}

var _ = Suite(&linkSnapSuite{})

type witnessRestartReqStateBackend struct {
	restartRequested []state.RestartType
}

func (b *witnessRestartReqStateBackend) Checkpoint([]byte) error {
	return nil
}

func (b *witnessRestartReqStateBackend) RequestRestart(t state.RestartType) {
	b.restartRequested = append(b.restartRequested, t)
}

func (b *witnessRestartReqStateBackend) EnsureBefore(time.Duration) {}

func (s *linkSnapSuite) SetUpTest(c *C) {
	s.stateBackend = &witnessRestartReqStateBackend{}

	s.setup(c, s.stateBackend)

	snapstate.MockModel()
}

func checkHasCookieForSnap(c *C, st *state.State, snapName string) {
	var contexts map[string]interface{}
	err := st.Get("snap-cookies", &contexts)
	c.Assert(err, IsNil)
	c.Check(contexts, HasLen, 1)

	for _, snap := range contexts {
		if snapName == snap {
			return
		}
	}
	panic(fmt.Sprintf("Cookie missing for snap %q", snapName))
}

func (s *linkSnapSuite) TestDoLinkSnapSuccess(c *C) {
	s.state.Lock()
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
		Channel: "beta",
		UserID:  2,
	})
	s.state.NewChange("dummy", "...").AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, IsNil)

	checkHasCookieForSnap(c, s.state, "foo")

	typ, err := snapst.Type()
	c.Check(err, IsNil)
	c.Check(typ, Equals, snap.TypeApp)

	c.Check(snapst.Active, Equals, true)
	c.Check(snapst.Sequence, HasLen, 1)
	c.Check(snapst.Current, Equals, snap.R(33))
	c.Check(snapst.Channel, Equals, "beta")
	c.Check(snapst.UserID, Equals, 2)
	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(s.stateBackend.restartRequested, HasLen, 0)
}

func (s *linkSnapSuite) TestDoLinkSnapSuccessNoUserID(c *C) {
	s.state.Lock()
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
		Channel: "beta",
	})
	s.state.NewChange("dummy", "...").AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()
	defer s.state.Unlock()

	// check that snapst.UserID does not get set
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.UserID, Equals, 0)

	var snaps map[string]*json.RawMessage
	err = s.state.Get("snaps", &snaps)
	c.Assert(err, IsNil)
	raw := []byte(*snaps["foo"])
	c.Check(string(raw), Not(testutil.Contains), "user-id")
}

func (s *linkSnapSuite) TestDoLinkSnapSuccessUserIDAlreadySet(c *C) {
	s.state.Lock()
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(1)},
		},
		Current: snap.R(1),
		UserID:  1,
	})
	// the user
	user, err := auth.NewUser(s.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	c.Assert(err, IsNil)
	c.Assert(user.ID, Equals, 1)

	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
		Channel: "beta",
		UserID:  2,
	})
	s.state.NewChange("dummy", "...").AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()
	defer s.state.Unlock()

	// check that snapst.UserID was not "transferred"
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.UserID, Equals, 1)
}

func (s *linkSnapSuite) TestDoLinkSnapSuccessUserLoggedOut(c *C) {
	s.state.Lock()
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(1)},
		},
		Current: snap.R(1),
		UserID:  1,
	})

	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
		Channel: "beta",
		UserID:  2,
	})
	s.state.NewChange("dummy", "...").AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()
	defer s.state.Unlock()

	// check that snapst.UserID was transferred
	// given that user 1 doesn't exist anymore
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.UserID, Equals, 2)
}

func (s *linkSnapSuite) TestDoUndoLinkSnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	si := &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(33),
	}
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Channel:  "beta",
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 6; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, Equals, state.ErrNoState)
	c.Check(t.Status(), Equals, state.UndoneStatus)
}

func (s *linkSnapSuite) TestDoLinkSnapTryToCleanupOnError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	si := &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(35),
	}
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Channel:  "beta",
	})

	s.fakeBackend.linkSnapFailTrigger = filepath.Join(dirs.SnapMountDir, "foo/35")
	s.state.NewChange("dummy", "...").AddTask(t)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	// state as expected
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, Equals, state.ErrNoState)

	// tried to cleanup
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:    "candidate",
			sinfo: *si,
		},
		{
			op:   "link-snap.failed",
			path: filepath.Join(dirs.SnapMountDir, "foo/35"),
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "foo/35"),
		},
	})
}

func (s *linkSnapSuite) TestDoLinkSnapSuccessCoreRestarts(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.state.Lock()
	si := &snap.SideInfo{
		RealName: "core",
		Revision: snap.R(33),
	}
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
	})
	s.state.NewChange("dummy", "...").AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "core", &snapst)
	c.Assert(err, IsNil)

	typ, err := snapst.Type()
	c.Check(err, IsNil)
	c.Check(typ, Equals, snap.TypeOS)

	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(s.stateBackend.restartRequested, DeepEquals, []state.RestartType{state.RestartDaemon})
	c.Check(t.Log(), HasLen, 1)
	c.Check(t.Log()[0], Matches, `.*INFO Requested daemon restart\.`)
}

func (s *linkSnapSuite) TestDoLinkSnapSuccessSnapdRestartsOnCoreWithBase(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = snapstate.MockModelWithBase("core18")
	defer restore()

	s.state.Lock()
	si := &snap.SideInfo{
		RealName: "snapd",
		Revision: snap.R(22),
	}
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
	})
	s.state.NewChange("dummy", "...").AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "snapd", &snapst)
	c.Assert(err, IsNil)

	typ, err := snapst.Type()
	c.Check(err, IsNil)
	c.Check(typ, Equals, snap.TypeApp)

	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(s.stateBackend.restartRequested, DeepEquals, []state.RestartType{state.RestartDaemon})
	c.Check(t.Log(), HasLen, 1)
	c.Check(t.Log()[0], Matches, `.*INFO Requested daemon restart \(snapd snap\)\.`)
}

func (s *linkSnapSuite) TestDoLinkSnapSuccessSnapdNoRestartWithoutBase(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.state.Lock()
	si := &snap.SideInfo{
		RealName: "snapd",
		Revision: snap.R(22),
	}
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
	})
	s.state.NewChange("dummy", "...").AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "snapd", &snapst)
	c.Assert(err, IsNil)

	typ, err := snapst.Type()
	c.Check(err, IsNil)
	c.Check(typ, Equals, snap.TypeApp)

	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(s.stateBackend.restartRequested, IsNil)
	c.Check(t.Log(), HasLen, 0)
}

func (s *linkSnapSuite) TestDoLinkSnapSuccessSnapdNoRestartOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.state.Lock()
	si := &snap.SideInfo{
		RealName: "snapd",
		Revision: snap.R(22),
	}
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
	})
	s.state.NewChange("dummy", "...").AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "snapd", &snapst)
	c.Assert(err, IsNil)

	typ, err := snapst.Type()
	c.Check(err, IsNil)
	c.Check(typ, Equals, snap.TypeApp)

	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(s.stateBackend.restartRequested, IsNil)
	c.Check(t.Log(), HasLen, 0)
}

func (s *linkSnapSuite) TestDoUndoLinkSnapSequenceDidNotHaveCandidate(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	si1 := &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(1),
	}
	si2 := &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(2),
	}
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{si1},
		Current:  si1.Revision,
	})
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si2,
		Channel:  "beta",
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 6; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Active, Equals, false)
	c.Check(snapst.Sequence, HasLen, 1)
	c.Check(snapst.Current, Equals, snap.R(1))
	c.Check(t.Status(), Equals, state.UndoneStatus)
}

func (s *linkSnapSuite) TestDoUndoLinkSnapSequenceHadCandidate(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	si1 := &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(1),
	}
	si2 := &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(2),
	}
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{si1, si2},
		Current:  si2.Revision,
	})
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si1,
		Channel:  "beta",
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 6; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Active, Equals, false)
	c.Check(snapst.Sequence, HasLen, 2)
	c.Check(snapst.Current, Equals, snap.R(2))
	c.Check(t.Status(), Equals, state.UndoneStatus)
}

func (s *linkSnapSuite) TestDoUndoUnlinkCurrentSnapCore(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()
	si1 := &snap.SideInfo{
		RealName: "core",
		Revision: snap.R(1),
	}
	si2 := &snap.SideInfo{
		RealName: "core",
		Revision: snap.R(2),
	}
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{si1},
		Current:  si1.Revision,
		Active:   true,
		SnapType: "os",
	})
	t := s.state.NewTask("unlink-current-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si2,
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "core", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Active, Equals, true)
	c.Check(snapst.Sequence, HasLen, 1)
	c.Check(snapst.Current, Equals, snap.R(1))
	c.Check(t.Status(), Equals, state.UndoneStatus)

	c.Check(s.stateBackend.restartRequested, DeepEquals, []state.RestartType{state.RestartDaemon})
}

func (s *linkSnapSuite) TestDoUndoLinkSnapCoreClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// no previous core snap and an error on link, in this
	// case we need to restart on classic back into the distro
	// package version
	si1 := &snap.SideInfo{
		RealName: "core",
		Revision: snap.R(1),
	}
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si1,
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "core", &snapst)
	c.Assert(err, Equals, state.ErrNoState)
	c.Check(t.Status(), Equals, state.UndoneStatus)

	c.Check(s.stateBackend.restartRequested, DeepEquals, []state.RestartType{state.RestartDaemon, state.RestartDaemon})

}

func (s *linkSnapSuite) TestLinkSnapInjectsAutoConnectIfMissing(c *C) {
	si1 := &snap.SideInfo{
		RealName: "snap1",
		Revision: snap.R(1),
	}
	sup1 := &snapstate.SnapSetup{SideInfo: si1}
	si2 := &snap.SideInfo{
		RealName: "snap2",
		Revision: snap.R(1),
	}
	sup2 := &snapstate.SnapSetup{SideInfo: si2}

	s.state.Lock()
	defer s.state.Unlock()

	task0 := s.state.NewTask("setup-profiles", "")
	task1 := s.state.NewTask("link-snap", "")
	task1.WaitFor(task0)
	task0.Set("snap-setup", sup1)
	task1.Set("snap-setup", sup1)

	task2 := s.state.NewTask("setup-profiles", "")
	task3 := s.state.NewTask("link-snap", "")
	task2.WaitFor(task1)
	task3.WaitFor(task2)
	task2.Set("snap-setup", sup2)
	task3.Set("snap-setup", sup2)

	chg := s.state.NewChange("test", "")
	chg.AddTask(task0)
	chg.AddTask(task1)
	chg.AddTask(task2)
	chg.AddTask(task3)

	s.state.Unlock()

	for i := 0; i < 10; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Tasks(), HasLen, 6)

	// sanity checks
	t := chg.Tasks()[1]
	c.Assert(t.Kind(), Equals, "link-snap")
	t = chg.Tasks()[3]
	c.Assert(t.Kind(), Equals, "link-snap")

	// check that auto-connect tasks were added and have snap-setup
	var autoconnectSup snapstate.SnapSetup
	t = chg.Tasks()[4]
	c.Assert(t.Kind(), Equals, "auto-connect")
	c.Assert(t.Get("snap-setup", &autoconnectSup), IsNil)
	c.Assert(autoconnectSup.Name(), Equals, "snap1")

	t = chg.Tasks()[5]
	c.Assert(t.Kind(), Equals, "auto-connect")
	c.Assert(t.Get("snap-setup", &autoconnectSup), IsNil)
	c.Assert(autoconnectSup.Name(), Equals, "snap2")
}
