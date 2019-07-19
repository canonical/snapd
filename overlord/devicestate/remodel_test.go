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
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storecontext"
	"github.com/snapcore/snapd/store/storetest"
)

type remodelLogicSuite struct {
	state *state.State
	mgr   *devicestate.DeviceManager

	storeSigning *assertstest.StoreStack
	brands       *assertstest.SigningAccounts

	capturedDevBE storecontext.DeviceBackend
	dummyStore    snapstate.StoreService
}

var _ = Suite(&remodelLogicSuite{})

func (s *remodelLogicSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	o := overlord.Mock()
	s.state = o.State()

	s.storeSigning = assertstest.NewStoreStack("canonical", nil)
	s.brands = assertstest.NewSigningAccounts(s.storeSigning)
	s.brands.Register("my-brand", brandPrivKey, nil)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.storeSigning.Trusted,
	})
	c.Assert(err, IsNil)

	func() {
		s.state.Lock()
		defer s.state.Unlock()
		assertstate.ReplaceDB(s.state, db)

		assertstatetest.AddMany(s.state, s.storeSigning.StoreAccountKey(""))
		assertstatetest.AddMany(s.state, s.brands.AccountsAndKeys("my-brand")...)
	}()

	s.dummyStore = new(storetest.Store)

	newStore := func(devBE storecontext.DeviceBackend) snapstate.StoreService {
		s.capturedDevBE = devBE
		return s.dummyStore
	}

	hookMgr, err := hookstate.Manager(s.state, o.TaskRunner())
	c.Assert(err, IsNil)
	s.mgr, err = devicestate.Manager(s.state, hookMgr, o.TaskRunner(), newStore)
	c.Assert(err, IsNil)
}

func (s *remodelLogicSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

var modelDefaults = map[string]interface{}{
	"architecture":   "amd64",
	"kernel":         "my-brand-kernel",
	"gadget":         "my-brand-gadget",
	"store":          "my-brand-store",
	"required-snaps": []interface{}{"required1"},
}

func fakeRemodelingModel(extra map[string]interface{}) *asserts.Model {
	primary := map[string]interface{}{
		"type":         "model",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model",
	}
	return assertstest.FakeAssertion(primary, modelDefaults, extra).(*asserts.Model)
}

func (s *remodelLogicSuite) TestClassifyRemodel(c *C) {
	oldModel := fakeRemodelingModel(nil)

	cases := []struct {
		newHeaders map[string]interface{}
		kind       devicestate.RemodelKind
	}{
		{map[string]interface{}{}, devicestate.UpdateRemodel},
		{map[string]interface{}{
			"required-snaps": []interface{}{"required1", "required2"},
			"revision":       "1",
		}, devicestate.UpdateRemodel},
		{map[string]interface{}{
			"store":    "my-other-store",
			"revision": "1",
		}, devicestate.StoreSwitchRemodel},
		{map[string]interface{}{
			"model": "my-other-model",
			"store": "my-other-store",
		}, devicestate.ReregRemodel},
		{map[string]interface{}{
			"authority-id": "other-brand",
			"brand-id":     "other-brand",
			"model":        "other-model",
		}, devicestate.ReregRemodel},
		{map[string]interface{}{
			"authority-id":   "other-brand",
			"brand-id":       "other-brand",
			"model":          "other-model",
			"required-snaps": []interface{}{"other-required1"},
		}, devicestate.ReregRemodel},
		{map[string]interface{}{
			"authority-id": "other-brand",
			"brand-id":     "other-brand",
			"model":        "other-model",
			"store":        "my-other-store",
		}, devicestate.ReregRemodel},
	}

	for _, t := range cases {
		newModel := fakeRemodelingModel(t.newHeaders)
		c.Check(devicestate.ClassifyRemodel(oldModel, newModel), Equals, t.kind)
	}
}

func (s *remodelLogicSuite) TestUpdateRemodelContext(c *C) {
	oldModel := fakeRemodelingModel(nil)
	newModel := fakeRemodelingModel(map[string]interface{}{
		"required-snaps": []interface{}{"required1", "required2"},
		"revision":       "1",
	})

	s.state.Lock()
	defer s.state.Unlock()

	remodCtx, err := devicestate.RemodelCtx(s.state, oldModel, newModel)
	c.Assert(err, IsNil)

	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.UpdateRemodel)

	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	var encNewModel string
	c.Assert(chg.Get("new-model", &encNewModel), IsNil)

	c.Check(encNewModel, Equals, string(asserts.Encode(newModel)))

	c.Check(remodCtx.Model(), DeepEquals, newModel)
	// an update remodel does not need a new/dedicated store
	c.Check(remodCtx.Store(), IsNil)
}

func (s *remodelLogicSuite) TestNewStoreRemodelContextInit(c *C) {
	oldModel := fakeRemodelingModel(nil)
	newModel := fakeRemodelingModel(map[string]interface{}{
		"store":    "my-other-store",
		"revision": "1",
	})

	s.state.Lock()
	defer s.state.Unlock()

	// we have a device state
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:           "my-brand",
		Model:           "my-model",
		Serial:          "serialserialserial",
		SessionMacaroon: "prev-session",
	})

	remodCtx, err := devicestate.RemodelCtx(s.state, oldModel, newModel)
	c.Assert(err, IsNil)

	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.StoreSwitchRemodel)

	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	var encNewModel string
	c.Assert(chg.Get("new-model", &encNewModel), IsNil)

	c.Check(encNewModel, Equals, string(asserts.Encode(newModel)))

	var device *auth.DeviceState
	c.Assert(chg.Get("device", &device), IsNil)
	// session macaroon was reset
	c.Check(device, DeepEquals, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})

	c.Check(remodCtx.Model(), DeepEquals, newModel)
}

func (s *remodelLogicSuite) TestRemodelDeviceBackendNoChangeYet(c *C) {
	oldModel := fakeRemodelingModel(nil)
	newModel := fakeRemodelingModel(map[string]interface{}{
		"store":    "my-other-store",
		"revision": "1",
	})

	s.state.Lock()
	defer s.state.Unlock()

	// we have a device state
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})

	remodCtx, err := devicestate.RemodelCtx(s.state, oldModel, newModel)
	c.Assert(err, IsNil)

	devBE := s.capturedDevBE
	c.Check(devBE, NotNil)

	device, err := devBE.Device()
	c.Assert(err, IsNil)
	c.Check(device, DeepEquals, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})

	mod, err := devBE.Model()
	c.Assert(err, IsNil)
	c.Check(mod, DeepEquals, newModel)

	// set device state for the context
	device1 := &auth.DeviceState{
		Brand:           "my-brand",
		Model:           "my-model",
		Serial:          "serialserialserial",
		SessionMacaroon: "session",
	}

	err = devBE.SetDevice(device1)
	c.Assert(err, IsNil)

	device, err = devBE.Device()
	c.Assert(err, IsNil)
	c.Check(device, DeepEquals, device1)

	// have a change
	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	// check device state is preserved across association with a Change
	device, err = devBE.Device()
	c.Assert(err, IsNil)
	c.Check(device, DeepEquals, device1)
}

func (s *remodelLogicSuite) TestRemodelDeviceBackend(c *C) {
	oldModel := fakeRemodelingModel(nil)
	newModel := fakeRemodelingModel(map[string]interface{}{
		"store":    "my-other-store",
		"revision": "1",
	})

	s.state.Lock()
	defer s.state.Unlock()

	// we have a device state
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})

	remodCtx, err := devicestate.RemodelCtx(s.state, oldModel, newModel)
	c.Assert(err, IsNil)

	devBE := devicestate.RemodelDeviceBackend(remodCtx)

	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	device, err := devBE.Device()
	c.Assert(err, IsNil)
	c.Check(device, DeepEquals, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})

	mod, err := devBE.Model()
	c.Assert(err, IsNil)
	c.Check(mod, DeepEquals, newModel)

	// set a device state for the context
	device1 := &auth.DeviceState{
		Brand:           "my-brand",
		Model:           "my-model",
		Serial:          "serialserialserial",
		SessionMacaroon: "session",
	}

	err = devBE.SetDevice(device1)
	c.Assert(err, IsNil)

	// it's stored on change now
	var device2 *auth.DeviceState
	c.Assert(chg.Get("device", &device2), IsNil)
	c.Check(device2, DeepEquals, device1)

	device, err = devBE.Device()
	c.Assert(err, IsNil)
	c.Check(device, DeepEquals, device1)
}

func (s *remodelLogicSuite) TestRemodelDeviceBackendIsolation(c *C) {
	oldModel := fakeRemodelingModel(nil)
	newModel := fakeRemodelingModel(map[string]interface{}{
		"store":    "my-other-store",
		"revision": "1",
	})

	s.state.Lock()
	defer s.state.Unlock()

	// we have a device state
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})

	remodCtx, err := devicestate.RemodelCtx(s.state, oldModel, newModel)
	c.Assert(err, IsNil)

	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	devBE := devicestate.RemodelDeviceBackend(remodCtx)

	err = devBE.SetDevice(&auth.DeviceState{
		Brand:           "my-brand",
		Model:           "my-model",
		Serial:          "serialserialserial",
		SessionMacaroon: "remodel-session",
	})
	c.Assert(err, IsNil)

	// the global device state is as before
	expectedGlobalDevice := &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	}

	device, err := s.mgr.StoreContextBackend().Device()
	c.Assert(err, IsNil)
	c.Check(device, DeepEquals, expectedGlobalDevice)
}
func (s *remodelLogicSuite) TestNewStoreRemodelContextStore(c *C) {
	oldModel := fakeRemodelingModel(nil)
	newModel := fakeRemodelingModel(map[string]interface{}{
		"store":    "my-other-store",
		"revision": "1",
	})

	s.state.Lock()
	defer s.state.Unlock()

	// we have a device state
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:           "my-brand",
		Model:           "my-model",
		Serial:          "serialserialserial",
		SessionMacaroon: "prev-session",
	})

	remodCtx, err := devicestate.RemodelCtx(s.state, oldModel, newModel)
	c.Assert(err, IsNil)

	c.Check(s.capturedDevBE, NotNil)

	// new store remodel context device state built ignoring the
	// previous session
	device1, err := s.capturedDevBE.Device()
	c.Assert(err, IsNil)
	c.Check(device1, DeepEquals, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})

	sto := remodCtx.Store()
	c.Check(sto, Equals, s.dummyStore)

	// store is kept and not rebuilt
	s.dummyStore = nil

	sto1 := remodCtx.Store()
	c.Check(sto1, Equals, sto)
}

func (s *remodelLogicSuite) TestNewStoreRemodelContextFinish(c *C) {
	oldModel := fakeRemodelingModel(nil)
	newModel := fakeRemodelingModel(map[string]interface{}{
		"store":    "my-other-store",
		"revision": "1",
	})

	s.state.Lock()
	defer s.state.Unlock()

	// we have a device state
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:           "my-brand",
		Model:           "my-model",
		Serial:          "serialserialserial",
		SessionMacaroon: "orig-session",
	})

	remodCtx, err := devicestate.RemodelCtx(s.state, oldModel, newModel)
	c.Assert(err, IsNil)

	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	devBE := devicestate.RemodelDeviceBackend(remodCtx)

	err = devBE.SetDevice(&auth.DeviceState{
		Brand:           "my-brand",
		Model:           "my-model",
		Serial:          "serialserialserial",
		SessionMacaroon: "new-session",
	})
	c.Assert(err, IsNil)

	err = remodCtx.Finish()
	c.Assert(err, IsNil)

	// the global device now matches the state reached in the remodel
	expectedGlobalDevice := &auth.DeviceState{
		Brand:           "my-brand",
		Model:           "my-model",
		Serial:          "serialserialserial",
		SessionMacaroon: "new-session",
	}

	device, err := s.mgr.StoreContextBackend().Device()
	c.Assert(err, IsNil)
	c.Check(device, DeepEquals, expectedGlobalDevice)
}

func (s *remodelLogicSuite) TestNewStoreRemodelContextFinishVsGlobalUpdateDeviceAuth(c *C) {
	oldModel := fakeRemodelingModel(nil)
	newModel := fakeRemodelingModel(map[string]interface{}{
		"store":    "my-other-store",
		"revision": "1",
	})

	s.state.Lock()
	defer s.state.Unlock()

	// we have a device state
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:           "my-brand",
		Model:           "my-model",
		Serial:          "serialserialserial",
		SessionMacaroon: "old-session",
	})

	remodCtx, err := devicestate.RemodelCtx(s.state, oldModel, newModel)
	c.Assert(err, IsNil)

	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	devBE := devicestate.RemodelDeviceBackend(remodCtx)

	err = devBE.SetDevice(&auth.DeviceState{
		Brand:           "my-brand",
		Model:           "my-model",
		Serial:          "serialserialserial",
		SessionMacaroon: "remodel-session",
	})
	c.Assert(err, IsNil)

	// global store device and auth context
	scb := s.mgr.StoreContextBackend()
	dac := storecontext.New(s.state, scb)
	// this is the unlikely start of the global store trying to
	// refresh the session
	s.state.Unlock()
	globalDevice, err := dac.Device()
	s.state.Lock()
	c.Assert(err, IsNil)
	c.Check(globalDevice.SessionMacaroon, Equals, "old-session")

	err = remodCtx.Finish()
	c.Assert(err, IsNil)

	s.state.Unlock()
	device1, err := dac.UpdateDeviceAuth(globalDevice, "fresh-session")
	s.state.Lock()
	c.Assert(err, IsNil)

	// the global device now matches the state reached in the remodel
	expectedGlobalDevice := &auth.DeviceState{
		Brand:           "my-brand",
		Model:           "my-model",
		Serial:          "serialserialserial",
		SessionMacaroon: "remodel-session",
	}

	s.state.Unlock()
	device, err := dac.Device()
	s.state.Lock()
	c.Assert(err, IsNil)
	c.Check(device, DeepEquals, expectedGlobalDevice)

	// also this was already the case
	c.Check(device1, DeepEquals, expectedGlobalDevice)
}

func (s *remodelLogicSuite) TestRemodelDeviceBackendKeptSerial(c *C) {
	oldModel := fakeRemodelingModel(nil)
	newModel := fakeRemodelingModel(map[string]interface{}{
		"store":    "my-other-store",
		"revision": "1",
	})

	s.state.Lock()
	defer s.state.Unlock()

	// we have a device state and serial
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial1",
	})

	makeSerialAssertionInState(c, s.brands, s.state, "my-brand", "my-model", "serialserialserial1")

	serial, err := s.mgr.Serial()
	c.Assert(err, IsNil)
	c.Check(serial.Serial(), Equals, "serialserialserial1")

	remodCtx, err := devicestate.RemodelCtx(s.state, oldModel, newModel)
	c.Assert(err, IsNil)

	devBE := devicestate.RemodelDeviceBackend(remodCtx)

	serial0, err := devBE.Serial()
	c.Assert(err, IsNil)
	c.Check(serial0.Serial(), Equals, "serialserialserial1")

	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	serial0, err = devBE.Serial()
	c.Assert(err, IsNil)
	c.Check(serial0.Serial(), Equals, "serialserialserial1")
}

func (s *remodelLogicSuite) TestRemodelContextForTaskAndCaching(c *C) {
	oldModel := s.brands.Model("my-brand", "my-model", modelDefaults)
	newModel := s.brands.Model("my-brand", "my-model", modelDefaults, map[string]interface{}{
		"store":    "my-other-store",
		"revision": "1",
	})

	s.state.Lock()
	defer s.state.Unlock()

	// we have a device state
	assertstatetest.AddMany(s.state, oldModel)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})

	remodCtx, err := devicestate.RemodelCtx(s.state, oldModel, newModel)
	c.Assert(err, IsNil)

	c.Check(remodCtx.ForRemodeling(), Equals, true)

	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	t := s.state.NewTask("remodel-task-1", "...")
	chg.AddTask(t)

	// caching
	remodCtx1, err := devicestate.RemodelCtxFromTask(t)
	c.Assert(err, IsNil)
	c.Check(remodCtx1, Equals, remodCtx)

	// if the context goes away (e.g. because of restart) we
	// compute a new one
	devicestate.CleanupRemodelCtx(chg)

	remodCtx2, err := devicestate.RemodelCtxFromTask(t)
	c.Assert(err, IsNil)
	c.Check(remodCtx2 != remodCtx, Equals, true)
	c.Check(remodCtx2.Model(), DeepEquals, newModel)
}

func (s *remodelLogicSuite) TestRemodelContextForTaskNo(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// task is nil
	remodCtx1, err := devicestate.RemodelCtxFromTask(nil)
	c.Check(err, Equals, state.ErrNoState)
	c.Check(remodCtx1, IsNil)

	// no change
	t := s.state.NewTask("random-task", "...")
	_, err = devicestate.RemodelCtxFromTask(t)
	c.Check(err, Equals, state.ErrNoState)

	// not a remodel change
	chg := s.state.NewChange("not-remodel", "...")
	chg.AddTask(t)
	_, err = devicestate.RemodelCtxFromTask(t)
	c.Check(err, Equals, state.ErrNoState)
}

func (s *remodelLogicSuite) setupForRereg(c *C) (oldModel, newModel *asserts.Model) {
	oldModel = s.brands.Model("my-brand", "my-model", modelDefaults)
	newModel = s.brands.Model("my-brand", "my-model", modelDefaults, map[string]interface{}{
		"authority-id": "other-brand",
		"brand-id":     "other-brand",
		"model":        "other-model",
		"store":        "other-store",
	})

	s.state.Lock()
	defer s.state.Unlock()

	encDevKey, err := asserts.EncodePublicKey(devKey.PublicKey())
	c.Assert(err, IsNil)
	serial, err := s.brands.Signing("my-brand").Sign(asserts.SerialType, map[string]interface{}{
		"authority-id":        "my-brand",
		"brand-id":            "my-brand",
		"model":               "my-model",
		"serial":              "orig-serial",
		"device-key":          string(encDevKey),
		"device-key-sha3-384": devKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	assertstatetest.AddMany(s.state, oldModel, serial)

	return oldModel, newModel
}

func (s *remodelLogicSuite) TestReregRemodelContextInit(c *C) {
	oldModel, newModel := s.setupForRereg(c)

	s.state.Lock()
	defer s.state.Unlock()

	// we have a device state
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:           "my-brand",
		Model:           "my-model",
		Serial:          "orig-serial",
		KeyID:           "device-key-id",
		SessionMacaroon: "prev-session",
	})

	remodCtx, err := devicestate.RemodelCtx(s.state, oldModel, newModel)
	c.Assert(err, IsNil)

	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.ReregRemodel)

	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	var encNewModel string
	c.Assert(chg.Get("new-model", &encNewModel), IsNil)

	c.Check(encNewModel, Equals, string(asserts.Encode(newModel)))

	var device *auth.DeviceState
	c.Assert(chg.Get("device", &device), IsNil)
	// fresh device state before registration but with device-key
	c.Check(device, DeepEquals, &auth.DeviceState{
		Brand: "other-brand",
		Model: "other-model",
		KeyID: "device-key-id",
	})

	c.Check(remodCtx.Model(), DeepEquals, newModel)

	// caching
	t := s.state.NewTask("remodel-task-1", "...")
	chg.AddTask(t)

	remodCtx1, err := devicestate.RemodelCtxFromTask(t)
	c.Assert(err, IsNil)
	c.Check(remodCtx1, Equals, remodCtx)
}

func (s *remodelLogicSuite) TestReregRemodelContextAsRegistrationContext(c *C) {
	oldModel, newModel := s.setupForRereg(c)

	s.state.Lock()
	defer s.state.Unlock()

	// we have a device state
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:           "my-brand",
		Model:           "my-model",
		Serial:          "orig-serial",
		KeyID:           "device-key-id",
		SessionMacaroon: "prev-session",
	})

	remodCtx, err := devicestate.RemodelCtx(s.state, oldModel, newModel)
	c.Assert(err, IsNil)

	c.Check(remodCtx.Kind(), Equals, devicestate.ReregRemodel)

	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	regCtx := remodCtx.(devicestate.RegistrationContext)

	c.Check(regCtx.ForRemodeling(), Equals, true)
	device1, err := regCtx.Device()
	c.Assert(err, IsNil)
	// fresh device state before registration but with device-key
	c.Check(device1, DeepEquals, &auth.DeviceState{
		Brand: "other-brand",
		Model: "other-model",
		KeyID: "device-key-id",
	})
	c.Check(regCtx.GadgetForSerialRequestConfig(), Equals, "my-brand-gadget")
	c.Check(regCtx.SerialRequestExtraHeaders(), DeepEquals, map[string]interface{}{
		"original-brand-id": "my-brand",
		"original-model":    "my-model",
		"original-serial":   "orig-serial",
	})

	serial, err := s.mgr.Serial()
	c.Assert(err, IsNil)
	c.Check(regCtx.SerialRequestAncillaryAssertions(), DeepEquals, []asserts.Assertion{newModel, serial})
}

func (s *remodelLogicSuite) TestReregRemodelContextNewSerial(c *C) {
	// re-registration case
	oldModel := s.brands.Model("my-brand", "my-model", modelDefaults)
	newModel := fakeRemodelingModel(map[string]interface{}{
		"model": "other-model",
	})

	s.state.Lock()
	defer s.state.Unlock()

	assertstatetest.AddMany(s.state, oldModel)

	// we have a device state and serial
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial1",
	})

	makeSerialAssertionInState(c, s.brands, s.state, "my-brand", "my-model", "serialserialserial1")

	serial, err := s.mgr.Serial()
	c.Assert(err, IsNil)
	c.Check(serial.Serial(), Equals, "serialserialserial1")

	remodCtx, err := devicestate.RemodelCtx(s.state, oldModel, newModel)
	c.Assert(err, IsNil)

	devBE := devicestate.RemodelDeviceBackend(remodCtx)

	// no new serial yet
	_, err = devBE.Serial()
	c.Assert(err, Equals, state.ErrNoState)

	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	// sanity check
	device1, err := devBE.Device()
	c.Assert(err, IsNil)
	c.Check(device1, DeepEquals, &auth.DeviceState{
		Brand: "my-brand",
		Model: "other-model",
	})

	// still no new serial
	_, err = devBE.Serial()
	c.Assert(err, Equals, state.ErrNoState)

	newSerial := makeSerialAssertionInState(c, s.brands, s.state, "my-brand", "other-model", "serialserialserial2")

	// same
	_, err = devBE.Serial()
	c.Check(err, Equals, state.ErrNoState)

	// finish registration
	regCtx := remodCtx.(devicestate.RegistrationContext)
	err = regCtx.FinishRegistration(newSerial)
	c.Assert(err, IsNil)

	serial, err = devBE.Serial()
	c.Check(err, IsNil)
	c.Check(serial.Model(), Equals, "other-model")
	c.Check(serial.Serial(), Equals, "serialserialserial2")

	// not exposed yet
	serial, err = s.mgr.Serial()
	c.Assert(err, IsNil)
	c.Check(serial.Model(), Equals, "my-model")
	c.Check(serial.Serial(), Equals, "serialserialserial1")

	// finish
	err = remodCtx.Finish()
	c.Assert(err, IsNil)

	serial, err = s.mgr.Serial()
	c.Assert(err, IsNil)
	c.Check(serial.Model(), Equals, "other-model")
	c.Check(serial.Serial(), Equals, "serialserialserial2")
}
