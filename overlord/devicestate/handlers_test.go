// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package devicestate_test

import (
	"context"
	"fmt"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storecontext"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

// TODO: should we move this into a new handlers suite?
func (s *deviceMgrSuite) TestSetModelHandlerNewRevision(c *C) {
	s.state.Lock()
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"revision":       "1",
		"required-snaps": []interface{}{"foo", "bar"},
	})
	// foo and bar
	fooSI := &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(1),
	}
	barSI := &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(1),
	}
	pcKernelSI := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(1),
	}
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		SnapType: "app",
		Active:   true,
		Sequence: []*snap.SideInfo{fooSI},
		Current:  fooSI.Revision,
		Flags:    snapstate.Flags{Required: true},
	})
	snapstate.Set(s.state, "bar", &snapstate.SnapState{
		SnapType: "app",
		Active:   true,
		Sequence: []*snap.SideInfo{barSI},
		Current:  barSI.Revision,
		Flags:    snapstate.Flags{Required: true},
	})
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: []*snap.SideInfo{pcKernelSI},
		Current:  pcKernelSI.Revision,
		Flags:    snapstate.Flags{Required: true},
	})
	s.state.Unlock()

	newModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "other-kernel",
		"gadget":         "pc",
		"revision":       "2",
		"required-snaps": []interface{}{"foo"},
	})

	s.state.Lock()
	t := s.state.NewTask("set-model", "set-model test")
	chg := s.state.NewChange("dummy", "...")
	chg.Set("new-model", string(asserts.Encode(newModel)))
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	m, err := s.mgr.Model()
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, newModel)

	c.Assert(chg.Err(), IsNil)

	// check required
	var fooState snapstate.SnapState
	var barState snapstate.SnapState
	err = snapstate.Get(s.state, "foo", &fooState)
	c.Assert(err, IsNil)
	err = snapstate.Get(s.state, "bar", &barState)
	c.Assert(err, IsNil)
	c.Check(fooState.Flags.Required, Equals, true)
	c.Check(barState.Flags.Required, Equals, false)
	// the kernel is no longer required
	var kernelState snapstate.SnapState
	err = snapstate.Get(s.state, "pc-kernel", &kernelState)
	c.Assert(err, IsNil)
	c.Check(kernelState.Flags.Required, Equals, false)
}

func (s *deviceMgrSuite) TestSetModelHandlerSameRevisionNoError(c *C) {
	model := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"revision":     "1",
	})

	s.state.Lock()

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})
	err := assertstate.Add(s.state, model)
	c.Assert(err, IsNil)

	t := s.state.NewTask("set-model", "set-model test")
	chg := s.state.NewChange("dummy", "...")
	chg.Set("new-model", string(asserts.Encode(model)))
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.Err(), IsNil)
}

func (s *deviceMgrSuite) TestSetModelHandlerStoreSwitch(c *C) {
	s.state.Lock()
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"revision":     "1",
	})
	s.state.Unlock()

	newModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"store":        "switched-store",
		"revision":     "2",
	})

	s.newFakeStore = func(devBE storecontext.DeviceBackend) snapstate.StoreService {
		mod, err := devBE.Model()
		c.Check(err, IsNil)
		if err == nil {
			c.Check(mod, DeepEquals, newModel)
		}
		return &freshSessionStore{}
	}

	s.state.Lock()
	t := s.state.NewTask("set-model", "set-model test")
	chg := s.state.NewChange("dummy", "...")
	chg.Set("new-model", string(asserts.Encode(newModel)))
	chg.Set("device", auth.DeviceState{
		Brand:           "canonical",
		Model:           "pc-model",
		SessionMacaroon: "switched-store-session",
	})
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.Err(), IsNil)

	m, err := s.mgr.Model()
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, newModel)

	device, err := devicestatetest.Device(s.state)
	c.Assert(err, IsNil)
	c.Check(device, DeepEquals, &auth.DeviceState{
		Brand:           "canonical",
		Model:           "pc-model",
		SessionMacaroon: "switched-store-session",
	})

	// cleanup
	_, ok := devicestate.CachedRemodelCtx(chg)
	c.Check(ok, Equals, true)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	_, ok = devicestate.CachedRemodelCtx(chg)
	c.Check(ok, Equals, false)
}

func (s *deviceMgrSuite) TestSetModelHandlerRereg(c *C) {
	s.state.Lock()
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "orig-serial",
	})
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "orig-serial")
	s.state.Unlock()

	newModel := s.brands.Model("canonical", "rereg-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	s.newFakeStore = func(devBE storecontext.DeviceBackend) snapstate.StoreService {
		mod, err := devBE.Model()
		c.Check(err, IsNil)
		if err == nil {
			c.Check(mod, DeepEquals, newModel)
		}
		return &freshSessionStore{}
	}

	s.state.Lock()
	t := s.state.NewTask("set-model", "set-model test")
	chg := s.state.NewChange("dummy", "...")
	chg.Set("new-model", string(asserts.Encode(newModel)))
	chg.Set("device", auth.DeviceState{
		Brand:           "canonical",
		Model:           "rereg-model",
		Serial:          "orig-serial",
		SessionMacaroon: "switched-store-session",
	})
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.Err(), IsNil)

	m, err := s.mgr.Model()
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, newModel)

	device, err := devicestatetest.Device(s.state)
	c.Assert(err, IsNil)
	c.Check(device, DeepEquals, &auth.DeviceState{
		Brand:           "canonical",
		Model:           "rereg-model",
		Serial:          "orig-serial",
		SessionMacaroon: "switched-store-session",
	})
}

func (s *deviceMgrSuite) TestDoPrepareRemodeling(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	var testStore snapstate.StoreService

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, true)
		c.Check(deviceCtx, NotNil)
		c.Check(deviceCtx.ForRemodeling(), Equals, true)

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.DownloadAndChecksDoneEdge)
		return ts, nil
	})
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "orig-serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:           "canonical",
		Model:           "pc-model",
		Serial:          "orig-serial",
		SessionMacaroon: "old-session",
	})

	new := s.brands.Model("canonical", "rereg-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1", "new-required-snap-2"},
	})

	freshStore := &freshSessionStore{}
	testStore = freshStore

	s.newFakeStore = func(devBE storecontext.DeviceBackend) snapstate.StoreService {
		mod, err := devBE.Model()
		c.Check(err, IsNil)
		if err == nil {
			c.Check(mod, DeepEquals, new)
		}
		return testStore
	}

	cur, err := s.mgr.Model()
	c.Assert(err, IsNil)

	remodCtx, err := devicestate.RemodelCtx(s.state, cur, new)
	c.Assert(err, IsNil)

	c.Check(remodCtx.Kind(), Equals, devicestate.ReregRemodel)

	chg := s.state.NewChange("remodel", "...")
	remodCtx.Init(chg)
	t := s.state.NewTask("prepare-remodeling", "...")
	chg.AddTask(t)

	// set new serial
	s.makeSerialAssertionInState(c, "canonical", "rereg-model", "orig-serial")
	chg.Set("device", auth.DeviceState{
		Brand:           "canonical",
		Model:           "rereg-model",
		Serial:          "orig-serial",
		SessionMacaroon: "switched-store-session",
	})

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	c.Assert(chg.Err(), IsNil)

	c.Check(freshStore.ensureDeviceSession, Equals, 1)

	// check that the expected tasks were injected
	tl := chg.Tasks()
	// 1 prepare-remodeling
	// 2 snaps * 3 tasks (from the mock install above) +
	// 1 "set-model" task at the end
	c.Assert(tl, HasLen, 1+2*3+1)

	// sanity
	c.Check(tl[1].Kind(), Equals, "fake-download")
	c.Check(tl[1+2*3].Kind(), Equals, "set-model")

	// cleanup
	// fake completion
	for _, t := range tl[1:] {
		t.SetStatus(state.DoneStatus)
	}
	_, ok := devicestate.CachedRemodelCtx(chg)
	c.Check(ok, Equals, true)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	_, ok = devicestate.CachedRemodelCtx(chg)
	c.Check(ok, Equals, false)
}

type preseedBaseSuite struct {
	deviceMgrBaseSuite
	restorePreseedMode func()
	cmdUmount          *testutil.MockCmd
	cmdSystemctl       *testutil.MockCmd
}

func (s *preseedBaseSuite) SetUpTest(c *C, preseed bool) {
	s.restorePreseedMode = release.MockPreseedMode(func() bool { return preseed })
	// preseed mode helper needs to be mocked before setting up
	// deviceMgrBaseSuite due to device Manager init.
	s.deviceMgrBaseSuite.SetUpTest(c)

	s.cmdUmount = testutil.MockCommand(c, "umount", "")
	s.cmdSystemctl = testutil.MockCommand(c, "systemctl", "")

	st := s.state
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(3), SnapID: "test-snap-id"}
	snaptest.MockSnap(c, `name: test-snap
version: 1.0
apps:
 srv:
  command: bin/service
  daemon: simple
`, si)

	snapstate.Set(st, "test-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
		SnapType: "app",
	})
}

func (s *preseedBaseSuite) TearDownTest(c *C) {
	s.deviceMgrBaseSuite.TearDownTest(c)
	s.cmdUmount.Restore()
	s.cmdSystemctl.Restore()
}

type preseedModeSuite struct {
	preseedBaseSuite
}

var _ = Suite(&preseedModeSuite{})

func (s *preseedModeSuite) SetUpTest(c *C) {
	s.preseedBaseSuite.SetUpTest(c, true)
}

func (s *preseedModeSuite) TearDownTest(c *C) {
	s.preseedBaseSuite.TearDownTest(c)
}

func (s *preseedModeSuite) TestDoMarkPreseeded(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("firstboot seeding", "...")
	t := st.NewTask("mark-preseeded", "...")
	chg.AddTask(t)

	st.Unlock()
	s.se.Ensure()
	s.se.Wait()
	st.Lock()

	// mark-preseeded task is left in Doing, meaning it will be re-executed
	// after restart in normal (not preseeding) mode.
	c.Check(t.Status(), Equals, state.DoingStatus)

	var preseeded bool
	c.Check(t.Get("preseeded", &preseeded), IsNil)
	c.Check(preseeded, Equals, true)

	// core snap was "manually" unmounted
	c.Check(s.cmdUmount.Calls(), DeepEquals, [][]string{
		{"umount", "-d", "-l", filepath.Join(dirs.GlobalRootDir, "/snap/test-snap/3")},
	})

	// and snapd stop was requested
	c.Check(s.restartRequests, DeepEquals, []state.RestartType{state.StopDaemon})

	s.cmdUmount.ForgetCalls()

	// re-trying mark-preseeded task has no effect
	st.Unlock()
	s.se.Ensure()
	s.se.Wait()
	st.Lock()

	c.Check(s.cmdUmount.Calls(), HasLen, 0)
	c.Check(t.Status(), Equals, state.DoingStatus)
}

func (s *preseedModeSuite) TestEnsureSeededPreseedFlag(c *C) {
	called := false
	restore := devicestate.MockPopulateStateFromSeed(func(st *state.State, opts *devicestate.PopulateStateFromSeedOptions, tm timings.Measurer) ([]*state.TaskSet, error) {
		called = true
		c.Check(opts.Preseed, Equals, true)
		return nil, nil
	})
	defer restore()

	err := devicestate.EnsureSeeded(s.mgr)
	c.Assert(err, IsNil)
	c.Check(called, Equals, true)
}

type preseedDoneSuite struct {
	preseedBaseSuite
}

var _ = Suite(&preseedDoneSuite{})

func (s *preseedDoneSuite) SetUpTest(c *C) {
	s.preseedBaseSuite.SetUpTest(c, false)
}

func (s *preseedDoneSuite) TearDownTest(c *C) {
	s.preseedBaseSuite.TearDownTest(c)
}

func (s *preseedDoneSuite) TestDoMarkPreseededAfterFirstboot(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("firstboot seeding", "...")
	t := st.NewTask("mark-preseeded", "...")
	chg.AddTask(t)
	t.SetStatus(state.DoingStatus)

	st.Unlock()
	s.se.Ensure()
	s.se.Wait()
	st.Lock()

	// no umount calls expected, just transitioned to Done status.
	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Check(s.cmdUmount.Calls(), HasLen, 0)
	c.Check(s.restartRequests, HasLen, 0)

	c.Check(s.cmdSystemctl.Calls(), DeepEquals, [][]string{
		{"systemctl", "--root", dirs.GlobalRootDir, "enable", "snap.test-snap.srv.service"},
	})
}
