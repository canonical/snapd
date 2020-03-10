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
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"syscall"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
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
	"github.com/snapcore/snapd/strutil"
)

var testKeyLength = 1024

type deviceMgrSerialSuite struct {
	deviceMgrBaseSuite
}

var _ = Suite(&deviceMgrSerialSuite{})

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

func (s *deviceMgrSerialSuite) TestFullDeviceRegistrationAltBrandHappy(c *C) {
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

func (s *deviceMgrSerialSuite) testDoRequestSerialKeepsRetrying(c *C, rt http.RoundTripper) {
	privKey, _ := assertstest.GenerateKey(testKeyLength)

	// immediate
	r := devicestate.MockRetryInterval(0)
	defer r()

	// set a low maxRetry value
	r = devicestate.MockMaxTentatives(3)
	defer r()

	mockServer := s.mockServer(c, "REQID-1", nil)
	defer mockServer.Close()

	restore := devicestate.MockBaseStoreURL(mockServer.URL)
	defer restore()

	restore = devicestate.MockRepeatRequestSerial("after-add-serial")
	defer restore()

	restore = devicestate.MockHttputilNewHTTPClient(func(opts *httputil.ClientOptions) *http.Client {
		return &http.Client{Transport: rt}
	})
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

	// ensure we keep trying even if we are well above maxTentative
	for i := 0; i < 10; i++ {
		s.state.Unlock()
		s.se.Ensure()
		s.se.Wait()
		s.state.Lock()

		c.Check(chg.Status(), Equals, state.DoingStatus)
		c.Check(chg.Err(), IsNil)
	}

	c.Check(chg.Status(), Equals, state.DoingStatus)

	var nTentatives int
	err := t.Get("pre-poll-tentatives", &nTentatives)
	c.Assert(err, IsNil)
	c.Check(nTentatives, Equals, 0)
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
	s.testFullDeviceRegistrationHappyWithHookAndProxy(c, true)
}

func (s *deviceMgrSerialSuite) TestFullDeviceRegistrationHappyWithHookAndOldProxy(c *C) {
	s.testFullDeviceRegistrationHappyWithHookAndProxy(c, false)
}

func (s *deviceMgrSerialSuite) testFullDeviceRegistrationHappyWithHookAndProxy(c *C, newEnough bool) {
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

func (s *deviceMgrSerialSuite) TestModelAndSerial(c *C) {
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

func (s *deviceMgrSerialSuite) TestStoreContextBackendDeviceSessionRequestParams(c *C) {
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

func (s *deviceMgrSerialSuite) TestStoreContextBackendProxyStore(c *C) {
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

	c.Check(regCtx.GadgetForSerialRequestConfig(), Equals, "pc-gadget")
	c.Check(regCtx.SerialRequestExtraHeaders(), HasLen, 0)
	c.Check(regCtx.SerialRequestAncillaryAssertions(), HasLen, 0)

}

func (s *deviceMgrSerialSuite) TestNewEnoughProxyParse(c *C) {
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

func (s *deviceMgrSerialSuite) TestNewEnoughProxy(c *C) {
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
	devicestate.SetOperatingMode(s.mgr, "install")
	st.Unlock()

	// runs the whole device registration process
	// but it will not actually create any changes because
	s.settle(c)

	st.Lock()
	defer st.Unlock()
	becomeOperational := s.findBecomeOperationalChange()
	c.Assert(becomeOperational, IsNil)
}
