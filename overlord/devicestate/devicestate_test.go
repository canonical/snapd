// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/strutil"
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
	brandSigning *assertstest.SigningDB

	reqID string

	restoreOnClassic         func()
	restoreGenericClassicMod func()
	restoreSanitize          func()
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

func (s *deviceMgrSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	os.MkdirAll(dirs.SnapRunDir, 0755)

	s.restoreSanitize = snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})

	s.bootloader = boottest.NewMockBootloader("mock", c.MkDir())
	partition.ForceBootloader(s.bootloader)

	s.restoreOnClassic = release.MockOnClassic(false)

	s.storeSigning = assertstest.NewStoreStack("canonical", nil)
	s.o = overlord.Mock()
	s.state = s.o.State()
	s.se = s.o.StateEngine()

	s.restoreGenericClassicMod = sysdb.MockGenericClassicModel(s.storeSigning.GenericClassicModel)

	brandPrivKey, _ := assertstest.GenerateKey(752)
	s.brandSigning = assertstest.NewSigningDB("my-brand", brandPrivKey)

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
	mgr, err := devicestate.Manager(s.state, hookMgr, s.o.TaskRunner())
	c.Assert(err, IsNil)

	s.db = db
	s.hookMgr = hookMgr
	s.o.AddManager(s.hookMgr)
	s.mgr = mgr
	s.o.AddManager(s.mgr)
	s.o.AddManager(s.o.TaskRunner())

	s.state.Lock()
	snapstate.ReplaceStore(s.state, &fakeStore{
		state: s.state,
		db:    s.storeSigning,
	})
	s.state.Unlock()
}

func (s *deviceMgrSuite) TearDownTest(c *C) {
	s.state.Lock()
	assertstate.ReplaceDB(s.state, nil)
	s.state.Unlock()
	partition.ForceBootloader(nil)
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

const (
	requestIDURLPath = "/api/v1/snaps/auth/request-id"
	serialURLPath    = "/api/v1/snaps/auth/devices"
)

// seeding avoids triggering a real full seeding, it simulates having it in process instead
func (s *deviceMgrSuite) seeding() {
	chg := s.state.NewChange("seed", "Seed system")
	chg.SetStatus(state.DoingStatus)
}

func (s *deviceMgrSuite) mockServer(c *C) *httptest.Server {
	expectedUserAgent := httputil.UserAgent()

	var mu sync.Mutex
	count := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		default:
			c.Fatalf("unexpected verb %q", r.Method)
		case "HEAD":
			if r.URL.Path != "/" {
				c.Fatalf("unexpected HEAD request %q", r.URL.String())
			}
			switch s.reqID {
			case "REQID-42":
				w.Header().Set("Snap-Store-Version", "6")
			case "REQID-41":
				w.Header().Set("Snap-Store-Version", "5")
			default:
				c.Fatalf("unexpected HEAD request w/reqID %q", s.reqID)
			}
			w.WriteHeader(200)
			return
		case "POST":
			// carry on
		}

		if s.reqID == "REQID-42" {
			c.Check(r.Header.Get("X-Snap-Device-Service-URL"), Matches, "http://[^/]*/bad/svc/")
		}

		switch r.URL.Path {
		default:
			c.Fatalf("unexpected POST request %q", r.URL.String())
		case requestIDURLPath, "/svc/request-id":
			if s.reqID == "REQID-501" {
				w.WriteHeader(501)
				return
			}
			w.WriteHeader(200)
			c.Check(r.Header.Get("User-Agent"), Equals, expectedUserAgent)
			io.WriteString(w, fmt.Sprintf(`{"request-id": "%s"}`, s.reqID))

		case "/svc/serial":
			c.Check(r.Header.Get("X-Extra-Header"), Equals, "extra")
			fallthrough
		case serialURLPath:
			if s.reqID == "REQID-42" {
				c.Check(r.Header.Get("X-Extra-Header"), Equals, "extra")
			}
			c.Check(r.Header.Get("User-Agent"), Equals, expectedUserAgent)

			mu.Lock()
			serialNum := 9999 + count
			count++
			mu.Unlock()

			b, err := ioutil.ReadAll(r.Body)
			c.Assert(err, IsNil)
			a, err := asserts.Decode(b)
			c.Assert(err, IsNil)
			serialReq, ok := a.(*asserts.SerialRequest)
			c.Assert(ok, Equals, true)
			err = asserts.SignatureCheck(serialReq, serialReq.DeviceKey())
			c.Assert(err, IsNil)
			brandID := serialReq.BrandID()
			model := serialReq.Model()
			authID := "canonical"
			keyID := ""
			switch model {
			case "pc", "pc2":
			case "classic-alt-store":
				c.Check(brandID, Equals, "canonical")
			case "generic-classic":
				c.Check(brandID, Equals, "generic")
				authID = "generic"
				keyID = s.storeSigning.GenericKey.PublicKeyID()
			/*case "my-model":
			c.Check(brandID, Equals, "my-brand")
			authID = "generic"
			keyID = s.storeSigning.GenericKey.PublicKeyID()
			*/
			default:
				c.Fatal("unknown model")
			}
			reqID := serialReq.RequestID()
			if reqID == "REQID-BADREQ" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(400)
				w.Write([]byte(`{
  "error_list": [{"message": "bad serial-request"}]
}`))
				return
			}
			if reqID == "REQID-POLL" && serialNum != 10002 {
				w.WriteHeader(202)
				return
			}
			serialStr := fmt.Sprintf("%d", serialNum)
			if serialReq.Serial() != "" {
				// use proposed serial
				serialStr = serialReq.Serial()
			}
			serial, err := s.storeSigning.Sign(asserts.SerialType, map[string]interface{}{
				"authority-id":        authID,
				"brand-id":            brandID,
				"model":               model,
				"serial":              serialStr,
				"device-key":          serialReq.HeaderString("device-key"),
				"device-key-sha3-384": serialReq.SignKeyID(),
				"timestamp":           time.Now().Format(time.RFC3339),
			}, serialReq.Body(), keyID)
			c.Assert(err, IsNil)
			w.Header().Set("Content-Type", asserts.MediaType)
			w.WriteHeader(200)
			encoded := asserts.Encode(serial)
			switch reqID {
			case "REQID-SERIAL-W-BAD-MODEL":
				encoded = bytes.Replace(encoded, []byte("model: pc"), []byte("model: foo"), 1)
			}
			w.Write(encoded)
		}
	}))
}

func (s *deviceMgrSuite) setupGadget(c *C, snapYaml string, snapContents string) {
	sideInfoGadget := &snap.SideInfo{
		RealName: "gadget",
		Revision: snap.R(2),
	}
	snaptest.MockSnap(c, snapYaml, sideInfoGadget)
	snapstate.Set(s.state, "gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfoGadget},
		Current:  sideInfoGadget.Revision,
	})
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

	s.reqID = "REQID-1"
	mockServer := s.mockServer(c)
	defer mockServer.Close()

	r2 := devicestate.MockBaseStoreURL(mockServer.URL)
	defer r2()

	// setup state as will be done by first-boot
	s.state.Lock()
	defer s.state.Unlock()

	s.makeModelAssertionInState(c, "canonical", "pc", map[string]string{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	// avoid full seeding
	s.seeding()

	// not started without gadget
	s.state.Unlock()
	s.se.Ensure()
	s.state.Lock()

	becomeOperational := s.findBecomeOperationalChange()
	c.Check(becomeOperational, IsNil)

	s.setupGadget(c, `
name: pc
type: gadget
version: gadget
`, "")

	// runs the whole device registration process
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	becomeOperational = s.findBecomeOperationalChange()
	c.Assert(becomeOperational, NotNil)

	c.Check(becomeOperational.Status().Ready(), Equals, true)
	c.Check(becomeOperational.Err(), IsNil)

	device, err := auth.Device(s.state)
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

	s.reqID = "REQID-1"
	mockServer := s.mockServer(c)
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
	c.Assert(assertstate.Add(s.state, operatorAcct), IsNil)

	// have a store assertion.
	stoAs, err := s.storeSigning.Sign(asserts.StoreType, map[string]interface{}{
		"store":       "foo",
		"url":         mockServer.URL,
		"operator-id": operatorAcct.AccountID(),
		"timestamp":   time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(s.state, stoAs), IsNil)

	s.makeModelAssertionInState(c, "canonical", "pc", map[string]string{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	// avoid full seeding
	s.seeding()

	// not started without gadget
	s.state.Unlock()
	s.se.Ensure()
	s.state.Lock()

	becomeOperational := s.findBecomeOperationalChange()
	c.Check(becomeOperational, IsNil)

	s.setupGadget(c, `
name: pc
type: gadget
version: gadget
`, "")

	// runs the whole device registration process
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	becomeOperational = s.findBecomeOperationalChange()
	c.Assert(becomeOperational, NotNil)

	c.Check(becomeOperational.Status().Ready(), Equals, true)
	c.Check(becomeOperational.Err(), IsNil)

	device, err := auth.Device(s.state)
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

	s.reqID = "REQID-1"
	mockServer := s.mockServer(c)
	defer mockServer.Close()

	r2 := devicestate.MockBaseStoreURL(mockServer.URL)
	defer r2()

	// setup state as will be done by first-boot
	s.state.Lock()
	defer s.state.Unlock()

	s.makeModelAssertionInState(c, "canonical", "classic-alt-store", map[string]string{
		"classic": "true",
		"store":   "alt-store",
	})

	auth.SetDevice(s.state, &auth.DeviceState{
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

	device, err := auth.Device(s.state)
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

	s.reqID = "REQID-1"
	mockServer := s.mockServer(c)
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

	device, err := auth.Device(s.state)
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

	s.reqID = "REQID-1"
	mockServer := s.mockServer(c)
	defer mockServer.Close()

	r2 := devicestate.MockBaseStoreURL(mockServer.URL)
	defer r2()

	// setup state as will be done by first-boot
	s.state.Lock()
	defer s.state.Unlock()

	s.makeModelAssertionInState(c, "my-brand", "my-model", map[string]string{
		"classic": "true",
		"store":   "alt-store",
	})

	s.setupGadget(c, `
name: gadget
type: gadget
version: gadget
`, "")

	auth.SetDevice(s.state, &auth.DeviceState{
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

	device, err := auth.Device(s.state)
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

	s.reqID = "REQID-1"
	mockServer := s.mockServer(c)
	defer mockServer.Close()

	restore := devicestate.MockBaseStoreURL(mockServer.URL)
	defer restore()

	restore = devicestate.MockRepeatRequestSerial("after-add-serial")
	defer restore()

	// setup state as done by first-boot/Ensure/doGenerateDeviceKey
	s.state.Lock()
	defer s.state.Unlock()

	s.setupGadget(c, `
name: gadget
type: gadget
version: gadget
`, "")

	auth.SetDevice(s.state, &auth.DeviceState{
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
	device, err := auth.Device(s.state)
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
	device, err = auth.Device(s.state)
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

	s.reqID = "REQID-1"
	mockServer := s.mockServer(c)
	defer mockServer.Close()

	restore := devicestate.MockBaseStoreURL(mockServer.URL)
	defer restore()

	restore = devicestate.MockRepeatRequestSerial("after-got-serial")
	defer restore()

	// setup state as done by first-boot/Ensure/doGenerateDeviceKey
	s.state.Lock()
	defer s.state.Unlock()

	s.setupGadget(c, `
name: gadget
type: gadget
version: gadget
`, "")

	auth.SetDevice(s.state, &auth.DeviceState{
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
	device, err := auth.Device(s.state)
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
	device, err = auth.Device(s.state)
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

	s.setupGadget(c, `
name: gadget
type: gadget
version: gadget
`, "")

	auth.SetDevice(s.state, &auth.DeviceState{
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

	s.reqID = "REQID-501"
	mockServer := s.mockServer(c)
	defer mockServer.Close()

	restore := devicestate.MockBaseStoreURL(mockServer.URL)
	defer restore()

	restore = devicestate.MockRepeatRequestSerial("after-add-serial")
	defer restore()

	// setup state as done by first-boot/Ensure/doGenerateDeviceKey
	s.state.Lock()
	defer s.state.Unlock()

	s.setupGadget(c, `
name: gadget
type: gadget
version: gadget
`, "")

	auth.SetDevice(s.state, &auth.DeviceState{
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

	s.reqID = "REQID-POLL"
	mockServer := s.mockServer(c)
	defer mockServer.Close()

	r2 := devicestate.MockBaseStoreURL(mockServer.URL)
	defer r2()

	// immediately
	r3 := devicestate.MockRetryInterval(0)
	defer r3()

	// setup state as will be done by first-boot
	s.state.Lock()
	defer s.state.Unlock()

	s.makeModelAssertionInState(c, "canonical", "pc", map[string]string{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	s.setupGadget(c, `
name: pc
type: gadget
version: gadget
`, "")

	// avoid full seeding
	s.seeding()

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

	device, err := auth.Device(s.state)
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

	s.reqID = "REQID-1"
	mockServer := s.mockServer(c)
	defer mockServer.Close()

	r2 := hookstate.MockRunHook(func(ctx *hookstate.Context, _ *tomb.Tomb) ([]byte, error) {
		c.Assert(ctx.HookName(), Equals, "prepare-device")

		// snapctl set the registration params
		_, _, err := ctlcmd.Run(ctx, []string{"set", fmt.Sprintf("device-service.url=%q", mockServer.URL+"/svc/")}, 0)
		c.Assert(err, IsNil)

		h, err := json.Marshal(map[string]string{
			"x-extra-header": "extra",
		})
		c.Assert(err, IsNil)
		_, _, err = ctlcmd.Run(ctx, []string{"set", fmt.Sprintf("device-service.headers=%s", string(h))}, 0)
		c.Assert(err, IsNil)

		_, _, err = ctlcmd.Run(ctx, []string{"set", fmt.Sprintf("registration.proposed-serial=%q", "Y9999")}, 0)
		c.Assert(err, IsNil)

		d, err := yaml.Marshal(map[string]string{
			"mac": "00:00:00:00:ff:00",
		})
		c.Assert(err, IsNil)
		_, _, err = ctlcmd.Run(ctx, []string{"set", fmt.Sprintf("registration.body=%q", d)}, 0)
		c.Assert(err, IsNil)

		return nil, nil
	})
	defer r2()

	// as device-service.url is set, should not need to do this but just in case
	r3 := devicestate.MockBaseStoreURL(mockServer.URL + "/direct/baad/")
	defer r3()

	// setup state as will be done by first-boot
	// & have a gadget with a prepare-device hook
	s.state.Lock()
	defer s.state.Unlock()

	s.makeModelAssertionInState(c, "canonical", "pc2", map[string]string{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "gadget",
	})

	s.setupGadget(c, `
name: gadget
type: gadget
version: gadget
hooks:
    prepare-device:
`, "")

	auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc2",
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

	device, err := auth.Device(s.state)
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

	if newEnough {
		s.reqID = "REQID-42"
	} else {
		s.reqID = "REQID-41"
	}
	mockServer := s.mockServer(c)
	defer mockServer.Close()

	r2 := hookstate.MockRunHook(func(ctx *hookstate.Context, _ *tomb.Tomb) ([]byte, error) {
		c.Assert(ctx.HookName(), Equals, "prepare-device")

		deviceURL := mockServer.URL + "/bad/svc/"
		if !newEnough {
			deviceURL = mockServer.URL + "/svc/"
		}

		// snapctl set the registration params
		_, _, err := ctlcmd.Run(ctx, []string{"set", fmt.Sprintf("device-service.url=%q", deviceURL)}, 0)
		c.Assert(err, IsNil)

		h, err := json.Marshal(map[string]string{
			"x-extra-header": "extra",
		})
		c.Assert(err, IsNil)
		_, _, err = ctlcmd.Run(ctx, []string{"set", fmt.Sprintf("device-service.headers=%s", string(h))}, 0)
		c.Assert(err, IsNil)

		return nil, nil
	})
	defer r2()

	// as device-service.url is set, should not need to do this but just in case
	r3 := devicestate.MockBaseStoreURL(mockServer.URL + "/direct/baad/")
	defer r3()

	// setup state as will be done by first-boot
	// & have a gadget with a prepare-device hook
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "proxy.store", "foo"), IsNil)
	tr.Commit()
	operatorAcct := assertstest.NewAccount(s.storeSigning, "foo-operator", nil, "")
	c.Assert(assertstate.Add(s.state, operatorAcct), IsNil)

	// have a store assertion.
	stoAs, err := s.storeSigning.Sign(asserts.StoreType, map[string]interface{}{
		"store":       "foo",
		"url":         mockServer.URL,
		"operator-id": operatorAcct.AccountID(),
		"timestamp":   time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(s.state, stoAs), IsNil)

	s.makeModelAssertionInState(c, "canonical", "pc2", map[string]string{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "gadget",
	})

	s.setupGadget(c, `
name: gadget
type: gadget
version: gadget
hooks:
    prepare-device:
`, "")

	auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc2",
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

	device, err := auth.Device(s.state)
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

	s.reqID = "REQID-BADREQ"
	mockServer := s.mockServer(c)
	defer mockServer.Close()

	r2 := devicestate.MockBaseStoreURL(mockServer.URL)
	defer r2()

	// setup state as will be done by first-boot
	s.state.Lock()
	defer s.state.Unlock()

	// sanity
	c.Check(devicestate.EnsureOperationalAttempts(s.state), Equals, 0)

	s.makeModelAssertionInState(c, "canonical", "pc", map[string]string{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	s.setupGadget(c, `
name: pc
type: gadget
version: gadget
`, "")

	// avoid full seeding
	s.seeding()

	// try the whole device registration process
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	becomeOperational := s.findBecomeOperationalChange()
	c.Assert(becomeOperational, NotNil)
	firstTryID := becomeOperational.ID()

	c.Check(becomeOperational.Status().Ready(), Equals, true)
	c.Check(becomeOperational.Err(), ErrorMatches, `(?s).*cannot deliver device serial request: bad serial-request.*`)

	device, err := auth.Device(s.state)
	c.Assert(err, IsNil)
	c.Check(device.KeyID, Not(Equals), "")
	keyID := device.KeyID

	c.Check(devicestate.EnsureOperationalShouldBackoff(s.mgr, time.Now()), Equals, true)
	c.Check(devicestate.EnsureOperationalShouldBackoff(s.mgr, time.Now().Add(6*time.Minute)), Equals, false)
	c.Check(devicestate.EnsureOperationalAttempts(s.state), Equals, 1)

	// try again the whole device registration process
	s.reqID = "REQID-1"
	devicestate.SetLastBecomeOperationalAttempt(s.mgr, time.Now().Add(-15*time.Minute))
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	becomeOperational = s.findBecomeOperationalChange(firstTryID)
	c.Assert(becomeOperational, NotNil)

	c.Check(becomeOperational.Status().Ready(), Equals, true)
	c.Check(becomeOperational.Err(), IsNil)

	c.Check(devicestate.EnsureOperationalAttempts(s.state), Equals, 2)

	device, err = auth.Device(s.state)
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

	s.reqID = "REQID-SERIAL-W-BAD-MODEL"
	mockServer := s.mockServer(c)
	defer mockServer.Close()

	r2 := devicestate.MockBaseStoreURL(mockServer.URL)
	defer r2()

	// setup state as will be done by first-boot
	s.state.Lock()
	defer s.state.Unlock()

	// sanity
	c.Check(devicestate.EnsureOperationalAttempts(s.state), Equals, 0)

	s.setupGadget(c, `
name: gadget
type: gadget
version: gadget
`, "")

	s.makeModelAssertionInState(c, "canonical", "pc", map[string]string{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	// avoid full seeding
	s.seeding()

	// try the whole device registration process
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	becomeOperational := s.findBecomeOperationalChange()
	c.Assert(becomeOperational, NotNil)

	c.Check(becomeOperational.Status().Ready(), Equals, true)
	c.Check(becomeOperational.Err(), ErrorMatches, `(?s).*obtained serial assertion does not match provided device identity information.*`)
}

func (s *deviceMgrSuite) TestDeviceAssertionsModelAndSerial(c *C) {
	// nothing in the state
	s.state.Lock()
	_, err := devicestate.Model(s.state)
	s.state.Unlock()
	c.Check(err, Equals, state.ErrNoState)
	s.state.Lock()
	_, err = devicestate.Serial(s.state)
	s.state.Unlock()
	c.Check(err, Equals, state.ErrNoState)

	_, err = s.mgr.Model()
	c.Check(err, Equals, state.ErrNoState)
	_, err = s.mgr.Serial()
	c.Check(err, Equals, state.ErrNoState)

	// just brand and model
	s.state.Lock()
	auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	s.state.Unlock()
	_, err = s.mgr.Model()
	c.Check(err, Equals, state.ErrNoState)
	_, err = s.mgr.Serial()
	c.Check(err, Equals, state.ErrNoState)

	// have a model assertion
	model, err := s.storeSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":       "16",
		"brand-id":     "canonical",
		"model":        "pc",
		"gadget":       "pc",
		"kernel":       "kernel",
		"architecture": "amd64",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	s.state.Lock()
	err = assertstate.Add(s.state, model)
	s.state.Unlock()
	c.Assert(err, IsNil)

	mod, err := s.mgr.Model()
	c.Assert(err, IsNil)
	c.Assert(mod.BrandID(), Equals, "canonical")

	s.state.Lock()
	mod, err = devicestate.Model(s.state)
	s.state.Unlock()
	c.Assert(err, IsNil)
	c.Assert(mod.BrandID(), Equals, "canonical")

	_, err = s.mgr.Serial()
	c.Check(err, Equals, state.ErrNoState)

	// have a serial as well
	s.state.Lock()
	auth.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc",
		Serial: "8989",
	})
	s.state.Unlock()
	_, err = s.mgr.Model()
	c.Assert(err, IsNil)
	_, err = s.mgr.Serial()
	c.Check(err, Equals, state.ErrNoState)

	// have a serial assertion
	s.state.Lock()
	s.makeSerialAssertionInState(c, "canonical", "pc", "8989")
	s.state.Unlock()

	_, err = s.mgr.Model()
	c.Assert(err, IsNil)
	ser, err := s.mgr.Serial()
	c.Assert(err, IsNil)
	c.Check(ser.Serial(), Equals, "8989")

	s.state.Lock()
	ser, err = devicestate.Serial(s.state)
	s.state.Unlock()
	c.Assert(err, IsNil)
	c.Check(ser.Serial(), Equals, "8989")
}

func (s *deviceMgrSuite) TestDeviceAssertionsDeviceSessionRequestParams(c *C) {
	// nothing there
	_, err := s.mgr.DeviceSessionRequestParams("NONCE-1")
	c.Check(err, Equals, state.ErrNoState)

	// have a model assertion
	modela, err := s.storeSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":       "16",
		"brand-id":     "canonical",
		"model":        "pc",
		"gadget":       "pc",
		"kernel":       "kernel",
		"architecture": "amd64",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	s.state.Lock()
	err = assertstate.Add(s.state, modela)
	s.state.Unlock()
	c.Assert(err, IsNil)

	// setup state as done by device initialisation
	s.state.Lock()
	devKey, _ := assertstest.GenerateKey(testKeyLength)
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
	err = assertstate.Add(s.state, seriala)
	c.Assert(err, IsNil)

	auth.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc",
		Serial: "8989",
		KeyID:  devKey.PublicKey().ID(),
	})
	devicestate.KeypairManager(s.mgr).Put(devKey)
	s.state.Unlock()

	params, err := s.mgr.DeviceSessionRequestParams("NONCE-1")
	c.Assert(err, IsNil)

	c.Check(params.Model.Model(), Equals, "pc")

	c.Check(params.Serial.Model(), Equals, "pc")
	c.Check(params.Serial.Serial(), Equals, "8989")

	sessReq := params.Request
	// correctly signed with device key
	err = asserts.SignatureCheck(sessReq, devKey.PublicKey())
	c.Check(err, IsNil)

	c.Check(sessReq.BrandID(), Equals, "canonical")
	c.Check(sessReq.Model(), Equals, "pc")
	c.Check(sessReq.Serial(), Equals, "8989")
	c.Check(sessReq.Nonce(), Equals, "NONCE-1")
}

func (s *deviceMgrSuite) TestDeviceAssertionsProxyStore(c *C) {
	mockServer := s.mockServer(c)
	defer mockServer.Close()

	// nothing in the state
	s.state.Lock()
	_, err := devicestate.ProxyStore(s.state)
	s.state.Unlock()
	c.Check(err, Equals, state.ErrNoState)

	_, err = s.mgr.ProxyStore()
	c.Check(err, Equals, state.ErrNoState)

	// have a store referenced
	s.state.Lock()
	tr := config.NewTransaction(s.state)
	err = tr.Set("core", "proxy.store", "foo")
	tr.Commit()
	s.state.Unlock()
	c.Assert(err, IsNil)
	_, err = s.mgr.ProxyStore()
	c.Check(err, Equals, state.ErrNoState)

	operatorAcct := assertstest.NewAccount(s.storeSigning, "foo-operator", nil, "")
	s.state.Lock()
	err = assertstate.Add(s.state, operatorAcct)
	s.state.Unlock()
	c.Assert(err, IsNil)

	// have a store assertion.
	stoAs, err := s.storeSigning.Sign(asserts.StoreType, map[string]interface{}{
		"store":       "foo",
		"operator-id": operatorAcct.AccountID(),
		"url":         mockServer.URL,
		"timestamp":   time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	s.state.Lock()
	err = assertstate.Add(s.state, stoAs)
	s.state.Unlock()
	c.Assert(err, IsNil)

	sto, err := s.mgr.ProxyStore()
	c.Assert(err, IsNil)
	c.Assert(sto.Store(), Equals, "foo")

	s.state.Lock()
	sto, err = devicestate.ProxyStore(s.state)
	s.state.Unlock()
	c.Assert(err, IsNil)
	c.Assert(sto.Store(), Equals, "foo")
	c.Assert(sto.URL().String(), Equals, mockServer.URL)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureSeedYamlAlreadySeeded(c *C) {
	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()

	called := false
	restore := devicestate.MockPopulateStateFromSeed(func(*state.State) ([]*state.TaskSet, error) {
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
	restore := devicestate.MockPopulateStateFromSeed(func(*state.State) ([]*state.TaskSet, error) {
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
	restore := devicestate.MockPopulateStateFromSeed(func(*state.State) ([]*state.TaskSet, error) {
		called = true
		return nil, nil
	})
	defer restore()

	err := devicestate.EnsureSeedYaml(s.mgr)
	c.Assert(err, IsNil)
	c.Assert(called, Equals, true)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureSeedYamlHappy(c *C) {
	restore := devicestate.MockPopulateStateFromSeed(func(*state.State) (ts []*state.TaskSet, err error) {
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
	auth.SetDevice(s.state, &auth.DeviceState{
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
	brandAcct := assertstest.NewAccount(s.storeSigning, "my-brand", map[string]interface{}{
		"account-id": "my-brand",
	}, "")
	err := assertstate.Add(s.state, brandAcct)
	c.Assert(err, IsNil)
	otherAcct := assertstest.NewAccount(s.storeSigning, "other-brand", map[string]interface{}{
		"account-id": "other-brand",
	}, "")
	err = assertstate.Add(s.state, otherAcct)
	c.Assert(err, IsNil)

	brandPubKey, err := s.brandSigning.PublicKey("")
	c.Assert(err, IsNil)
	brandAccKey := assertstest.NewAccountKey(s.storeSigning, brandAcct, nil, brandPubKey, "")
	err = assertstate.Add(s.state, brandAccKey)
	c.Assert(err, IsNil)
}

func (s *deviceMgrSuite) setupSnapDecl(c *C, name, snapID, publisherID string) {
	brandGadgetDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-name":    name,
		"snap-id":      snapID,
		"publisher-id": publisherID,
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, brandGadgetDecl)
	c.Assert(err, IsNil)
}

func (s *deviceMgrSuite) TestCheckGadget(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	// nothing is setup
	gadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: other-gadget, version: 0}", nil)

	err := devicestate.CheckGadgetOrKernel(s.state, gadgetInfo, nil, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install gadget without model assertion`)

	// setup model assertion
	s.setupBrands(c)

	model, err := s.brandSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"gadget":       "gadget",
		"kernel":       "krnl",
		"architecture": "amd64",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, model)
	c.Assert(err, IsNil)
	err = auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "my-brand",
		Model: "my-model",
	})
	c.Assert(err, IsNil)

	err = devicestate.CheckGadgetOrKernel(s.state, gadgetInfo, nil, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install gadget "other-gadget", model assertion requests "gadget"`)

	// brand gadget
	s.setupSnapDecl(c, "gadget", "brand-gadget-id", "my-brand")
	brandGadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)
	brandGadgetInfo.SnapID = "brand-gadget-id"

	// canonical gadget
	s.setupSnapDecl(c, "gadget", "canonical-gadget-id", "canonical")
	canonicalGadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)
	canonicalGadgetInfo.SnapID = "canonical-gadget-id"

	// other gadget
	s.setupSnapDecl(c, "gadget", "other-gadget-id", "other-brand")
	otherGadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)
	otherGadgetInfo.SnapID = "other-gadget-id"

	// install brand gadget ok
	err = devicestate.CheckGadgetOrKernel(s.state, brandGadgetInfo, nil, snapstate.Flags{})
	c.Check(err, IsNil)

	// install canonical gadget ok
	err = devicestate.CheckGadgetOrKernel(s.state, canonicalGadgetInfo, nil, snapstate.Flags{})
	c.Check(err, IsNil)

	// install other gadget fails
	err = devicestate.CheckGadgetOrKernel(s.state, otherGadgetInfo, nil, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install gadget "gadget" published by "other-brand" for model by "my-brand"`)

	// unasserted installation of other works
	otherGadgetInfo.SnapID = ""
	err = devicestate.CheckGadgetOrKernel(s.state, otherGadgetInfo, nil, snapstate.Flags{})
	c.Check(err, IsNil)

	// parallel install fails
	otherGadgetInfo.InstanceKey = "foo"
	err = devicestate.CheckGadgetOrKernel(s.state, otherGadgetInfo, nil, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install "gadget_foo", parallel installation of kernel or gadget snaps is not supported`)
}

func (s *deviceMgrSuite) TestCheckGadgetOnClassic(c *C) {
	release.OnClassic = true

	s.state.Lock()
	defer s.state.Unlock()

	gadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: other-gadget, version: 0}", nil)

	// setup model assertion
	s.setupBrands(c)

	model, err := s.brandSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":    "16",
		"brand-id":  "my-brand",
		"model":     "my-model",
		"classic":   "true",
		"gadget":    "gadget",
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, model)
	c.Assert(err, IsNil)
	err = auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "my-brand",
		Model: "my-model",
	})
	c.Assert(err, IsNil)

	err = devicestate.CheckGadgetOrKernel(s.state, gadgetInfo, nil, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install gadget "other-gadget", model assertion requests "gadget"`)

	// brand gadget
	s.setupSnapDecl(c, "gadget", "brand-gadget-id", "my-brand")
	brandGadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)
	brandGadgetInfo.SnapID = "brand-gadget-id"

	// canonical gadget
	s.setupSnapDecl(c, "gadget", "canonical-gadget-id", "canonical")
	canonicalGadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)
	canonicalGadgetInfo.SnapID = "canonical-gadget-id"

	// other gadget
	s.setupSnapDecl(c, "gadget", "other-gadget-id", "other-brand")
	otherGadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)
	otherGadgetInfo.SnapID = "other-gadget-id"

	// install brand gadget ok
	err = devicestate.CheckGadgetOrKernel(s.state, brandGadgetInfo, nil, snapstate.Flags{})
	c.Check(err, IsNil)

	// install canonical gadget ok
	err = devicestate.CheckGadgetOrKernel(s.state, canonicalGadgetInfo, nil, snapstate.Flags{})
	c.Check(err, IsNil)

	// install other gadget fails
	err = devicestate.CheckGadgetOrKernel(s.state, otherGadgetInfo, nil, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install gadget "gadget" published by "other-brand" for model by "my-brand"`)

	// unasserted installation of other works
	otherGadgetInfo.SnapID = ""
	err = devicestate.CheckGadgetOrKernel(s.state, otherGadgetInfo, nil, snapstate.Flags{})
	c.Check(err, IsNil)
}

func (s *deviceMgrSuite) TestCheckGadgetOnClassicGadgetNotSpecified(c *C) {
	release.OnClassic = true

	s.state.Lock()
	defer s.state.Unlock()

	gadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)

	// setup model assertion
	s.setupBrands(c)

	model, err := s.brandSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":    "16",
		"brand-id":  "my-brand",
		"model":     "my-model",
		"classic":   "true",
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, model)
	c.Assert(err, IsNil)
	err = auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "my-brand",
		Model: "my-model",
	})
	c.Assert(err, IsNil)

	err = devicestate.CheckGadgetOrKernel(s.state, gadgetInfo, nil, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install gadget snap on classic if not requested by the model`)
}

func (s *deviceMgrSuite) TestCheckKernel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	kernelInfo := snaptest.MockInfo(c, "{type: kernel, name: lnrk, version: 0}", nil)

	// not on classic
	release.OnClassic = true
	err := devicestate.CheckGadgetOrKernel(s.state, kernelInfo, nil, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install a kernel snap on classic`)
	release.OnClassic = false

	// nothing is setup
	err = devicestate.CheckGadgetOrKernel(s.state, kernelInfo, nil, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install kernel without model assertion`)

	// setup model assertion
	s.setupBrands(c)

	model, err := s.brandSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"gadget":       "gadget",
		"kernel":       "krnl",
		"architecture": "amd64",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, model)
	c.Assert(err, IsNil)
	err = auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "my-brand",
		Model: "my-model",
	})
	c.Assert(err, IsNil)

	err = devicestate.CheckGadgetOrKernel(s.state, kernelInfo, nil, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install kernel "lnrk", model assertion requests "krnl"`)

	// brand kernel
	s.setupSnapDecl(c, "krnl", "brand-krnl-id", "my-brand")
	brandKrnlInfo := snaptest.MockInfo(c, "{type: kernel, name: krnl, version: 0}", nil)
	brandKrnlInfo.SnapID = "brand-krnl-id"

	// canonical kernel
	s.setupSnapDecl(c, "krnl", "canonical-krnl-id", "canonical")
	canonicalKrnlInfo := snaptest.MockInfo(c, "{type: kernel, name: krnl, version: 0}", nil)
	canonicalKrnlInfo.SnapID = "canonical-krnl-id"

	// other kernel
	s.setupSnapDecl(c, "krnl", "other-krnl-id", "other-brand")
	otherKrnlInfo := snaptest.MockInfo(c, "{type: kernel, name: krnl, version: 0}", nil)
	otherKrnlInfo.SnapID = "other-krnl-id"

	// install brand kernel ok
	err = devicestate.CheckGadgetOrKernel(s.state, brandKrnlInfo, nil, snapstate.Flags{})
	c.Check(err, IsNil)

	// install canonical kernel ok
	err = devicestate.CheckGadgetOrKernel(s.state, canonicalKrnlInfo, nil, snapstate.Flags{})
	c.Check(err, IsNil)

	// install other kernel fails
	err = devicestate.CheckGadgetOrKernel(s.state, otherKrnlInfo, nil, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install kernel "krnl" published by "other-brand" for model by "my-brand"`)

	// unasserted installation of other works
	otherKrnlInfo.SnapID = ""
	err = devicestate.CheckGadgetOrKernel(s.state, otherKrnlInfo, nil, snapstate.Flags{})
	c.Check(err, IsNil)

	// parallel install fails
	otherKrnlInfo.InstanceKey = "foo"
	err = devicestate.CheckGadgetOrKernel(s.state, otherKrnlInfo, nil, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install "krnl_foo", parallel installation of kernel or gadget snaps is not supported`)
}

func (s *deviceMgrSuite) makeModelAssertionInState(c *C, brandID, model string, extras map[string]string) {
	headers := map[string]interface{}{
		"series":    "16",
		"brand-id":  brandID,
		"model":     model,
		"timestamp": time.Now().Format(time.RFC3339),
	}
	for k, v := range extras {
		headers[k] = v
	}
	var signer assertstest.SignerDB
	switch brandID {
	case "canonical":
		signer = s.storeSigning.RootSigning
	case "my-brand":
		s.setupBrands(c)
		signer = s.brandSigning
	}
	modelAs, err := signer.Sign(asserts.ModelType, headers, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, modelAs)
	c.Assert(err, IsNil)
}

func (s *deviceMgrSuite) makeSerialAssertionInState(c *C, brandID, model, serialN string) {
	devKey, _ := assertstest.GenerateKey(752)
	encDevKey, err := asserts.EncodePublicKey(devKey.PublicKey())
	c.Assert(err, IsNil)
	serial, err := s.storeSigning.Sign(asserts.SerialType, map[string]interface{}{
		"brand-id":            brandID,
		"model":               model,
		"serial":              serialN,
		"device-key":          string(encDevKey),
		"device-key-sha3-384": devKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, serial)
	c.Assert(err, IsNil)
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
	auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	s.makeModelAssertionInState(c, "canonical", "pc", map[string]string{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	c.Check(canAutoRefresh(), Equals, false)

	// seeded, model, serial -> auto-refresh
	auth.SetDevice(s.state, &auth.DeviceState{
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
	auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	s.makeModelAssertionInState(c, "canonical", "pc", map[string]string{
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
	auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	s.makeModelAssertionInState(c, "canonical", "pc", map[string]string{
		"classic": "true",
	})
	c.Check(canAutoRefresh(), Equals, false)

	// seeded, model, serial -> auto-refresh
	auth.SetDevice(s.state, &auth.DeviceState{
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
	}, nil, nil, nil)
	c.Assert(err, IsNil)
	conns, err := repo.Connected("snap-with-snapd-control", "snapd-control")
	c.Assert(err, IsNil)
	c.Assert(conns, HasLen, 1)
	ifacerepo.Replace(st, repo)
}

func (s *deviceMgrSuite) makeSnapDeclaration(c *C, st *state.State, info *snap.Info) {
	decl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-name":    info.SnapName(),
		"snap-id":      info.SideInfo.SnapID,
		"publisher-id": "canonical",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, decl)
	c.Assert(err, IsNil)
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
	s.makeSnapDeclaration(c, st, info11)
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
	s.makeSnapDeclaration(c, st, info11)

	c.Check(devicestate.CanManageRefreshes(st), Equals, false)
}

func (s *deviceMgrSuite) TestReloadRegistered(c *C) {
	st := state.New(nil)

	runner1 := state.NewTaskRunner(st)
	hookMgr1, err := hookstate.Manager(st, runner1)
	c.Assert(err, IsNil)
	mgr1, err := devicestate.Manager(st, hookMgr1, runner1)
	c.Assert(err, IsNil)

	ok := false
	select {
	case <-mgr1.Registered():
	default:
		ok = true
	}
	c.Check(ok, Equals, true)

	st.Lock()
	auth.SetDevice(st, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc",
		Serial: "serial",
	})
	st.Unlock()

	runner2 := state.NewTaskRunner(st)
	hookMgr2, err := hookstate.Manager(st, runner2)
	c.Assert(err, IsNil)
	mgr2, err := devicestate.Manager(st, hookMgr2, runner2)
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
	auth.SetDevice(s.state, &auth.DeviceState{
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
	log, restore := logger.MockLogger()
	defer restore()
	os.Setenv("SNAPD_DEBUG", "1")
	defer os.Unsetenv("SNAPD_DEBUG")

	badURL := &url.URL{Opaque: "%a"} // url.Parse(badURL.String()) needs to fail, which isn't easy :-)
	c.Check(devicestate.NewEnoughProxy(badURL, http.DefaultClient), Equals, false)
	c.Check(log.String(), Matches, "(?m).* DEBUG: Cannot check whether proxy store supports a custom serial vault: parse .*")
}

func (s *deviceMgrSuite) TestNewEnoughProxy(c *C) {
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
		c.Check(devicestate.NewEnoughProxy(u, http.DefaultClient), Equals, false)
		if len(expected) > 0 {
			expected = "(?m).* DEBUG: Cannot check whether proxy store supports a custom serial vault: " + expected
		}
		c.Check(log.String(), Matches, expected)
	}
	c.Check(n, Equals, len(expecteds))

	// and success at last
	log.Reset()
	c.Check(devicestate.NewEnoughProxy(u, http.DefaultClient), Equals, true)
	c.Check(log.String(), Equals, "")
	c.Check(n, Equals, len(expecteds)+1)
}
