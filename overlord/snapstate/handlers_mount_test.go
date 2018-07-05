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
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type mountSnapSuite struct {
	state   *state.State
	snapmgr *snapstate.SnapManager

	fakeBackend *fakeSnappyBackend

	reset func()
}

var _ = Suite(&mountSnapSuite{})

func (s *mountSnapSuite) SetUpTest(c *C) {
	oldDir := dirs.GlobalRootDir
	dirs.SetRootDir(c.MkDir())

	s.fakeBackend = &fakeSnappyBackend{}
	s.state = state.New(nil)

	var err error
	s.snapmgr, err = snapstate.Manager(s.state)
	c.Assert(err, IsNil)
	s.snapmgr.AddForeignTaskHandlers(s.fakeBackend)

	snapstate.SetSnapManagerBackend(s.snapmgr, s.fakeBackend)

	reset1 := snapstate.MockSnapReadInfo(s.fakeBackend.ReadInfo)
	s.reset = func() {
		reset1()
		dirs.SetRootDir(oldDir)
	}
}

func (s *mountSnapSuite) TearDownTest(c *C) {
	s.reset()
}

func (s *mountSnapSuite) TestDoMountSnapDoesNotRemovesSnaps(c *C) {
	v1 := "name: mock\nversion: 1.0\n"
	testSnap := snaptest.MakeTestSnapWithFiles(c, v1, nil)

	s.state.Lock()

	t := s.state.NewTask("mount-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
		SnapPath:     testSnap,
		DownloadInfo: &snap.DownloadInfo{DownloadURL: "https://some"},
	})
	s.state.NewChange("dummy", "...").AddTask(t)

	s.state.Unlock()

	s.snapmgr.Ensure()
	s.snapmgr.Wait()

	c.Assert(osutil.FileExists(testSnap), Equals, true)
}

func (s *mountSnapSuite) TestDoUndoMountSnap(c *C) {
	v1 := "name: core\nversion: 1.0\n"
	testSnap := snaptest.MakeTestSnapWithFiles(c, v1, nil)

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
		SnapType: "os",
	})

	t := s.state.NewTask("mount-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si2,
		SnapPath: testSnap,
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.snapmgr.Ensure()
		s.snapmgr.Wait()
	}

	s.state.Lock()

	// ensure undo was called the right way
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:  "current",
			old: filepath.Join(dirs.SnapMountDir, "core/1"),
		},
		{
			op:    "setup-snap",
			path:  testSnap,
			revno: snap.R(2),
		},
		{
			op:    "undo-setup-snap",
			path:  filepath.Join(dirs.SnapMountDir, "core/2"),
			stype: "os",
		},
	})

}

func (s *mountSnapSuite) TestDoMountSnapError(c *C) {
	v1 := "name: borken\nversion: 1.0\n"
	testSnap := snaptest.MakeTestSnapWithFiles(c, v1, nil)

	s.state.Lock()
	defer s.state.Unlock()
	si1 := &snap.SideInfo{
		RealName: "borken",
		Revision: snap.R(1),
	}
	si2 := &snap.SideInfo{
		RealName: "borken",
		Revision: snap.R(2),
	}
	snapstate.Set(s.state, "borken", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{si1},
		Current:  si1.Revision,
		SnapType: "app",
	})

	t := s.state.NewTask("mount-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si2,
		SnapPath: testSnap,
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.snapmgr.Ensure()
		s.snapmgr.Wait()
	}

	s.state.Lock()

	c.Check(chg.Err(), ErrorMatches, `(?s).*cannot read info for "borken" snap.*`)

	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:  "current",
			old: filepath.Join(dirs.SnapMountDir, "borken/1"),
		},
		{
			op:    "setup-snap",
			path:  testSnap,
			revno: snap.R(2),
		},
		{
			op:    "undo-setup-snap",
			path:  filepath.Join(dirs.SnapMountDir, "borken/2"),
			stype: "app",
		},
	})
}

func (s *mountSnapSuite) TestDoMountSnapErrorNotFound(c *C) {
	r := snapstate.MockMountPollInterval(10 * time.Millisecond)
	defer r()

	v1 := "name: not-there\nversion: 1.0\n"
	testSnap := snaptest.MakeTestSnapWithFiles(c, v1, nil)

	s.state.Lock()
	defer s.state.Unlock()
	si1 := &snap.SideInfo{
		RealName: "not-there",
		Revision: snap.R(1),
	}
	si2 := &snap.SideInfo{
		RealName: "not-there",
		Revision: snap.R(2),
	}
	snapstate.Set(s.state, "not-there", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{si1},
		Current:  si1.Revision,
		SnapType: "app",
	})

	t := s.state.NewTask("mount-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si2,
		SnapPath: testSnap,
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.snapmgr.Ensure()
		s.snapmgr.Wait()
	}

	s.state.Lock()

	c.Check(chg.Err(), ErrorMatches, `(?s).*cannot proceed, expected snap "not-there" revision 2 to be mounted but is not.*`)

	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:  "current",
			old: filepath.Join(dirs.SnapMountDir, "not-there/1"),
		},
		{
			op:    "setup-snap",
			path:  testSnap,
			revno: snap.R(2),
		},
		{
			op:    "undo-setup-snap",
			path:  filepath.Join(dirs.SnapMountDir, "not-there/2"),
			stype: "app",
		},
	})
}

func (s *mountSnapSuite) TestDoMountNotMountedRetryRetry(c *C) {
	r := snapstate.MockMountPollInterval(10 * time.Millisecond)
	defer r()
	n := 0
	slowMountedReadInfo := func(name string, si *snap.SideInfo) (*snap.Info, error) {
		n++
		if n < 3 {
			return nil, &snap.NotFoundError{Snap: "not-there", Revision: si.Revision}
		}
		return &snap.Info{
			SideInfo: *si,
		}, nil
	}

	r1 := snapstate.MockSnapReadInfo(slowMountedReadInfo)
	defer r1()

	v1 := "name: not-there\nversion: 1.0\n"
	testSnap := snaptest.MakeTestSnapWithFiles(c, v1, nil)

	s.state.Lock()
	defer s.state.Unlock()
	si1 := &snap.SideInfo{
		RealName: "not-there",
		Revision: snap.R(1),
	}
	si2 := &snap.SideInfo{
		RealName: "not-there",
		Revision: snap.R(2),
	}
	snapstate.Set(s.state, "not-there", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{si1},
		Current:  si1.Revision,
		SnapType: "app",
	})

	t := s.state.NewTask("mount-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si2,
		SnapPath: testSnap,
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.snapmgr.Ensure()
		s.snapmgr.Wait()
	}

	s.state.Lock()

	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), IsNil)

	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:  "current",
			old: filepath.Join(dirs.SnapMountDir, "not-there/1"),
		},
		{
			op:    "setup-snap",
			path:  testSnap,
			revno: snap.R(2),
		},
	})
}
