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

package exportstate_test

import (
	"errors"
	"path/filepath"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/exportstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type handlersSuite struct {
	testutil.BaseTest
	o  *overlord.Overlord
	se *overlord.StateEngine
	st *state.State
}

var _ = Suite(&handlersSuite{})

func (s *handlersSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.o = overlord.Mock()
	s.se = s.o.StateEngine()
	s.st = s.o.State()
	s.AddCleanup(s.se.Stop)

	runner := s.o.TaskRunner()
	runner.AddHandler("fail", func(task *state.Task, _ *tomb.Tomb) error {
		return errors.New("injected test failure")
	}, nil)

	mgr, err := exportstate.Manager(s.st, runner)
	c.Assert(err, IsNil)
	s.o.AddManager(mgr)
	s.o.AddManager(runner)
	c.Assert(s.o.ExportManager(), Equals, mgr)
	c.Assert(s.o.StartUp(), IsNil)

}

func (s *handlersSuite) TestDoExportContentNop(c *C) {
	st := s.st
	st.Lock()
	si := &snap.SideInfo{RealName: "snap-name", Revision: snap.R(1)}
	snaptest.MockSnap(c, "name: snap-name\nversion: 1\n", si)
	snapstate.Set(st, "snap-name", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		SnapType: "app",
	})
	change := st.NewChange("change", "...")
	task := st.NewTask("export-content", "...")
	task.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "snap-name", Revision: snap.R(1)},
	})
	change.AddTask(task)
	st.Unlock()

	c.Check(s.se.Ensure(), IsNil)
	s.se.Wait()

	st.Lock()
	defer st.Unlock()
	c.Check(change.Err(), IsNil)
	c.Check(task.Status(), Equals, state.DoneStatus)
	c.Check(change.Status(), Equals, state.DoneStatus)
}

func (s *handlersSuite) TestDoUnexportContentNop(c *C) {
	st := s.st
	st.Lock()
	si := &snap.SideInfo{RealName: "snap-name", Revision: snap.R(1)}
	snaptest.MockSnap(c, "name: snap-name\nversion: 1\n", si)
	snapstate.Set(st, "snap-name", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		SnapType: "app",
	})
	task := st.NewTask("unexport-content", "...")
	task.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "snap-name", Revision: snap.R(1)},
	})
	change := st.NewChange("change", "...")
	change.AddTask(task)
	st.Unlock()

	c.Check(s.se.Ensure(), IsNil)
	s.se.Wait()

	st.Lock()
	defer st.Unlock()
	c.Check(change.Err(), IsNil)
	c.Check(task.Status(), Equals, state.DoneStatus)
	c.Check(change.Status(), Equals, state.DoneStatus)
}

func (s *handlersSuite) TestDoExportContentSnapd(c *C) {
	st := s.st
	st.Lock()
	si := &snap.SideInfo{RealName: "snapd", Revision: snap.R(1)}
	snaptest.MockSnap(c, "name: snapd\nversion: 1\n", si)
	snapstate.Set(st, "snapd", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		SnapType: "snapd",
	})
	task := st.NewTask("export-content", "...")
	task.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "snapd", Revision: snap.R(1)},
	})
	change := st.NewChange("change", "...")
	change.AddTask(task)
	st.Unlock()

	c.Check(s.se.Ensure(), IsNil)
	s.se.Wait()

	st.Lock()
	defer st.Unlock()
	c.Check(change.Err(), IsNil)
	c.Check(task.Status(), Equals, state.DoneStatus)
	c.Check(change.Status(), Equals, state.DoneStatus)

	// System state retains non-empty manifest describing snapd.
	var m exportstate.Manifest
	c.Assert(exportstate.Get(st, "snapd", snap.R(1), &m), IsNil)
	c.Check(m.IsEmpty(), Equals, false)
	// Exported content is now on disk.
	for _, set := range m.Sets {
		for _, exported := range set.Exports {
			c.Check(exportstate.ExportedFilePath(&m, &set, &exported), testutil.SymlinkTargetEquals,
				exportstate.ExportedFileSourcePath(&m, &set, &exported))
		}
	}
}

func (s *handlersSuite) TestUndoExportContentSnapd(c *C) {
	st := s.st
	st.Lock()
	si := &snap.SideInfo{RealName: "snapd", Revision: snap.R(1)}
	snaptest.MockSnap(c, "name: snapd\nversion: 1\n", si)
	snapstate.Set(st, "snapd", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		SnapType: "snapd",
	})
	exportTask := st.NewTask("export-content", "...")
	exportTask.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "snapd", Revision: snap.R(1)},
	})
	failTask := st.NewTask("fail", "...")
	failTask.WaitFor(exportTask)
	change := st.NewChange("change", "...")
	change.AddTask(exportTask)
	change.AddTask(failTask)
	st.Unlock()

	for i := 0; i < 3; i++ { // do export-task, do fail, undo export-task
		c.Check(s.se.Ensure(), IsNil)
		s.se.Wait()
	}

	st.Lock()
	defer st.Unlock()
	c.Check(change.Err(), ErrorMatches, "cannot perform the following tasks:\n- ... \\(injected test failure\\)")
	c.Check(exportTask.Status(), Equals, state.DoneStatus) // XXX: confusing, it was really undone.
	c.Check(failTask.Status(), Equals, state.ErrorStatus)
	c.Check(change.Status(), Equals, state.ErrorStatus)

	// System state does not contain the export manifest.
	var m exportstate.Manifest
	c.Assert(exportstate.Get(st, "snapd", snap.R(1), &m), Equals, state.ErrNoState)

	// Exported content is not on disk.
	c.Check(filepath.Join(dirs.ExportDir, "snapd", "tools", "1"), testutil.FileAbsent)
}

func (s *handlersSuite) TestDoUnexportContentSnapd(c *C) {
	st := s.st
	st.Lock()
	initialManifest := &exportstate.Manifest{
		SnapInstanceName: "snapd",
		SnapRevision:     snap.R(1),
		Sets: map[string]exportstate.ExportSet{
			"tools": {
				Name: "tools",
				Exports: map[string]exportstate.ExportedFile{
					"snap-exec": {
						Name:       "snap-exec",
						SourcePath: "usr/lib/snapd/snap-exec",
					},
				},
			},
		},
	}
	c.Assert(exportstate.CreateExportedFiles(initialManifest), IsNil)
	exportstate.Set(st, "snapd", snap.R(1), initialManifest)
	task := st.NewTask("unexport-content", "...")
	task.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "snapd", Revision: snap.R(1)},
	})
	change := st.NewChange("change", "...")
	change.AddTask(task)
	st.Unlock()

	c.Check(s.se.Ensure(), IsNil)
	s.se.Wait()

	st.Lock()
	defer st.Unlock()
	c.Check(change.Err(), IsNil)
	c.Check(task.Status(), Equals, state.DoneStatus)
	c.Check(change.Status(), Equals, state.DoneStatus)

	// Exported content is no longer on disk.
	for _, set := range initialManifest.Sets {
		for _, exported := range set.Exports {
			c.Check(exportstate.ExportedFilePath(initialManifest, &set, &exported), testutil.FileAbsent)
		}
	}

	// System state is updated to reflect that.
	var m exportstate.Manifest
	c.Assert(exportstate.Get(st, "snapd", snap.R(1), &m), Equals, state.ErrNoState)

	// Task state retains the old manifest.
	c.Assert(task.Get("old-manifest", &m), IsNil)
	c.Check(m, DeepEquals, *initialManifest)
}

func (s *handlersSuite) TestUndoUnexportContentSnapd(c *C) {
	st := s.st
	st.Lock()

	initialManifest := &exportstate.Manifest{
		SnapInstanceName: "snapd",
		SnapRevision:     snap.R(1),
		Sets: map[string]exportstate.ExportSet{
			"tools": {
				Name: "tools",
				Exports: map[string]exportstate.ExportedFile{
					"snap-exec": {
						Name:       "snap-exec",
						SourcePath: "usr/lib/snapd/snap-exec",
					},
				},
			},
		},
	}
	c.Assert(exportstate.CreateExportedFiles(initialManifest), IsNil)
	exportstate.Set(st, "snapd", snap.R(1), initialManifest)
	unexportTask := st.NewTask("unexport-content", "...")
	unexportTask.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "snapd", Revision: snap.R(1)},
	})
	failTask := st.NewTask("fail", "...")
	failTask.WaitFor(unexportTask)
	change := st.NewChange("change", "...")
	change.AddTask(unexportTask)
	change.AddTask(failTask)
	st.Unlock()

	for i := 0; i < 3; i++ { // do unexport-task, do fail, undo unexport-task
		c.Check(s.se.Ensure(), IsNil)
		s.se.Wait()
	}

	st.Lock()
	defer st.Unlock()
	c.Check(change.Err(), ErrorMatches, "cannot perform the following tasks:\n- ... \\(injected test failure\\)")
	c.Check(unexportTask.Status(), Equals, state.UndoneStatus)
	c.Check(failTask.Status(), Equals, state.ErrorStatus)
	c.Check(change.Status(), Equals, state.ErrorStatus)

	// Exported content is back on disk.
	for _, set := range initialManifest.Sets {
		for _, exported := range set.Exports {
			c.Check(exportstate.ExportedFilePath(initialManifest, &set, &exported), testutil.SymlinkTargetEquals,
				exportstate.ExportedFileSourcePath(initialManifest, &set, &exported))
		}
	}
	// System state is updated to reflect that.
	var m exportstate.Manifest
	c.Assert(exportstate.Get(st, "snapd", snap.R(1), &m), IsNil)
	c.Check(m, DeepEquals, *initialManifest)
}
