// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2021 Canonical Ltd
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
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storecontext"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/testutil"
)

var testKeyLength = 1024

type deviceMgrSerialSuite struct {
	deviceMgrBaseSuite
}

var _ = Suite(&deviceMgrSerialSuite{})

func (s *deviceMgrSerialSuite) SetUpTest(c *C) {
	classic := false
	s.setupBaseTest(c, classic)
}

func (s *deviceMgrSerialSuite) signSerial(c *C, bhv *devicestatetest.DeviceServiceBehavior, headers map[string]interface{}, body []byte) (serial asserts.Assertion, ancillary []asserts.Assertion, err error) {
	brandID := headers["brand-id"].(string)
	model := headers["model"].(string)
	keyID := ""

	var signing assertstest.SignerDB = s.storeSigning

	switch model {
	case "pc", "pc2", "pc-20":
		fallthrough
	case "classic-alt-store":
		c.Check(brandID, Equals, "canonical")
	case "my-model-accept-generic":
		c.Check(brandID, Equals, "my-brand")
		headers["authority-id"] = "generic"
		keyID = s.storeSigning.GenericKey.PublicKeyID()
	case "generic-classic":
		c.Check(brandID, Equals, "generic")
		headers["authority-id"] = "generic"
		keyID = s.storeSigning.GenericKey.PublicKeyID()
	case "rereg-model":
		headers["authority-id"] = "rereg-brand"
		signing = s.brands.Signing("rereg-brand")
	default:
		return nil, nil, fmt.Errorf("unknown model: %s", model)
	}
	a, err := signing.Sign(asserts.SerialType, headers, body, keyID)
	return a, s.ancillary, err
}

func (s *deviceMgrSerialSuite) mockServer(c *C, reqID string, bhv *devicestatetest.DeviceServiceBehavior) *httptest.Server {
	if bhv == nil {
		bhv = &devicestatetest.DeviceServiceBehavior{}
	}

	bhv.ReqID = reqID
	bhv.SignSerial = s.signSerial
	bhv.ExpectedCapabilities = "serial-stream"

	mockServer, extraCerts := devicestatetest.MockDeviceService(c, bhv)
	fname := filepath.Join(dirs.SnapdStoreSSLCertsDir, "test-server-certs.pem")
	err := os.MkdirAll(filepath.Dir(fname), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(fname, extraCerts, 0644)
	c.Assert(err, IsNil)
	return mockServer
}

func (s *deviceMgrSerialSuite) findBecomeOperationalChange(skipIDs ...string) *state.Change {
	for _, chg := range s.state.Changes() {
		if chg.Kind() == "become-operational" && !strutil.ListContains(skipIDs, chg.ID()) {
			return chg
		}
	}
	return nil
}

func (s *deviceMgrSerialSuite) TestFullDeviceRegistrationHappy(c *C) {
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

	// check that keypair manager is under device
	c.Check(osutil.IsDirectory(filepath.Join(dirs.SnapDeviceDir, "private-keys-v1")), Equals, true)

	// cannot unregister
	c.Check(s.mgr.Unregister(nil), ErrorMatches, `cannot currently unregister device if not classic or model brand is not generic or canonical`)
}

func (s *deviceMgrSerialSuite) TestFullDeviceRegistrationHappyWithProxy(c *C) {
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

func (s *deviceMgrSerialSuite) TestFullDeviceRegistrationHappyClassicNoGadget(c *C) {
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

func (s *deviceMgrSerialSuite) TestFullDeviceRegistrationHappyClassicFallback(c *C) {
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

func (s *deviceMgrSerialSuite) TestFullDeviceRegistrationMyBrandAcceptGenericHappy(c *C) {
	r1 := devicestate.MockKeyLength(testKeyLength)
	defer r1()

	mockServer := s.mockServer(c, "REQID-1", nil)
	defer mockServer.Close()

	r2 := devicestate.MockBaseStoreURL(mockServer.URL)
	defer r2()

	// setup state as will be done by first-boot
	s.state.Lock()
	defer s.state.Unlock()

	s.makeModelAssertionInState(c, "my-brand", "my-model-accept-generic", map[string]interface{}{
		"classic": "true",
		"store":   "alt-store",
		// accept generic as well to sign serials for this
		"serial-authority": []interface{}{"generic"},
	})

	devicestatetest.MockGadget(c, s.state, "gadget", snap.R(2), nil)

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "my-brand",
		Model: "my-model-accept-generic",
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
	c.Check(device.Model, Equals, "my-model-accept-generic")
	c.Check(device.Serial, Equals, "9999")

	a, err := s.db.Find(asserts.SerialType, map[string]string{
		"brand-id": "my-brand",
		"model":    "my-model-accept-generic",
		"serial":   "9999",
	})
	c.Assert(err, IsNil)
	serial := a.(*asserts.Serial)
	c.Check(serial.AuthorityID(), Equals, "generic")

	privKey, err := devicestate.KeypairManager(s.mgr).Get(serial.DeviceKey().ID())
	c.Assert(err, IsNil)
	c.Check(privKey, NotNil)

	c.Check(device.KeyID, Equals, privKey.PublicKey().ID())
}

func (s *deviceMgrSerialSuite) TestFullDeviceRegistrationMyBrandMismatchedAuthority(c *C) {
	r1 := devicestate.MockKeyLength(testKeyLength)
	defer r1()

	mockServer := s.mockServer(c, "REQID-1", nil)
	defer mockServer.Close()

	r2 := devicestate.MockBaseStoreURL(mockServer.URL)
	defer r2()

	// setup state as will be done by first-boot
	s.state.Lock()
	defer s.state.Unlock()

	s.makeModelAssertionInState(c, "my-brand", "my-model-accept-generic", map[string]interface{}{
		"classic": "true",
		"store":   "alt-store",
		// no serial-authority set
	})

	devicestatetest.MockGadget(c, s.state, "gadget", snap.R(2), nil)

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "my-brand",
		Model: "my-model-accept-generic",
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
	c.Check(becomeOperational.Err(), ErrorMatches, `(?s).*obtained serial assertion is signed by authority "generic" different from brand "my-brand" without model assertion with serial-authority set to to allow for them.*`)
}

func (s *deviceMgrSerialSuite) TestDoRequestSerialIdempotentAfterAddSerial(c *C) {
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

func (s *deviceMgrSerialSuite) TestDoRequestSerialIdempotentAfterGotSerial(c *C) {
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
	_, err := devicestatetest.Device(s.state)
	c.Check(err, IsNil)
	_, err = s.db.Find(asserts.SerialType, map[string]string{
		"brand-id": "canonical",
		"model":    "pc",
		"serial":   "9999",
	})
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	// Repeated handler run but set original serial.
	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Check(chg.Err(), IsNil)
	device, err := devicestatetest.Device(s.state)
	c.Check(err, IsNil)
	c.Check(device.Serial, Equals, "9999")
}

func (s *deviceMgrSerialSuite) TestDoRequestSerialErrorsOnNoHost(c *C) {
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

	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

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

func (s *deviceMgrSerialSuite) TestDoRequestSerialMaxTentatives(c *C) {
	privKey, _ := assertstest.GenerateKey(testKeyLength)

	// immediate
	r := devicestate.MockRetryInterval(0)
	defer r()

	r = devicestate.MockMaxTentatives(2)
	defer r()

	// this will trigger a reply 501 in the mock server
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

type simulateNoNetRoundTripper struct{}

func (s *simulateNoNetRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, &net.OpError{
		Op:   "dial",
		Net:  "tcp",
		Addr: &net.TCPAddr{IP: net.IPv4(10, 0, 0, 2), Port: 80},
		Err: &os.SyscallError{
			Syscall: "connect",
			Err:     syscall.ENETUNREACH,
		},
	}
}

func (s *deviceMgrSerialSuite) TestDoRequestSerialNoNetwork(c *C) {
	s.testDoRequestSerialKeepsRetrying(c, &simulateNoNetRoundTripper{})
}

type simulateNoDNSRoundTripper struct{}

func (s *simulateNoDNSRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: &net.DNSError{
			Err:         "Temporary failure in name resolution",
			Name:        "www.ubuntu.com",
			Server:      "",
			IsTemporary: true,
		},
	}
}

func (s *deviceMgrSerialSuite) TestDoRequestSerialNoReachableDNS(c *C) {
	s.testDoRequestSerialKeepsRetrying(c, &simulateNoDNSRoundTripper{})
}

func (s *deviceMgrSerialSuite) TestDoRequestSerialOffline(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	err := tr.Set("pc", "device-service.access", "offline")
	c.Assert(err, IsNil)
	tr.Commit()

	s.state.Unlock()

	chg, _ := s.makeRequestChangeWithTransport(c, http.DefaultTransport)

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	// task will appear done, since we don't want to pollute system with retries
	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Assert(chg.Err(), IsNil)

	device, err := devicestatetest.Device(s.state)
	c.Assert(err, IsNil)

	// but the serial will not be there
	c.Check(device.Serial, Equals, "")
}

func (s *deviceMgrSerialSuite) testDoRequestSerialKeepsRetrying(c *C, rt http.RoundTripper) {
	chg, t := s.makeRequestChangeWithTransport(c, rt)

	s.state.Lock()
	defer s.state.Unlock()

	// ensure we keep trying even if we are well above maxTentative
	for i := 0; i < 10; i++ {
		s.state.Unlock()
		s.se.Ensure()
		s.se.Wait()
		s.state.Lock()

		c.Check(chg.Status(), Equals, state.DoingStatus)
		c.Assert(chg.Err(), IsNil)
	}

	c.Check(chg.Status(), Equals, state.DoingStatus)

	var nTentatives int
	err := t.Get("pre-poll-tentatives", &nTentatives)
	c.Assert(err, IsNil)
	c.Check(nTentatives, Equals, 0)
}

func (s *deviceMgrSerialSuite) makeRequestChangeWithTransport(c *C, rt http.RoundTripper) (*state.Change, *state.Task) {
	privKey, _ := assertstest.GenerateKey(testKeyLength)

	// immediate
	r := devicestate.MockRetryInterval(0)
	s.AddCleanup(r)

	// set a low maxRetry value
	r = devicestate.MockMaxTentatives(3)
	s.AddCleanup(r)

	mockServer := s.mockServer(c, "REQID-1", nil)
	s.AddCleanup(mockServer.Close)

	restore := devicestate.MockBaseStoreURL(mockServer.URL)
	s.AddCleanup(restore)

	restore = devicestate.MockRepeatRequestSerial("after-add-serial")
	s.AddCleanup(restore)

	restore = devicestate.MockHttputilNewHTTPClient(func(opts *httputil.ClientOptions) *http.Client {
		c.Check(opts.ProxyConnectHeader, NotNil)
		return &http.Client{Transport: rt}
	})
	s.AddCleanup(restore)

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

	return chg, t
}

type simulateCertExpiredErrorRoundTripper struct{}

func (s *simulateCertExpiredErrorRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, x509.CertificateInvalidError{
		Reason: x509.Expired,
	}
}

func (s *deviceMgrSerialSuite) TestDoRequestSerialCertExpired(c *C) {
	chg, t := s.makeRequestChangeWithTransport(c, &simulateCertExpiredErrorRoundTripper{})

	s.state.Lock()
	defer s.state.Unlock()

	// keep trying well beyond the 21 retry attempts we do
	for i := 0; i < 100; i++ {
		s.state.Unlock()
		s.se.Ensure()
		s.se.Wait()
		s.state.Lock()

		if chg.Status() == state.ErrorStatus {
			break
		}
	}

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Assert(chg.Err(), ErrorMatches, `(?ms).*cannot retrieve request-id for making a request for a serial: Post \"?https://.*/request-id\"?: x509: certificate has expired or is not yet valid.*`)

	var nTentatives int
	err := t.Get("pre-poll-tentatives", &nTentatives)
	c.Assert(err, IsNil)
	// this is one above maxTentativesCertExpired (35)
	c.Check(nTentatives, Equals, 21)

}

func (s *deviceMgrSerialSuite) TestFullDeviceRegistrationPollHappy(c *C) {
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

func (s *deviceMgrSerialSuite) TestFullDeviceRegistrationHappyPrepareDeviceHook(c *C) {
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

func (s *deviceMgrSerialSuite) TestFullDeviceRegistrationHappyWithHookAndNewProxy(c *C) {
	s.testFullDeviceRegistrationHappyWithHookAndProxy(c, "new-enough")
}

func (s *deviceMgrSerialSuite) TestFullDeviceRegistrationHappyWithHookAndOldProxy(c *C) {
	s.testFullDeviceRegistrationHappyWithHookAndProxy(c, "old-proxy")
}

func (s *deviceMgrSerialSuite) TestFullDeviceRegistrationHappyWithHookAndBrokenProxy(c *C) {
	s.testFullDeviceRegistrationHappyWithHookAndProxy(c, "error-from-proxy")
}

func (s *deviceMgrSerialSuite) testFullDeviceRegistrationHappyWithHookAndProxy(c *C, proxyBehavior string) {
	r1 := devicestate.MockKeyLength(testKeyLength)
	defer r1()

	var reqID string
	var storeVersion string
	head := func(c *C, bhv *devicestatetest.DeviceServiceBehavior, w http.ResponseWriter, r *http.Request) {
		switch proxyBehavior {
		case "error-from-proxy":
			w.WriteHeader(500)
		default:
			w.Header().Set("Snap-Store-Version", storeVersion)
		}
	}
	bhv := &devicestatetest.DeviceServiceBehavior{
		Head: head,
	}
	svcPath := "/svc/"
	switch proxyBehavior {
	case "new-enough":
		reqID = "REQID-42"
		storeVersion = "6"
		bhv.PostPreflight = func(c *C, bhv *devicestatetest.DeviceServiceBehavior, w http.ResponseWriter, r *http.Request) {
			c.Check(r.Header.Get("X-Snap-Device-Service-URL"), Matches, "https://[^/]*/bad/svc/")
			c.Check(r.Header.Get("X-Extra-Header"), Equals, "extra")
		}
		svcPath = "/bad/svc/"
	case "old-proxy", "error-from-proxy":
		reqID = "REQID-41"
		storeVersion = "5"
		bhv.RequestIDURLPath = "/svc/request-id"
		bhv.SerialURLPath = "/svc/serial"
		bhv.PostPreflight = func(c *C, bhv *devicestatetest.DeviceServiceBehavior, w http.ResponseWriter, r *http.Request) {
			c.Check(r.Header.Get("X-Extra-Header"), Equals, "extra")
		}
	default:
		c.Fatalf("unknown proxy behavior %v", proxyBehavior)
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

func (s *deviceMgrSerialSuite) TestFullDeviceRegistrationErrorBackoff(c *C) {
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

	// validity
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

func (s *deviceMgrSerialSuite) TestEnsureBecomeOperationalShouldBackoff(c *C) {
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

func (s *deviceMgrSerialSuite) TestFullDeviceRegistrationMismatchedSerial(c *C) {
	r1 := devicestate.MockKeyLength(testKeyLength)
	defer r1()

	mockServer := s.mockServer(c, devicestatetest.ReqIDSerialWithBadModel, nil)
	defer mockServer.Close()

	r2 := devicestate.MockBaseStoreURL(mockServer.URL)
	defer r2()

	// setup state as will be done by first-boot
	s.state.Lock()
	defer s.state.Unlock()

	// validity
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

func (s *deviceMgrSerialSuite) TestModelAndSerial(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	// nothing in the state
	_, err := s.mgr.Model()
	c.Check(err, testutil.ErrorIs, state.ErrNoState)
	_, err = s.mgr.Serial()
	c.Check(err, testutil.ErrorIs, state.ErrNoState)

	// just brand and model
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	_, err = s.mgr.Model()
	c.Check(err, testutil.ErrorIs, state.ErrNoState)
	_, err = s.mgr.Serial()
	c.Check(err, testutil.ErrorIs, state.ErrNoState)

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
	c.Check(err, testutil.ErrorIs, state.ErrNoState)

	// have a serial as well
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc",
		Serial: "8989",
	})
	_, err = s.mgr.Model()
	c.Assert(err, IsNil)
	_, err = s.mgr.Serial()
	c.Check(err, testutil.ErrorIs, state.ErrNoState)

	// have a serial assertion
	s.makeSerialAssertionInState(c, "canonical", "pc", "8989")

	_, err = s.mgr.Model()
	c.Assert(err, IsNil)
	ser, err := s.mgr.Serial()
	c.Assert(err, IsNil)
	c.Check(ser.Serial(), Equals, "8989")
}

func (s *deviceMgrSerialSuite) TestStoreContextBackendSetDevice(c *C) {
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

func (s *deviceMgrSerialSuite) TestStoreContextBackendModelAndSerial(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	scb := s.mgr.StoreContextBackend()

	// nothing in the state
	_, err := scb.Model()
	c.Check(err, testutil.ErrorIs, state.ErrNoState)
	_, err = scb.Serial()
	c.Check(err, testutil.ErrorIs, state.ErrNoState)

	// just brand and model
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	_, err = scb.Model()
	c.Check(err, testutil.ErrorIs, state.ErrNoState)
	_, err = scb.Serial()
	c.Check(err, testutil.ErrorIs, state.ErrNoState)

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
	c.Check(err, testutil.ErrorIs, state.ErrNoState)

	// have a serial as well
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc",
		Serial: "8989",
	})
	_, err = scb.Model()
	c.Assert(err, IsNil)
	_, err = scb.Serial()
	c.Check(err, testutil.ErrorIs, state.ErrNoState)

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

func (s *deviceMgrSerialSuite) TestStoreContextBackendDeviceSessionRequestParams(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// set model as seeding would
	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

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

func (s *deviceMgrSerialSuite) TestStoreContextBackendProxyStore(c *C) {
	mockServer := s.mockServer(c, "", nil)
	defer mockServer.Close()
	s.state.Lock()
	defer s.state.Unlock()

	scb := s.mgr.StoreContextBackend()

	// nothing in the state
	_, err := scb.ProxyStore()
	c.Check(err, testutil.ErrorIs, state.ErrNoState)

	// have a store referenced
	tr := config.NewTransaction(s.state)
	err = tr.Set("core", "proxy.store", "foo")
	tr.Commit()
	c.Assert(err, IsNil)

	_, err = scb.ProxyStore()
	c.Check(err, testutil.ErrorIs, state.ErrNoState)

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

func (s *deviceMgrSerialSuite) TestStoreContextBackendStoreAccess(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	scb := s.mgr.StoreContextBackend()

	// nothing in the state
	_, err := scb.StoreOffline()
	c.Check(err, testutil.ErrorIs, state.ErrNoState)

	// set the store access to offline
	tr := config.NewTransaction(s.state)
	err = tr.Set("core", "store.access", "offline")
	tr.Commit()
	c.Assert(err, IsNil)

	offline, err := scb.StoreOffline()
	c.Check(err, IsNil)
	c.Check(offline, Equals, true)
}

func (s *deviceMgrSerialSuite) TestInitialRegistrationContext(c *C) {
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

	c.Check(regCtx.Model(), DeepEquals, model)

	c.Check(regCtx.GadgetForSerialRequestConfig(), Equals, "pc-gadget")
	c.Check(regCtx.SerialRequestExtraHeaders(), HasLen, 0)
	ancillary := regCtx.SerialRequestAncillaryAssertions()
	c.Check(ancillary, HasLen, 1)
	reqMod, ok := ancillary[0].(*asserts.Model)
	c.Assert(ok, Equals, true)
	c.Check(reqMod, DeepEquals, model)

}

func (s *deviceMgrSerialSuite) TestNewEnoughProxyParse(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	os.Setenv("SNAPD_DEBUG", "1")
	defer os.Unsetenv("SNAPD_DEBUG")

	badURL := &url.URL{Opaque: "%a"} // url.Parse(badURL.String()) needs to fail, which isn't easy :-)
	newEnoughProxy, err := devicestate.NewEnoughProxy(s.state, badURL, http.DefaultClient)
	c.Check(err, ErrorMatches, "cannot check whether proxy store supports a custom serial vault: parse .*")
	c.Check(newEnoughProxy, Equals, false)
}

func (s *deviceMgrSerialSuite) TestNewEnoughProxy(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	expectedUserAgent := snapdenv.UserAgent()
	log, restore := logger.MockLogger()
	defer restore()
	os.Setenv("SNAPD_DEBUG", "1")
	defer os.Unsetenv("SNAPD_DEBUG")

	expecteds := []string{
		`Head \"?http://\S+\"?: EOF`,
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
		newEnoughProxy, err := devicestate.NewEnoughProxy(s.state, u, http.DefaultClient)
		if expected != "" {
			expected = "cannot check whether proxy store supports a custom serial vault: " + expected
			c.Check(err, ErrorMatches, expected)
		}
		c.Check(newEnoughProxy, Equals, false)
	}
	c.Check(n, Equals, len(expecteds))

	// and success at last
	newEnoughProxy, err := devicestate.NewEnoughProxy(s.state, u, http.DefaultClient)
	c.Check(err, IsNil)
	c.Check(newEnoughProxy, Equals, true)
	c.Check(log.String(), Equals, "")
	c.Check(n, Equals, len(expecteds)+1)
}

func (s *deviceMgrSerialSuite) testDoRequestSerialReregistration(c *C, setAncillary func(origSerial *asserts.Serial)) *state.Task {
	mockServer := s.mockServer(c, "REQID-1", nil)
	defer mockServer.Close()

	restore := devicestate.MockBaseStoreURL(mockServer.URL)
	defer restore()

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

	// have a serial assertion
	serial0 := s.makeSerialAssertionInState(c, "my-brand", "my-model", "9999")
	// give a chance to the test to setup returning a stream vs
	// just the serial assertion
	if setAncillary != nil {
		setAncillary(serial0)
	}

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

	// validity
	regCtx, err := devicestate.RegistrationCtx(s.mgr, t)
	c.Assert(err, IsNil)
	c.Check(regCtx, Equals, remodCtx.(devicestate.RegistrationContext))

	// avoid full seeding
	s.seeding()

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	return t
}

func (s *deviceMgrSerialSuite) TestDoRequestSerialReregistration(c *C) {
	assertstest.AddMany(s.storeSigning, s.brands.AccountsAndKeys("rereg-brand")...)

	t := s.testDoRequestSerialReregistration(c, nil)

	s.state.Lock()
	defer s.state.Unlock()
	chg := t.Change()

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

func (s *deviceMgrSerialSuite) TestDoRequestSerialReregistrationStreamFromService(c *C) {
	setAncillary := func(_ *asserts.Serial) {
		// sets up such that re-registration returns a stream
		// of assertions
		s.ancillary = s.brands.AccountsAndKeys("rereg-brand")
	}

	t := s.testDoRequestSerialReregistration(c, setAncillary)

	s.state.Lock()
	defer s.state.Unlock()
	chg := t.Change()

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

func (s *deviceMgrSerialSuite) TestDoRequestSerialReregistrationIncompleteStreamFromService(c *C) {
	setAncillary := func(_ *asserts.Serial) {
		// will produce an incomplete stream!
		s.ancillary = s.brands.AccountsAndKeys("rereg-brand")[:1]
	}

	t := s.testDoRequestSerialReregistration(c, setAncillary)

	s.state.Lock()
	defer s.state.Unlock()
	chg := t.Change()

	c.Check(chg.Status(), Equals, state.ErrorStatus, Commentf("%s", t.Log()))
	c.Check(chg.Err(), ErrorMatches, `(?ms).*cannot accept stream of assertions from device service:.*`)
}

func (s *deviceMgrSerialSuite) TestDoRequestSerialReregistrationDoubleSerialStreamFromService(c *C) {
	setAncillary := func(serial0 *asserts.Serial) {
		// will produce a stream with confusingly two serial
		// assertions
		s.ancillary = s.brands.AccountsAndKeys("rereg-brand")
		s.ancillary = append(s.ancillary, serial0)
	}

	t := s.testDoRequestSerialReregistration(c, setAncillary)

	s.state.Lock()
	defer s.state.Unlock()
	chg := t.Change()

	c.Check(chg.Status(), Equals, state.ErrorStatus, Commentf("%s", t.Log()))
	c.Check(chg.Err(), ErrorMatches, `(?ms).*cannot accept more than a single device serial assertion from the device service.*`)
}

func (s *deviceMgrSerialSuite) TestDeviceRegistrationNotInInstallMode(c *C) {
	st := s.state
	// setup state as will be done by first-boot
	st.Lock()
	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	// mark it as seeded
	st.Set("seeded", true)
	// set run mode to "install"
	devicestate.SetSystemMode(s.mgr, "install")

	devicestate.SetInstalledRan(s.mgr, true)

	st.Unlock()

	// runs the whole device registration process
	// but it will not actually create any changes because device registration
	// does not happen in install mode by default
	s.settle(c)

	st.Lock()
	defer st.Unlock()
	becomeOperational := s.findBecomeOperationalChange()
	c.Assert(becomeOperational, IsNil)
}

func (s *deviceMgrSerialSuite) TestFullDeviceRegistrationUC20Happy(c *C) {
	defer sysdb.InjectTrusted([]asserts.Assertion{s.storeSigning.TrustedKey})()

	r1 := devicestate.MockKeyLength(testKeyLength)
	defer r1()

	mockServer := s.mockServer(c, "REQID-1", nil)
	defer mockServer.Close()

	r2 := devicestate.MockBaseStoreURL(mockServer.URL)
	defer r2()

	s.setUC20PCModelInState(c)

	// setup state as will be done by first-boot
	s.state.Lock()
	defer s.state.Unlock()

	s.makeModelAssertionInState(c, "canonical", "pc-20", map[string]interface{}{
		"architecture": "amd64",
		// UC20
		"base": "core20",
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
	})

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-20",
	})

	// save is available
	devicestate.SetSaveAvailable(s.mgr, true)

	// avoid full seeding
	s.seeding()

	becomeOperational := s.findBecomeOperationalChange()
	c.Check(becomeOperational, IsNil)

	devicestatetest.MockGadget(c, s.state, "pc", snap.R(2), nil)
	// mark it as seeded
	s.state.Set("seeded", true)
	// skip boot ok logic
	devicestate.SetBootOkRan(s.mgr, true)

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
	c.Check(device.Model, Equals, "pc-20")
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
		"model":    "pc-20",
		"serial":   "9999",
	})
	c.Assert(err, IsNil)
	serial := a.(*asserts.Serial)

	privKey, err := devicestate.KeypairManager(s.mgr).Get(serial.DeviceKey().ID())
	c.Assert(err, IsNil)
	c.Check(privKey, NotNil)

	c.Check(device.KeyID, Equals, privKey.PublicKey().ID())

	// check that keypair manager is under save
	c.Check(osutil.IsDirectory(filepath.Join(dirs.SnapDeviceSaveDir, "private-keys-v1")), Equals, true)
	c.Check(filepath.Join(dirs.SnapDeviceDir, "private-keys-v1"), testutil.FileAbsent)

	// check that the serial was saved to the device save assertion db
	// as well
	savedb, err := sysdb.OpenAt(dirs.SnapDeviceSaveDir)
	c.Assert(err, IsNil)
	// a copy of model was saved there
	_, err = savedb.Find(asserts.ModelType, map[string]string{
		"series":   "16",
		"brand-id": "canonical",
		"model":    "pc-20",
	})
	c.Assert(err, IsNil)
	// a copy of serial was backed up there
	_, err = savedb.Find(asserts.SerialType, map[string]string{
		"brand-id": "canonical",
		"model":    "pc-20",
		"serial":   "9999",
	})
	c.Assert(err, IsNil)
}

func (s *deviceMgrSerialSuite) TestFullDeviceUnregisterReregisterClassicGeneric(c *C) {
	s.testFullDeviceUnregisterReregisterClassicGeneric(c, nil)
}

func (s *deviceMgrSerialSuite) TestFullDeviceUnregisterBlockReregisterUntilRebootClassicGeneric(c *C) {
	s.testFullDeviceUnregisterReregisterClassicGeneric(c, &devicestate.UnregisterOptions{
		NoRegistrationUntilReboot: true,
	})
}

func (s *deviceMgrSerialSuite) testFullDeviceUnregisterReregisterClassicGeneric(c *C, opts *devicestate.UnregisterOptions) {
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
	// have an in-progress installation
	inst := s.state.NewChange("install", "...")
	task := s.state.NewTask("mount-snap", "...")
	inst.AddTask(task)

	// runs the whole device registration process
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	becomeOperational := s.findBecomeOperationalChange()
	c.Assert(becomeOperational, NotNil)

	c.Check(becomeOperational.Status().Ready(), Equals, true)
	c.Check(becomeOperational.Err(), IsNil)
	becomeOperational1 := becomeOperational.ID()

	device, err := devicestatetest.Device(s.state)
	c.Assert(err, IsNil)
	c.Check(device.Brand, Equals, "generic")
	c.Check(device.Model, Equals, "generic-classic")
	c.Check(device.Serial, Equals, "9999")

	// serial is there
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
	keyID1 := device.KeyID

	// mock having a store session
	device.SessionMacaroon = "session-macaroon"
	devicestatetest.SetDevice(s.state, device)

	err = s.mgr.Unregister(opts)
	c.Assert(err, IsNil)

	device, err = devicestatetest.Device(s.state)
	c.Assert(err, IsNil)
	c.Check(device.Brand, Equals, "generic")
	c.Check(device.Model, Equals, "generic-classic")
	// unregistered
	c.Check(device.Serial, Equals, "")
	// forgot key
	c.Check(device.KeyID, Equals, "")
	// and session
	c.Check(device.SessionMacaroon, Equals, "")
	// key was deleted
	_, err = devicestate.KeypairManager(s.mgr).Get(keyID1)
	c.Check(err, ErrorMatches, "cannot find key pair")

	noRegistrationUntilReboot := opts != nil && opts.NoRegistrationUntilReboot
	noregister := filepath.Join(dirs.SnapRunDir, "noregister")
	if noRegistrationUntilReboot {
		c.Check(noregister, testutil.FilePresent)
		c.Assert(os.Remove(noregister), IsNil)
	} else {
		c.Check(noregister, testutil.FileAbsent)
	}

	// runs the whole device registration process again
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	becomeOperational = s.findBecomeOperationalChange(becomeOperational1)
	c.Assert(becomeOperational, NotNil)

	c.Check(becomeOperational.Status().Ready(), Equals, true)
	c.Check(becomeOperational.Err(), IsNil)

	device, err = devicestatetest.Device(s.state)
	c.Assert(err, IsNil)
	c.Check(device.Brand, Equals, "generic")
	c.Check(device.Model, Equals, "generic-classic")
	c.Check(device.Serial, Equals, "10000")

	// serial is there
	a, err = s.db.Find(asserts.SerialType, map[string]string{
		"brand-id": "generic",
		"model":    "generic-classic",
		"serial":   "10000",
	})
	c.Assert(err, IsNil)
	serial = a.(*asserts.Serial)

	privKey, err = devicestate.KeypairManager(s.mgr).Get(serial.DeviceKey().ID())
	c.Assert(err, IsNil)
	c.Check(privKey, NotNil)
	c.Check(device.KeyID, Equals, privKey.PublicKey().ID())
	// different from previous key
	c.Check(device.KeyID, Not(Equals), keyID1)
}

func (s *deviceMgrSerialSuite) TestFullDeviceRegistrationBlockedByNoRegister(c *C) {
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
	// have a in-progress installation
	inst := s.state.NewChange("install", "...")
	task := s.state.NewTask("mount-snap", "...")
	inst.AddTask(task)

	// create /run/snapd/noregister
	c.Assert(os.MkdirAll(dirs.SnapRunDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapRunDir, "noregister"), nil, 0644), IsNil)

	// attempt to run the whole device registration process
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	// noregister blocked it
	becomeOperational := s.findBecomeOperationalChange()
	c.Assert(becomeOperational, IsNil)

	s.state.Unlock()
	s.se.Ensure()
	s.state.Lock()

	// same, noregister blocked it
	becomeOperational = s.findBecomeOperationalChange()
	c.Assert(becomeOperational, IsNil)
}

func (s *deviceMgrSerialSuite) TestDeviceSerialRestoreHappy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	log, restore := logger.MockLogger()
	defer restore()

	// setup state as will be done by first-boot
	s.state.Lock()
	defer s.state.Unlock()

	// in this case is just marked seeded without snaps
	s.state.Set("seeded", true)

	// save is available (where device keys are kept)
	devicestate.SetSaveAvailable(s.mgr, true)

	// this is the regular assertions DB
	c.Assert(os.MkdirAll(dirs.SnapAssertsDBDir, 0755), IsNil)
	// this is the ubuntu-save is bind mounted under /var/lib/snapd/save,
	// there is a device directory under it
	c.Assert(os.MkdirAll(dirs.SnapDeviceSaveDir, 0755), IsNil)

	bs, err := asserts.OpenFSBackstore(dirs.SnapAssertsDBDir)
	c.Assert(err, IsNil)

	// the test suite uses a memory backstore DB, but we need to look at the
	// filesystem
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore:       bs,
		Trusted:         s.storeSigning.Trusted,
		OtherPredefined: s.storeSigning.Generic,
	})
	c.Assert(err, IsNil)
	// cleanup is done by the suite
	assertstate.ReplaceDB(s.state, db)
	err = db.Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	model := s.makeModelAssertionInState(c, "my-brand", "pc-20", map[string]interface{}{
		"architecture": "amd64",
		// UC20
		"base": "core20",
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
	})

	devicestatetest.MockGadget(c, s.state, "pc", snap.R(2), nil)

	// the mock has written key under snap asserts dir, but when ubuntu-save
	// exists, the key is written under ubuntu-save/device, thus
	// factory-reset never restores is to the asserts dir
	kp, err := asserts.OpenFSKeypairManager(dirs.SnapAssertsDBDir)
	c.Assert(err, IsNil)

	otherKey, _ := assertstest.GenerateKey(testKeyLength)
	// an assertion for which there is no corresponding device key
	makeDeviceSerialAssertionInDir(c, dirs.SnapAssertsDBDir, s.storeSigning, s.brands,
		model, otherKey, "serial-other-key")
	c.Assert(kp.Delete(otherKey.PublicKey().ID()), IsNil)
	// an assertion which has a device key, which needs to be moved to the
	// right location
	makeDeviceSerialAssertionInDir(c, dirs.SnapAssertsDBDir, s.storeSigning, s.brands,
		model, devKey, "serial-1234")
	c.Assert(kp.Delete(devKey.PublicKey().ID()), IsNil)
	// write the key under a location which corresponds to ubuntu-save/device
	kp, err = asserts.OpenFSKeypairManager(dirs.SnapDeviceSaveDir)
	c.Assert(err, IsNil)
	c.Assert(kp.Put(devKey), IsNil)

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "my-brand",
		Model: "pc-20",
	})

	s.state.Unlock()
	s.se.Ensure()
	s.state.Lock()

	// no need for the operational change
	becomeOperational := s.findBecomeOperationalChange()
	c.Check(becomeOperational, IsNil)

	device, err := devicestatetest.Device(s.state)
	c.Assert(err, IsNil)
	c.Check(device.Brand, Equals, "my-brand")
	c.Check(device.Model, Equals, "pc-20")
	// serial was restored
	c.Check(device.Serial, Equals, "serial-1234")
	// key ID was restored
	c.Check(device.KeyID, Equals, devKey.PublicKey().ID())
	// key is present
	_, err = devicestate.KeypairManager(s.mgr).Get(devKey.PublicKey().ID())
	c.Check(err, IsNil)
	// no session yet
	c.Check(device.SessionMacaroon, Equals, "")
	// and something was logged
	c.Check(log.String(), testutil.Contains,
		fmt.Sprintf("restored serial serial-1234 for my-brand/pc-20 signed with key %v", devKey.PublicKey().ID()))
}

func (s *deviceMgrSerialSuite) TestShouldRequestSerial(c *C) {
	type testCase struct {
		deviceServiceAccess string
		deviceServiceURL    string
		storeAccess         string
		gadgetName          string
		expected            bool
	}

	testCases := []testCase{
		{
			storeAccess:         "",
			gadgetName:          "",
			deviceServiceAccess: "",
			deviceServiceURL:    "",
			expected:            true,
		},
		{
			storeAccess:         "offline",
			gadgetName:          "",
			deviceServiceAccess: "",
			deviceServiceURL:    "",
			expected:            false,
		},
		{
			storeAccess:         "",
			gadgetName:          "gadget",
			deviceServiceAccess: "offline",
			deviceServiceURL:    "",
			expected:            false,
		},
		{
			storeAccess:         "",
			gadgetName:          "gadget",
			deviceServiceAccess: "",
			deviceServiceURL:    "",
			expected:            true,
		},
		{
			storeAccess:         "offline",
			gadgetName:          "gadget",
			deviceServiceAccess: "",
			deviceServiceURL:    "https://example.com",
			expected:            true,
		},
		{
			storeAccess:         "offline",
			gadgetName:          "gadget",
			deviceServiceAccess: "",
			deviceServiceURL:    "",
			expected:            false,
		},
		{
			storeAccess:         "offline",
			gadgetName:          "gadget",
			deviceServiceAccess: "",
			deviceServiceURL:    "https://example.com",
			expected:            true,
		},
	}

	s.state.Lock()
	defer s.state.Unlock()

	for i, t := range testCases {
		tr := config.NewTransaction(s.state)
		err := tr.Set("core", "store.access", t.storeAccess)
		c.Assert(err, IsNil)

		if t.gadgetName != "" {
			err = tr.Set(t.gadgetName, "device-service.access", t.deviceServiceAccess)
			c.Assert(err, IsNil)
			err = tr.Set(t.gadgetName, "device-service.url", t.deviceServiceURL)
			c.Assert(err, IsNil)
		}
		tr.Commit()

		shouldRequest, err := devicestate.ShouldRequestSerial(s.state, t.gadgetName)
		c.Check(err, IsNil)
		c.Check(shouldRequest, Equals, t.expected, Commentf("testcase %d: %+v", i, t))
	}
}

func (s *deviceMgrSerialSuite) TestDeviceManagerFullAccess(c *C) {
	s.state.Lock()
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	devicestatetest.MockGadget(c, s.state, "pc", snap.R(2), nil)
	s.state.Set("seeded", true)
	s.state.Unlock()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	s.state.Lock()
	change := s.findBecomeOperationalChange()
	tasks := change.Tasks()
	s.state.Unlock()

	sort.Slice(tasks, func(l, r int) bool {
		return tasks[l].Kind() < tasks[r].Kind()
	})

	// since device-service.access is unset, then both tasks should be queued
	c.Assert(tasks, HasLen, 2)
	c.Check(tasks[0].Kind(), Equals, "generate-device-key")
	c.Check(tasks[1].Kind(), Equals, "request-serial")
}

func (s *deviceMgrSerialSuite) TestDeviceManagerNoAccessHasKeyID(c *C) {
	s.state.Lock()
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
		KeyID: "key-id",
	})

	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	devicestatetest.MockGadget(c, s.state, "pc", snap.R(2), nil)
	s.state.Set("seeded", true)

	tr := config.NewTransaction(s.state)
	err := tr.Set("pc", "device-service.access", "offline")
	c.Assert(err, IsNil)
	tr.Commit()

	s.state.Unlock()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	s.state.Lock()
	change := s.findBecomeOperationalChange()
	s.state.Unlock()

	// since device-service.access=offline and the device key ID not set, then
	// no tasks should have been queued
	c.Assert(change, IsNil)
}

func (s *deviceMgrSerialSuite) TestDeviceManagerNoAccessNoKeyID(c *C) {
	s.state.Lock()
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	devicestatetest.MockGadget(c, s.state, "pc", snap.R(2), nil)
	s.state.Set("seeded", true)

	tr := config.NewTransaction(s.state)
	err := tr.Set("pc", "device-service.access", "offline")
	c.Assert(err, IsNil)
	tr.Commit()

	s.state.Unlock()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	s.state.Lock()
	change := s.findBecomeOperationalChange()
	tasks := change.Tasks()
	s.state.Unlock()

	// since device-service.access=offline and the device key ID is not set,
	// then the "generate-device-key" task should be queued
	c.Assert(tasks, HasLen, 1)
	c.Check(tasks[0].Kind(), Equals, "generate-device-key")
}
