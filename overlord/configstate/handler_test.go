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

package configstate_test

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

func TestConfigState(t *testing.T) { TestingT(t) }

type configureHandlerSuite struct {
	state   *state.State
	context *hookstate.Context
	handler hookstate.Handler
	restore func()
}

var _ = Suite(&configureHandlerSuite{})

func (s *configureHandlerSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	s.state = state.New(nil)
	s.state.Lock()
	defer s.state.Unlock()

	coreSnapYaml := `name: core
version: 1.0
type: os
`
	snaptest.MockSnap(c, coreSnapYaml, &snap.SideInfo{
		RealName: "core",
		Revision: snap.R(1),
	})
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1), SnapID: "core-snap-id"},
		},
		Current:  snap.R(1),
		SnapType: "os",
	})

	s.restore = snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})

	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}

	var err error
	s.context, err = hookstate.NewContext(task, task.State(), setup, hooktest.NewMockHandler(), "")
	c.Assert(err, IsNil)

	s.handler = configstate.NewConfigureHandler(s.context)
}

func (s *configureHandlerSuite) TearDownTest(c *C) {
	s.restore()
	dirs.SetRootDir("/")
}

func (s *configureHandlerSuite) TestBeforeInitializesTransaction(c *C) {
	// Initialize context
	s.context.Lock()
	s.context.Set("patch", map[string]interface{}{
		"foo": "bar",
	})
	s.context.Unlock()

	c.Check(s.handler.Before(), IsNil)

	s.context.Lock()
	tr := configstate.ContextTransaction(s.context)
	s.context.Unlock()

	var value string
	c.Check(tr.Get("test-snap", "foo", &value), IsNil)
	c.Check(value, Equals, "bar")
}

func (s *configureHandlerSuite) TestBeforeInitializesTransactionUseDefaults(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	const mockGadgetSnapYaml = `
name: canonical-pc
type: gadget
`
	var mockGadgetYaml = []byte(`
defaults:
  test-snap-ididididididididididid:
      bar: baz
      num: 1.305

volumes:
    volume-id:
        bootloader: grub
`)

	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(1)})
	err := ioutil.WriteFile(filepath.Join(info.MountDir(), "meta", "gadget.yaml"), mockGadgetYaml, 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	snapstate.Set(s.state, "canonical-pc", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "canonical-pc", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "gadget",
	})

	const mockTestSnapYaml = `
name: test-snap
hooks:
    configure:
`

	snaptest.MockSnap(c, mockTestSnapYaml, &snap.SideInfo{Revision: snap.R(11)})
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "test-snap", Revision: snap.R(11), SnapID: "test-snap-ididididididididididid"},
		},
		Current:  snap.R(11),
		SnapType: "app",
	})
	s.state.Unlock()

	// Initialize context
	s.context.Lock()
	s.context.Set("use-defaults", true)
	s.context.Unlock()

	c.Check(s.handler.Before(), IsNil)

	s.context.Lock()
	tr := configstate.ContextTransaction(s.context)
	s.context.Unlock()

	var value string
	c.Check(tr.Get("test-snap", "bar", &value), IsNil)
	c.Check(value, Equals, "baz")
	var fl float64
	c.Check(tr.Get("test-snap", "num", &fl), IsNil)
	c.Check(fl, Equals, 1.305)
}

func (s *configureHandlerSuite) TestBeforeUseDefaultsMissingHook(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	const mockGadgetSnapYaml = `
name: canonical-pc
type: gadget
`
	var mockGadgetYaml = []byte(`
defaults:
  test-snap-ididididididididididid:
      bar: baz
      num: 1.305

volumes:
    volume-id:
        bootloader: grub
`)

	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(1)})
	err := ioutil.WriteFile(filepath.Join(info.MountDir(), "meta", "gadget.yaml"), mockGadgetYaml, 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	snapstate.Set(s.state, "canonical-pc", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "canonical-pc", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "gadget",
	})

	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "test-snap", Revision: snap.R(11), SnapID: "test-snap-ididididididididididid"},
		},
		Current:  snap.R(11),
		SnapType: "app",
	})
	s.state.Unlock()

	// Initialize context
	s.context.Lock()
	s.context.Set("use-defaults", true)
	s.context.Unlock()

	err = s.handler.Before()
	c.Check(err, ErrorMatches, `cannot apply gadget config defaults for snap "test-snap", no configure hook`)
}
