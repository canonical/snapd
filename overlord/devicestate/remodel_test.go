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
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
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
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/testutil"
)

type remodelLogicBaseSuite struct {
	testutil.BaseTest

	state *state.State
	mgr   *devicestate.DeviceManager

	storeSigning *assertstest.StoreStack
	brands       *assertstest.SigningAccounts

	capturedDevBE storecontext.DeviceBackend
	testStore     snapstate.StoreService
}

func (s *remodelLogicBaseSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })

	o := overlord.Mock()
	s.state = o.State()

	s.storeSigning = assertstest.NewStoreStack("canonical", nil)
	s.brands = assertstest.NewSigningAccounts(s.storeSigning)
	s.brands.Register("my-brand", brandPrivKey, nil)

	db := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.storeSigning.Trusted,
	}))


	func() {
		s.state.Lock()
		defer s.state.Unlock()
		assertstate.ReplaceDB(s.state, db)

		assertstatetest.AddMany(s.state, s.storeSigning.StoreAccountKey(""))
		assertstatetest.AddMany(s.state, s.brands.AccountsAndKeys("my-brand")...)
	}()

	s.testStore = new(storetest.Store)

	newStore := func(devBE storecontext.DeviceBackend) snapstate.StoreService {
		s.capturedDevBE = devBE
		return s.testStore
	}

	hookMgr := mylog.Check2(hookstate.Manager(s.state, o.TaskRunner()))

	s.mgr = mylog.Check2(devicestate.Manager(s.state, hookMgr, o.TaskRunner(), newStore))

}

type remodelLogicSuite struct {
	remodelLogicBaseSuite
}

var _ = Suite(&remodelLogicSuite{})

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

	remodCtx := mylog.Check2(devicestate.RemodelCtx(s.state, oldModel, newModel))


	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.UpdateRemodel)
	groundCtx := remodCtx.GroundContext()
	c.Check(groundCtx.ForRemodeling(), Equals, false)
	c.Check(groundCtx.Model().Revision(), Equals, 0)
	c.Check(groundCtx.Store, PanicMatches, `retrieved ground context is not intended to drive store operations`)

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

	remodCtx := mylog.Check2(devicestate.RemodelCtx(s.state, oldModel, newModel))


	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.StoreSwitchRemodel)
	groundCtx := remodCtx.GroundContext()
	c.Check(groundCtx.ForRemodeling(), Equals, false)
	c.Check(groundCtx.Model().Revision(), Equals, 0)

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

	remodCtx := mylog.Check2(devicestate.RemodelCtx(s.state, oldModel, newModel))


	devBE := s.capturedDevBE
	c.Check(devBE, NotNil)

	device := mylog.Check2(devBE.Device())

	c.Check(device, DeepEquals, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})

	mod := mylog.Check2(devBE.Model())

	c.Check(mod, DeepEquals, newModel)

	// set device state for the context
	device1 := &auth.DeviceState{
		Brand:           "my-brand",
		Model:           "my-model",
		Serial:          "serialserialserial",
		SessionMacaroon: "session",
	}
	mylog.Check(devBE.SetDevice(device1))


	device = mylog.Check2(devBE.Device())

	c.Check(device, DeepEquals, device1)

	// have a change
	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	// check device state is preserved across association with a Change
	device = mylog.Check2(devBE.Device())

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

	remodCtx := mylog.Check2(devicestate.RemodelCtx(s.state, oldModel, newModel))


	devBE := devicestate.RemodelDeviceBackend(remodCtx)

	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	device := mylog.Check2(devBE.Device())

	c.Check(device, DeepEquals, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})

	mod := mylog.Check2(devBE.Model())

	c.Check(mod, DeepEquals, newModel)

	// set a device state for the context
	device1 := &auth.DeviceState{
		Brand:           "my-brand",
		Model:           "my-model",
		Serial:          "serialserialserial",
		SessionMacaroon: "session",
	}
	mylog.Check(devBE.SetDevice(device1))


	// it's stored on change now
	var device2 *auth.DeviceState
	c.Assert(chg.Get("device", &device2), IsNil)
	c.Check(device2, DeepEquals, device1)

	device = mylog.Check2(devBE.Device())

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

	remodCtx := mylog.Check2(devicestate.RemodelCtx(s.state, oldModel, newModel))


	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	devBE := devicestate.RemodelDeviceBackend(remodCtx)
	mylog.Check(devBE.SetDevice(&auth.DeviceState{
		Brand:           "my-brand",
		Model:           "my-model",
		Serial:          "serialserialserial",
		SessionMacaroon: "remodel-session",
	}))


	// the global device state is as before
	expectedGlobalDevice := &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	}

	device := mylog.Check2(s.mgr.StoreContextBackend().Device())

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

	remodCtx := mylog.Check2(devicestate.RemodelCtx(s.state, oldModel, newModel))


	c.Check(s.capturedDevBE, NotNil)

	// new store remodel context device state built ignoring the
	// previous session
	device1 := mylog.Check2(s.capturedDevBE.Device())

	c.Check(device1, DeepEquals, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})

	sto := remodCtx.Store()
	c.Check(sto, Equals, s.testStore)

	// store is kept and not rebuilt
	s.testStore = nil

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

	remodCtx := mylog.Check2(devicestate.RemodelCtx(s.state, oldModel, newModel))


	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	devBE := devicestate.RemodelDeviceBackend(remodCtx)
	mylog.Check(devBE.SetDevice(&auth.DeviceState{
		Brand:           "my-brand",
		Model:           "my-model",
		Serial:          "serialserialserial",
		SessionMacaroon: "new-session",
	}))

	mylog.Check(remodCtx.Finish())


	// the global device now matches the state reached in the remodel
	expectedGlobalDevice := &auth.DeviceState{
		Brand:           "my-brand",
		Model:           "my-model",
		Serial:          "serialserialserial",
		SessionMacaroon: "new-session",
	}

	device := mylog.Check2(s.mgr.StoreContextBackend().Device())

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

	remodCtx := mylog.Check2(devicestate.RemodelCtx(s.state, oldModel, newModel))


	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	devBE := devicestate.RemodelDeviceBackend(remodCtx)
	mylog.Check(devBE.SetDevice(&auth.DeviceState{
		Brand:           "my-brand",
		Model:           "my-model",
		Serial:          "serialserialserial",
		SessionMacaroon: "remodel-session",
	}))


	// global store device and auth context
	scb := s.mgr.StoreContextBackend()
	dac := storecontext.New(s.state, scb)
	// this is the unlikely start of the global store trying to
	// refresh the session
	s.state.Unlock()
	globalDevice := mylog.Check2(dac.Device())
	s.state.Lock()

	c.Check(globalDevice.SessionMacaroon, Equals, "old-session")
	mylog.Check(remodCtx.Finish())


	s.state.Unlock()
	device1 := mylog.Check2(dac.UpdateDeviceAuth(globalDevice, "fresh-session"))
	s.state.Lock()


	// the global device now matches the state reached in the remodel
	expectedGlobalDevice := &auth.DeviceState{
		Brand:           "my-brand",
		Model:           "my-model",
		Serial:          "serialserialserial",
		SessionMacaroon: "remodel-session",
	}

	s.state.Unlock()
	device := mylog.Check2(dac.Device())
	s.state.Lock()

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

	serial := mylog.Check2(s.mgr.Serial())

	c.Check(serial.Serial(), Equals, "serialserialserial1")

	remodCtx := mylog.Check2(devicestate.RemodelCtx(s.state, oldModel, newModel))


	devBE := devicestate.RemodelDeviceBackend(remodCtx)

	serial0 := mylog.Check2(devBE.Serial())

	c.Check(serial0.Serial(), Equals, "serialserialserial1")

	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	serial0 = mylog.Check2(devBE.Serial())

	c.Check(serial0.Serial(), Equals, "serialserialserial1")
}

func (s *remodelLogicSuite) TestRemodelContextSystemModeDefaultRun(c *C) {
	oldModel := s.brands.Model("my-brand", "my-model", modelDefaults)
	newModel := s.brands.Model("my-brand", "my-model", modelDefaults, map[string]interface{}{"revision": "2"})

	s.state.Lock()
	defer s.state.Unlock()

	assertstatetest.AddMany(s.state, oldModel)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})

	remodCtx := mylog.Check2(devicestate.RemodelCtx(s.state, oldModel, newModel))

	c.Check(remodCtx.SystemMode(), Equals, "run")
}

func (s *remodelLogicSuite) TestRemodelContextSystemModeWorks(c *C) {
	oldModel := s.brands.Model("my-brand", "my-model", modelDefaults)
	newModel := s.brands.Model("my-brand", "my-model", modelDefaults, map[string]interface{}{"revision": "2"})

	s.state.Lock()
	defer s.state.Unlock()

	assertstatetest.AddMany(s.state, oldModel)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	devicestate.SetSystemMode(s.mgr, "install")

	remodCtx := mylog.Check2(devicestate.RemodelCtx(s.state, oldModel, newModel))

	c.Check(remodCtx.SystemMode(), Equals, "install")
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

	remodCtx := mylog.Check2(devicestate.RemodelCtx(s.state, oldModel, newModel))


	c.Check(remodCtx.ForRemodeling(), Equals, true)

	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	t := s.state.NewTask("remodel-task-1", "...")
	chg.AddTask(t)

	// caching, internally this use remodelCtxFromTask
	remodCtx1 := mylog.Check2(devicestate.DeviceCtx(s.state, t, nil))

	c.Check(remodCtx1, Equals, remodCtx)

	// if the context goes away (e.g. because of restart) we
	// compute a new one
	devicestate.CleanupRemodelCtx(chg)

	remodCtx2 := mylog.Check2(devicestate.DeviceCtx(s.state, t, nil))

	c.Check(remodCtx2 != remodCtx, Equals, true)
	c.Check(remodCtx2.Model(), DeepEquals, newModel)
}

func (s *remodelLogicSuite) TestRemodelContextForTaskNo(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// internally these use remodelCtxFromTask

	// task is nil
	remodCtx1 := mylog.Check2(devicestate.DeviceCtx(s.state, nil, nil))
	c.Check(err, testutil.ErrorIs, state.ErrNoState)
	c.Check(remodCtx1, IsNil)

	// no change
	t := s.state.NewTask("random-task", "...")
	_ = mylog.Check2(devicestate.DeviceCtx(s.state, t, nil))
	c.Check(err, testutil.ErrorIs, state.ErrNoState)

	// not a remodel change
	chg := s.state.NewChange("not-remodel", "...")
	chg.AddTask(t)
	_ = mylog.Check2(devicestate.DeviceCtx(s.state, t, nil))
	c.Check(err, testutil.ErrorIs, state.ErrNoState)
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

	encDevKey := mylog.Check2(asserts.EncodePublicKey(devKey.PublicKey()))

	serial := mylog.Check2(s.brands.Signing("my-brand").Sign(asserts.SerialType, map[string]interface{}{
		"authority-id":        "my-brand",
		"brand-id":            "my-brand",
		"model":               "my-model",
		"serial":              "orig-serial",
		"device-key":          string(encDevKey),
		"device-key-sha3-384": devKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, ""))


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

	remodCtx := mylog.Check2(devicestate.RemodelCtx(s.state, oldModel, newModel))


	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.ReregRemodel)
	groundCtx := remodCtx.GroundContext()
	c.Check(groundCtx.ForRemodeling(), Equals, false)
	c.Check(groundCtx.Model().BrandID(), Equals, "my-brand")

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

	remodCtx1 := mylog.Check2(devicestate.DeviceCtx(s.state, t, nil))

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

	remodCtx := mylog.Check2(devicestate.RemodelCtx(s.state, oldModel, newModel))


	c.Check(remodCtx.Kind(), Equals, devicestate.ReregRemodel)

	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	regCtx := remodCtx.(devicestate.RegistrationContext)

	c.Check(regCtx.ForRemodeling(), Equals, true)
	device1 := mylog.Check2(regCtx.Device())

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

	serial := mylog.Check2(s.mgr.Serial())

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

	serial := mylog.Check2(s.mgr.Serial())

	c.Check(serial.Serial(), Equals, "serialserialserial1")

	remodCtx := mylog.Check2(devicestate.RemodelCtx(s.state, oldModel, newModel))


	devBE := devicestate.RemodelDeviceBackend(remodCtx)

	// no new serial yet
	_ = mylog.Check2(devBE.Serial())
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)

	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	// validity check
	device1 := mylog.Check2(devBE.Device())

	c.Check(device1, DeepEquals, &auth.DeviceState{
		Brand: "my-brand",
		Model: "other-model",
	})

	// still no new serial
	_ = mylog.Check2(devBE.Serial())
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)

	newSerial := makeSerialAssertionInState(c, s.brands, s.state, "my-brand", "other-model", "serialserialserial2")

	// same
	_ = mylog.Check2(devBE.Serial())
	c.Check(err, testutil.ErrorIs, state.ErrNoState)

	// finish registration
	regCtx := remodCtx.(devicestate.RegistrationContext)
	mylog.Check(regCtx.FinishRegistration(newSerial))


	serial = mylog.Check2(devBE.Serial())
	c.Check(err, IsNil)
	c.Check(serial.Model(), Equals, "other-model")
	c.Check(serial.Serial(), Equals, "serialserialserial2")

	// not exposed yet
	serial = mylog.Check2(s.mgr.Serial())

	c.Check(serial.Model(), Equals, "my-model")
	c.Check(serial.Serial(), Equals, "serialserialserial1")
	mylog.

		// finish
		Check(remodCtx.Finish())


	serial = mylog.Check2(s.mgr.Serial())

	c.Check(serial.Model(), Equals, "other-model")
	c.Check(serial.Serial(), Equals, "serialserialserial2")
}

type uc20RemodelLogicSuite struct {
	remodelLogicBaseSuite

	oldModel    *asserts.Model
	bootloader  *bootloadertest.MockRecoveryAwareTrustedAssetsBootloader
	oldSeededTs time.Time
}

var _ = Suite(&uc20RemodelLogicSuite{})

func writeDeviceModelToUbuntuBoot(c *C, model *asserts.Model) {
	var buf bytes.Buffer
	c.Assert(asserts.NewEncoder(&buf).Encode(model), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "device"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"),
		buf.Bytes(), 0755),
		IsNil)
}

func (s *uc20RemodelLogicSuite) SetUpTest(c *C) {
	s.remodelLogicBaseSuite.SetUpTest(c)

	s.oldModel = s.brands.Model("my-brand", "my-model", uc20ModelDefaults)
	writeDeviceModelToUbuntuBoot(c, s.oldModel)
	s.bootloader = bootloadertest.Mock("trusted", c.MkDir()).WithRecoveryAwareTrustedAssets()
	bootloader.Force(s.bootloader)
	s.AddCleanup(func() { bootloader.Force(nil) })

	m := boot.Modeenv{
		Mode: "run",

		CurrentRecoverySystems: []string{"0000"},
		GoodRecoverySystems:    []string{"0000"},

		Model:          s.oldModel.Model(),
		Grade:          string(s.oldModel.Grade()),
		BrandID:        s.oldModel.BrandID(),
		ModelSignKeyID: s.oldModel.SignKeyID(),
	}
	mylog.Check(m.WriteTo(""))


	restore := boot.MockResealKeyToModeenv(func(_ string, m *boot.Modeenv, expectReseal bool, _ boot.Unlocker) error {
		return fmt.Errorf("not expected to be called")
	})
	s.AddCleanup(restore)

	s.state.Lock()
	defer s.state.Unlock()
	sys := devicestate.SeededSystem{
		System: "0000",

		Model:     "my-model",
		BrandID:   "my-brand",
		Revision:  0,
		Timestamp: s.oldModel.Timestamp(),

		SeedTime: time.Now(),
	}
	mylog.Check(devicestate.RecordSeededSystem(s.mgr, s.state, &sys))

	s.oldSeededTs = sys.SeedTime
	mylog.Check(s.bootloader.SetBootVars(map[string]string{
		"snapd_good_recovery_systems": "0000",
	}))

}

var uc20ModelDefaults = map[string]interface{}{
	"architecture": "amd64",
	"base":         "core20",
	"grade":        "dangerous",
	"snaps": []interface{}{
		map[string]interface{}{
			"name":            "pc-kernel",
			"id":              snaptest.AssertedSnapID("pc-kernel"),
			"type":            "kernel",
			"default-channel": "20",
		},
		map[string]interface{}{
			"name":            "pc",
			"id":              snaptest.AssertedSnapID("pc"),
			"type":            "gadget",
			"default-channel": "20",
		},
	},
}

func (s *uc20RemodelLogicSuite) TestReregRemodelContextUC20(c *C) {
	newModel := s.brands.Model("my-brand", "other-model", uc20ModelDefaults)

	m := mylog.Check2(boot.ReadModeenv(""))

	// the system has already been promoted
	m.CurrentRecoverySystems = append(m.CurrentRecoverySystems, "1234")
	m.GoodRecoverySystems = append(m.GoodRecoverySystems, "1234")
	c.Assert(m.Write(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	assertstatetest.AddMany(s.state, s.oldModel)

	// we have a device state and serial
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial1",
	})

	makeSerialAssertionInState(c, s.brands, s.state, "my-brand", "my-model", "serialserialserial1")

	serial := mylog.Check2(s.mgr.Serial())

	c.Check(serial.Serial(), Equals, "serialserialserial1")

	remodCtx := mylog.Check2(devicestate.RemodelCtx(s.state, s.oldModel, newModel))

	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.ReregRemodel)

	devBE := devicestate.RemodelDeviceBackend(remodCtx)

	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	// validity check
	device1 := mylog.Check2(devBE.Device())

	c.Check(device1, DeepEquals, &auth.DeviceState{
		Brand: "my-brand",
		Model: "other-model",
	})

	newSerial := makeSerialAssertionInState(c, s.brands, s.state, "my-brand", "other-model", "serialserialserial2")

	// finish registration
	regCtx := remodCtx.(devicestate.RegistrationContext)
	mylog.Check(regCtx.FinishRegistration(newSerial))


	resealKeysCalls := 0
	restore := boot.MockResealKeyToModeenv(func(_ string, m *boot.Modeenv, expectReseal bool, u boot.Unlocker) error {
		resealKeysCalls++
		c.Check(m.CurrentRecoverySystems, DeepEquals, []string{"0000", "1234"})
		c.Check(m.GoodRecoverySystems, DeepEquals, []string{"0000", "1234"})
		switch resealKeysCalls {
		case 1:
			// intermediate step, new and old models
			c.Check(m.ModelForSealing().Model(), Equals, "my-model")
			c.Check(m.TryModelForSealing().Model(), Equals, "other-model")
			c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains, "model: my-model\n")
		case 2:
			// new model
			c.Check(m.ModelForSealing().Model(), Equals, "other-model")
			c.Check(m.TryModelForSealing().Model(), Equals, "")
			c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains, "model: other-model\n")
		default:
			c.Fatalf("unexpected call #%v to reseal key to modeenv", resealKeysCalls)
		}
		// check unlocker
		u()()
		// this is running as part of post finish step, so the state has
		// already been updated
		serial = mylog.Check2(s.mgr.Serial())

		c.Check(serial.Model(), Equals, "other-model")
		c.Check(serial.Serial(), Equals, "serialserialserial2")
		return nil
	})
	s.AddCleanup(restore)
	mylog.

		// finish fails because we haven't set the seed system label yet
		Check(remodCtx.Finish())
	c.Assert(err, ErrorMatches, "internal error: recovery system label is unset during remodel finish")
	c.Check(resealKeysCalls, Equals, 0)

	// set the label internally
	devicestate.RemodelSetRecoverySystemLabel(remodCtx, "1234")
	mylog.Check(remodCtx.Finish())

	c.Check(resealKeysCalls, Equals, 2)

	var seededSystemsFromState []map[string]interface{}
	mylog.Check(s.state.Get("seeded-systems", &seededSystemsFromState))

	c.Assert(seededSystemsFromState, HasLen, 2)
	c.Assert(seededSystemsFromState[1], DeepEquals, map[string]interface{}{
		"system":    "0000",
		"model":     "my-model",
		"brand-id":  "my-brand",
		"revision":  float64(0),
		"timestamp": s.oldModel.Timestamp().Format(time.RFC3339Nano),
		"seed-time": s.oldSeededTs.Format(time.RFC3339Nano),
	})
	// new system is prepended, since timestamps are involved clear ones that weren't mocked
	c.Assert(seededSystemsFromState[0]["seed-time"], FitsTypeOf, "")
	newSeedTs := mylog.Check2(time.Parse(time.RFC3339Nano, seededSystemsFromState[0]["seed-time"].(string)))

	seededSystemsFromState[0]["seed-time"] = ""
	c.Assert(seededSystemsFromState[0], DeepEquals, map[string]interface{}{
		"system":    "1234",
		"model":     "other-model",
		"brand-id":  "my-brand",
		"revision":  float64(0),
		"timestamp": newModel.Timestamp().Format(time.RFC3339Nano),
		"seed-time": "",
	})
	c.Assert(newSeedTs.After(s.oldSeededTs), Equals, true)
	env := mylog.Check2(s.bootloader.GetBootVars("snapd_good_recovery_systems"))

	c.Assert(env, DeepEquals, map[string]string{
		"snapd_good_recovery_systems": "0000,1234",
	})
}

func (s *uc20RemodelLogicSuite) TestUpdateRemodelContext(c *C) {
	modelDefaults := make(map[string]interface{}, len(uc20ModelDefaults))
	for k, v := range uc20ModelDefaults {
		modelDefaults[k] = v
	}
	// simple model update with bumped revision
	modelDefaults["revision"] = "2"
	newModel := s.brands.Model("my-brand", "my-model", modelDefaults)

	s.state.Lock()
	defer s.state.Unlock()

	m := mylog.Check2(boot.ReadModeenv(""))

	// the system has already been promoted
	m.CurrentRecoverySystems = append(m.CurrentRecoverySystems, "1234")
	m.GoodRecoverySystems = append(m.GoodRecoverySystems, "1234")
	c.Assert(m.Write(), IsNil)

	remodCtx := mylog.Check2(devicestate.RemodelCtx(s.state, s.oldModel, newModel))

	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.UpdateRemodel)
	groundCtx := remodCtx.GroundContext()
	c.Check(groundCtx.ForRemodeling(), Equals, false)
	c.Check(groundCtx.Model().Revision(), Equals, 0)
	c.Check(groundCtx.Store, PanicMatches, `retrieved ground context is not intended to drive store operations`)

	chg := s.state.NewChange("remodel", "...")

	remodCtx.Init(chg)

	var encNewModel string
	c.Assert(chg.Get("new-model", &encNewModel), IsNil)

	resealKeysCalls := 0
	restore := boot.MockResealKeyToModeenv(func(_ string, m *boot.Modeenv, expectReseal bool, u boot.Unlocker) error {
		resealKeysCalls++
		c.Check(m.CurrentRecoverySystems, DeepEquals, []string{"0000", "1234"})
		c.Check(m.GoodRecoverySystems, DeepEquals, []string{"0000", "1234"})
		switch resealKeysCalls {
		case 1:
			// intermediate step, new and old models
			c.Check(m.ModelForSealing().Model(), Equals, "my-model")
			c.Check(m.TryModelForSealing().Model(), Equals, "my-model")
			c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains, "model: my-model\n")
			c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), Not(testutil.FileContains), "revision:")
		case 2:
			// new model
			c.Check(m.ModelForSealing().Model(), Equals, "my-model")
			c.Check(m.TryModelForSealing().Model(), Equals, "")
			c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains, "model: my-model\n")
			c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains, "revision: 2\n")
		default:
			c.Fatalf("unexpected call #%v to reseal key to modeenv", resealKeysCalls)
		}
		// check unlocker
		u()()
		return nil
	})
	s.AddCleanup(restore)
	mylog.

		// finish fails because we haven't set the seed system label yet
		Check(remodCtx.Finish())
	c.Assert(err, ErrorMatches, "internal error: recovery system label is unset during remodel finish")
	c.Check(resealKeysCalls, Equals, 0)

	// set the label internally
	devicestate.RemodelSetRecoverySystemLabel(remodCtx, "1234")
	mylog.Check(remodCtx.Finish())

	c.Check(resealKeysCalls, Equals, 2)

	var seededSystemsFromState []map[string]interface{}
	mylog.Check(s.state.Get("seeded-systems", &seededSystemsFromState))

	c.Assert(seededSystemsFromState, HasLen, 2)
	c.Assert(seededSystemsFromState[1], DeepEquals, map[string]interface{}{
		"system":    "0000",
		"model":     "my-model",
		"brand-id":  "my-brand",
		"revision":  float64(0),
		"timestamp": s.oldModel.Timestamp().Format(time.RFC3339Nano),
		"seed-time": s.oldSeededTs.Format(time.RFC3339Nano),
	})
	// new system is prepended, since timestamps are involved clear ones that weren't mocked
	c.Assert(seededSystemsFromState[0]["seed-time"], FitsTypeOf, "")
	newSeedTs := mylog.Check2(time.Parse(time.RFC3339Nano, seededSystemsFromState[0]["seed-time"].(string)))

	seededSystemsFromState[0]["seed-time"] = ""
	c.Assert(seededSystemsFromState[0], DeepEquals, map[string]interface{}{
		"system":    "1234",
		"model":     "my-model",
		"brand-id":  "my-brand",
		"revision":  float64(2),
		"timestamp": newModel.Timestamp().Format(time.RFC3339Nano),
		"seed-time": "",
	})
	c.Assert(newSeedTs.After(s.oldSeededTs), Equals, true)
	env := mylog.Check2(s.bootloader.GetBootVars("snapd_good_recovery_systems"))

	c.Assert(env, DeepEquals, map[string]string{
		"snapd_good_recovery_systems": "0000,1234",
	})
}

func (s *uc20RemodelLogicSuite) TestSimpleRemodelErr(c *C) {
	modelDefaults := make(map[string]interface{}, len(uc20ModelDefaults))
	for k, v := range uc20ModelDefaults {
		modelDefaults[k] = v
	}
	// simple model update with bumped revision
	modelDefaults["revision"] = "2"
	newModel := s.brands.Model("my-brand", "my-model", modelDefaults)

	s.state.Lock()
	defer s.state.Unlock()

	m := mylog.Check2(boot.ReadModeenv(""))

	// the system has already been promoted
	m.CurrentRecoverySystems = append(m.CurrentRecoverySystems, "1234")
	m.GoodRecoverySystems = append(m.GoodRecoverySystems, "1234")
	c.Assert(m.Write(), IsNil)

	remodCtx := mylog.Check2(devicestate.RemodelCtx(s.state, s.oldModel, newModel))

	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.UpdateRemodel)

	chg := s.state.NewChange("remodel", "...")
	remodCtx.Init(chg)

	var encNewModel string
	c.Assert(chg.Get("new-model", &encNewModel), IsNil)

	resealKeysCalls := 0
	restore := boot.MockResealKeyToModeenv(func(_ string, m *boot.Modeenv, expectReseal bool, u boot.Unlocker) error {
		resealKeysCalls++
		c.Check(m.CurrentRecoverySystems, DeepEquals, []string{"0000", "1234"})
		c.Check(m.GoodRecoverySystems, DeepEquals, []string{"0000", "1234"})
		switch resealKeysCalls {
		case 1:
			// intermediate step, new and old models
			c.Check(m.ModelForSealing().Model(), Equals, "my-model")
			c.Check(m.TryModelForSealing().Model(), Equals, "my-model")
			c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains, "model: my-model\n")
			c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), Not(testutil.FileContains), "revision:")
			return fmt.Errorf("mock reseal failure")
		default:
			c.Fatalf("unexpected call #%v to reseal key to modeenv", resealKeysCalls)
		}
		// check unlocker
		u()()
		return nil
	})
	s.AddCleanup(restore)

	// set the label internally
	devicestate.RemodelSetRecoverySystemLabel(remodCtx, "1234")
	mylog.Check(remodCtx.Finish())
	c.Assert(err, ErrorMatches, "cannot switch device: mock reseal failure")
	c.Check(resealKeysCalls, Equals, 1)

	// the error occurred before seeded systems was updated
	var seededSystemsFromState []map[string]interface{}
	mylog.Check(s.state.Get("seeded-systems", &seededSystemsFromState))

	c.Assert(seededSystemsFromState, DeepEquals, []map[string]interface{}{{
		"system":    "0000",
		"model":     "my-model",
		"brand-id":  "my-brand",
		"revision":  float64(0),
		"timestamp": s.oldModel.Timestamp().Format(time.RFC3339Nano),
		"seed-time": s.oldSeededTs.Format(time.RFC3339Nano),
	}})
	env := mylog.Check2(s.bootloader.GetBootVars("snapd_good_recovery_systems"))

	c.Assert(env, DeepEquals, map[string]string{
		"snapd_good_recovery_systems": "0000",
	})
}
