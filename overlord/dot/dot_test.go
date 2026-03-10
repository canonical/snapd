// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package dot_test

import (
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/dot"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/state"
)

func TestDot(t *testing.T) { TestingT(t) }

type dotSuite struct{}

var _ = Suite(&dotSuite{})

func (s *dotSuite) TestTaskLabelTaskSnapSetupError(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("task-kind", "task-with-snap-setup")
	task.Set("snap-setup-task", "0")

	_, _, err := dot.TaskLabel(task)
	c.Assert(err, ErrorMatches, "internal error: tasks are being pruned")
}

func (s *dotSuite) TestTaskLabelRunHook(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("run-hook", "task-with-run-hook")
	task.Set("hook-setup", map[string]string{
		"snap": "snap",
		"hook": "hook",
	})

	str, attrs, err := dot.TaskLabel(task)
	c.Assert(err, IsNil)
	c.Assert(str, Equals, "snap:run-hook[hook] [1]")
	c.Assert(attrs, HasLen, 0)
}

func (s *dotSuite) TestTaskLabelRunHookErrorNoHookSetup(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("run-hook", "task-with-run-hook")

	_, _, err := dot.TaskLabel(task)
	c.Assert(err, ErrorMatches, "no state entry for key \"hook-setup\"")
}

func (s *dotSuite) TestTaskLabelWithSnapSetup(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("task-kind", "task-with-snap-setup")
	task.Set("snap-setup", map[string]any{
		"side-info": map[string]any{
			"name": "snap-name",
		},
	})

	str, attrs, err := dot.TaskLabel(task)
	c.Assert(err, IsNil)
	c.Assert(str, Equals, "snap-name:task-kind [1]")
	c.Assert(attrs, HasLen, 0)
}

func (s *dotSuite) TestTaskLabelConnect(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("connect", "task-connect-like")
	pref := &interfaces.PlugRef{
		Snap: "plug-snap",
		Name: "plug-name",
	}
	task.Set("plug", pref)
	sref := &interfaces.SlotRef{
		Snap: "slot-snap",
		Name: "slot-name",
	}
	task.Set("slot", sref)

	str, attrs, err := dot.TaskLabel(task)
	c.Assert(err, IsNil)
	c.Assert(str, Equals, "connect[plug-snap:plug-name slot-snap:slot-name] [1]")
	c.Assert(attrs, HasLen, 0)
}

func (s *dotSuite) TestTaskLabelConnectMissingSnapName(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("connect", "task-connect-like")
	pref := &interfaces.PlugRef{
		Snap: "",
		Name: "plug-name",
	}
	task.Set("plug", pref)
	sref := &interfaces.SlotRef{
		Snap: "slot-snap",
		Name: "slot-name",
	}
	task.Set("slot", sref)

	str, attrs, err := dot.TaskLabel(task)
	c.Assert(err, IsNil)
	c.Assert(str, Equals, "connect [1]")
	c.Assert(attrs, HasLen, 0)
}

func (s *dotSuite) TestTaskLabelWithComponentSetupTask(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	chg := st.NewChange("change-kind", "summary")

	setupTask := st.NewTask("prepare-component", "prepare")
	setupTask.Set("component-setup", map[string]any{
		"comp-side-info": map[string]any{
			"component": map[string]any{
				"snap-name":      "mysnap",
				"component-name": "my-component",
			},
		},
	})

	task := st.NewTask("link-component", "link")
	task.Set("component-setup-task", setupTask.ID())
	chg.AddTask(setupTask)
	chg.AddTask(task)

	str, attrs, err := dot.TaskLabel(task)
	c.Assert(err, IsNil)
	c.Assert(str, Equals, "mysnap:link-component [2]")
	c.Assert(attrs, HasLen, 0)
}

func (s *dotSuite) TestNewChangeGraphUsesDefaultTaskLabel(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("task-kind", "task")
	chg := st.NewChange("change-kind", "summary")
	chg.AddTask(task)

	g, err := dot.NewChangeGraph(chg, "my-tag")
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(g.Dot(), `"task-kind [1]"`), Equals, true)
}

func (s *dotSuite) TestTaskLabelRestartBoundaryAttrs(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("task-kind", "task")
	restart.MarkTaskAsRestartBoundary(task, restart.RestartBoundaryDirectionDo|restart.RestartBoundaryDirectionUndo)

	str, attrs, err := dot.TaskLabel(task)
	c.Assert(err, IsNil)
	c.Assert(str, Equals, "task-kind [1]\\nreboot: do|undo")
	c.Assert(attrs, DeepEquals, []string{"peripheries=2"})
}

func (s *dotSuite) TestNewChangeGraphIncludesRestartBoundaryAttrs(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("task-kind", "task")
	restart.MarkTaskAsRestartBoundary(task, restart.RestartBoundaryDirectionDo)
	chg := st.NewChange("change-kind", "summary")
	chg.AddTask(task)

	g, err := dot.NewChangeGraph(chg, "my-tag")
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(g.Dot(), `"task-kind [1]\nreboot: do" [peripheries=2]`), Equals, true)
}
