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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storecontext"
	"github.com/snapcore/snapd/snap"
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
