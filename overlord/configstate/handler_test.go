// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers
// +build !nomanagers

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
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/testutil"
)

func TestConfigState(t *testing.T) { TestingT(t) }

type configureHandlerSuite struct {
	testutil.BaseTest

	state   *state.State
	context *hookstate.Context
	handler hookstate.Handler
}

var _ = Suite(&configureHandlerSuite{})

func (s *configureHandlerSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })

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

	s.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}

	var err error
	s.context, err = hookstate.NewContext(task, task.State(), setup, hooktest.NewMockHandler(), "")
	c.Assert(err, IsNil)

	s.handler = configstate.NewConfigureHandler(s.context)
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

func makeModel(override map[string]interface{}) *asserts.Model {
	model := map[string]interface{}{
		"type":         "model",
		"authority-id": "brand",
		"series":       "16",
		"brand-id":     "brand",
		"model":        "baz-3000",
		"architecture": "armhf",
		"gadget":       "brand-gadget",
		"kernel":       "kernel",
		"timestamp":    "2018-01-01T08:00:00+00:00",
	}
	return assertstest.FakeAssertion(model, override).(*asserts.Model)
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
  testsnapidididididididididididid:
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

	r = snapstatetest.MockDeviceModel(makeModel(map[string]interface{}{
		"gadget": "canonical-pc",
	}))
	defer r()

	const mockTestSnapYaml = `
name: test-snap
hooks:
    configure:
`

	snaptest.MockSnap(c, mockTestSnapYaml, &snap.SideInfo{Revision: snap.R(11)})
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "test-snap", Revision: snap.R(11), SnapID: "testsnapidididididididididididid"},
		},
		Current:  snap.R(11),
		SnapType: "app",
	})
	s.state.Unlock()

	// Initialize context
	s.context.Lock()
	s.context.Set("use-defaults", true)
	s.context.Unlock()

	c.Assert(s.handler.Before(), IsNil)

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
  testsnapidididididididididididid:
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

	r = snapstatetest.MockDeviceModel(makeModel(map[string]interface{}{
		"gadget": "canonical-pc",
	}))
	defer r()

	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "test-snap", Revision: snap.R(11), SnapID: "testsnapidididididididididididid"},
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

type configcoreHandlerSuite struct {
	testutil.BaseTest

	o     *overlord.Overlord
	state *state.State
}

var _ = Suite(&configcoreHandlerSuite{})

func (s *configcoreHandlerSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })

	s.o = overlord.Mock()
	s.state = s.o.State()

	restore := snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	s.AddCleanup(restore)

	hookMgr, err := hookstate.Manager(s.state, s.o.TaskRunner())
	c.Assert(err, IsNil)
	s.o.AddManager(hookMgr)
	r := configstate.MockConfigcoreExportExperimentalFlags(func(_ configcore.ConfGetter) error {
		return nil
	})
	s.AddCleanup(r)

	err = configstate.Init(s.state, hookMgr)

	c.Assert(err, IsNil)
	s.o.AddManager(s.o.TaskRunner())

	r = snapstatetest.MockDeviceModel(makeModel(map[string]interface{}{
		"gadget": "canonical-pc",
	}))
	s.AddCleanup(r)

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", true)
	snapstate.Set(s.state, "canonical-pc", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "canonical-pc", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "gadget",
	})
}

const mockGadgetSnapYaml = `
name: canonical-pc
type: gadget
`

func (s *configcoreHandlerSuite) TestRunsWhenSnapdOnly(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	var mockGadgetYaml = `
defaults:
  system:
      foo: bar

volumes:
    volume-id:
        bootloader: grub
`
	s.state.Lock()
	defer s.state.Unlock()

	ts := configstate.Configure(s.state, "core", nil, snapstate.UseConfigDefaults)
	chg := s.state.NewChange("configure-core", "configure core")
	chg.AddAll(ts)

	snaptest.MockSnapWithFiles(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(1)}, [][]string{
		{"meta/gadget.yaml", mockGadgetYaml},
	})

	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "snapd", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "snapd",
	})

	witnessConfigcoreRun := func(dev sysconfig.Device, conf configcore.Conf) error {
		c.Check(dev.Kernel(), Equals, "kernel")
		// called with no state lock!
		conf.State().Lock()
		defer conf.State().Unlock()
		var val string
		err := conf.Get("core", "foo", &val)
		c.Assert(err, IsNil)
		c.Check(val, Equals, "bar")
		return nil
	}
	r = configstate.MockConfigcoreRun(witnessConfigcoreRun)
	defer r()

	s.state.Unlock()
	err := s.o.Settle(5 * time.Second)
	s.state.Lock()
	// Initialize context
	c.Assert(err, IsNil)

	tr := config.NewTransaction(s.state)
	var foo string
	err = tr.Get("core", "foo", &foo)
	c.Assert(err, IsNil)
	c.Check(foo, Equals, "bar")
}

func (s *configcoreHandlerSuite) TestRunsWhenCoreOnly(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	var mockGadgetYaml = `
defaults:
  system:
      foo: bar

volumes:
    volume-id:
        bootloader: grub
`
	s.state.Lock()
	defer s.state.Unlock()

	ts := configstate.Configure(s.state, "core", nil, snapstate.UseConfigDefaults)
	chg := s.state.NewChange("configure-core", "configure core")
	chg.AddAll(ts)

	snaptest.MockSnapWithFiles(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(1)}, [][]string{
		{"meta/gadget.yaml", mockGadgetYaml},
	})

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "os",
	})

	witnessConfigcoreRun := func(dev sysconfig.Device, conf configcore.Conf) error {
		c.Check(dev.Kernel(), Equals, "kernel")
		// called with no state lock!
		conf.State().Lock()
		defer conf.State().Unlock()
		var val string
		err := conf.Get("core", "foo", &val)
		c.Assert(err, IsNil)
		c.Check(val, Equals, "bar")
		return nil
	}
	r = configstate.MockConfigcoreRun(witnessConfigcoreRun)
	defer r()

	s.state.Unlock()
	err := s.o.Settle(5 * time.Second)
	s.state.Lock()
	// Initialize context
	c.Assert(err, IsNil)

	tr := config.NewTransaction(s.state)
	var foo string
	err = tr.Get("core", "foo", &foo)
	c.Assert(err, IsNil)
	c.Check(foo, Equals, "bar")
}
