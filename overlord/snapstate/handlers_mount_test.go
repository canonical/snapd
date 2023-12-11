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
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type mountSnapSuite struct {
	baseHandlerSuite
}

var _ = Suite(&mountSnapSuite{})

func (s *mountSnapSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)
	s.AddCleanup(snapstatetest.MockDeviceModel(DefaultModel()))
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
	s.state.NewChange("sample", "...").AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	c.Assert(osutil.FileExists(testSnap), Equals, true)
}

func (s *mountSnapSuite) TestDoUndoMountSnap(c *C) {
	v1 := "name: core\nversion: 1.0\nepoch: 1\n"
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
		SnapType: "os",
	})

	t := s.state.NewTask("mount-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si2,
		SnapPath: testSnap,
	})
	chg := s.state.NewChange("sample", "...")
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

	// ensure undo was called the right way
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:  "current",
			old: filepath.Join(dirs.SnapMountDir, "core/1"),
		},
		{
			op:    "setup-snap",
			name:  "core",
			path:  testSnap,
			revno: snap.R(2),
		},
		{
			op:    "undo-setup-snap",
			name:  "core",
			path:  filepath.Join(dirs.SnapMountDir, "core/2"),
			stype: "os",
		},
		{
			op:   "remove-snap-dir",
			name: "core",
			path: filepath.Join(dirs.SnapMountDir, "core"),
		},
	})

}

func (s *mountSnapSuite) TestDoMountSnapErrorReadInfo(c *C) {
	v1 := "name: borken\nversion: 1.0\nepoch: 1\n"
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
		SnapType: "app",
	})

	t := s.state.NewTask("mount-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si2,
		SnapPath: testSnap,
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
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
			name:  "borken",
			path:  testSnap,
			revno: snap.R(2),
		},
		{
			op:    "undo-setup-snap",
			name:  "borken",
			path:  filepath.Join(dirs.SnapMountDir, "borken/2"),
			stype: "app",
		},
		{
			op:   "remove-snap-dir",
			name: "borken",
			path: filepath.Join(dirs.SnapMountDir, "borken"),
		},
	})
}

func (s *mountSnapSuite) TestDoMountSnapEpochError(c *C) {
	v1 := "name: some-snap\nversion: 1.0\nepoch: 13\n"
	testSnap := snaptest.MakeTestSnapWithFiles(c, v1, nil)

	s.state.Lock()
	defer s.state.Unlock()
	si1 := &snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	}
	si2 := &snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
		SnapType: "app",
	})

	t := s.state.NewTask("mount-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si2,
		SnapPath: testSnap,
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(chg.Err(), ErrorMatches, `(?s).* new revision 2 with epoch .* can't read the current epoch of [^ ]*`)
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:  "current",
			old: filepath.Join(dirs.SnapMountDir, "some-snap/1"),
		},
	})
}

func (s *mountSnapSuite) TestDoMountSnapErrorSetupSnap(c *C) {
	v1 := "name: borken\nversion: 1.0\n"
	testSnap := snaptest.MakeTestSnapWithFiles(c, v1, nil)

	s.state.Lock()
	defer s.state.Unlock()
	si := &snap.SideInfo{
		RealName: "borken-in-setup",
		Revision: snap.R(2),
	}

	t := s.state.NewTask("mount-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		SnapPath: testSnap,
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(chg.Err(), ErrorMatches, `(?s).*cannot install snap "borken-in-setup".*`)

	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:  "current",
			old: "<no-current>",
		},
		{
			op:    "setup-snap",
			name:  "borken-in-setup",
			path:  testSnap,
			revno: snap.R(2),
		},
		{
			op:   "remove-snap-dir",
			name: "borken-in-setup",
			path: filepath.Join(dirs.SnapMountDir, "borken-in-setup"),
		},
	})
}

func (s *mountSnapSuite) TestDoMountSnapUndoError(c *C) {
	v1 := "name: borken-undo-setup\nversion: 1.0\nepoch: 1\n"
	testSnap := snaptest.MakeTestSnapWithFiles(c, v1, nil)

	s.state.Lock()
	defer s.state.Unlock()
	si1 := &snap.SideInfo{
		RealName: "borken-undo-setup",
		Revision: snap.R(1),
	}
	si2 := &snap.SideInfo{
		RealName: "borken-undo-setup",
		Revision: snap.R(2),
	}
	snapstate.Set(s.state, "borken-undo-setup", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
		SnapType: "app",
	})

	t := s.state.NewTask("mount-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si2,
		SnapPath: testSnap,
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(chg.Err(), ErrorMatches, `(?s).*cannot undo partial setup snap "borken-undo-setup": cannot undo setup of "borken-undo-setup" snap.*cannot read info for "borken-undo-setup" snap.*`)

	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:  "current",
			old: filepath.Join(dirs.SnapMountDir, "borken-undo-setup/1"),
		},
		{
			op:    "setup-snap",
			name:  "borken-undo-setup",
			path:  testSnap,
			revno: snap.R(2),
		},
		{
			op:    "undo-setup-snap",
			name:  "borken-undo-setup",
			path:  filepath.Join(dirs.SnapMountDir, "borken-undo-setup/2"),
			stype: "app",
		},
		{
			op:   "remove-snap-dir",
			name: "borken-undo-setup",
			path: filepath.Join(dirs.SnapMountDir, "borken-undo-setup"),
		},
	})
}

func (s *mountSnapSuite) TestDoMountSnapErrorNotFound(c *C) {
	r := snapstate.MockMountPollInterval(10 * time.Millisecond)
	defer r()

	v1 := "name: not-there\nversion: 1.0\nepoch: 1\n"
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
		SnapType: "app",
	})

	t := s.state.NewTask("mount-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si2,
		SnapPath: testSnap,
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
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
			name:  "not-there",
			path:  testSnap,
			revno: snap.R(2),
		},
		{
			op:    "undo-setup-snap",
			name:  "not-there",
			path:  filepath.Join(dirs.SnapMountDir, "not-there/2"),
			stype: "app",
		},
		{
			op:   "remove-snap-dir",
			name: "not-there",
			path: filepath.Join(dirs.SnapMountDir, "not-there"),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
		SnapType: "app",
	})

	t := s.state.NewTask("mount-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si2,
		SnapPath: testSnap,
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
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
			name:  "not-there",
			path:  testSnap,
			revno: snap.R(2),
		},
	})
}
