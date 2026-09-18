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

package tasktest_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/tasktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
)

type snapQuerySuite struct{}

var _ = Suite(&snapQuerySuite{})

func (s *snapQuerySuite) TestSnap(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	mount := st.NewTask("mount-snap", "...")
	mount.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "some-snap"},
	})

	link := st.NewTask("link-snap", "...")
	link.Set("snap-setup-task", mount.ID())

	other := st.NewTask("link-snap", "...")
	other.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "other-snap"},
	})

	unrelated := st.NewTask("unrelated", "...")

	selection := tasktest.NewSelection([]*state.Task{mount, link, other, unrelated})

	query := tasktest.Snap("some-snap")
	c.Check(selection.Select(query.WithKind("link-snap")).Tasks(), DeepEquals, []*state.Task{link})
	c.Check(selection.Select(query.All()).Tasks(), DeepEquals, []*state.Task{mount, link})
	c.Check(selection.Select(tasktest.Snap("other-snap")).Tasks(), DeepEquals, []*state.Task{other})
}

func (s *snapQuerySuite) TestSnapInstanceName(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	mount := st.NewTask("mount-snap", "...")
	mount.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo:    &snap.SideInfo{RealName: "some-snap"},
		InstanceKey: "instance",
	})

	link := st.NewTask("link-snap", "...")
	link.Set("snap-setup-task", mount.ID())

	other := st.NewTask("link-snap", "...")
	other.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "some-snap"},
	})

	selection := tasktest.NewSelection([]*state.Task{mount, link, other})

	c.Check(selection.Select(tasktest.Snap("some-snap_instance").All()).Tasks(), DeepEquals, []*state.Task{mount, link})
	c.Check(selection.Select(tasktest.Snap("some-snap")).Tasks(), DeepEquals, []*state.Task{other})
}

func (s *snapQuerySuite) TestSnapQueryAfterFiltering(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	mount := st.NewTask("mount-snap", "...")
	mount.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "some-snap"},
	})

	link := st.NewTask("link-snap", "...")
	link.Set("snap-setup-task", mount.ID())

	selection := tasktest.NewSelection([]*state.Task{mount, link})

	// filter out the setup task before the first snap query builds the cache.
	filtered := selection.Select(tasktest.Kind("link-snap"))
	c.Assert(filtered.Tasks(), DeepEquals, []*state.Task{link})
	c.Check(filtered.Select(tasktest.Snap("some-snap")).Tasks(), DeepEquals, []*state.Task{link})
}

func (s *snapQuerySuite) TestComponents(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	snapTask := st.NewTask("mount-snap", "...")
	snapTask.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "some-snap"},
	})

	mount := st.NewTask("mount-component", "...")
	mount.Set("snap-setup-task", snapTask.ID())
	mount.Set("component-setup", &snapstate.ComponentSetup{
		CompSideInfo: snap.NewComponentSideInfo(naming.NewComponentRef("some-snap", "comp"), snap.R(1)),
	})

	link := st.NewTask("link-component", "...")
	link.Set("snap-setup-task", snapTask.ID())
	link.Set("component-setup-task", mount.ID())

	other := st.NewTask("mount-component", "...")
	other.Set("snap-setup-task", snapTask.ID())
	other.Set("component-setup", &snapstate.ComponentSetup{
		CompSideInfo: snap.NewComponentSideInfo(naming.NewComponentRef("some-snap", "other"), snap.R(1)),
	})

	selection := tasktest.NewSelection([]*state.Task{snapTask, mount, link, other})

	query := tasktest.Snap("some-snap")
	c.Check(selection.Select(query.WithComponent("comp").All()).Tasks(), DeepEquals, []*state.Task{mount, link})
	c.Check(selection.Select(query.WithComponent("other")).Tasks(), DeepEquals, []*state.Task{other})
	c.Check(selection.Select(query.Components().All()).Tasks(), DeepEquals, []*state.Task{mount, link, other})
}

func (s *snapQuerySuite) TestComponentsInstanceName(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	snapTask := st.NewTask("mount-snap", "...")
	snapTask.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo:    &snap.SideInfo{RealName: "some-snap"},
		InstanceKey: "instance",
	})

	compsup := &snapstate.ComponentSetup{
		CompSideInfo: snap.NewComponentSideInfo(naming.NewComponentRef("some-snap", "comp"), snap.R(1)),
	}

	mount := st.NewTask("mount-component", "...")
	mount.Set("snap-setup-task", snapTask.ID())
	mount.Set("component-setup", compsup)

	link := st.NewTask("link-component", "...")
	link.Set("snap-setup-task", snapTask.ID())
	link.Set("component-setup-task", mount.ID())

	other := st.NewTask("mount-component", "...")
	other.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "some-snap"},
	})
	other.Set("component-setup", compsup)

	selection := tasktest.NewSelection([]*state.Task{snapTask, mount, link, other})

	query := tasktest.Snap("some-snap_instance")
	c.Check(selection.Select(query.WithComponent("comp").All()).Tasks(), DeepEquals, []*state.Task{mount, link})
	c.Check(selection.Select(tasktest.Snap("some-snap").WithComponent("comp")).Tasks(), DeepEquals, []*state.Task{other})
}

func (s *snapQuerySuite) TestWithHook(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	install := st.NewTask("run-hook", "...")
	install.Set("hook-setup", &hookstate.HookSetup{Snap: "some-snap", Hook: "install"})

	configure := st.NewTask("run-hook", "...")
	configure.Set("hook-setup", &hookstate.HookSetup{Snap: "some-snap", Hook: "configure"})

	component := st.NewTask("run-hook", "...")
	component.Set("hook-setup", &hookstate.HookSetup{Snap: "some-snap", Hook: "install", Component: "comp"})

	selection := tasktest.NewSelection([]*state.Task{install, configure, component})

	query := tasktest.Snap("some-snap").WithHook("install")
	c.Check(selection.Select(query.All()).Tasks(), DeepEquals, []*state.Task{install, component})
	c.Check(selection.Select(query.WithComponent("comp")).Tasks(), DeepEquals, []*state.Task{component})
	c.Check(selection.Select(tasktest.Snap("some-snap").WithHook("configure")).Tasks(), DeepEquals, []*state.Task{configure})
}

func (s *snapQuerySuite) TestWithHookInstanceName(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	install := st.NewTask("run-hook", "...")
	install.Set("hook-setup", &hookstate.HookSetup{Snap: "some-snap_instance", Hook: "install"})

	component := st.NewTask("run-hook", "...")
	component.Set("hook-setup", &hookstate.HookSetup{Snap: "some-snap_instance", Hook: "install", Component: "comp"})

	other := st.NewTask("run-hook", "...")
	other.Set("hook-setup", &hookstate.HookSetup{Snap: "some-snap", Hook: "install"})

	otherComponent := st.NewTask("run-hook", "...")
	otherComponent.Set("hook-setup", &hookstate.HookSetup{Snap: "some-snap", Hook: "install", Component: "comp"})

	selection := tasktest.NewSelection([]*state.Task{install, component, other, otherComponent})

	query := tasktest.Snap("some-snap_instance").WithHook("install")
	c.Check(selection.Select(query.All()).Tasks(), DeepEquals, []*state.Task{install, component})
	c.Check(selection.Select(query.WithComponent("comp")).Tasks(), DeepEquals, []*state.Task{component})

	otherQuery := tasktest.Snap("some-snap").WithHook("install")
	c.Check(selection.Select(otherQuery.All()).Tasks(), DeepEquals, []*state.Task{other, otherComponent})
	c.Check(selection.Select(otherQuery.WithComponent("comp")).Tasks(), DeepEquals, []*state.Task{otherComponent})
}
