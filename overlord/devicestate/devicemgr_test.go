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

package devicestate_test

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

func TestDeviceManager(t *testing.T) { TestingT(t) }

type deviceMgrSuite struct {
	state *state.State
	mgr   *devicestate.DeviceManager
	db    *asserts.Database

	storeSigning *assertstest.StoreStack
}

var _ = Suite(&deviceMgrSuite{})

type fakeStore struct {
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
	a, err := ref.Resolve(sto.db.Find)
	if err != nil {
		return nil, &store.AssertionNotFoundError{Ref: ref}
	}
	return a, nil
}

func (*fakeStore) Snap(string, string, bool, snap.Revision, *auth.UserState) (*snap.Info, error) {
	panic("fakeStore.Snap not expected")
}

func (sto *fakeStore) Find(*store.Search, *auth.UserState) ([]*snap.Info, error) {
	panic("fakeStore.Find not expected")
}

func (sto *fakeStore) ListRefresh([]*store.RefreshCandidate, *auth.UserState) ([]*snap.Info, error) {
	panic("fakeStore.ListRefresh not expected")
}

func (sto *fakeStore) Download(string, *snap.DownloadInfo, progress.Meter, *auth.UserState) (string, error) {
	panic("fakeStore.Download not expected")
}

func (sto *fakeStore) SuggestedCurrency() string {
	panic("fakeStore.SuggestedCurrency not expected")
}

func (sto *fakeStore) Buy(*store.BuyOptions, *auth.UserState) (*store.BuyResult, error) {
	panic("fakeStore.Buy not expected")
}

func (sto *fakeStore) PaymentMethods(*auth.UserState) (*store.PaymentInformation, error) {
	panic("fakeStore.PaymentMethods not expected")
}

func (s *deviceMgrSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	rootPrivKey, _ := assertstest.GenerateKey(1024)
	storePrivKey, _ := assertstest.GenerateKey(752)
	s.storeSigning = assertstest.NewStoreStack("canonical", rootPrivKey, storePrivKey)
	s.state = state.New(nil)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.storeSigning.Trusted,
	})
	c.Assert(err, IsNil)

	s.state.Lock()
	assertstate.ReplaceDB(s.state, db)
	s.state.Unlock()

	err = db.Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	mgr, err := devicestate.Manager(s.state)
	c.Assert(err, IsNil)

	s.db = db
	s.mgr = mgr

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
	dirs.SetRootDir("")
}

func (s *deviceMgrSuite) settle() {
	for i := 0; i < 50; i++ {
		s.mgr.Ensure()
		s.mgr.Wait()
	}
}

func (s *deviceMgrSuite) mockServer(c *C, reqID string) *httptest.Server {
	var mu sync.Mutex
	count := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/identity/api/v1/request-id":
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, fmt.Sprintf(`{"request-id": "%s"}`, reqID))

		case "/identity/api/v1/devices":
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
			c.Check(serialReq.BrandID(), Equals, "canonical")
			c.Check(serialReq.Model(), Equals, "pc")
			if reqID == "REQID-POLL" && serialNum != 10002 {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			serial, err := s.storeSigning.Sign(asserts.SerialType, map[string]interface{}{
				"brand-id":            "canonical",
				"model":               "pc",
				"serial":              fmt.Sprintf("%d", serialNum),
				"device-key":          serialReq.HeaderString("device-key"),
				"device-key-sha3-384": serialReq.SignKeyID(),
				"timestamp":           time.Now().Format(time.RFC3339),
			}, nil, "")
			c.Assert(err, IsNil)
			w.Header().Set("Content-Type", asserts.MediaType)
			w.WriteHeader(http.StatusOK)
			w.Write(asserts.Encode(serial))
		}
	}))
}

func (s *deviceMgrSuite) TestFullDeviceRegistrationHappy(c *C) {
	r1 := devicestate.MockKeyLength(752)
	defer r1()

	mockServer := s.mockServer(c, "REQID-1")
	defer mockServer.Close()

	mockRequestIDURL := mockServer.URL + "/identity/api/v1/request-id"
	r2 := devicestate.MockRequestIDURL(mockRequestIDURL)
	defer r2()

	mockSerialRequestURL := mockServer.URL + "/identity/api/v1/devices"
	r3 := devicestate.MockSerialRequestURL(mockSerialRequestURL)
	defer r3()

	s.state.Lock()
	// setup state as will be done by first-boot
	auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	s.state.Unlock()

	// runs the whole device registration process
	s.settle()

	s.state.Lock()
	defer s.state.Unlock()

	var becomeOperational *state.Change
	for _, chg := range s.state.Changes() {
		if chg.Kind() == "become-operational" {
			becomeOperational = chg
			break
		}
	}
	c.Assert(becomeOperational, NotNil)

	c.Check(becomeOperational.Status().Ready(), Equals, true)
	c.Check(becomeOperational.Err(), IsNil)

	device, err := auth.Device(s.state)
	c.Assert(err, IsNil)
	c.Check(device.Brand, Equals, "canonical")
	c.Check(device.Model, Equals, "pc")
	c.Check(device.Serial, Equals, "9999")

	a, err := s.db.Find(asserts.SerialType, map[string]string{
		"brand-id": "canonical",
		"model":    "pc",
		"serial":   "9999",
	})
	c.Assert(err, IsNil)
	serial := a.(*asserts.Serial)

	privKey, err := s.mgr.KeypairManager().Get(serial.DeviceKey().ID())
	c.Assert(err, IsNil)
	c.Check(privKey, NotNil)

	c.Check(device.KeyID, Equals, privKey.PublicKey().ID())
}

func (s *deviceMgrSuite) TestDoRequestSerialIdempotentAfterAddSerial(c *C) {
	privKey, _ := assertstest.GenerateKey(1024)

	mockServer := s.mockServer(c, "REQID-1")
	defer mockServer.Close()

	mockRequestIDURL := mockServer.URL + "/identity/api/v1/request-id"
	restore := devicestate.MockRequestIDURL(mockRequestIDURL)
	defer restore()

	mockSerialRequestURL := mockServer.URL + "/identity/api/v1/devices"
	restore = devicestate.MockSerialRequestURL(mockSerialRequestURL)
	defer restore()

	restore = devicestate.MockRepeatRequestSerial("after-add-serial")
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// setup state as done by first-boot/Ensure/doGenerateDeviceKey
	auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
		KeyID: privKey.PublicKey().ID(),
	})
	s.mgr.KeypairManager().Put(privKey)

	t := s.state.NewTask("request-serial", "test")
	chg := s.state.NewChange("become-operational", "...")
	chg.AddTask(t)

	s.state.Unlock()
	s.mgr.Ensure()
	s.mgr.Wait()
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

	s.state.Unlock()
	s.mgr.Ensure()
	s.mgr.Wait()
	s.state.Lock()

	// Repeated handler run but set original serial.
	c.Check(chg.Status(), Equals, state.DoneStatus)
	device, err = auth.Device(s.state)
	c.Check(err, IsNil)
	c.Check(device.Serial, Equals, "9999")
}

func (s *deviceMgrSuite) TestDoRequestSerialIdempotentAfterGotSerial(c *C) {
	privKey, _ := assertstest.GenerateKey(1024)

	mockServer := s.mockServer(c, "REQID-1")
	defer mockServer.Close()

	mockRequestIDURL := mockServer.URL + "/identity/api/v1/request-id"
	restore := devicestate.MockRequestIDURL(mockRequestIDURL)
	defer restore()

	mockSerialRequestURL := mockServer.URL + "/identity/api/v1/devices"
	restore = devicestate.MockSerialRequestURL(mockSerialRequestURL)
	defer restore()

	restore = devicestate.MockRepeatRequestSerial("after-got-serial")
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// setup state as done by first-boot/Ensure/doGenerateDeviceKey
	auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
		KeyID: privKey.PublicKey().ID(),
	})
	s.mgr.KeypairManager().Put(privKey)

	t := s.state.NewTask("request-serial", "test")
	chg := s.state.NewChange("become-operational", "...")
	chg.AddTask(t)

	s.state.Unlock()
	s.mgr.Ensure()
	s.mgr.Wait()
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.DoingStatus)
	device, err := auth.Device(s.state)
	c.Check(err, IsNil)
	_, err = s.db.Find(asserts.SerialType, map[string]string{
		"brand-id": "canonical",
		"model":    "pc",
		"serial":   "9999",
	})
	c.Assert(err, Equals, asserts.ErrNotFound)

	s.state.Unlock()
	s.mgr.Ensure()
	s.mgr.Wait()
	s.state.Lock()

	// Repeated handler run but set original serial.
	c.Check(chg.Status(), Equals, state.DoneStatus)
	device, err = auth.Device(s.state)
	c.Check(err, IsNil)
	c.Check(device.Serial, Equals, "9999")
}

func (s *deviceMgrSuite) TestFullDeviceRegistrationPollHappy(c *C) {
	r1 := devicestate.MockKeyLength(752)
	defer r1()

	mockServer := s.mockServer(c, "REQID-POLL")
	defer mockServer.Close()

	mockRequestIDURL := mockServer.URL + "/identity/api/v1/request-id"
	r2 := devicestate.MockRequestIDURL(mockRequestIDURL)
	defer r2()

	mockSerialRequestURL := mockServer.URL + "/identity/api/v1/devices"
	r3 := devicestate.MockSerialRequestURL(mockSerialRequestURL)
	defer r3()

	// immediately
	r4 := devicestate.MockRetryInterval(0)
	defer r4()

	s.state.Lock()
	// setup state as will be done by first-boot
	auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	s.state.Unlock()

	// runs the whole device registration process with polling
	s.settle()

	s.state.Lock()
	defer s.state.Unlock()

	var becomeOperational *state.Change
	for _, chg := range s.state.Changes() {
		if chg.Kind() == "become-operational" {
			becomeOperational = chg
			break
		}
	}
	c.Assert(becomeOperational, NotNil)

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

	privKey, err := s.mgr.KeypairManager().Get(serial.DeviceKey().ID())
	c.Assert(err, IsNil)
	c.Check(privKey, NotNil)

	c.Check(device.KeyID, Equals, privKey.PublicKey().ID())
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
	devKey, _ := assertstest.GenerateKey(752)
	encDevKey, err := asserts.EncodePublicKey(devKey.PublicKey())
	c.Assert(err, IsNil)
	serial, err := s.storeSigning.Sign(asserts.SerialType, map[string]interface{}{
		"brand-id":            "canonical",
		"model":               "pc",
		"serial":              "8989",
		"device-key":          string(encDevKey),
		"device-key-sha3-384": devKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	s.state.Lock()
	err = assertstate.Add(s.state, serial)
	s.state.Unlock()
	c.Assert(err, IsNil)

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

func (s *deviceMgrSuite) TestDeviceAssertionsSerialProof(c *C) {
	// nothing there
	_, err := s.mgr.SerialProof("NONCE-1")
	c.Check(err, Equals, state.ErrNoState)

	s.state.Lock()
	privKey, _ := assertstest.GenerateKey(1024)
	// setup state as done by first-boot/Ensure/doGenerateDeviceKey
	auth.SetDevice(s.state, &auth.DeviceState{
		KeyID: privKey.PublicKey().ID(),
	})
	s.mgr.KeypairManager().Put(privKey)
	s.state.Unlock()

	serialProof, err := s.mgr.SerialProof("NONCE-1")
	c.Assert(err, IsNil)

	// correctly signed with device key
	err = asserts.SignatureCheck(serialProof, privKey.PublicKey())
	c.Check(err, IsNil)
}
