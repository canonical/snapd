// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2019 Canonical Ltd
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
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storecontext"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

func TestDeviceManager(t *testing.T) { TestingT(t) }

type deviceMgrSuite struct {
	o       *overlord.Overlord
	state   *state.State
	se      *overlord.StateEngine
	hookMgr *hookstate.HookManager
	mgr     *devicestate.DeviceManager
	db      *asserts.Database

	bootloader *boottest.MockBootloader

	storeSigning *assertstest.StoreStack
	brands       *assertstest.SigningAccounts

	reqID string

	ancillary []asserts.Assertion

	restartRequests []state.RestartType

	restoreOnClassic         func()
	restoreGenericClassicMod func()
	restoreSanitize          func()

	newFakeStore func(storecontext.DeviceBackend) snapstate.StoreService
}

var _ = Suite(&deviceMgrSuite{})
var testKeyLength = 1024

type fakeStore struct {
	storetest.Store

	state *state.State
	db    asserts.RODatabase
}

func (sto *fakeStore) pokeStateLock() {
	// the store should be called without the state lock held. Try
	// to acquire it.
	sto.state.Lock()
	sto.state.Unlock()
}

func (sto *fakeStore) Assertion(assertType *asserts.AssertionType, key []string, _ *auth.UserState) (asserts.Assertion, error) {
	sto.pokeStateLock()
	ref := &asserts.Ref{Type: assertType, PrimaryKey: key}
	return ref.Resolve(sto.db.Find)
}

var (
	brandPrivKey, _  = assertstest.GenerateKey(752)
	brandPrivKey2, _ = assertstest.GenerateKey(752)
)

func (s *deviceMgrSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	os.MkdirAll(dirs.SnapRunDir, 0755)

	s.restartRequests = nil

	s.restoreSanitize = snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})

	s.bootloader = boottest.NewMockBootloader("mock", c.MkDir())
	bootloader.Force(s.bootloader)

	s.restoreOnClassic = release.MockOnClassic(false)

	s.storeSigning = assertstest.NewStoreStack("canonical", nil)
	s.o = overlord.MockWithRestartHandler(func(req state.RestartType) {
		s.restartRequests = append(s.restartRequests, req)
	})
	s.state = s.o.State()
	s.state.Lock()
	s.state.VerifyReboot("boot-id-0")
	s.state.Unlock()
	s.se = s.o.StateEngine()

	s.restoreGenericClassicMod = sysdb.MockGenericClassicModel(s.storeSigning.GenericClassicModel)

	s.brands = assertstest.NewSigningAccounts(s.storeSigning)
	s.brands.Register("my-brand", brandPrivKey, nil)
	s.brands.Register("rereg-brand", brandPrivKey2, nil)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore:       asserts.NewMemoryBackstore(),
		Trusted:         s.storeSigning.Trusted,
		OtherPredefined: s.storeSigning.Generic,
	})
	c.Assert(err, IsNil)

	s.state.Lock()
	assertstate.ReplaceDB(s.state, db)
	s.state.Unlock()

	err = db.Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	hookMgr, err := hookstate.Manager(s.state, s.o.TaskRunner())
	c.Assert(err, IsNil)
	mgr, err := devicestate.Manager(s.state, hookMgr, s.o.TaskRunner(), s.newStore)
	c.Assert(err, IsNil)

	s.db = db
	s.hookMgr = hookMgr
	s.o.AddManager(s.hookMgr)
	s.mgr = mgr
	s.o.AddManager(s.mgr)
	s.o.AddManager(s.o.TaskRunner())

	// For triggering errors
	erroringHandler := func(task *state.Task, _ *tomb.Tomb) error {
		return errors.New("error out")
	}
	s.o.TaskRunner().AddHandler("error-trigger", erroringHandler, nil)

	c.Assert(s.o.StartUp(), IsNil)

	s.state.Lock()
	snapstate.ReplaceStore(s.state, &fakeStore{
		state: s.state,
		db:    s.storeSigning,
	})
	s.state.Unlock()
}

func (s *deviceMgrSuite) newStore(devBE storecontext.DeviceBackend) snapstate.StoreService {
	return s.newFakeStore(devBE)
}

func (s *deviceMgrSuite) TearDownTest(c *C) {
	s.ancillary = nil
	s.state.Lock()
	assertstate.ReplaceDB(s.state, nil)
	s.state.Unlock()
	bootloader.Force(nil)
	dirs.SetRootDir("")
	s.restoreGenericClassicMod()
	s.restoreOnClassic()
	s.restoreSanitize()
}

var settleTimeout = 15 * time.Second

func (s *deviceMgrSuite) settle(c *C) {
	err := s.o.Settle(settleTimeout)
	c.Assert(err, IsNil)
}

// seeding avoids triggering a real full seeding, it simulates having it in process instead
func (s *deviceMgrSuite) seeding() {
	chg := s.state.NewChange("seed", "Seed system")
	chg.SetStatus(state.DoingStatus)
}

func (s *deviceMgrSuite) signSerial(c *C, bhv *devicestatetest.DeviceServiceBehavior, headers map[string]interface{}, body []byte) (serial asserts.Assertion, ancillary []asserts.Assertion, err error) {
	brandID := headers["brand-id"].(string)
	model := headers["model"].(string)
	keyID := ""

	var signing assertstest.SignerDB = s.storeSigning

	switch model {
	case "pc", "pc2":
	case "classic-alt-store":
		c.Check(brandID, Equals, "canonical")
	case "generic-classic":
		c.Check(brandID, Equals, "generic")
		headers["authority-id"] = "generic"
		keyID = s.storeSigning.GenericKey.PublicKeyID()
	case "rereg-model":
		headers["authority-id"] = "rereg-brand"
		signing = s.brands.Signing("rereg-brand")
	default:
		c.Fatalf("unknown model: %s", model)
	}
	a, err := signing.Sign(asserts.SerialType, headers, body, keyID)
	return a, s.ancillary, err
}

func (s *deviceMgrSuite) mockServer(c *C, reqID string, bhv *devicestatetest.DeviceServiceBehavior) *httptest.Server {
	if bhv == nil {
		bhv = &devicestatetest.DeviceServiceBehavior{}
	}

	bhv.ReqID = reqID
	bhv.SignSerial = s.signSerial

	return devicestatetest.MockDeviceService(c, bhv)
}

func (s *deviceMgrSuite) setupCore(c *C, name, snapYaml string, snapContents string) {
	sideInfoCore := &snap.SideInfo{
		RealName: name,
		Revision: snap.R(3),
	}
	snaptest.MockSnap(c, snapYaml, sideInfoCore)
	snapstate.Set(s.state, name, &snapstate.SnapState{
		SnapType: "os",
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfoCore},
		Current:  sideInfoCore.Revision,
	})
}

func (s *deviceMgrSuite) findBecomeOperationalChange(skipIDs ...string) *state.Change {
	for _, chg := range s.state.Changes() {
		if chg.Kind() == "become-operational" && !strutil.ListContains(skipIDs, chg.ID()) {
			return chg
		}
	}
	return nil
}

func (s *deviceMgrSuite) TestFullDeviceRegistrationHappy(c *C) {
	r1 := devicestate.MockKeyLength(testKeyLength)
	defer r1()

	mockServer := s.mockServer(c, "REQID-1", nil)
	defer mockServer.Close()

	r2 := devicestate.MockBaseStoreURL(mockServer.URL)
	defer r2()

	// setup state as will be done by first-boot
	s.state.Lock()
	defer s.state.Unlock()

	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	// avoid full seeding
	s.seeding()

	// not started if not seeded
	s.state.Unlock()
	s.se.Ensure()
	s.state.Lock()

	becomeOperational := s.findBecomeOperationalChange()
	c.Check(becomeOperational, IsNil)

	devicestatetest.MockGadget(c, s.state, "pc", snap.R(2), nil)
	// mark it as seeded
	s.state.Set("seeded", true)

	// runs the whole device registration process
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	becomeOperational = s.findBecomeOperationalChange()
	c.Assert(becomeOperational, NotNil)

	c.Check(becomeOperational.Status().Ready(), Equals, true)
	c.Check(becomeOperational.Err(), IsNil)

	device, err := devicestatetest.Device(s.state)
	c.Assert(err, IsNil)
	c.Check(device.Brand, Equals, "canonical")
	c.Check(device.Model, Equals, "pc")
	c.Check(device.Serial, Equals, "9999")

	ok := false
	select {
	case <-s.mgr.Registered():
		ok = true
	case <-time.After(5 * time.Second):
		c.Fatal("should have been marked registered")
	}
	c.Check(ok, Equals, true)

	a, err := s.db.Find(asserts.SerialType, map[string]string{
		"brand-id": "canonical",
		"model":    "pc",
		"serial":   "9999",
	})
	c.Assert(err, IsNil)
	serial := a.(*asserts.Serial)

	privKey, err := devicestate.KeypairManager(s.mgr).Get(serial.DeviceKey().ID())
	c.Assert(err, IsNil)
	c.Check(privKey, NotNil)

	c.Check(device.KeyID, Equals, privKey.PublicKey().ID())
}

func (s *deviceMgrSuite) TestFullDeviceRegistrationHappyWithProxy(c *C) {
	r1 := devicestate.MockKeyLength(testKeyLength)
	defer r1()

	mockServer := s.mockServer(c, "REQID-1", nil)
	defer mockServer.Close()

	// as core.proxy.store is set, should not need to do this but just in case
	r2 := devicestate.MockBaseStoreURL(mockServer.URL + "/direct/baaad/")
	defer r2()

	// setup state as will be done by first-boot
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "proxy.store", "foo"), IsNil)
	tr.Commit()
	operatorAcct := assertstest.NewAccount(s.storeSigning, "foo-operator", nil, "")

	// have a store assertion.
	stoAs, err := s.storeSigning.Sign(asserts.StoreType, map[string]interface{}{
		"store":       "foo",
		"url":         mockServer.URL,
		"operator-id": operatorAcct.AccountID(),
		"timestamp":   time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	assertstatetest.AddMany(s.state, operatorAcct, stoAs)

	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	devicestatetest.MockGadget(c, s.state, "pc", snap.R(2), nil)
	// mark as seeded
	s.state.Set("seeded", true)

	// runs the whole device registration process
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	becomeOperational := s.findBecomeOperationalChange()
	c.Assert(becomeOperational, NotNil)

	c.Check(becomeOperational.Status().Ready(), Equals, true)
	c.Check(becomeOperational.Err(), IsNil)

	device, err := devicestatetest.Device(s.state)
	c.Assert(err, IsNil)
	c.Check(device.Brand, Equals, "canonical")
	c.Check(device.Model, Equals, "pc")
	c.Check(device.Serial, Equals, "9999")

	ok := false
	select {
	case <-s.mgr.Registered():
		ok = true
	case <-time.After(5 * time.Second):
		c.Fatal("should have been marked registered")
	}
	c.Check(ok, Equals, true)

	a, err := s.db.Find(asserts.SerialType, map[string]string{
		"brand-id": "canonical",
		"model":    "pc",
		"serial":   "9999",
	})
	c.Assert(err, IsNil)
	serial := a.(*asserts.Serial)

	privKey, err := devicestate.KeypairManager(s.mgr).Get(serial.DeviceKey().ID())
	c.Assert(err, IsNil)
	c.Check(privKey, NotNil)

	c.Check(device.KeyID, Equals, privKey.PublicKey().ID())
}

func (s *deviceMgrSuite) TestFullDeviceRegistrationHappyClassicNoGadget(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	r1 := devicestate.MockKeyLength(testKeyLength)
	defer r1()

	mockServer := s.mockServer(c, "REQID-1", nil)
	defer mockServer.Close()

	r2 := devicestate.MockBaseStoreURL(mockServer.URL)
	defer r2()

	// setup state as will be done by first-boot
	s.state.Lock()
	defer s.state.Unlock()

	s.makeModelAssertionInState(c, "canonical", "classic-alt-store", map[string]interface{}{
		"classic": "true",
		"store":   "alt-store",
	})

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "classic-alt-store",
	})

	// avoid full seeding
	s.seeding()

	// runs the whole device registration process
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	becomeOperational := s.findBecomeOperationalChange()
	c.Assert(becomeOperational, NotNil)

	c.Check(becomeOperational.Status().Ready(), Equals, true)
	c.Check(becomeOperational.Err(), IsNil)

	device, err := devicestatetest.Device(s.state)
	c.Assert(err, IsNil)
	c.Check(device.Brand, Equals, "canonical")
	c.Check(device.Model, Equals, "classic-alt-store")
	c.Check(device.Serial, Equals, "9999")

	a, err := s.db.Find(asserts.SerialType, map[string]string{
		"brand-id": "canonical",
		"model":    "classic-alt-store",
		"serial":   "9999",
	})
	c.Assert(err, IsNil)
	serial := a.(*asserts.Serial)

	privKey, err := devicestate.KeypairManager(s.mgr).Get(serial.DeviceKey().ID())
	c.Assert(err, IsNil)
	c.Check(privKey, NotNil)

	c.Check(device.KeyID, Equals, privKey.PublicKey().ID())
}

func (s *deviceMgrSuite) TestFullDeviceRegistrationHappyClassicFallback(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	r1 := devicestate.MockKeyLength(testKeyLength)
	defer r1()

	mockServer := s.mockServer(c, "REQID-1", nil)
	defer mockServer.Close()

	r2 := devicestate.MockBaseStoreURL(mockServer.URL)
	defer r2()

	// setup state as will be done by first-boot
	s.state.Lock()
	defer s.state.Unlock()

	// in this case is just marked seeded without snaps
	s.state.Set("seeded", true)

	// not started without some installation happening or happened
	s.state.Unlock()
	s.se.Ensure()
	s.state.Lock()

	becomeOperational := s.findBecomeOperationalChange()
	c.Check(becomeOperational, IsNil)

	// have a in-progress installation
	inst := s.state.NewChange("install", "...")
	task := s.state.NewTask("mount-snap", "...")
	inst.AddTask(task)

	// runs the whole device registration process
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	becomeOperational = s.findBecomeOperationalChange()
	c.Assert(becomeOperational, NotNil)

	c.Check(becomeOperational.Status().Ready(), Equals, true)
	c.Check(becomeOperational.Err(), IsNil)

	device, err := devicestatetest.Device(s.state)
	c.Assert(err, IsNil)
	c.Check(device.Brand, Equals, "generic")
	c.Check(device.Model, Equals, "generic-classic")
	c.Check(device.Serial, Equals, "9999")

	// model was installed
	_, err = s.db.Find(asserts.ModelType, map[string]string{
		"series":   "16",
		"brand-id": "generic",
		"model":    "generic-classic",
		"classic":  "true",
	})
	c.Assert(err, IsNil)

	a, err := s.db.Find(asserts.SerialType, map[string]string{
		"brand-id": "generic",
		"model":    "generic-classic",
		"serial":   "9999",
	})
	c.Assert(err, IsNil)
	serial := a.(*asserts.Serial)

	privKey, err := devicestate.KeypairManager(s.mgr).Get(serial.DeviceKey().ID())
	c.Assert(err, IsNil)
	c.Check(privKey, NotNil)

	c.Check(device.KeyID, Equals, privKey.PublicKey().ID())

	// auto-refreshes are possible
	ok, err := devicestate.CanAutoRefresh(s.state)
	c.Assert(err, IsNil)
	c.Check(ok, Equals, true)
}

func (s *deviceMgrSuite) TestFullDeviceRegistrationAltBrandHappy(c *C) {
	c.Skip("not yet supported")
	r1 := devicestate.MockKeyLength(testKeyLength)
	defer r1()

	mockServer := s.mockServer(c, "REQID-1", nil)
	defer mockServer.Close()

	r2 := devicestate.MockBaseStoreURL(mockServer.URL)
	defer r2()

	// setup state as will be done by first-boot
	s.state.Lock()
	defer s.state.Unlock()

	s.makeModelAssertionInState(c, "my-brand", "my-model", map[string]interface{}{
		"classic": "true",
		"store":   "alt-store",
	})

	devicestatetest.MockGadget(c, s.state, "gadget", snap.R(2), nil)

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "my-brand",
		Model: "my-model",
	})

	// avoid full seeding
	s.seeding()

	// runs the whole device registration process
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	becomeOperational := s.findBecomeOperationalChange()
	c.Assert(becomeOperational, NotNil)

	c.Check(becomeOperational.Status().Ready(), Equals, true)
	c.Check(becomeOperational.Err(), IsNil)

	device, err := devicestatetest.Device(s.state)
	c.Assert(err, IsNil)
	c.Check(device.Brand, Equals, "my-brand")
	c.Check(device.Model, Equals, "my-model")
	c.Check(device.Serial, Equals, "9999")

	a, err := s.db.Find(asserts.SerialType, map[string]string{
		"brand-id": "my-brand",
		"model":    "my-model",
		"serial":   "9999",
	})
	c.Assert(err, IsNil)
	serial := a.(*asserts.Serial)

	privKey, err := devicestate.KeypairManager(s.mgr).Get(serial.DeviceKey().ID())
	c.Assert(err, IsNil)
	c.Check(privKey, NotNil)

	c.Check(device.KeyID, Equals, privKey.PublicKey().ID())
}

func (s *deviceMgrSuite) TestDoRequestSerialIdempotentAfterAddSerial(c *C) {
	privKey, _ := assertstest.GenerateKey(testKeyLength)

	mockServer := s.mockServer(c, "REQID-1", nil)
	defer mockServer.Close()

	restore := devicestate.MockBaseStoreURL(mockServer.URL)
	defer restore()

	restore = devicestate.MockRepeatRequestSerial("after-add-serial")
	defer restore()

	// setup state as done by first-boot/Ensure/doGenerateDeviceKey
	s.state.Lock()
	defer s.state.Unlock()

	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	devicestatetest.MockGadget(c, s.state, "pc", snap.R(2), nil)

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
		KeyID: privKey.PublicKey().ID(),
	})
	devicestate.KeypairManager(s.mgr).Put(privKey)

	t := s.state.NewTask("request-serial", "test")
	chg := s.state.NewChange("become-operational", "...")
	chg.AddTask(t)

	// avoid full seeding
	s.seeding()

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.DoingStatus)
	c.Check(chg.Err(), IsNil)
	device, err := devicestatetest.Device(s.state)
	c.Check(err, IsNil)
	_, err = s.db.Find(asserts.SerialType, map[string]string{
		"brand-id": "canonical",
		"model":    "pc",
		"serial":   "9999",
	})
	c.Assert(err, IsNil)

	ok := false
	select {
	case <-s.mgr.Registered():
	default:
		ok = true
	}
	c.Check(ok, Equals, true)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	// Repeated handler run but set original serial.
	c.Check(chg.Status(), Equals, state.DoneStatus)
	device, err = devicestatetest.Device(s.state)
	c.Check(err, IsNil)
	c.Check(device.Serial, Equals, "9999")

	ok = false
	select {
	case <-s.mgr.Registered():
		ok = true
	case <-time.After(5 * time.Second):
		c.Fatal("should have been marked registered")
	}
	c.Check(ok, Equals, true)
}

func (s *deviceMgrSuite) TestDoRequestSerialIdempotentAfterGotSerial(c *C) {
	privKey, _ := assertstest.GenerateKey(testKeyLength)

	mockServer := s.mockServer(c, "REQID-1", nil)
	defer mockServer.Close()

	restore := devicestate.MockBaseStoreURL(mockServer.URL)
	defer restore()

	restore = devicestate.MockRepeatRequestSerial("after-got-serial")
	defer restore()

	// setup state as done by first-boot/Ensure/doGenerateDeviceKey
	s.state.Lock()
	defer s.state.Unlock()

	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	devicestatetest.MockGadget(c, s.state, "pc", snap.R(2), nil)

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
		KeyID: privKey.PublicKey().ID(),
	})
	devicestate.KeypairManager(s.mgr).Put(privKey)

	t := s.state.NewTask("request-serial", "test")
	chg := s.state.NewChange("become-operational", "...")
	chg.AddTask(t)

	// avoid full seeding
	s.seeding()

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.DoingStatus)
	device, err := devicestatetest.Device(s.state)
	c.Check(err, IsNil)
	_, err = s.db.Find(asserts.SerialType, map[string]string{
		"brand-id": "canonical",
		"model":    "pc",
		"serial":   "9999",
	})
	c.Assert(asserts.IsNotFound(err), Equals, true)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	// Repeated handler run but set original serial.
	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Check(chg.Err(), IsNil)
	device, err = devicestatetest.Device(s.state)
	c.Check(err, IsNil)
	c.Check(device.Serial, Equals, "9999")
}

func (s *deviceMgrSuite) TestDoRequestSerialErrorsOnNoHost(c *C) {
	if os.Getenv("http_proxy") != "" {
		c.Skip("cannot run test when http proxy is in use, the error pattern is different")
	}

	const nonexistent_host = "nowhere.nowhere.test"

	// check internet access
	_, err := net.LookupHost(nonexistent_host)
	if netErr, ok := err.(net.Error); !ok || netErr.Temporary() {
		c.Skip("cannot run test with no internet access, the error pattern is different")
	}

	privKey, _ := assertstest.GenerateKey(testKeyLength)

	nowhere := "http://" + nonexistent_host

	restore := devicestate.MockBaseStoreURL(nowhere)
	defer restore()

	// setup state as done by first-boot/Ensure/doGenerateDeviceKey
	s.state.Lock()
	defer s.state.Unlock()

	devicestatetest.MockGadget(c, s.state, "gadget", snap.R(2), nil)

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
		KeyID: privKey.PublicKey().ID(),
	})
	devicestate.KeypairManager(s.mgr).Put(privKey)

	t := s.state.NewTask("request-serial", "test")
	chg := s.state.NewChange("become-operational", "...")
	chg.AddTask(t)

	// avoid full seeding
	s.seeding()

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.ErrorStatus)
}

func (s *deviceMgrSuite) TestDoRequestSerialMaxTentatives(c *C) {
	privKey, _ := assertstest.GenerateKey(testKeyLength)

	// immediate
	r := devicestate.MockRetryInterval(0)
	defer r()

	r = devicestate.MockMaxTentatives(2)
	defer r()

	mockServer := s.mockServer(c, devicestatetest.ReqIDFailID501, nil)
	defer mockServer.Close()

	restore := devicestate.MockBaseStoreURL(mockServer.URL)
	defer restore()

	restore = devicestate.MockRepeatRequestSerial("after-add-serial")
	defer restore()

	// setup state as done by first-boot/Ensure/doGenerateDeviceKey
	s.state.Lock()
	defer s.state.Unlock()

	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	devicestatetest.MockGadget(c, s.state, "pc", snap.R(2), nil)

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
		KeyID: privKey.PublicKey().ID(),
	})
	devicestate.KeypairManager(s.mgr).Put(privKey)

	t := s.state.NewTask("request-serial", "test")
	chg := s.state.NewChange("become-operational", "...")
	chg.AddTask(t)

	// avoid full seeding
	s.seeding()

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.DoingStatus)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*cannot retrieve request-id for making a request for a serial: unexpected status 501.*`)
}

func (s *deviceMgrSuite) TestFullDeviceRegistrationPollHappy(c *C) {
	r1 := devicestate.MockKeyLength(testKeyLength)
	defer r1()

	mockServer := s.mockServer(c, devicestatetest.ReqIDPoll, nil)
	defer mockServer.Close()

	r2 := devicestate.MockBaseStoreURL(mockServer.URL)
	defer r2()

	// immediately
	r3 := devicestate.MockRetryInterval(0)
	defer r3()

	// setup state as will be done by first-boot
	s.state.Lock()
	defer s.state.Unlock()

	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	devicestatetest.MockGadget(c, s.state, "pc", snap.R(2), nil)
	// mark as seeded
	s.state.Set("seeded", true)

	// runs the whole device registration process with polling
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	becomeOperational := s.findBecomeOperationalChange()
	c.Assert(becomeOperational, NotNil)

	// needs 3 more Retry passes of polling
	for i := 0; i < 3; i++ {
		s.state.Unlock()
		s.settle(c)
		s.state.Lock()
	}

	c.Check(becomeOperational.Status().Ready(), Equals, true)
	c.Check(becomeOperational.Err(), IsNil)

	device, err := devicestatetest.Device(s.state)
	c.Assert(err, IsNil)
	c.Check(device.Brand, Equals, "canonical")
	c.Check(device.Model, Equals, "pc")
	c.Check(device.Serial, Equals, "10002")

	a, err := s.db.Find(asserts.SerialType, map[string]string{
		"brand-id": "canonical",
		"model":    "pc",
		"serial":   "10002",
	})
	c.Assert(err, IsNil)
	serial := a.(*asserts.Serial)

	privKey, err := devicestate.KeypairManager(s.mgr).Get(serial.DeviceKey().ID())
	c.Assert(err, IsNil)
	c.Check(privKey, NotNil)

	c.Check(device.KeyID, Equals, privKey.PublicKey().ID())
}

func (s *deviceMgrSuite) TestFullDeviceRegistrationHappyPrepareDeviceHook(c *C) {
	r1 := devicestate.MockKeyLength(testKeyLength)
	defer r1()

	bhv := &devicestatetest.DeviceServiceBehavior{
		RequestIDURLPath: "/svc/request-id",
		SerialURLPath:    "/svc/serial",
	}
	bhv.PostPreflight = func(c *C, bhv *devicestatetest.DeviceServiceBehavior, w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("X-Extra-Header"), Equals, "extra")
	}

	mockServer := s.mockServer(c, "REQID-1", bhv)
	defer mockServer.Close()

	// setup state as will be done by first-boot
	// & have a gadget with a prepare-device hook
	s.state.Lock()
	defer s.state.Unlock()

	pDBhv := &devicestatetest.PrepareDeviceBehavior{
		DeviceSvcURL: mockServer.URL + "/svc/",
		Headers: map[string]string{
			"x-extra-header": "extra",
		},
		RegBody: map[string]string{
			"mac": "00:00:00:00:ff:00",
		},
		ProposedSerial: "Y9999",
	}

	r2 := devicestatetest.MockGadget(c, s.state, "gadget", snap.R(2), pDBhv)
	defer r2()

	// as device-service.url is set, should not need to do this but just in case
	r3 := devicestate.MockBaseStoreURL(mockServer.URL + "/direct/baad/")
	defer r3()

	s.makeModelAssertionInState(c, "canonical", "pc2", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "gadget",
	})

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc2",
	})

	// avoid full seeding
	s.seeding()

	// runs the whole device registration process, note that the
	// device is not seeded yet
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	// without a seeded device, there is no become-operational change
	becomeOperational := s.findBecomeOperationalChange()
	c.Assert(becomeOperational, IsNil)

	// now mark it as seeded
	s.state.Set("seeded", true)
	// and run the device registration again
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	becomeOperational = s.findBecomeOperationalChange()
	c.Assert(becomeOperational, NotNil)

	c.Check(becomeOperational.Status().Ready(), Equals, true)
	c.Check(becomeOperational.Err(), IsNil)

	device, err := devicestatetest.Device(s.state)
	c.Assert(err, IsNil)
	c.Check(device.Brand, Equals, "canonical")
	c.Check(device.Model, Equals, "pc2")
	c.Check(device.Serial, Equals, "Y9999")

	a, err := s.db.Find(asserts.SerialType, map[string]string{
		"brand-id": "canonical",
		"model":    "pc2",
		"serial":   "Y9999",
	})
	c.Assert(err, IsNil)
	serial := a.(*asserts.Serial)

	var details map[string]interface{}
	err = yaml.Unmarshal(serial.Body(), &details)
	c.Assert(err, IsNil)

	c.Check(details, DeepEquals, map[string]interface{}{
		"mac": "00:00:00:00:ff:00",
	})

	privKey, err := devicestate.KeypairManager(s.mgr).Get(serial.DeviceKey().ID())
	c.Assert(err, IsNil)
	c.Check(privKey, NotNil)

	c.Check(device.KeyID, Equals, privKey.PublicKey().ID())
}

func (s *deviceMgrSuite) TestFullDeviceRegistrationHappyWithHookAndNewProxy(c *C) {
	s.testFullDeviceRegistrationHappyWithHookAndProxy(c, true)
}

func (s *deviceMgrSuite) TestFullDeviceRegistrationHappyWithHookAndOldProxy(c *C) {
	s.testFullDeviceRegistrationHappyWithHookAndProxy(c, false)
}

func (s *deviceMgrSuite) testFullDeviceRegistrationHappyWithHookAndProxy(c *C, newEnough bool) {
	r1 := devicestate.MockKeyLength(testKeyLength)
	defer r1()

	var reqID string
	var storeVersion string
	head := func(c *C, bhv *devicestatetest.DeviceServiceBehavior, w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Snap-Store-Version", storeVersion)
	}
	bhv := &devicestatetest.DeviceServiceBehavior{
		Head: head,
	}
	svcPath := "/svc/"
	if newEnough {
		reqID = "REQID-42"
		storeVersion = "6"
		bhv.PostPreflight = func(c *C, bhv *devicestatetest.DeviceServiceBehavior, w http.ResponseWriter, r *http.Request) {
			c.Check(r.Header.Get("X-Snap-Device-Service-URL"), Matches, "http://[^/]*/bad/svc/")
			c.Check(r.Header.Get("X-Extra-Header"), Equals, "extra")
		}
		svcPath = "/bad/svc/"
	} else {
		reqID = "REQID-41"
		storeVersion = "5"
		bhv.RequestIDURLPath = "/svc/request-id"
		bhv.SerialURLPath = "/svc/serial"
		bhv.PostPreflight = func(c *C, bhv *devicestatetest.DeviceServiceBehavior, w http.ResponseWriter, r *http.Request) {
			c.Check(r.Header.Get("X-Extra-Header"), Equals, "extra")
		}
	}

	mockServer := s.mockServer(c, reqID, bhv)
	defer mockServer.Close()

	// setup state as will be done by first-boot
	// & have a gadget with a prepare-device hook
	s.state.Lock()
	defer s.state.Unlock()

	pDBhv := &devicestatetest.PrepareDeviceBehavior{
		DeviceSvcURL: mockServer.URL + svcPath,
		Headers: map[string]string{
			"x-extra-header": "extra",
		},
	}
	r2 := devicestatetest.MockGadget(c, s.state, "gadget", snap.R(2), pDBhv)
	defer r2()

	// as device-service.url is set, should not need to do this but just in case
	r3 := devicestate.MockBaseStoreURL(mockServer.URL + "/direct/baad/")
	defer r3()

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "proxy.store", "foo"), IsNil)
	tr.Commit()
	operatorAcct := assertstest.NewAccount(s.storeSigning, "foo-operator", nil, "")

	// have a store assertion.
	stoAs, err := s.storeSigning.Sign(asserts.StoreType, map[string]interface{}{
		"store":       "foo",
		"url":         mockServer.URL,
		"operator-id": operatorAcct.AccountID(),
		"timestamp":   time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	assertstatetest.AddMany(s.state, operatorAcct, stoAs)

	s.makeModelAssertionInState(c, "canonical", "pc2", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "gadget",
	})

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc2",
	})

	// mark it as seeded
	s.state.Set("seeded", true)

	// runs the whole device registration process
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	becomeOperational := s.findBecomeOperationalChange()
	c.Assert(becomeOperational, NotNil)

	c.Check(becomeOperational.Status().Ready(), Equals, true)
	c.Check(becomeOperational.Err(), IsNil)

	device, err := devicestatetest.Device(s.state)
	c.Assert(err, IsNil)
	c.Check(device.Brand, Equals, "canonical")
	c.Check(device.Model, Equals, "pc2")
	c.Check(device.Serial, Equals, "9999")

	a, err := s.db.Find(asserts.SerialType, map[string]string{
		"brand-id": "canonical",
		"model":    "pc2",
		"serial":   "9999",
	})
	c.Assert(err, IsNil)
	serial := a.(*asserts.Serial)

	privKey, err := devicestate.KeypairManager(s.mgr).Get(serial.DeviceKey().ID())
	c.Assert(err, IsNil)
	c.Check(privKey, NotNil)

	c.Check(device.KeyID, Equals, privKey.PublicKey().ID())
}

func (s *deviceMgrSuite) TestFullDeviceRegistrationErrorBackoff(c *C) {
	r1 := devicestate.MockKeyLength(testKeyLength)
	defer r1()

	bhv := &devicestatetest.DeviceServiceBehavior{}
	mockServer := s.mockServer(c, devicestatetest.ReqIDBadRequest, bhv)
	defer mockServer.Close()

	r2 := devicestate.MockBaseStoreURL(mockServer.URL)
	defer r2()

	// setup state as will be done by first-boot
	s.state.Lock()
	defer s.state.Unlock()

	// sanity
	c.Check(devicestate.EnsureOperationalAttempts(s.state), Equals, 0)

	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	devicestatetest.MockGadget(c, s.state, "pc", snap.R(2), nil)
	// mark as seeded
	s.state.Set("seeded", true)

	// try the whole device registration process
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	becomeOperational := s.findBecomeOperationalChange()
	c.Assert(becomeOperational, NotNil)
	firstTryID := becomeOperational.ID()

	c.Check(becomeOperational.Status().Ready(), Equals, true)
	c.Check(becomeOperational.Err(), ErrorMatches, `(?s).*cannot deliver device serial request: bad serial-request.*`)

	device, err := devicestatetest.Device(s.state)
	c.Assert(err, IsNil)
	c.Check(device.KeyID, Not(Equals), "")
	keyID := device.KeyID

	c.Check(devicestate.EnsureOperationalShouldBackoff(s.mgr, time.Now()), Equals, true)
	c.Check(devicestate.EnsureOperationalShouldBackoff(s.mgr, time.Now().Add(6*time.Minute)), Equals, false)
	c.Check(devicestate.EnsureOperationalAttempts(s.state), Equals, 1)

	// try again the whole device registration process
	bhv.ReqID = "REQID-1"
	devicestate.SetLastBecomeOperationalAttempt(s.mgr, time.Now().Add(-15*time.Minute))
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	becomeOperational = s.findBecomeOperationalChange(firstTryID)
	c.Assert(becomeOperational, NotNil)

	c.Check(becomeOperational.Status().Ready(), Equals, true)
	c.Check(becomeOperational.Err(), IsNil)

	c.Check(devicestate.EnsureOperationalAttempts(s.state), Equals, 2)

	device, err = devicestatetest.Device(s.state)
	c.Assert(err, IsNil)
	c.Check(device.KeyID, Equals, keyID)
	c.Check(device.Serial, Equals, "10000")
}

func (s *deviceMgrSuite) TestEnsureBecomeOperationalShouldBackoff(c *C) {
	t0 := time.Now()
	c.Check(devicestate.EnsureOperationalShouldBackoff(s.mgr, t0), Equals, false)
	c.Check(devicestate.BecomeOperationalBackoff(s.mgr), Equals, 5*time.Minute)

	backoffs := []time.Duration{5, 10, 20, 40, 80, 160, 320, 640, 1440, 1440}
	t1 := t0
	for _, m := range backoffs {
		c.Check(devicestate.EnsureOperationalShouldBackoff(s.mgr, t1.Add(time.Duration(m-1)*time.Minute)), Equals, true)

		t1 = t1.Add(time.Duration(m+1) * time.Minute)
		c.Check(devicestate.EnsureOperationalShouldBackoff(s.mgr, t1), Equals, false)
		m *= 2
		if m > (12 * 60) {
			m = 24 * 60
		}
		c.Check(devicestate.BecomeOperationalBackoff(s.mgr), Equals, m*time.Minute)
	}
}

func (s *deviceMgrSuite) TestFullDeviceRegistrationMismatchedSerial(c *C) {
	r1 := devicestate.MockKeyLength(testKeyLength)
	defer r1()

	mockServer := s.mockServer(c, devicestatetest.ReqIDSerialWithBadModel, nil)
	defer mockServer.Close()

	r2 := devicestate.MockBaseStoreURL(mockServer.URL)
	defer r2()

	// setup state as will be done by first-boot
	s.state.Lock()
	defer s.state.Unlock()

	// sanity
	c.Check(devicestate.EnsureOperationalAttempts(s.state), Equals, 0)

	devicestatetest.MockGadget(c, s.state, "gadget", snap.R(2), nil)

	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "gadget",
	})

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	// mark as seeded
	s.state.Set("seeded", true)

	// try the whole device registration process
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	becomeOperational := s.findBecomeOperationalChange()
	c.Assert(becomeOperational, NotNil)

	c.Check(becomeOperational.Status().Ready(), Equals, true)
	c.Check(becomeOperational.Err(), ErrorMatches, `(?s).*obtained serial assertion does not match provided device identity information.*`)
}

func (s *deviceMgrSuite) TestModelAndSerial(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	// nothing in the state
	_, err := s.mgr.Model()
	c.Check(err, Equals, state.ErrNoState)
	_, err = s.mgr.Serial()
	c.Check(err, Equals, state.ErrNoState)

	// just brand and model
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	_, err = s.mgr.Model()
	c.Check(err, Equals, state.ErrNoState)
	_, err = s.mgr.Serial()
	c.Check(err, Equals, state.ErrNoState)

	// have a model assertion
	model := s.brands.Model("canonical", "pc", map[string]interface{}{
		"gadget":       "pc",
		"kernel":       "kernel",
		"architecture": "amd64",
	})
	assertstatetest.AddMany(s.state, model)

	mod, err := s.mgr.Model()
	c.Assert(err, IsNil)
	c.Assert(mod.BrandID(), Equals, "canonical")

	_, err = s.mgr.Serial()
	c.Check(err, Equals, state.ErrNoState)

	// have a serial as well
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc",
		Serial: "8989",
	})
	_, err = s.mgr.Model()
	c.Assert(err, IsNil)
	_, err = s.mgr.Serial()
	c.Check(err, Equals, state.ErrNoState)

	// have a serial assertion
	s.makeSerialAssertionInState(c, "canonical", "pc", "8989")

	_, err = s.mgr.Model()
	c.Assert(err, IsNil)
	ser, err := s.mgr.Serial()
	c.Assert(err, IsNil)
	c.Check(ser.Serial(), Equals, "8989")
}

func (s *deviceMgrSuite) TestStoreContextBackendSetDevice(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	scb := s.mgr.StoreContextBackend()

	device, err := scb.Device()
	c.Check(err, IsNil)
	c.Check(device, DeepEquals, &auth.DeviceState{})

	err = scb.SetDevice(&auth.DeviceState{Brand: "some-brand"})
	c.Check(err, IsNil)
	device, err = scb.Device()
	c.Check(err, IsNil)
	c.Check(device, DeepEquals, &auth.DeviceState{Brand: "some-brand"})
}

func (s *deviceMgrSuite) TestStoreContextBackendModelAndSerial(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	scb := s.mgr.StoreContextBackend()

	// nothing in the state
	_, err := scb.Model()
	c.Check(err, Equals, state.ErrNoState)
	_, err = scb.Serial()
	c.Check(err, Equals, state.ErrNoState)

	// just brand and model
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	_, err = scb.Model()
	c.Check(err, Equals, state.ErrNoState)
	_, err = scb.Serial()
	c.Check(err, Equals, state.ErrNoState)

	// have a model assertion
	model := s.brands.Model("canonical", "pc", map[string]interface{}{
		"gadget":       "pc",
		"kernel":       "kernel",
		"architecture": "amd64",
	})
	assertstatetest.AddMany(s.state, model)

	mod, err := scb.Model()
	c.Assert(err, IsNil)
	c.Assert(mod.BrandID(), Equals, "canonical")

	_, err = scb.Serial()
	c.Check(err, Equals, state.ErrNoState)

	// have a serial as well
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc",
		Serial: "8989",
	})
	_, err = scb.Model()
	c.Assert(err, IsNil)
	_, err = scb.Serial()
	c.Check(err, Equals, state.ErrNoState)

	// have a serial assertion
	s.makeSerialAssertionInState(c, "canonical", "pc", "8989")

	_, err = scb.Model()
	c.Assert(err, IsNil)
	ser, err := scb.Serial()
	c.Assert(err, IsNil)
	c.Check(ser.Serial(), Equals, "8989")
}

var (
	devKey, _ = assertstest.GenerateKey(testKeyLength)
)

func (s *deviceMgrSuite) TestStoreContextBackendDeviceSessionRequestParams(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	scb := s.mgr.StoreContextBackend()

	// nothing there
	_, err := scb.SignDeviceSessionRequest(nil, "NONCE-1")
	c.Check(err, ErrorMatches, "internal error: cannot sign a session request without a serial")

	// setup state as done by device initialisation
	encDevKey, err := asserts.EncodePublicKey(devKey.PublicKey())
	c.Check(err, IsNil)
	seriala, err := s.storeSigning.Sign(asserts.SerialType, map[string]interface{}{
		"brand-id":            "canonical",
		"model":               "pc",
		"serial":              "8989",
		"device-key":          string(encDevKey),
		"device-key-sha3-384": devKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	assertstatetest.AddMany(s.state, seriala)
	serial := seriala.(*asserts.Serial)

	_, err = scb.SignDeviceSessionRequest(serial, "NONCE-1")
	c.Check(err, ErrorMatches, "internal error: inconsistent state with serial but no device key")

	// have a key
	devicestate.KeypairManager(s.mgr).Put(devKey)

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc",
		Serial: "8989",
		KeyID:  devKey.PublicKey().ID(),
	})

	sessReq, err := scb.SignDeviceSessionRequest(serial, "NONCE-1")
	c.Assert(err, IsNil)

	// correctly signed with device key
	err = asserts.SignatureCheck(sessReq, devKey.PublicKey())
	c.Check(err, IsNil)

	c.Check(sessReq.BrandID(), Equals, "canonical")
	c.Check(sessReq.Model(), Equals, "pc")
	c.Check(sessReq.Serial(), Equals, "8989")
	c.Check(sessReq.Nonce(), Equals, "NONCE-1")
}

func (s *deviceMgrSuite) TestStoreContextBackendProxyStore(c *C) {
	mockServer := s.mockServer(c, "", nil)
	defer mockServer.Close()
	s.state.Lock()
	defer s.state.Unlock()

	scb := s.mgr.StoreContextBackend()

	// nothing in the state
	_, err := scb.ProxyStore()
	c.Check(err, Equals, state.ErrNoState)

	// have a store referenced
	tr := config.NewTransaction(s.state)
	err = tr.Set("core", "proxy.store", "foo")
	tr.Commit()
	c.Assert(err, IsNil)

	_, err = scb.ProxyStore()
	c.Check(err, Equals, state.ErrNoState)

	operatorAcct := assertstest.NewAccount(s.storeSigning, "foo-operator", nil, "")

	// have a store assertion.
	stoAs, err := s.storeSigning.Sign(asserts.StoreType, map[string]interface{}{
		"store":       "foo",
		"operator-id": operatorAcct.AccountID(),
		"url":         mockServer.URL,
		"timestamp":   time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	assertstatetest.AddMany(s.state, operatorAcct, stoAs)

	sto, err := scb.ProxyStore()
	c.Assert(err, IsNil)
	c.Assert(sto.Store(), Equals, "foo")
	c.Assert(sto.URL().String(), Equals, mockServer.URL)
}

func (s *deviceMgrSuite) TestInitialRegistrationContext(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// have a model assertion
	model, err := s.storeSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":       "16",
		"brand-id":     "canonical",
		"model":        "pc",
		"gadget":       "pc-gadget",
		"kernel":       "kernel",
		"architecture": "amd64",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, model)
	c.Assert(err, IsNil)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	// TODO: will need to pass in a task later
	regCtx, err := devicestate.RegistrationCtx(s.mgr, nil)
	c.Assert(err, IsNil)
	c.Assert(regCtx, NotNil)

	c.Check(regCtx.ForRemodeling(), Equals, false)

	device, err := regCtx.Device()
	c.Check(err, IsNil)
	c.Check(device, DeepEquals, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	c.Check(regCtx.GadgetForSerialRequestConfig(), Equals, "pc-gadget")
	c.Check(regCtx.SerialRequestExtraHeaders(), HasLen, 0)
	c.Check(regCtx.SerialRequestAncillaryAssertions(), HasLen, 0)

}

func (s *deviceMgrSuite) TestDeviceManagerEnsureSeedYamlAlreadySeeded(c *C) {
	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()

	called := false
	restore := devicestate.MockPopulateStateFromSeed(func(*state.State, timings.Measurer) ([]*state.TaskSet, error) {
		called = true
		return nil, nil
	})
	defer restore()

	err := devicestate.EnsureSeedYaml(s.mgr)
	c.Assert(err, IsNil)
	c.Assert(called, Equals, false)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureSeedYamlChangeInFlight(c *C) {
	s.state.Lock()
	chg := s.state.NewChange("seed", "just for testing")
	chg.AddTask(s.state.NewTask("test-task", "the change needs a task"))
	s.state.Unlock()

	called := false
	restore := devicestate.MockPopulateStateFromSeed(func(*state.State, timings.Measurer) ([]*state.TaskSet, error) {
		called = true
		return nil, nil
	})
	defer restore()

	err := devicestate.EnsureSeedYaml(s.mgr)
	c.Assert(err, IsNil)
	c.Assert(called, Equals, false)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureSeedYamlAlsoOnClassic(c *C) {
	release.OnClassic = true

	called := false
	restore := devicestate.MockPopulateStateFromSeed(func(*state.State, timings.Measurer) ([]*state.TaskSet, error) {
		called = true
		return nil, nil
	})
	defer restore()

	err := devicestate.EnsureSeedYaml(s.mgr)
	c.Assert(err, IsNil)
	c.Assert(called, Equals, true)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureSeedYamlHappy(c *C) {
	restore := devicestate.MockPopulateStateFromSeed(func(*state.State, timings.Measurer) (ts []*state.TaskSet, err error) {
		t := s.state.NewTask("test-task", "a random task")
		ts = append(ts, state.NewTaskSet(t))
		return ts, nil
	})
	defer restore()

	err := devicestate.EnsureSeedYaml(s.mgr)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.state.Changes(), HasLen, 1)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureBootOkSkippedOnClassic(c *C) {
	release.OnClassic = true

	err := devicestate.EnsureBootOk(s.mgr)
	c.Assert(err, IsNil)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureBootOkBootloaderHappy(c *C) {
	s.bootloader.SetBootVars(map[string]string{
		"snap_mode":     "trying",
		"snap_try_core": "core_1.snap",
	})

	s.state.Lock()
	defer s.state.Unlock()
	siCore1 := &snap.SideInfo{RealName: "core", Revision: snap.R(1)}
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		SnapType: "os",
		Active:   true,
		Sequence: []*snap.SideInfo{siCore1},
		Current:  siCore1.Revision,
	})

	s.state.Unlock()
	err := devicestate.EnsureBootOk(s.mgr)
	s.state.Lock()
	c.Assert(err, IsNil)

	m, err := s.bootloader.GetBootVars("snap_mode")
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]string{"snap_mode": ""})
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureBootOkUpdateBootRevisionsHappy(c *C) {
	// simulate that we have a new core_2, tried to boot it but that failed
	s.bootloader.SetBootVars(map[string]string{
		"snap_mode":     "",
		"snap_kernel":   "kernel_1.snap",
		"snap_try_core": "core_2.snap",
		"snap_core":     "core_1.snap",
	})

	s.state.Lock()
	defer s.state.Unlock()
	siKernel1 := &snap.SideInfo{RealName: "kernel", Revision: snap.R(1)}
	snapstate.Set(s.state, "kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: []*snap.SideInfo{siKernel1},
		Current:  siKernel1.Revision,
	})

	siCore1 := &snap.SideInfo{RealName: "core", Revision: snap.R(1)}
	siCore2 := &snap.SideInfo{RealName: "core", Revision: snap.R(2)}
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		SnapType: "os",
		Active:   true,
		Sequence: []*snap.SideInfo{siCore1, siCore2},
		Current:  siCore2.Revision,
	})

	s.state.Unlock()
	err := devicestate.EnsureBootOk(s.mgr)
	s.state.Lock()
	c.Assert(err, IsNil)

	c.Check(s.state.Changes(), HasLen, 1)
	c.Check(s.state.Changes()[0].Kind(), Equals, "update-revisions")
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureBootOkNotRunAgain(c *C) {
	s.bootloader.SetBootVars(map[string]string{
		"snap_mode":     "trying",
		"snap_try_core": "core_1.snap",
	})
	s.bootloader.SetErr = fmt.Errorf("ensure bootloader is not used")

	devicestate.SetBootOkRan(s.mgr, true)

	err := devicestate.EnsureBootOk(s.mgr)
	c.Assert(err, IsNil)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureBootOkError(c *C) {
	s.state.Lock()
	// seeded
	s.state.Set("seeded", true)
	// has serial
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc",
		Serial: "8989",
	})
	s.state.Unlock()

	s.bootloader.GetErr = fmt.Errorf("bootloader err")

	devicestate.SetBootOkRan(s.mgr, false)

	err := s.mgr.Ensure()
	c.Assert(err, ErrorMatches, "devicemgr: bootloader err")
}

func (s *deviceMgrSuite) setupBrands(c *C) {
	assertstatetest.AddMany(s.state, s.brands.AccountsAndKeys("my-brand")...)
	otherAcct := assertstest.NewAccount(s.storeSigning, "other-brand", map[string]interface{}{
		"account-id": "other-brand",
	}, "")
	assertstatetest.AddMany(s.state, otherAcct)
}

func (s *deviceMgrSuite) setupSnapDecl(c *C, info *snap.Info, publisherID string) {
	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-name":    info.SnapName(),
		"snap-id":      info.SnapID,
		"publisher-id": publisherID,
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	assertstatetest.AddMany(s.state, snapDecl)
}

func fakeMyModel(extra map[string]interface{}) *asserts.Model {
	model := map[string]interface{}{
		"type":         "model",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model",
	}
	return assertstest.FakeAssertion(model, extra).(*asserts.Model)
}

func (s *deviceMgrSuite) TestCheckGadget(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	gadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: other-gadget, version: 0}", nil)

	s.setupBrands(c)
	// model assertion in device context
	model := fakeMyModel(map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "gadget",
		"kernel":       "krnl",
	})
	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: model}

	err := devicestate.CheckGadgetOrKernel(s.state, gadgetInfo, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, ErrorMatches, `cannot install gadget "other-gadget", model assertion requests "gadget"`)

	// brand gadget
	brandGadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)
	brandGadgetInfo.SnapID = "brand-gadget-id"
	s.setupSnapDecl(c, brandGadgetInfo, "my-brand")

	// canonical gadget
	canonicalGadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)
	canonicalGadgetInfo.SnapID = "canonical-gadget-id"
	s.setupSnapDecl(c, canonicalGadgetInfo, "canonical")

	// other gadget
	otherGadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)
	otherGadgetInfo.SnapID = "other-gadget-id"
	s.setupSnapDecl(c, otherGadgetInfo, "other-brand")

	// install brand gadget ok
	err = devicestate.CheckGadgetOrKernel(s.state, brandGadgetInfo, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)

	// install canonical gadget ok
	err = devicestate.CheckGadgetOrKernel(s.state, canonicalGadgetInfo, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)

	// install other gadget fails
	err = devicestate.CheckGadgetOrKernel(s.state, otherGadgetInfo, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, ErrorMatches, `cannot install gadget "gadget" published by "other-brand" for model by "my-brand"`)

	// unasserted installation of other works
	otherGadgetInfo.SnapID = ""
	err = devicestate.CheckGadgetOrKernel(s.state, otherGadgetInfo, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)

	// parallel install fails
	otherGadgetInfo.InstanceKey = "foo"
	err = devicestate.CheckGadgetOrKernel(s.state, otherGadgetInfo, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, ErrorMatches, `cannot install "gadget_foo", parallel installation of kernel or gadget snaps is not supported`)
}

func (s *deviceMgrSuite) TestCheckGadgetOnClassic(c *C) {
	release.OnClassic = true

	s.state.Lock()
	defer s.state.Unlock()

	gadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: other-gadget, version: 0}", nil)

	s.setupBrands(c)
	// model assertion in device context
	model := fakeMyModel(map[string]interface{}{
		"classic": "true",
		"gadget":  "gadget",
	})
	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: model}

	err := devicestate.CheckGadgetOrKernel(s.state, gadgetInfo, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, ErrorMatches, `cannot install gadget "other-gadget", model assertion requests "gadget"`)

	// brand gadget
	brandGadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)
	brandGadgetInfo.SnapID = "brand-gadget-id"
	s.setupSnapDecl(c, brandGadgetInfo, "my-brand")

	// canonical gadget
	canonicalGadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)
	canonicalGadgetInfo.SnapID = "canonical-gadget-id"
	s.setupSnapDecl(c, canonicalGadgetInfo, "canonical")

	// other gadget
	otherGadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)
	otherGadgetInfo.SnapID = "other-gadget-id"
	s.setupSnapDecl(c, otherGadgetInfo, "other-brand")

	// install brand gadget ok
	err = devicestate.CheckGadgetOrKernel(s.state, brandGadgetInfo, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)

	// install canonical gadget ok
	err = devicestate.CheckGadgetOrKernel(s.state, canonicalGadgetInfo, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)

	// install other gadget fails
	err = devicestate.CheckGadgetOrKernel(s.state, otherGadgetInfo, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, ErrorMatches, `cannot install gadget "gadget" published by "other-brand" for model by "my-brand"`)

	// unasserted installation of other works
	otherGadgetInfo.SnapID = ""
	err = devicestate.CheckGadgetOrKernel(s.state, otherGadgetInfo, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)
}

func (s *deviceMgrSuite) TestCheckGadgetOnClassicGadgetNotSpecified(c *C) {
	release.OnClassic = true

	s.state.Lock()
	defer s.state.Unlock()

	gadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)

	s.setupBrands(c)
	// model assertion in device context
	model := fakeMyModel(map[string]interface{}{
		"classic": "true",
	})
	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: model}

	err := devicestate.CheckGadgetOrKernel(s.state, gadgetInfo, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, ErrorMatches, `cannot install gadget snap on classic if not requested by the model`)
}

func (s *deviceMgrSuite) TestCheckKernel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	kernelInfo := snaptest.MockInfo(c, "{type: kernel, name: lnrk, version: 0}", nil)

	// not on classic
	release.OnClassic = true
	err := devicestate.CheckGadgetOrKernel(s.state, kernelInfo, nil, snapstate.Flags{}, nil)
	c.Check(err, ErrorMatches, `cannot install a kernel snap on classic`)
	release.OnClassic = false

	s.setupBrands(c)
	// model assertion in device context
	model := fakeMyModel(map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "gadget",
		"kernel":       "krnl",
	})
	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: model}

	err = devicestate.CheckGadgetOrKernel(s.state, kernelInfo, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, ErrorMatches, `cannot install kernel "lnrk", model assertion requests "krnl"`)

	// brand kernel
	brandKrnlInfo := snaptest.MockInfo(c, "{type: kernel, name: krnl, version: 0}", nil)
	brandKrnlInfo.SnapID = "brand-krnl-id"
	s.setupSnapDecl(c, brandKrnlInfo, "my-brand")

	// canonical kernel
	canonicalKrnlInfo := snaptest.MockInfo(c, "{type: kernel, name: krnl, version: 0}", nil)
	canonicalKrnlInfo.SnapID = "canonical-krnl-id"
	s.setupSnapDecl(c, canonicalKrnlInfo, "canonical")

	// other kernel
	otherKrnlInfo := snaptest.MockInfo(c, "{type: kernel, name: krnl, version: 0}", nil)
	otherKrnlInfo.SnapID = "other-krnl-id"
	s.setupSnapDecl(c, otherKrnlInfo, "other-brand")

	// install brand kernel ok
	err = devicestate.CheckGadgetOrKernel(s.state, brandKrnlInfo, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)

	// install canonical kernel ok
	err = devicestate.CheckGadgetOrKernel(s.state, canonicalKrnlInfo, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)

	// install other kernel fails
	err = devicestate.CheckGadgetOrKernel(s.state, otherKrnlInfo, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, ErrorMatches, `cannot install kernel "krnl" published by "other-brand" for model by "my-brand"`)

	// unasserted installation of other works
	otherKrnlInfo.SnapID = ""
	err = devicestate.CheckGadgetOrKernel(s.state, otherKrnlInfo, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)

	// parallel install fails
	otherKrnlInfo.InstanceKey = "foo"
	err = devicestate.CheckGadgetOrKernel(s.state, otherKrnlInfo, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, ErrorMatches, `cannot install "krnl_foo", parallel installation of kernel or gadget snaps is not supported`)
}

func (s *deviceMgrSuite) makeModelAssertionInState(c *C, brandID, model string, extras map[string]interface{}) {
	modelAs := s.brands.Model(brandID, model, extras)

	s.setupBrands(c)
	assertstatetest.AddMany(s.state, modelAs)
}

func makeSerialAssertionInState(c *C, brands *assertstest.SigningAccounts, st *state.State, brandID, model, serialN string) *asserts.Serial {
	encDevKey, err := asserts.EncodePublicKey(devKey.PublicKey())
	c.Assert(err, IsNil)
	serial, err := brands.Signing(brandID).Sign(asserts.SerialType, map[string]interface{}{
		"brand-id":            brandID,
		"model":               model,
		"serial":              serialN,
		"device-key":          string(encDevKey),
		"device-key-sha3-384": devKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(st, serial)
	c.Assert(err, IsNil)
	return serial.(*asserts.Serial)
}

func (s *deviceMgrSuite) makeSerialAssertionInState(c *C, brandID, model, serialN string) *asserts.Serial {
	return makeSerialAssertionInState(c, s.brands, s.state, brandID, model, serialN)
}

func (s *deviceMgrSuite) TestCanAutoRefreshOnCore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	canAutoRefresh := func() bool {
		ok, err := devicestate.CanAutoRefresh(s.state)
		c.Assert(err, IsNil)
		return ok
	}

	// not seeded, no model, no serial -> no auto-refresh
	s.state.Set("seeded", false)
	c.Check(canAutoRefresh(), Equals, false)

	// seeded, model, no serial -> no auto-refresh
	s.state.Set("seeded", true)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	c.Check(canAutoRefresh(), Equals, false)

	// seeded, model, serial -> auto-refresh
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc",
		Serial: "8989",
	})
	s.makeSerialAssertionInState(c, "canonical", "pc", "8989")
	c.Check(canAutoRefresh(), Equals, true)

	// not seeded, model, serial -> no auto-refresh
	s.state.Set("seeded", false)
	c.Check(canAutoRefresh(), Equals, false)
}

func (s *deviceMgrSuite) TestCanAutoRefreshNoSerialFallback(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	canAutoRefresh := func() bool {
		ok, err := devicestate.CanAutoRefresh(s.state)
		c.Assert(err, IsNil)
		return ok
	}

	// seeded, model, no serial, two attempts at getting serial
	// -> no auto-refresh
	devicestate.IncEnsureOperationalAttempts(s.state)
	devicestate.IncEnsureOperationalAttempts(s.state)
	s.state.Set("seeded", true)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	c.Check(canAutoRefresh(), Equals, false)

	// third attempt ongoing, or done
	// fallback, try auto-refresh
	devicestate.IncEnsureOperationalAttempts(s.state)
	// sanity
	c.Check(devicestate.EnsureOperationalAttempts(s.state), Equals, 3)
	c.Check(canAutoRefresh(), Equals, true)
}

func (s *deviceMgrSuite) TestCanAutoRefreshOnClassic(c *C) {
	release.OnClassic = true

	s.state.Lock()
	defer s.state.Unlock()

	canAutoRefresh := func() bool {
		ok, err := devicestate.CanAutoRefresh(s.state)
		c.Assert(err, IsNil)
		return ok
	}

	// not seeded, no model, no serial -> no auto-refresh
	s.state.Set("seeded", false)
	c.Check(canAutoRefresh(), Equals, false)

	// seeded, no model -> auto-refresh
	s.state.Set("seeded", true)
	c.Check(canAutoRefresh(), Equals, false)

	// seeded, model, no serial -> no auto-refresh
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"classic": "true",
	})
	c.Check(canAutoRefresh(), Equals, false)

	// seeded, model, serial -> auto-refresh
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc",
		Serial: "8989",
	})
	s.makeSerialAssertionInState(c, "canonical", "pc", "8989")
	c.Check(canAutoRefresh(), Equals, true)

	// not seeded, model, serial -> no auto-refresh
	s.state.Set("seeded", false)
	c.Check(canAutoRefresh(), Equals, false)
}

func makeInstalledMockCoreSnapWithSnapdControl(c *C, st *state.State) *snap.Info {
	sideInfoCore11 := &snap.SideInfo{RealName: "core", Revision: snap.R(11), SnapID: "core-id"}
	snapstate.Set(st, "core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfoCore11},
		Current:  sideInfoCore11.Revision,
		SnapType: "os",
	})
	core11 := snaptest.MockSnap(c, `
name: core
version: 1.0
slots:
 snapd-control:
`, sideInfoCore11)
	c.Assert(core11.Slots, HasLen, 1)

	return core11
}

var snapWithSnapdControlRefreshScheduleManagedYAML = `
name: snap-with-snapd-control
version: 1.0
plugs:
 snapd-control:
  refresh-schedule: managed
`

var snapWithSnapdControlOnlyYAML = `
name: snap-with-snapd-control
version: 1.0
plugs:
 snapd-control:
`

func makeInstalledMockSnap(c *C, st *state.State, yml string) *snap.Info {
	sideInfo11 := &snap.SideInfo{RealName: "snap-with-snapd-control", Revision: snap.R(11), SnapID: "snap-with-snapd-control-id"}
	snapstate.Set(st, "snap-with-snapd-control", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfo11},
		Current:  sideInfo11.Revision,
		SnapType: "app",
	})
	info11 := snaptest.MockSnap(c, yml, sideInfo11)
	c.Assert(info11.Plugs, HasLen, 1)

	return info11
}

func makeMockRepoWithConnectedSnaps(c *C, st *state.State, info11, core11 *snap.Info, ifname string) {
	repo := interfaces.NewRepository()
	for _, iface := range builtin.Interfaces() {
		err := repo.AddInterface(iface)
		c.Assert(err, IsNil)
	}
	err := repo.AddSnap(info11)
	c.Assert(err, IsNil)
	err = repo.AddSnap(core11)
	c.Assert(err, IsNil)
	_, err = repo.Connect(&interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: info11.InstanceName(), Name: ifname},
		SlotRef: interfaces.SlotRef{Snap: core11.InstanceName(), Name: ifname},
	}, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	conns, err := repo.Connected("snap-with-snapd-control", "snapd-control")
	c.Assert(err, IsNil)
	c.Assert(conns, HasLen, 1)
	ifacerepo.Replace(st, repo)
}

func (s *deviceMgrSuite) TestCanManageRefreshes(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// not possbile to manage by default
	c.Check(devicestate.CanManageRefreshes(st), Equals, false)

	// not possible with just a snap with "snapd-control" plug with the
	// right attribute
	info11 := makeInstalledMockSnap(c, st, snapWithSnapdControlRefreshScheduleManagedYAML)
	c.Check(devicestate.CanManageRefreshes(st), Equals, false)

	// not possible with a core snap with snapd control
	core11 := makeInstalledMockCoreSnapWithSnapdControl(c, st)
	c.Check(devicestate.CanManageRefreshes(st), Equals, false)

	// not possible even with connected interfaces
	makeMockRepoWithConnectedSnaps(c, st, info11, core11, "snapd-control")
	c.Check(devicestate.CanManageRefreshes(st), Equals, false)

	// if all of the above plus a snap declaration are in place we can
	// manage schedules
	s.setupSnapDecl(c, info11, "canonical")
	c.Check(devicestate.CanManageRefreshes(st), Equals, true)

	// works if the snap is not active as well (to fix race when a
	// snap is refreshed)
	var sideInfo11 snapstate.SnapState
	err := snapstate.Get(st, "snap-with-snapd-control", &sideInfo11)
	c.Assert(err, IsNil)
	sideInfo11.Active = false
	snapstate.Set(st, "snap-with-snapd-control", &sideInfo11)
	c.Check(devicestate.CanManageRefreshes(st), Equals, true)
}

func (s *deviceMgrSuite) TestCanManageRefreshesNoRefreshScheduleManaged(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// just having a connected "snapd-control" interface is not enough
	// for setting refresh.schedule=managed
	info11 := makeInstalledMockSnap(c, st, snapWithSnapdControlOnlyYAML)
	core11 := makeInstalledMockCoreSnapWithSnapdControl(c, st)
	makeMockRepoWithConnectedSnaps(c, st, info11, core11, "snapd-control")
	s.setupSnapDecl(c, info11, "canonical")

	c.Check(devicestate.CanManageRefreshes(st), Equals, false)
}

func (s *deviceMgrSuite) TestReloadRegistered(c *C) {
	st := state.New(nil)

	runner1 := state.NewTaskRunner(st)
	hookMgr1, err := hookstate.Manager(st, runner1)
	c.Assert(err, IsNil)
	mgr1, err := devicestate.Manager(st, hookMgr1, runner1, nil)
	c.Assert(err, IsNil)

	ok := false
	select {
	case <-mgr1.Registered():
	default:
		ok = true
	}
	c.Check(ok, Equals, true)

	st.Lock()
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc",
		Serial: "serial",
	})
	st.Unlock()

	runner2 := state.NewTaskRunner(st)
	hookMgr2, err := hookstate.Manager(st, runner2)
	c.Assert(err, IsNil)
	mgr2, err := devicestate.Manager(st, hookMgr2, runner2, nil)
	c.Assert(err, IsNil)

	ok = false
	select {
	case <-mgr2.Registered():
		ok = true
	case <-time.After(5 * time.Second):
		c.Fatal("should have been marked registered")
	}
	c.Check(ok, Equals, true)
}

func (s *deviceMgrSuite) TestMarkSeededInConfig(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// avoid device registration
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Serial: "123",
	})

	// avoid full seeding
	s.seeding()

	// not seeded -> no config is set
	s.state.Unlock()
	s.mgr.Ensure()
	s.state.Lock()

	var seedLoaded bool
	tr := config.NewTransaction(st)
	tr.Get("core", "seed.loaded", &seedLoaded)
	c.Check(seedLoaded, Equals, false)

	// pretend we are seeded now
	s.state.Set("seeded", true)

	// seeded -> config got updated
	s.state.Unlock()
	s.mgr.Ensure()
	s.state.Lock()

	tr = config.NewTransaction(st)
	tr.Get("core", "seed.loaded", &seedLoaded)
	c.Check(seedLoaded, Equals, true)

	// only the fake seeding change is in the state, no further
	// changes
	c.Check(s.state.Changes(), HasLen, 1)
}

func (s *deviceMgrSuite) TestNewEnoughProxyParse(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	log, restore := logger.MockLogger()
	defer restore()
	os.Setenv("SNAPD_DEBUG", "1")
	defer os.Unsetenv("SNAPD_DEBUG")

	badURL := &url.URL{Opaque: "%a"} // url.Parse(badURL.String()) needs to fail, which isn't easy :-)
	c.Check(devicestate.NewEnoughProxy(s.state, badURL, http.DefaultClient), Equals, false)
	c.Check(log.String(), Matches, "(?m).* DEBUG: Cannot check whether proxy store supports a custom serial vault: parse .*")
}

func (s *deviceMgrSuite) TestNewEnoughProxy(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	expectedUserAgent := httputil.UserAgent()
	log, restore := logger.MockLogger()
	defer restore()
	os.Setenv("SNAPD_DEBUG", "1")
	defer os.Unsetenv("SNAPD_DEBUG")

	expecteds := []string{
		`Head http://\S+: EOF`,
		`Head request returned 403 Forbidden.`,
		`Bogus Snap-Store-Version header "5pre1".`,
		``,
	}

	n := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("User-Agent"), Equals, expectedUserAgent)
		n++
		switch n {
		case 1:
			conn, _, err := w.(http.Hijacker).Hijack()
			c.Assert(err, IsNil)
			conn.Close()
		case 2:
			w.WriteHeader(403)
		case 3:
			w.Header().Set("Snap-Store-Version", "5pre1")
			w.WriteHeader(200)
		case 4:
			w.Header().Set("Snap-Store-Version", "5")
			w.WriteHeader(200)
		case 5:
			w.Header().Set("Snap-Store-Version", "6")
			w.WriteHeader(200)
		default:
			c.Errorf("expected %d results, now on %d", len(expecteds), n)
		}
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	c.Assert(err, IsNil)
	for _, expected := range expecteds {
		log.Reset()
		c.Check(devicestate.NewEnoughProxy(s.state, u, http.DefaultClient), Equals, false)
		if len(expected) > 0 {
			expected = "(?m).* DEBUG: Cannot check whether proxy store supports a custom serial vault: " + expected
		}
		c.Check(log.String(), Matches, expected)
	}
	c.Check(n, Equals, len(expecteds))

	// and success at last
	log.Reset()
	c.Check(devicestate.NewEnoughProxy(s.state, u, http.DefaultClient), Equals, true)
	c.Check(log.String(), Equals, "")
	c.Check(n, Equals, len(expecteds)+1)
}

func (s *deviceMgrSuite) TestDevicemgrCanStandby(c *C) {
	st := state.New(nil)

	runner := state.NewTaskRunner(st)
	hookMgr, err := hookstate.Manager(st, runner)
	c.Assert(err, IsNil)
	mgr, err := devicestate.Manager(st, hookMgr, runner, nil)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()
	c.Check(mgr.CanStandby(), Equals, false)

	st.Set("seeded", true)
	c.Check(mgr.CanStandby(), Equals, true)
}

type testModel struct {
	brand, model               string
	arch, base, kernel, gadget string
}

func (s *deviceMgrSuite) TestRemodelUnhappyNotSeeded(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", false)

	newModel := s.brands.Model("canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	_, err := devicestate.Remodel(s.state, newModel)
	c.Assert(err, ErrorMatches, "cannot remodel until fully seeded")
}

func (s *deviceMgrSuite) TestRemodelUnhappy(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)

	// set a model assertion
	cur := map[string]string{
		"brand":        "canonical",
		"model":        "pc-model",
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	}
	s.makeModelAssertionInState(c, cur["brand"], cur["model"], map[string]interface{}{
		"architecture": cur["architecture"],
		"kernel":       cur["kernel"],
		"gadget":       cur["gadget"],
		"base":         cur["base"],
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: cur["brand"],
		Model: cur["model"],
	})

	// ensure all error cases are checked
	for _, t := range []struct {
		new    map[string]string
		errStr string
	}{
		{map[string]string{"architecture": "pdp-7"}, "cannot remodel to different architectures yet"},
		{map[string]string{"base": "core20"}, "cannot remodel to different bases yet"},
		{map[string]string{"kernel": "other-kernel"}, "cannot remodel to different kernels yet"},
		{map[string]string{"gadget": "other-gadget"}, "cannot remodel to different gadgets yet"},
	} {
		// copy current model unless new model test data is different
		for k, v := range cur {
			if t.new[k] != "" {
				continue
			}
			t.new[k] = v
		}
		new := s.brands.Model(t.new["brand"], t.new["model"], map[string]interface{}{
			"architecture": t.new["architecture"],
			"kernel":       t.new["kernel"],
			"gadget":       t.new["gadget"],
			"base":         t.new["base"],
		})
		chg, err := devicestate.Remodel(s.state, new)
		c.Check(chg, IsNil)
		c.Check(err, ErrorMatches, t.errStr)
	}
}

func (s *deviceMgrSuite) TestRemodelTasksSmoke(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	var testDeviceCtx snapstate.DeviceContext

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, true)
		c.Check(deviceCtx, Equals, testDeviceCtx)
		c.Check(fromChange, Equals, "99")

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

	restore = devicestate.MockSnapstateUpdateWithDeviceContext(func(st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, false)
		c.Check(flags.NoReRefresh, Equals, true)
		c.Check(deviceCtx, Equals, testDeviceCtx)
		c.Check(fromChange, Equals, "99")

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s to track %s", name, opts.Channel))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tUpdate := s.state.NewTask("fake-update", fmt.Sprintf("Update %s to track %s", name, opts.Channel))
		tUpdate.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tUpdate)
		ts.MarkEdge(tValidate, snapstate.DownloadAndChecksDoneEdge)
		return ts, nil
	})
	defer restore()

	// set a model assertion
	current := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	err := assertstate.Add(s.state, current)
	c.Assert(err, IsNil)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel=18",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1", "new-required-snap-2"},
		"revision":       "1",
	})

	testDeviceCtx = &snapstatetest.TrivialDeviceContext{Remodeling: true}

	tss, err := devicestate.RemodelTasks(context.Background(), s.state, current, new, testDeviceCtx, "99")
	c.Assert(err, IsNil)
	// 2 snaps, plus one track switch plus the remodel task, the
	// wait chain is tested in TestRemodel*
	c.Assert(tss, HasLen, 4)
}

func (s *deviceMgrSuite) TestRemodelRequiredSnaps(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

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
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1", "new-required-snap-2"},
		"revision":       "1",
	})
	chg, err := devicestate.Remodel(s.state, new)
	c.Assert(err, IsNil)
	c.Assert(chg.Summary(), Equals, "Refresh model assertion from revision 0 to 1")

	tl := chg.Tasks()
	// 2 snaps,
	c.Assert(tl, HasLen, 2*3+1)

	deviceCtx, err := devicestate.DeviceCtx(s.state, tl[0], nil)
	c.Assert(err, IsNil)
	// deviceCtx is actually a remodelContext here
	remodCtx, ok := deviceCtx.(devicestate.RemodelContext)
	c.Assert(ok, Equals, true)
	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.UpdateRemodel)
	c.Check(remodCtx.Model(), DeepEquals, new)
	c.Check(remodCtx.Store(), IsNil)

	// check the tasks
	tDownloadSnap1 := tl[0]
	tValidateSnap1 := tl[1]
	tInstallSnap1 := tl[2]
	tDownloadSnap2 := tl[3]
	tValidateSnap2 := tl[4]
	tInstallSnap2 := tl[5]
	tSetModel := tl[6]

	// check the tasks
	c.Assert(tDownloadSnap1.Kind(), Equals, "fake-download")
	c.Assert(tDownloadSnap1.Summary(), Equals, "Download new-required-snap-1")
	c.Assert(tDownloadSnap1.WaitTasks(), HasLen, 0)
	c.Assert(tValidateSnap1.Kind(), Equals, "validate-snap")
	c.Assert(tValidateSnap1.Summary(), Equals, "Validate new-required-snap-1")
	c.Assert(tDownloadSnap1.WaitTasks(), HasLen, 0)
	c.Assert(tDownloadSnap2.Kind(), Equals, "fake-download")
	c.Assert(tDownloadSnap2.Summary(), Equals, "Download new-required-snap-2")
	// check the ordering, download/validate everything first, then install

	// snap2 downloads wait for the downloads of snap1
	c.Assert(tDownloadSnap1.WaitTasks(), HasLen, 0)
	c.Assert(tValidateSnap1.WaitTasks(), DeepEquals, []*state.Task{
		tDownloadSnap1,
	})
	c.Assert(tDownloadSnap2.WaitTasks(), DeepEquals, []*state.Task{
		tValidateSnap1,
	})
	c.Assert(tValidateSnap2.WaitTasks(), DeepEquals, []*state.Task{
		tDownloadSnap2,
	})
	c.Assert(tInstallSnap1.WaitTasks(), DeepEquals, []*state.Task{
		// wait for own check-snap
		tValidateSnap1,
		// and also the last check-snap of the download chain
		tValidateSnap2,
	})
	c.Assert(tInstallSnap2.WaitTasks(), DeepEquals, []*state.Task{
		// last snap of the download chain
		tValidateSnap2,
		// previous install chain
		tInstallSnap1,
	})

	c.Assert(tSetModel.Kind(), Equals, "set-model")
	c.Assert(tSetModel.Summary(), Equals, "Set new model assertion")
	// setModel waits for everything in the change
	c.Assert(tSetModel.WaitTasks(), DeepEquals, []*state.Task{tDownloadSnap1, tValidateSnap1, tInstallSnap1, tDownloadSnap2, tValidateSnap2, tInstallSnap2})
}

func (s *deviceMgrSuite) TestRemodelSwitchKernelTrack(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

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

	restore = devicestate.MockSnapstateUpdateWithDeviceContext(func(st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, false)
		c.Check(flags.NoReRefresh, Equals, true)
		c.Check(deviceCtx, NotNil)
		c.Check(deviceCtx.ForRemodeling(), Equals, true)

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s to track %s", name, opts.Channel))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tUpdate := s.state.NewTask("fake-update", fmt.Sprintf("Update %s to track %s", name, opts.Channel))
		tUpdate.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tUpdate)
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
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel=18",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1"},
		"revision":       "1",
	})
	chg, err := devicestate.Remodel(s.state, new)
	c.Assert(err, IsNil)
	c.Assert(chg.Summary(), Equals, "Refresh model assertion from revision 0 to 1")

	tl := chg.Tasks()
	c.Assert(tl, HasLen, 2*3+1)

	tDownloadKernel := tl[0]
	tValidateKernel := tl[1]
	tUpdateKernel := tl[2]
	tDownloadSnap1 := tl[3]
	tValidateSnap1 := tl[4]
	tInstallSnap1 := tl[5]
	tSetModel := tl[6]

	c.Assert(tDownloadKernel.Kind(), Equals, "fake-download")
	c.Assert(tDownloadKernel.Summary(), Equals, "Download pc-kernel to track 18")
	c.Assert(tValidateKernel.Kind(), Equals, "validate-snap")
	c.Assert(tValidateKernel.Summary(), Equals, "Validate pc-kernel")
	c.Assert(tUpdateKernel.Kind(), Equals, "fake-update")
	c.Assert(tUpdateKernel.Summary(), Equals, "Update pc-kernel to track 18")
	c.Assert(tDownloadSnap1.Kind(), Equals, "fake-download")
	c.Assert(tDownloadSnap1.Summary(), Equals, "Download new-required-snap-1")
	c.Assert(tValidateSnap1.Kind(), Equals, "validate-snap")
	c.Assert(tValidateSnap1.Summary(), Equals, "Validate new-required-snap-1")
	c.Assert(tInstallSnap1.Kind(), Equals, "fake-install")
	c.Assert(tInstallSnap1.Summary(), Equals, "Install new-required-snap-1")

	c.Assert(tSetModel.Kind(), Equals, "set-model")
	c.Assert(tSetModel.Summary(), Equals, "Set new model assertion")

	// check the ordering
	c.Assert(tDownloadSnap1.WaitTasks(), DeepEquals, []*state.Task{
		// previous download finished
		tValidateKernel,
	})
	c.Assert(tInstallSnap1.WaitTasks(), DeepEquals, []*state.Task{
		// last download in the chain finished
		tValidateSnap1,
		// and kernel got updated
		tUpdateKernel,
	})
	c.Assert(tUpdateKernel.WaitTasks(), DeepEquals, []*state.Task{
		// kernel is valid
		tValidateKernel,
		// and last download in the chain finished
		tValidateSnap1,
	})
}

func (s *deviceMgrSuite) TestRemodelLessRequiredSnaps(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"some-required-snap"},
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
		"revision":     "1",
	})
	chg, err := devicestate.Remodel(s.state, new)
	c.Assert(err, IsNil)
	c.Assert(chg.Summary(), Equals, "Refresh model assertion from revision 0 to 1")

	tl := chg.Tasks()
	c.Assert(tl, HasLen, 1)
	tSetModel := tl[0]
	c.Assert(tSetModel.Kind(), Equals, "set-model")
	c.Assert(tSetModel.Summary(), Equals, "Set new model assertion")
}

type freshSessionStore struct {
	storetest.Store

	ensureDeviceSession int
}

func (sto *freshSessionStore) EnsureDeviceSession() (*auth.DeviceState, error) {
	sto.ensureDeviceSession += 1
	return nil, nil
}

func (s *deviceMgrSuite) TestRemodelStoreSwitch(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	var testStore snapstate.StoreService

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, true)
		c.Check(deviceCtx, NotNil)
		c.Check(deviceCtx.ForRemodeling(), Equals, true)

		c.Check(deviceCtx.Store(), Equals, testStore)

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
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"store":          "switched-store",
		"required-snaps": []interface{}{"new-required-snap-1", "new-required-snap-2"},
		"revision":       "1",
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

	chg, err := devicestate.Remodel(s.state, new)
	c.Assert(err, IsNil)
	c.Assert(chg.Summary(), Equals, "Refresh model assertion from revision 0 to 1")

	c.Check(freshStore.ensureDeviceSession, Equals, 1)

	tl := chg.Tasks()
	// 2 snaps * 3 tasks (from the mock install above) +
	// 1 "set-model" task at the end
	c.Assert(tl, HasLen, 2*3+1)

	deviceCtx, err := devicestate.DeviceCtx(s.state, tl[0], nil)
	c.Assert(err, IsNil)
	// deviceCtx is actually a remodelContext here
	remodCtx, ok := deviceCtx.(devicestate.RemodelContext)
	c.Assert(ok, Equals, true)
	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.StoreSwitchRemodel)
	c.Check(remodCtx.Model(), DeepEquals, new)
	c.Check(remodCtx.Store(), Equals, testStore)
}

func (s *deviceMgrSuite) TestRemodelRereg(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)

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

	s.newFakeStore = func(devBE storecontext.DeviceBackend) snapstate.StoreService {
		mod, err := devBE.Model()
		c.Check(err, IsNil)
		if err == nil {
			c.Check(mod, DeepEquals, new)
		}
		return nil
	}

	chg, err := devicestate.Remodel(s.state, new)
	c.Assert(err, IsNil)

	c.Assert(chg.Summary(), Equals, "Remodel device to canonical/rereg-model (0)")

	tl := chg.Tasks()
	c.Assert(tl, HasLen, 2)

	// check the tasks
	tRequestSerial := tl[0]
	tPrepareRemodeling := tl[1]

	// check the tasks
	c.Assert(tRequestSerial.Kind(), Equals, "request-serial")
	c.Assert(tRequestSerial.Summary(), Equals, "Request new device serial")
	c.Assert(tRequestSerial.WaitTasks(), HasLen, 0)

	c.Assert(tPrepareRemodeling.Kind(), Equals, "prepare-remodeling")
	c.Assert(tPrepareRemodeling.Summary(), Equals, "Prepare remodeling")
	c.Assert(tPrepareRemodeling.WaitTasks(), DeepEquals, []*state.Task{tRequestSerial})
}

func (s *deviceMgrSuite) TestRemodelClash(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	var clashing *asserts.Model

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// simulate things changing under our feet
		assertstatetest.AddMany(st, clashing)
		devicestatetest.SetDevice(s.state, &auth.DeviceState{
			Brand: "canonical",
			Model: clashing.Model(),
		})

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
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1", "new-required-snap-2"},
		"revision":       "1",
	})
	other := s.brands.Model("canonical", "pc-model-other", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1", "new-required-snap-2"},
	})

	clashing = other
	_, err := devicestate.Remodel(s.state, new)
	c.Check(err, DeepEquals, &snapstate.ChangeConflictError{
		Message: "cannot start remodel, clashing with concurrent remodel to canonical/pc-model-other (0)",
	})

	// reset
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})
	clashing = new
	_, err = devicestate.Remodel(s.state, new)
	c.Check(err, DeepEquals, &snapstate.ChangeConflictError{
		Message: "cannot start remodel, clashing with concurrent remodel to canonical/pc-model (1)",
	})
}

func (s *deviceMgrSuite) TestRemodelClashInProgress(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// simulate another started remodeling
		st.NewChange("remodel", "other remodel")

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
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1", "new-required-snap-2"},
		"revision":       "1",
	})

	_, err := devicestate.Remodel(s.state, new)
	c.Check(err, DeepEquals, &snapstate.ChangeConflictError{
		Message: "cannot start remodel, clashing with concurrent one",
	})
}

func (s *deviceMgrSuite) TestReregRemodelClashAnyChange(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)

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

	new := s.brands.Model("canonical", "pc-model-2", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1", "new-required-snap-2"},
		"revision":       "1",
	})

	// simulate any other change
	s.state.NewChange("chg", "other change")

	_, err := devicestate.Remodel(s.state, new)
	c.Check(err, DeepEquals, &snapstate.ChangeConflictError{
		Message: "cannot start complete remodel, other changes are in progress",
	})
}

func (s *deviceMgrSuite) TestRemodeling(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// no changes
	c.Check(devicestate.Remodeling(s.state), Equals, false)

	// other change
	s.state.NewChange("other", "...")
	c.Check(devicestate.Remodeling(s.state), Equals, false)

	// remodel change
	chg := s.state.NewChange("remodel", "...")
	c.Check(devicestate.Remodeling(s.state), Equals, true)

	// done
	chg.SetStatus(state.DoneStatus)
	c.Check(devicestate.Remodeling(s.state), Equals, false)
}

func (s *deviceMgrSuite) TestDoRequestSerialReregistration(c *C) {
	mockServer := s.mockServer(c, "REQID-1", nil)
	defer mockServer.Close()

	restore := devicestate.MockBaseStoreURL(mockServer.URL)
	defer restore()

	assertstest.AddMany(s.storeSigning, s.brands.AccountsAndKeys("rereg-brand")...)

	// setup state as done by first-boot/Ensure/doGenerateDeviceKey
	s.state.Lock()
	defer s.state.Unlock()

	s.makeModelAssertionInState(c, "my-brand", "my-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	devicestatetest.MockGadget(c, s.state, "pc", snap.R(2), nil)

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		KeyID:  devKey.PublicKey().ID(),
		Serial: "9999",
	})
	devicestate.KeypairManager(s.mgr).Put(devKey)

	// have a serial assertion
	s.makeSerialAssertionInState(c, "my-brand", "my-model", "9999")

	new := s.brands.Model("rereg-brand", "rereg-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	cur, err := s.mgr.Model()
	c.Assert(err, IsNil)

	s.newFakeStore = func(devBE storecontext.DeviceBackend) snapstate.StoreService {
		mod, err := devBE.Model()
		c.Check(err, IsNil)
		if err == nil {
			c.Check(mod, DeepEquals, new)
		}
		return nil
	}

	remodCtx, err := devicestate.RemodelCtx(s.state, cur, new)
	c.Assert(err, IsNil)
	c.Check(remodCtx.Kind(), Equals, devicestate.ReregRemodel)

	t := s.state.NewTask("request-serial", "test")
	chg := s.state.NewChange("remodel", "...")
	// associate with context
	remodCtx.Init(chg)
	chg.AddTask(t)

	// sanity
	regCtx, err := devicestate.RegistrationCtx(s.mgr, t)
	c.Assert(err, IsNil)
	c.Check(regCtx, Equals, remodCtx.(devicestate.RegistrationContext))

	// avoid full seeding
	s.seeding()

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("%s", t.Log()))
	c.Check(chg.Err(), IsNil)
	device, err := devicestatetest.Device(s.state)
	c.Check(err, IsNil)
	c.Check(device.Serial, Equals, "9999")
	_, err = s.db.Find(asserts.SerialType, map[string]string{
		"brand-id": "rereg-brand",
		"model":    "rereg-model",
		"serial":   "9999",
	})
	c.Assert(err, IsNil)
}

func (s *deviceMgrSuite) TestDoRequestSerialReregistrationStreamFromService(c *C) {
	mockServer := s.mockServer(c, "REQID-1", nil)
	defer mockServer.Close()

	restore := devicestate.MockBaseStoreURL(mockServer.URL)
	defer restore()

	s.ancillary = s.brands.AccountsAndKeys("rereg-brand")

	// setup state as after initial registration
	s.state.Lock()
	defer s.state.Unlock()

	s.makeModelAssertionInState(c, "my-brand", "my-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	devicestatetest.MockGadget(c, s.state, "pc", snap.R(2), nil)

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		KeyID:  devKey.PublicKey().ID(),
		Serial: "9999",
	})
	devicestate.KeypairManager(s.mgr).Put(devKey)

	// have the serial assertion
	s.makeSerialAssertionInState(c, "my-brand", "my-model", "9999")

	new := s.brands.Model("rereg-brand", "rereg-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	cur, err := s.mgr.Model()
	c.Assert(err, IsNil)

	s.newFakeStore = func(devBE storecontext.DeviceBackend) snapstate.StoreService {
		mod, err := devBE.Model()
		c.Check(err, IsNil)
		if err == nil {
			c.Check(mod, DeepEquals, new)
		}
		return nil
	}

	remodCtx, err := devicestate.RemodelCtx(s.state, cur, new)
	c.Assert(err, IsNil)
	c.Check(remodCtx.Kind(), Equals, devicestate.ReregRemodel)

	t := s.state.NewTask("request-serial", "test")
	chg := s.state.NewChange("remodel", "...")
	// associate with context
	remodCtx.Init(chg)
	chg.AddTask(t)

	// sanity
	regCtx, err := devicestate.RegistrationCtx(s.mgr, t)
	c.Assert(err, IsNil)
	c.Check(regCtx, Equals, remodCtx.(devicestate.RegistrationContext))

	// avoid full seeding
	s.seeding()

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("%s", t.Log()))
	c.Check(chg.Err(), IsNil)
	device, err := devicestatetest.Device(s.state)
	c.Check(err, IsNil)
	c.Check(device.Serial, Equals, "9999")
	_, err = s.db.Find(asserts.SerialType, map[string]string{
		"brand-id": "rereg-brand",
		"model":    "rereg-model",
		"serial":   "9999",
	})
	c.Assert(err, IsNil)
}

func (s *deviceMgrSuite) TestDoRequestSerialReregistrationIncompleteStreamFromService(c *C) {
	mockServer := s.mockServer(c, "REQID-1", nil)
	defer mockServer.Close()

	restore := devicestate.MockBaseStoreURL(mockServer.URL)
	defer restore()

	// will produce an incomplete stream!
	s.ancillary = s.brands.AccountsAndKeys("rereg-brand")[:1]

	// setup state as after initial registration
	s.state.Lock()
	defer s.state.Unlock()

	s.makeModelAssertionInState(c, "my-brand", "my-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	devicestatetest.MockGadget(c, s.state, "pc", snap.R(2), nil)

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		KeyID:  devKey.PublicKey().ID(),
		Serial: "9999",
	})
	devicestate.KeypairManager(s.mgr).Put(devKey)

	// have the serial assertion
	s.makeSerialAssertionInState(c, "my-brand", "my-model", "9999")

	new := s.brands.Model("rereg-brand", "rereg-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	cur, err := s.mgr.Model()
	c.Assert(err, IsNil)

	s.newFakeStore = func(devBE storecontext.DeviceBackend) snapstate.StoreService {
		mod, err := devBE.Model()
		c.Check(err, IsNil)
		if err == nil {
			c.Check(mod, DeepEquals, new)
		}
		return nil
	}

	remodCtx, err := devicestate.RemodelCtx(s.state, cur, new)
	c.Assert(err, IsNil)
	c.Check(remodCtx.Kind(), Equals, devicestate.ReregRemodel)

	t := s.state.NewTask("request-serial", "test")
	chg := s.state.NewChange("remodel", "...")
	// associate with context
	remodCtx.Init(chg)
	chg.AddTask(t)

	// sanity
	regCtx, err := devicestate.RegistrationCtx(s.mgr, t)
	c.Assert(err, IsNil)
	c.Check(regCtx, Equals, remodCtx.(devicestate.RegistrationContext))

	// avoid full seeding
	s.seeding()

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.ErrorStatus, Commentf("%s", t.Log()))
	c.Check(chg.Err(), ErrorMatches, `(?ms).*cannot accept stream of assertions from device service:.*`)
}

func (s *deviceMgrSuite) TestDoRequestSerialReregistrationDoubleSerialStreamFromService(c *C) {
	mockServer := s.mockServer(c, "REQID-1", nil)
	defer mockServer.Close()

	restore := devicestate.MockBaseStoreURL(mockServer.URL)
	defer restore()

	// will produce an incomplete stream!
	s.ancillary = s.brands.AccountsAndKeys("rereg-brand")

	// setup state as after initial registration
	s.state.Lock()
	defer s.state.Unlock()

	s.makeModelAssertionInState(c, "my-brand", "my-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	devicestatetest.MockGadget(c, s.state, "pc", snap.R(2), nil)

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		KeyID:  devKey.PublicKey().ID(),
		Serial: "9999",
	})
	devicestate.KeypairManager(s.mgr).Put(devKey)

	// have the serial assertion
	serial0 := s.makeSerialAssertionInState(c, "my-brand", "my-model", "9999")
	s.ancillary = append(s.ancillary, serial0)

	new := s.brands.Model("rereg-brand", "rereg-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	cur, err := s.mgr.Model()
	c.Assert(err, IsNil)

	s.newFakeStore = func(devBE storecontext.DeviceBackend) snapstate.StoreService {
		mod, err := devBE.Model()
		c.Check(err, IsNil)
		if err == nil {
			c.Check(mod, DeepEquals, new)
		}
		return nil
	}

	remodCtx, err := devicestate.RemodelCtx(s.state, cur, new)
	c.Assert(err, IsNil)
	c.Check(remodCtx.Kind(), Equals, devicestate.ReregRemodel)

	t := s.state.NewTask("request-serial", "test")
	chg := s.state.NewChange("remodel", "...")
	// associate with context
	remodCtx.Init(chg)
	chg.AddTask(t)

	// sanity
	regCtx, err := devicestate.RegistrationCtx(s.mgr, t)
	c.Assert(err, IsNil)
	c.Check(regCtx, Equals, remodCtx.(devicestate.RegistrationContext))

	// avoid full seeding
	s.seeding()

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.ErrorStatus, Commentf("%s", t.Log()))
	c.Check(chg.Err(), ErrorMatches, `(?ms).*cannot accept more than a single device serial assertion from the device service.*`)
}

func (s *deviceMgrSuite) TestDeviceCtxNoTask(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	// nothing in the state

	_, err := devicestate.DeviceCtx(s.state, nil, nil)
	c.Check(err, Equals, state.ErrNoState)

	// have a model assertion
	model := s.brands.Model("canonical", "pc", map[string]interface{}{
		"gadget":       "pc",
		"kernel":       "kernel",
		"architecture": "amd64",
	})
	assertstatetest.AddMany(s.state, model)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	deviceCtx, err := devicestate.DeviceCtx(s.state, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(deviceCtx.Model().BrandID(), Equals, "canonical")
}

func (s *deviceMgrSuite) TestDeviceCtxProvided(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	model := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "model",
		"authority-id": "canonical",
		"series":       "16",
		"brand-id":     "canonical",
		"model":        "pc",
		"gadget":       "pc",
		"kernel":       "kernel",
		"architecture": "amd64",
	}).(*asserts.Model)

	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: model}

	deviceCtx1, err := devicestate.DeviceCtx(s.state, nil, deviceCtx)
	c.Assert(err, IsNil)
	c.Assert(deviceCtx1, Equals, deviceCtx)
}

var snapYaml = `
name: foo-gadget
type: gadget
`

var gadgetYaml = `
volumes:
  pc:
    bootloader: grub
`

func setupGadgetUpdate(c *C, st *state.State) (chg *state.Change, tsk *state.Task) {
	siCurrent := &snap.SideInfo{
		RealName: "foo-gadget",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	si := &snap.SideInfo{
		RealName: "foo-gadget",
		Revision: snap.R(34),
		SnapID:   "foo-id",
	}
	snaptest.MockSnapWithFiles(c, snapYaml, siCurrent, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})
	snaptest.MockSnapWithFiles(c, snapYaml, si, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})

	st.Lock()

	snapstate.Set(st, "foo-gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: []*snap.SideInfo{siCurrent},
		Current:  siCurrent.Revision,
		Active:   true,
	})

	tsk = st.NewTask("update-gadget-assets", "update gadget")
	tsk.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Type:     snap.TypeGadget,
	})
	chg = st.NewChange("dummy", "...")
	chg.AddTask(tsk)

	st.Unlock()

	return chg, tsk
}

func (s *deviceMgrSuite) TestUpdateGadgetOnCoreSimple(c *C) {
	var updateCalled bool
	var passedRollbackDir string
	restore := devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string) error {
		updateCalled = true
		passedRollbackDir = path
		st, err := os.Stat(path)
		c.Assert(err, IsNil)
		m := st.Mode()
		c.Assert(m.IsDir(), Equals, true)
		c.Check(m.Perm(), Equals, os.FileMode(0750))
		return nil
	})
	defer restore()

	chg, t := setupGadgetUpdate(c, s.state)

	for i := 0; i < 6; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), IsNil)
	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(updateCalled, Equals, true)
	rollbackDir := filepath.Join(dirs.SnapRollbackDir, "foo-gadget_34")
	c.Check(rollbackDir, Equals, passedRollbackDir)
	// should have been removed right after update
	c.Check(osutil.IsDirectory(rollbackDir), Equals, false)
	c.Check(s.restartRequests, DeepEquals, []state.RestartType{state.RestartSystem})
}

func (s *deviceMgrSuite) TestUpdateGadgetOnCoreNoUpdateNeeded(c *C) {
	var called bool
	restore := devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string) error {
		called = true
		return gadget.ErrNoUpdate
	})
	defer restore()

	chg, t := setupGadgetUpdate(c, s.state)

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), IsNil)
	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(t.Log(), HasLen, 1)
	c.Check(t.Log()[0], Matches, ".* INFO No gadget assets update needed")
	c.Check(called, Equals, true)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrSuite) TestUpdateGadgetOnCoreRollbackDirCreateFailed(c *C) {
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root (permissions are not honored)")
	}

	restore := devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string) error {
		return errors.New("unexpected call")
	})
	defer restore()

	chg, t := setupGadgetUpdate(c, s.state)

	rollbackDir := filepath.Join(dirs.SnapRollbackDir, "foo-gadget_34")
	err := os.MkdirAll(dirs.SnapRollbackDir, 0000)
	c.Assert(err, IsNil)

	for i := 0; i < 6; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), ErrorMatches, `(?s).*cannot prepare update rollback directory: .*`)
	c.Check(t.Status(), Equals, state.ErrorStatus)
	c.Check(osutil.IsDirectory(rollbackDir), Equals, false)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrSuite) TestUpdateGadgetOnCoreUpdateFailed(c *C) {
	restore := devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string) error {
		return errors.New("gadget exploded")
	})
	defer restore()
	chg, t := setupGadgetUpdate(c, s.state)

	for i := 0; i < 6; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), ErrorMatches, `(?s).*update gadget \(gadget exploded\).*`)
	c.Check(t.Status(), Equals, state.ErrorStatus)
	rollbackDir := filepath.Join(dirs.SnapRollbackDir, "foo-gadget_34")
	// update rollback left for inspection
	c.Check(osutil.IsDirectory(rollbackDir), Equals, true)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrSuite) TestUpdateGadgetOnCoreNotDuringFirstboot(c *C) {
	restore := devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string) error {
		return errors.New("unexpected call")
	})
	defer restore()

	// simulate first-boot/seeding, there is no existing snap state information

	si := &snap.SideInfo{
		RealName: "foo-gadget",
		Revision: snap.R(34),
		SnapID:   "foo-id",
	}
	snaptest.MockSnapWithFiles(c, snapYaml, si, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})

	s.state.Lock()

	t := s.state.NewTask("update-gadget-assets", "update gadget")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Type:     snap.TypeGadget,
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	for i := 0; i < 6; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), IsNil)
	c.Check(t.Status(), Equals, state.DoneStatus)
	rollbackDir := filepath.Join(dirs.SnapRollbackDir, "foo-gadget")
	c.Check(osutil.IsDirectory(rollbackDir), Equals, false)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrSuite) TestUpdateGadgetOnCoreBadGadgetYaml(c *C) {
	restore := devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string) error {
		return errors.New("unexpected call")
	})
	defer restore()
	siCurrent := &snap.SideInfo{
		RealName: "foo-gadget",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	si := &snap.SideInfo{
		RealName: "foo-gadget",
		Revision: snap.R(34),
		SnapID:   "foo-id",
	}
	snaptest.MockSnapWithFiles(c, snapYaml, siCurrent, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})
	// invalid gadget.yaml data
	snaptest.MockSnapWithFiles(c, snapYaml, si, [][]string{
		{"meta/gadget.yaml", "foobar"},
	})

	s.state.Lock()

	snapstate.Set(s.state, "foo-gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: []*snap.SideInfo{siCurrent},
		Current:  siCurrent.Revision,
		Active:   true,
	})

	t := s.state.NewTask("update-gadget-assets", "update gadget")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Type:     snap.TypeGadget,
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	for i := 0; i < 6; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), ErrorMatches, `(?s).*update gadget \(cannot read candidate gadget snap details: .*\).*`)
	c.Check(t.Status(), Equals, state.ErrorStatus)
	rollbackDir := filepath.Join(dirs.SnapRollbackDir, "foo-gadget")
	c.Check(osutil.IsDirectory(rollbackDir), Equals, false)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrSuite) TestUpdateGadgetOnClassicErrorsOut(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	restore = devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string) error {
		return errors.New("unexpected call")
	})
	defer restore()

	s.state.Lock()

	t := s.state.NewTask("update-gadget-assets", "update gadget")
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	// we cannot use "s.o.Settle()" here because this change has an
	// error which means that the settle will never converge
	for i := 0; i < 50; i++ {
		s.se.Ensure()
		s.se.Wait()

		s.state.Lock()
		ready := chg.IsReady()
		s.state.Unlock()
		if ready {
			break
		}
	}

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), ErrorMatches, `(?s).*update gadget \(cannot run update gadget assets task on a classic system\).*`)
	c.Check(t.Status(), Equals, state.ErrorStatus)
}

type mockUpdater struct{}

func (m *mockUpdater) Backup() error { return nil }

func (m *mockUpdater) Rollback() error { return nil }

func (m *mockUpdater) Update() error { return nil }

func (s *deviceMgrSuite) TestUpdateGadgetCallsToGadget(c *C) {
	siCurrent := &snap.SideInfo{
		RealName: "foo-gadget",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	si := &snap.SideInfo{
		RealName: "foo-gadget",
		Revision: snap.R(34),
		SnapID:   "foo-id",
	}
	var gadgetCurrentYaml = `
volumes:
  pc:
    bootloader: grub
    structure:
       - name: foo
         size: 10M
         type: bare
         content:
            - image: content.img
`
	var gadgetUpdateYaml = `
volumes:
  pc:
    bootloader: grub
    structure:
       - name: foo
         size: 10M
         type: bare
         content:
            - image: content.img
         update:
           edition: 2
`
	snaptest.MockSnapWithFiles(c, snapYaml, siCurrent, [][]string{
		{"meta/gadget.yaml", gadgetCurrentYaml},
		{"content.img", "some content"},
	})
	updateInfo := snaptest.MockSnapWithFiles(c, snapYaml, si, [][]string{
		{"meta/gadget.yaml", gadgetUpdateYaml},
		{"content.img", "updated content"},
	})

	expectedRollbackDir := filepath.Join(dirs.SnapRollbackDir, "foo-gadget_34")
	updaterForStructureCalls := 0
	gadget.MockUpdaterForStructure(func(ps *gadget.LaidOutStructure, rootDir, rollbackDir string) (gadget.Updater, error) {
		updaterForStructureCalls++

		c.Assert(ps.Name, Equals, "foo")
		c.Assert(rootDir, Equals, updateInfo.MountDir())
		c.Assert(filepath.Join(rootDir, "content.img"), testutil.FileEquals, "updated content")
		c.Assert(strings.HasPrefix(rollbackDir, expectedRollbackDir), Equals, true)
		c.Assert(osutil.IsDirectory(rollbackDir), Equals, true)
		return &mockUpdater{}, nil
	})

	s.state.Lock()

	snapstate.Set(s.state, "foo-gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: []*snap.SideInfo{siCurrent},
		Current:  siCurrent.Revision,
		Active:   true,
	})

	t := s.state.NewTask("update-gadget-assets", "update gadget")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Type:     snap.TypeGadget,
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	for i := 0; i < 6; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(s.restartRequests, HasLen, 1)
	c.Check(updaterForStructureCalls, Equals, 1)
}

func (s *deviceMgrSuite) TestCurrentAndUpdateInfo(c *C) {
	siCurrent := &snap.SideInfo{
		RealName: "foo-gadget",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	si := &snap.SideInfo{
		RealName: "foo-gadget",
		Revision: snap.R(34),
		SnapID:   "foo-id",
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapsup := &snapstate.SnapSetup{
		SideInfo: si,
		Type:     snap.TypeGadget,
	}

	current, update, err := devicestate.GadgetCurrentAndUpdate(s.state, snapsup)
	c.Assert(current, IsNil)
	c.Assert(update, IsNil)
	c.Assert(err, IsNil)

	snapstate.Set(s.state, "foo-gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: []*snap.SideInfo{siCurrent},
		Current:  siCurrent.Revision,
		Active:   true,
	})

	// mock current first, but gadget.yaml is still missing
	ci := snaptest.MockSnapWithFiles(c, snapYaml, siCurrent, nil)

	current, update, err = devicestate.GadgetCurrentAndUpdate(s.state, snapsup)
	c.Assert(current, IsNil)
	c.Assert(update, IsNil)
	c.Assert(err, ErrorMatches, "cannot read current gadget snap details: .*/33/meta/gadget.yaml: no such file or directory")

	// drop gadget.yaml for current snap
	ioutil.WriteFile(filepath.Join(ci.MountDir(), "meta/gadget.yaml"), []byte(gadgetYaml), 0644)

	// update missing snap.yaml
	current, update, err = devicestate.GadgetCurrentAndUpdate(s.state, snapsup)
	c.Assert(current, IsNil)
	c.Assert(update, IsNil)
	c.Assert(err, ErrorMatches, "cannot read candidate gadget snap details: cannot find installed snap .* .*/34/meta/snap.yaml")

	ui := snaptest.MockSnapWithFiles(c, snapYaml, si, nil)

	current, update, err = devicestate.GadgetCurrentAndUpdate(s.state, snapsup)
	c.Assert(current, IsNil)
	c.Assert(update, IsNil)
	c.Assert(err, ErrorMatches, "cannot read candidate gadget snap details: .*/34/meta/gadget.yaml: no such file or directory")

	var updateGadgetYaml = `
volumes:
  pc:
    bootloader: grub
    id: 123
`

	// drop gadget.yaml for update snap
	ioutil.WriteFile(filepath.Join(ui.MountDir(), "meta/gadget.yaml"), []byte(updateGadgetYaml), 0644)

	current, update, err = devicestate.GadgetCurrentAndUpdate(s.state, snapsup)
	c.Assert(err, IsNil)
	c.Assert(current, DeepEquals, &gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]gadget.Volume{
				"pc": {
					Bootloader: "grub",
				},
			},
		},
		RootDir: ci.MountDir(),
	})
	c.Assert(update, DeepEquals, &gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]gadget.Volume{
				"pc": {
					Bootloader: "grub",
					ID:         "123",
				},
			},
		},
		RootDir: ui.MountDir(),
	})
}

func (s *deviceMgrSuite) TestGadgetUpdateBlocksWhenOtherTasks(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tUpdate := s.state.NewTask("update-gadget-assets", "update gadget")
	t1 := s.state.NewTask("other-task-1", "other 1")
	t2 := s.state.NewTask("other-task-2", "other 2")

	// no other running tasks, does not block
	c.Assert(devicestate.GadgetUpdateBlocked(tUpdate, nil), Equals, false)

	// list of running tasks actually contains ones that are in the 'running' state
	t1.SetStatus(state.DoingStatus)
	t2.SetStatus(state.UndoingStatus)
	// block on any other running tasks
	c.Assert(devicestate.GadgetUpdateBlocked(tUpdate, []*state.Task{t1, t2}), Equals, true)
}

func (s *deviceMgrSuite) TestGadgetUpdateBlocksOtherTasks(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tUpdate := s.state.NewTask("update-gadget-assets", "update gadget")
	tUpdate.SetStatus(state.DoingStatus)
	t1 := s.state.NewTask("other-task-1", "other 1")
	t2 := s.state.NewTask("other-task-2", "other 2")

	// block on any other running tasks
	c.Assert(devicestate.GadgetUpdateBlocked(t1, []*state.Task{tUpdate}), Equals, true)
	c.Assert(devicestate.GadgetUpdateBlocked(t2, []*state.Task{tUpdate}), Equals, true)

	t2.SetStatus(state.UndoingStatus)
	// update-gadget should be the only running task, for the sake of
	// completeness pretend it's one of many running tasks
	c.Assert(devicestate.GadgetUpdateBlocked(t1, []*state.Task{tUpdate, t2}), Equals, true)

	// not blocking without gadget update task
	c.Assert(devicestate.GadgetUpdateBlocked(t1, []*state.Task{t2}), Equals, false)
}
