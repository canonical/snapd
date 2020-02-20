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
	"io/ioutil"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
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

	s.AddCleanup(snapstatetest.MockDeviceModel(DefaultModel()))
	s.AddCleanup(snap.MockSnapdSnapID("snapd-snap-id"))
}

func checkHasCookieForSnap(c *C, st *state.State, instanceName string) {
	var contexts map[string]interface{}
	err := st.Get("snap-cookies", &contexts)
	c.Assert(err, IsNil)
	c.Check(contexts, HasLen, 1)

	for _, snap := range contexts {
		if instanceName == snap {
			return
		}
	}
	panic(fmt.Sprintf("Cookie missing for snap %q", instanceName))
}

func (s *linkSnapSuite) TestDoLinkSnapSuccess(c *C) {
	// we start without the auxiliary store info
	c.Check(snapstate.AuxStoreInfoFilename("foo-id"), testutil.FileAbsent)

	s.state.Lock()
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
			SnapID:   "foo-id",
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
	c.Check(snapst.TrackingChannel, Equals, "latest/beta")
	c.Check(snapst.UserID, Equals, 2)
	c.Check(snapst.CohortKey, Equals, "")
	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(s.stateBackend.restartRequested, HasLen, 0)

	// we end with the auxiliary store info
	c.Check(snapstate.AuxStoreInfoFilename("foo-id"), testutil.FilePresent)
}

func (s *linkSnapSuite) TestDoLinkSnapSuccessWithCohort(c *C) {
	// we start without the auxiliary store info
	c.Check(snapstate.AuxStoreInfoFilename("foo-id"), testutil.FileAbsent)

	s.state.Lock()
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
			SnapID:   "foo-id",
		},
		Channel:   "beta",
		UserID:    2,
		CohortKey: "wobbling",
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
	c.Check(snapst.TrackingChannel, Equals, "latest/beta")
	c.Check(snapst.UserID, Equals, 2)
	c.Check(snapst.CohortKey, Equals, "wobbling")
	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(s.stateBackend.restartRequested, HasLen, 0)

	// we end with the auxiliary store info
	c.Check(snapstate.AuxStoreInfoFilename("foo-id"), testutil.FilePresent)
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

func (s *linkSnapSuite) TestDoLinkSnapSeqFile(c *C) {
	s.state.Lock()
	// pretend we have an installed snap
	si11 := &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(11),
	}
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{si11},
		Current:  si11.Revision,
	})
	// add a new one
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
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, IsNil)

	// and check that the sequence file got updated
	seqContent, err := ioutil.ReadFile(filepath.Join(dirs.SnapSeqDir, "foo.json"))
	c.Assert(err, IsNil)
	c.Check(string(seqContent), Equals, `{"sequence":[{"name":"foo","snap-id":"","revision":"11"},{"name":"foo","snap-id":"","revision":"33"}],"current":"33"}`)
}

func (s *linkSnapSuite) TestDoUndoLinkSnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	// a hook might have set some config
	cfg := json.RawMessage(`{"c":true}`)
	err := config.SetSnapConfig(s.state, "foo", &cfg)
	c.Assert(err, IsNil)

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
	err = snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, Equals, state.ErrNoState)
	c.Check(t.Status(), Equals, state.UndoneStatus)

	// and check that the sequence file got updated
	seqContent, err := ioutil.ReadFile(filepath.Join(dirs.SnapSeqDir, "foo.json"))
	c.Assert(err, IsNil)
	c.Check(string(seqContent), Equals, `{"sequence":[],"current":"unset"}`)

	// nothing in config
	var config map[string]*json.RawMessage
	err = s.state.Get("config", &config)
	c.Assert(err, IsNil)
	c.Check(config, HasLen, 1)
	_, ok := config["core"]
	c.Check(ok, Equals, true)
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
	expected := fakeOps{
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

			unlinkFirstInstallUndo: true,
		},
	}

	// start with an easier-to-read error if this fails:
	c.Check(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Check(s.fakeBackend.ops, DeepEquals, expected)
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

	r := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer r()

	s.state.Lock()
	si := &snap.SideInfo{
		RealName: "snapd",
		SnapID:   "snapd-snap-id",
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
	c.Check(typ, Equals, snap.TypeSnapd)

	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(s.stateBackend.restartRequested, DeepEquals, []state.RestartType{state.RestartDaemon})
	c.Check(t.Log(), HasLen, 1)
	c.Check(t.Log()[0], Matches, `.*INFO Requested daemon restart \(snapd snap\)\.`)
}

func (s *linkSnapSuite) TestDoLinkSnapSuccessRebootForCoreBase(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	r := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer r()

	s.fakeBackend.linkSnapMaybeReboot = true

	s.state.Lock()
	defer s.state.Unlock()

	// we need to init the boot-id
	err := s.state.VerifyReboot("some-boot-id")
	c.Assert(err, IsNil)

	si := &snap.SideInfo{
		RealName: "core18",
		SnapID:   "core18-id",
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

	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(s.stateBackend.restartRequested, DeepEquals, []state.RestartType{state.RestartSystem})
	c.Assert(t.Log(), HasLen, 1)
	c.Check(t.Log()[0], Matches, `.*INFO Requested system restart.*`)
}

func (s *linkSnapSuite) TestDoLinkSnapSuccessSnapdRestartsOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.state.Lock()
	si := &snap.SideInfo{
		RealName: "snapd",
		SnapID:   "snapd-snap-id",
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
	c.Check(typ, Equals, snap.TypeSnapd)

	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(s.stateBackend.restartRequested, DeepEquals, []state.RestartType{state.RestartDaemon})
	c.Check(t.Log(), HasLen, 1)
}

func (s *linkSnapSuite) TestDoLinkSnapSuccessCoreAndSnapdNoCoreRestart(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.state.Lock()
	siSnapd := &snap.SideInfo{
		RealName: "snapd",
		Revision: snap.R(64),
	}
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{siSnapd},
		Current:  siSnapd.Revision,
		Active:   true,
	})

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

func (s *linkSnapSuite) TestDoUndoUnlinkCurrentSnapCoreBase(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	r := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer r()

	s.fakeBackend.linkSnapMaybeReboot = true

	s.state.Lock()
	defer s.state.Unlock()
	// we need to init the boot-id
	err := s.state.VerifyReboot("some-boot-id")
	c.Assert(err, IsNil)

	si1 := &snap.SideInfo{
		RealName: "core18",
		Revision: snap.R(1),
	}
	si2 := &snap.SideInfo{
		RealName: "core18",
		Revision: snap.R(2),
	}
	snapstate.Set(s.state, "core18", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{si1},
		Current:  si1.Revision,
		Active:   true,
		SnapType: "base",
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
	err = snapstate.Get(s.state, "core18", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Active, Equals, true)
	c.Check(snapst.Sequence, HasLen, 1)
	c.Check(snapst.Current, Equals, snap.R(1))
	c.Check(t.Status(), Equals, state.UndoneStatus)

	c.Check(s.stateBackend.restartRequested, DeepEquals, []state.RestartType{state.RestartSystem})
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
	c.Assert(autoconnectSup.InstanceName(), Equals, "snap1")

	t = chg.Tasks()[5]
	c.Assert(t.Kind(), Equals, "auto-connect")
	c.Assert(t.Get("snap-setup", &autoconnectSup), IsNil)
	c.Assert(autoconnectSup.InstanceName(), Equals, "snap2")
}

func (s *linkSnapSuite) TestDoLinkSnapFailureCleansUpAux(c *C) {
	// this is very chummy with the order of LinkSnap
	c.Assert(ioutil.WriteFile(dirs.SnapSeqDir, nil, 0644), IsNil)

	// we start without the auxiliary store info
	c.Check(snapstate.AuxStoreInfoFilename("foo-id"), testutil.FileAbsent)

	s.state.Lock()
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
			SnapID:   "foo-id",
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

	c.Check(t.Status(), Equals, state.ErrorStatus)
	c.Check(s.stateBackend.restartRequested, HasLen, 0)

	// we end without the auxiliary store info
	c.Check(snapstate.AuxStoreInfoFilename("foo-id"), testutil.FileAbsent)
}

func (s *linkSnapSuite) TestLinkSnapResetsRefreshInhibitedTime(c *C) {
	// When a snap is linked the refresh-inhibited-time is reset to zero
	// to indicate a successful refresh. The old value is stored in task
	// state for task undo logic.
	s.state.Lock()
	defer s.state.Unlock()

	instant := time.Now()

	si := &snap.SideInfo{RealName: "snap", Revision: snap.R(1)}
	sup := &snapstate.SnapSetup{SideInfo: si}
	snapstate.Set(s.state, "snap", &snapstate.SnapState{
		Sequence:             []*snap.SideInfo{si},
		Current:              si.Revision,
		RefreshInhibitedTime: &instant,
	})

	task := s.state.NewTask("link-snap", "")
	task.Set("snap-setup", sup)
	chg := s.state.NewChange("test", "")
	chg.AddTask(task)

	s.state.Unlock()

	for i := 0; i < 10; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Tasks(), HasLen, 1)

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.RefreshInhibitedTime, IsNil)

	var oldTime time.Time
	c.Assert(task.Get("old-refresh-inhibited-time", &oldTime), IsNil)
	c.Check(oldTime.Equal(instant), Equals, true)
}

func (s *linkSnapSuite) TestDoUndoLinkSnapRestoresRefreshInhibitedTime(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	instant := time.Now()

	si := &snap.SideInfo{RealName: "snap", Revision: snap.R(1)}
	sup := &snapstate.SnapSetup{SideInfo: si}
	snapstate.Set(s.state, "snap", &snapstate.SnapState{
		Sequence:             []*snap.SideInfo{si},
		Current:              si.Revision,
		RefreshInhibitedTime: &instant,
	})

	task := s.state.NewTask("link-snap", "")
	task.Set("snap-setup", sup)
	chg := s.state.NewChange("test", "")
	chg.AddTask(task)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(task)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 6; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Assert(chg.Err(), NotNil)
	c.Assert(chg.Tasks(), HasLen, 2)
	c.Check(task.Status(), Equals, state.UndoneStatus)

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.RefreshInhibitedTime.Equal(instant), Equals, true)
}

func (s *linkSnapSuite) TestUndoLinkSnapdFirstInstall(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.state.Lock()
	si := &snap.SideInfo{
		RealName: "snapd",
		SnapID:   "snapd-snap-id",
		Revision: snap.R(22),
	}
	chg := s.state.NewChange("dummy", "...")
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Type:     snap.TypeSnapd,
	})
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
	defer s.state.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "snapd", &snapst)
	c.Assert(err, Equals, state.ErrNoState)
	c.Check(t.Status(), Equals, state.UndoneStatus)

	expected := fakeOps{
		{
			op:    "candidate",
			sinfo: *si,
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "snapd/22"),
		},
		{
			op:   "discard-namespace",
			name: "snapd",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "snapd/22"),

			unlinkFirstInstallUndo: true,
		},
	}

	// start with an easier-to-read error if this fails:
	c.Check(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Check(s.fakeBackend.ops, DeepEquals, expected)

	// 2 restarts, one from link snap, another one from undo
	c.Check(s.stateBackend.restartRequested, DeepEquals, []state.RestartType{state.RestartDaemon, state.RestartDaemon})
	c.Check(t.Log(), HasLen, 3)
	c.Check(t.Log()[0], Matches, `.*INFO Requested daemon restart \(snapd snap\)\.`)
	c.Check(t.Log()[2], Matches, `.*INFO Requested daemon restart \(snapd snap\)\.`)

}

func (s *linkSnapSuite) TestUndoLinkSnapdNthInstall(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.state.Lock()
	si := &snap.SideInfo{
		RealName: "snapd",
		SnapID:   "snapd-snap-id",
		Revision: snap.R(22),
	}
	siOld := *si
	siOld.Revision = snap.R(20)
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{&siOld},
		Current:  siOld.Revision,
		Active:   true,
	})
	chg := s.state.NewChange("dummy", "...")
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Type:     snap.TypeSnapd,
	})
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
	defer s.state.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "snapd", &snapst)
	c.Assert(err, IsNil)
	snapst.Current = siOld.Revision
	c.Check(t.Status(), Equals, state.UndoneStatus)

	expected := fakeOps{
		{
			op:    "candidate",
			sinfo: *si,
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "snapd/22"),
		},
		{
			op: "current-snap-service-states",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "snapd/22"),
		},
	}

	// start with an easier-to-read error if this fails:
	c.Check(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Check(s.fakeBackend.ops, DeepEquals, expected)

	// 2 restarts, one from link snap, another one from undo
	c.Check(s.stateBackend.restartRequested, DeepEquals, []state.RestartType{state.RestartDaemon, state.RestartDaemon})
	c.Check(t.Log(), HasLen, 3)
	c.Check(t.Log()[0], Matches, `.*INFO Requested daemon restart \(snapd snap\)\.`)
	c.Check(t.Log()[1], Matches, `.*INFO unlink`)
	c.Check(t.Log()[2], Matches, `.*INFO Requested daemon restart \(snapd snap\)\.`)
}

func (s *linkSnapSuite) TestDoUnlinkSnapRefreshAwarenessHardCheck(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.refresh-app-awareness", true)
	tr.Commit()

	chg := s.testDoUnlinkSnapRefreshAwareness(c)

	c.Check(chg.Err(), ErrorMatches, `(?ms).*^- some-change-descr \(snap "some-snap" has running apps \(some-app\)\).*`)
}

func (s *linkSnapSuite) TestDoUnlinkSnapRefreshHardCheckOff(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.testDoUnlinkSnapRefreshAwareness(c)

	c.Check(chg.Err(), IsNil)
}

func (s *linkSnapSuite) testDoUnlinkSnapRefreshAwareness(c *C) *state.Change {
	restore := release.MockOnClassic(true)
	defer restore()
	mockPidsCgroupDir := c.MkDir()
	restore = snapstate.MockPidsCgroupDir(mockPidsCgroupDir)
	defer restore()

	// mock that "some-snap" has an app and that this app has pids running
	writePids(c, filepath.Join(mockPidsCgroupDir, "snap.some-snap.some-app"), []int{1234})
	snapstate.MockSnapReadInfo(func(name string, si *snap.SideInfo) (*snap.Info, error) {
		info := &snap.Info{SuggestedName: name, SideInfo: *si, SnapType: snap.TypeApp}
		info.Apps = map[string]*snap.AppInfo{
			"some-app": {Snap: info, Name: "some-app"},
		}
		return info, nil
	})

	si1 := &snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{si1},
		Current:  si1.Revision,
		Active:   true,
	})
	t := s.state.NewTask("unlink-current-snap", "some-change-descr")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si1,
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()
	defer s.state.Lock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	return chg
}

func (s *linkSnapSuite) setMockKernelRemodelCtx(c *C, oldKernel, newKernel string) {
	newModel := MakeModel(map[string]interface{}{"kernel": newKernel})
	oldModel := MakeModel(map[string]interface{}{"kernel": oldKernel})
	mockRemodelCtx := &snapstatetest.TrivialDeviceContext{
		DeviceModel:    newModel,
		OldDeviceModel: oldModel,
		Remodeling:     true,
	}
	restore := snapstatetest.MockDeviceContext(mockRemodelCtx)
	s.AddCleanup(restore)
}

func (s *linkSnapSuite) TestMaybeUndoRemodelBootChangesUnrelatedAppDoesNothing(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.setMockKernelRemodelCtx(c, "kernel", "new-kernel")
	t := s.state.NewTask("link-snap", "...")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "some-app",
			Revision: snap.R(1),
		},
	})

	err := snapstate.MaybeUndoRemodelBootChanges(t)
	c.Assert(err, IsNil)
	c.Check(s.stateBackend.restartRequested, HasLen, 0)
}

func (s *linkSnapSuite) TestMaybeUndoRemodelBootChangesSameKernel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.setMockKernelRemodelCtx(c, "kernel", "kernel")
	t := s.state.NewTask("link-snap", "...")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "kernel",
			Revision: snap.R(1),
		},
		Type: "kernel",
	})

	err := snapstate.MaybeUndoRemodelBootChanges(t)
	c.Assert(err, IsNil)
	c.Check(s.stateBackend.restartRequested, HasLen, 0)
}

func (s *linkSnapSuite) TestMaybeUndoRemodelBootChangesNeedsUndo(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// undoing remodel bootenv changes is only relevant on !classic
	restore := release.MockOnClassic(false)
	defer restore()

	// using "snaptest.MockSnap()" is more convenient here so we avoid
	// the (default) mocking of snapReadInfo()
	restore = snapstate.MockSnapReadInfo(snap.ReadInfo)
	defer restore()

	// we need to init the boot-id
	err := s.state.VerifyReboot("some-boot-id")
	c.Assert(err, IsNil)

	// we pretend we do a remodel from kernel -> new-kernel
	s.setMockKernelRemodelCtx(c, "kernel", "new-kernel")

	// and we pretend that we booted into the "new-kernel" already
	// and now that needs to be undone
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	bloader.SetBootKernel("new-kernel_1.snap")

	// both kernel and new-kernel are instaleld at this point
	si := &snap.SideInfo{RealName: "kernel", Revision: snap.R(1)}
	snapstate.Set(s.state, "kernel", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{si},
		SnapType: "kernel",
		Current:  snap.R(1),
	})
	snaptest.MockSnap(c, "name: kernel\ntype: kernel\nversion: 1.0", si)
	si2 := &snap.SideInfo{RealName: "new-kernel", Revision: snap.R(1)}
	snapstate.Set(s.state, "new-kernel", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{si2},
		SnapType: "kernel",
		Current:  snap.R(1),
	})
	snaptest.MockSnap(c, "name: new-kernel\ntype: kernel\nversion: 1.0", si)

	t := s.state.NewTask("link-snap", "...")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "new-kernel",
			Revision: snap.R(1),
			SnapID:   "new-kernel-id",
		},
		Type: "kernel",
	})

	// now we simulate that the new kernel is getting undone
	err = snapstate.MaybeUndoRemodelBootChanges(t)
	c.Assert(err, IsNil)

	// that will schedule a boot into the previous kernel
	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"snap_mode":       "try",
		"snap_kernel":     "new-kernel_1.snap",
		"snap_try_kernel": "kernel_1.snap",
	})
	c.Check(s.stateBackend.restartRequested, HasLen, 1)
	c.Check(s.stateBackend.restartRequested[0], Equals, state.RestartSystem)
}
