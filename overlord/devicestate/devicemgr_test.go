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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/context"
	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hook"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
)

func TestDeviceManager(t *testing.T) { TestingT(t) }

type deviceMgrSuite struct {
	state   *state.State
	hookMgr *hook.HookManager
	mgr     *devicestate.DeviceManager
	db      *asserts.Database

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

func (sto *fakeStore) Download(context.Context, string, string, *snap.DownloadInfo, progress.Meter, *auth.UserState) error {
	panic("fakeStore.Download not expected")
}

func (sto *fakeStore) SuggestedCurrency() string {
	panic("fakeStore.SuggestedCurrency not expected")
}

func (sto *fakeStore) Buy(*store.BuyOptions, *auth.UserState) (*store.BuyResult, error) {
	panic("fakeStore.Buy not expected")
}

func (sto *fakeStore) ReadyToBuy(*auth.UserState) error {
	panic("fakeStore.ReadyToBuy not expected")
}

func (sto *fakeStore) Sections(*auth.UserState) ([]string, error) {
	panic("fakeStore.Sections not expected")
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

	hookMgr, err := hook.Manager(s.state)
	c.Assert(err, IsNil)
	mgr, err := devicestate.Manager(s.state, hookMgr)
	c.Assert(err, IsNil)

	s.db = db
	s.hookMgr = hookMgr
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
	release.OnClassic = true
}

func (s *deviceMgrSuite) settle() {
	for i := 0; i < 50; i++ {
		s.hookMgr.Ensure()
		s.mgr.Ensure()
		s.hookMgr.Wait()
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

		case "/identity/api/v1/serial":
			c.Check(r.Header.Get("X-Extra-Header"), Equals, "extra")
			fallthrough
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
			serialStr := fmt.Sprintf("%d", serialNum)
			if serialReq.Serial() != "" {
				// use proposed serial
				serialStr = serialReq.Serial()
			}
			serial, err := s.storeSigning.Sign(asserts.SerialType, map[string]interface{}{
				"brand-id":            "canonical",
				"model":               "pc",
				"serial":              serialStr,
				"device-key":          serialReq.HeaderString("device-key"),
				"device-key-sha3-384": serialReq.SignKeyID(),
				"timestamp":           time.Now().Format(time.RFC3339),
			}, serialReq.Body(), "")
			c.Assert(err, IsNil)
			w.Header().Set("Content-Type", asserts.MediaType)
			w.WriteHeader(http.StatusOK)
			w.Write(asserts.Encode(serial))
		}
	}))
}

func (s *deviceMgrSuite) setupGadget(c *C, snapYaml string, snapContents string) {
	sideInfoGadget := &snap.SideInfo{
		RealName: "gadget",
		Revision: snap.R(2),
	}
	snaptest.MockSnap(c, snapYaml, snapContents, sideInfoGadget)
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
	snaptest.MockSnap(c, snapYaml, snapContents, sideInfoCore)
	snapstate.Set(s.state, name, &snapstate.SnapState{
		SnapType: "os",
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfoCore},
		Current:  sideInfoCore.Revision,
	})
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

	// setup state as will be done by first-boot
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
	})

	// runs the whole device registration process
	s.state.Unlock()
	s.settle()
	s.state.Lock()

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

	// setup state as will be done by first-boot
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
	})

	// runs the whole device registration process with polling
	s.state.Unlock()
	s.settle()
	s.state.Lock()

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

func (s *deviceMgrSuite) TestFullDeviceRegistrationHappyPrepareDeviceHook(c *C) {
	r1 := devicestate.MockKeyLength(752)
	defer r1()

	mockServer := s.mockServer(c, "REQID-1")
	defer mockServer.Close()

	r2 := hook.MockRunHook(func(ctx *hookstate.Context, _ *tomb.Tomb) ([]byte, error) {
		c.Assert(ctx.HookName(), Equals, "prepare-device")

		// snapctl set the registration params
		_, _, err := ctlcmd.Run(ctx, []string{"set", fmt.Sprintf("device-service.url=%q", mockServer.URL+"/identity/api/v1/")})
		c.Assert(err, IsNil)

		h, err := json.Marshal(map[string]string{
			"x-extra-header": "extra",
		})
		c.Assert(err, IsNil)
		_, _, err = ctlcmd.Run(ctx, []string{"set", fmt.Sprintf("device-service.headers=%s", string(h))})
		c.Assert(err, IsNil)

		_, _, err = ctlcmd.Run(ctx, []string{"set", fmt.Sprintf("registration.proposed-serial=%q", "Y9999")})
		c.Assert(err, IsNil)

		d, err := yaml.Marshal(map[string]string{
			"mac": "00:00:00:00:ff:00",
		})
		c.Assert(err, IsNil)
		_, _, err = ctlcmd.Run(ctx, []string{"set", fmt.Sprintf("registration.body=%q", d)})
		c.Assert(err, IsNil)

		return nil, nil
	})
	defer r2()

	// setup state as will be done by first-boot
	// & have a gadget with a prepare-device hook
	s.state.Lock()
	defer s.state.Unlock()

	s.setupGadget(c, `
name: gadget
type: gadget
version: gadget
hooks:
    prepare-device:
`, "")

	auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	// runs the whole device registration process
	s.state.Unlock()
	s.settle()
	s.state.Lock()

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
	c.Check(device.Serial, Equals, "Y9999")

	a, err := s.db.Find(asserts.SerialType, map[string]string{
		"brand-id": "canonical",
		"model":    "pc",
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

func (s *deviceMgrSuite) TestDeviceAssertionsDeviceSessionRequest(c *C) {
	// nothing there
	_, _, err := s.mgr.DeviceSessionRequest("NONCE-1")
	c.Check(err, Equals, state.ErrNoState)

	// setup state as done by device initialisation
	s.state.Lock()
	devKey, _ := assertstest.GenerateKey(1024)
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
	s.mgr.KeypairManager().Put(devKey)
	s.state.Unlock()

	sessReq, serial, err := s.mgr.DeviceSessionRequest("NONCE-1")
	c.Assert(err, IsNil)

	c.Check(serial.Serial(), Equals, "8989")

	// correctly signed with device key
	err = asserts.SignatureCheck(sessReq, devKey.PublicKey())
	c.Check(err, IsNil)

	c.Check(sessReq.BrandID(), Equals, "canonical")
	c.Check(sessReq.Model(), Equals, "pc")
	c.Check(sessReq.Serial(), Equals, "8989")
	c.Check(sessReq.Nonce(), Equals, "NONCE-1")
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureSeedYamlAlreadySeeded(c *C) {
	release.OnClassic = false

	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()

	called := false
	restore := devicestate.MockPopulateStateFromSeed(func(*state.State) ([]*state.TaskSet, error) {
		called = true
		return nil, nil
	})
	defer restore()

	err := s.mgr.EnsureSeedYaml()
	c.Assert(err, IsNil)
	c.Assert(called, Equals, false)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureSeedYamlChangeInFlight(c *C) {
	release.OnClassic = false

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

	err := s.mgr.EnsureSeedYaml()
	c.Assert(err, IsNil)
	c.Assert(called, Equals, false)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureSeedYamlSkippedOnClassic(c *C) {
	release.OnClassic = true

	called := false
	restore := devicestate.MockPopulateStateFromSeed(func(*state.State) ([]*state.TaskSet, error) {
		called = true
		return nil, nil
	})
	defer restore()

	err := s.mgr.EnsureSeedYaml()
	c.Assert(err, IsNil)
	c.Assert(called, Equals, false)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureSeedYamlHappy(c *C) {
	release.OnClassic = false

	restore := devicestate.MockPopulateStateFromSeed(func(*state.State) (ts []*state.TaskSet, err error) {
		t := s.state.NewTask("test-task", "a random task")
		ts = append(ts, state.NewTaskSet(t))
		return ts, nil
	})
	defer restore()

	err := s.mgr.EnsureSeedYaml()
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.state.Changes(), HasLen, 1)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureSeedYamlRecover(c *C) {
	release.OnClassic = false

	restore := devicestate.MockPopulateStateFromSeed(func(*state.State) (ts []*state.TaskSet, err error) {
		return nil, errors.New("should not be called")
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	s.setupCore(c, "ubuntu-core", `
name: ubuntu-core
type: os
version: ubuntu-core
`, "")

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
	err = assertstate.Add(s.state, model)
	c.Assert(err, IsNil)

	// have a serial assertion
	devKey, _ := assertstest.GenerateKey(752)
	encDevKey, err := asserts.EncodePublicKey(devKey.PublicKey())
	keyID := devKey.PublicKey().ID()
	c.Assert(err, IsNil)
	serial, err := s.storeSigning.Sign(asserts.SerialType, map[string]interface{}{
		"brand-id":            "canonical",
		"model":               "pc",
		"serial":              "8989",
		"device-key":          string(encDevKey),
		"device-key-sha3-384": keyID,
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, serial)
	c.Assert(err, IsNil)

	// forgotten key id and serial
	auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	// put key on disk
	err = s.mgr.KeypairManager().Put(devKey)
	c.Assert(err, IsNil)
	// extra unused stuff
	junk1 := filepath.Join(dirs.SnapDeviceDir, "private-keys-v1", "junkjunk1")
	err = ioutil.WriteFile(junk1, nil, 0644)
	c.Assert(err, IsNil)
	junk2 := filepath.Join(dirs.SnapDeviceDir, "private-keys-v1", "junkjunk2")
	err = ioutil.WriteFile(junk2, nil, 0644)
	c.Assert(err, IsNil)
	// double check
	pat := filepath.Join(dirs.SnapDeviceDir, "private-keys-v1", "*")
	onDisk, err := filepath.Glob(pat)
	c.Assert(err, IsNil)
	c.Check(onDisk, HasLen, 3)

	s.state.Unlock()
	err = s.mgr.EnsureSeedYaml()
	s.state.Lock()
	c.Assert(err, IsNil)

	c.Check(s.state.Changes(), HasLen, 0)

	var seeded bool
	err = s.state.Get("seeded", &seeded)
	c.Assert(err, IsNil)
	c.Check(seeded, Equals, true)

	device, err := auth.Device(s.state)
	c.Assert(err, IsNil)
	c.Check(device, DeepEquals, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc",
		KeyID:  keyID,
		Serial: "8989",
	})
	// key is still there
	_, err = s.mgr.KeypairManager().Get(keyID)
	c.Assert(err, IsNil)
	onDisk, err = filepath.Glob(pat)
	c.Assert(err, IsNil)
	// junk was removed
	c.Check(onDisk, HasLen, 1)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureBootOkSkippedOnClassic(c *C) {
	release.OnClassic = true

	err := s.mgr.EnsureBootOk()
	c.Assert(err, IsNil)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureBootOkBootloaderHappy(c *C) {
	release.OnClassic = false

	bootloader := boottest.NewMockBootloader("mock", c.MkDir())
	partition.ForceBootloader(bootloader)
	defer partition.ForceBootloader(nil)
	bootloader.SetBootVars(map[string]string{
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
	err := s.mgr.EnsureBootOk()
	s.state.Lock()
	c.Assert(err, IsNil)

	m, err := bootloader.GetBootVars("snap_mode")
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]string{"snap_mode": ""})
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureBootOkUpdateBootRevisionsHappy(c *C) {
	release.OnClassic = false

	bootloader := boottest.NewMockBootloader("mock", c.MkDir())
	partition.ForceBootloader(bootloader)
	defer partition.ForceBootloader(nil)

	// simulate that we have a new core_2, tried to boot it but that failed
	bootloader.SetBootVars(map[string]string{
		"snap_mode":     "",
		"snap_try_core": "core_2.snap",
		"snap_core":     "core_1.snap",
	})

	s.state.Lock()
	defer s.state.Unlock()
	siCore1 := &snap.SideInfo{RealName: "core", Revision: snap.R(1)}
	siCore2 := &snap.SideInfo{RealName: "core", Revision: snap.R(2)}
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		SnapType: "os",
		Active:   true,
		Sequence: []*snap.SideInfo{siCore1, siCore2},
		Current:  siCore2.Revision,
	})

	s.state.Unlock()
	err := s.mgr.EnsureBootOk()
	s.state.Lock()
	c.Assert(err, IsNil)

	c.Check(s.state.Changes(), HasLen, 1)
	c.Check(s.state.Changes()[0].Kind(), Equals, "update-revisions")
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureBootOkNotRunAgain(c *C) {
	release.OnClassic = false

	bootloader := boottest.NewMockBootloader("mock", c.MkDir())
	bootloader.SetBootVars(map[string]string{
		"snap_mode":     "trying",
		"snap_try_core": "core_1.snap",
	})
	bootloader.SetErr = fmt.Errorf("ensure bootloader is not used")
	partition.ForceBootloader(bootloader)
	defer partition.ForceBootloader(nil)

	s.mgr.SetBootOkRan(true)

	err := s.mgr.EnsureBootOk()
	c.Assert(err, IsNil)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureBootOkError(c *C) {
	release.OnClassic = false

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

	bootloader := boottest.NewMockBootloader("mock", c.MkDir())
	bootloader.GetErr = fmt.Errorf("bootloader err")
	partition.ForceBootloader(bootloader)
	defer partition.ForceBootloader(nil)

	s.mgr.SetBootOkRan(false)

	err := s.mgr.Ensure()
	c.Assert(err, ErrorMatches, "devicemgr: bootloader err")
}

func (s *deviceMgrSuite) TestCheckGadget(c *C) {
	release.OnClassic = false
	s.state.Lock()
	defer s.state.Unlock()
	// nothing is setup
	gadgetInfo := snaptest.MockInfo(c, `type: gadget
name: gadget`, nil)

	err := devicestate.CheckGadgetOrKernel(s.state, gadgetInfo, nil, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install gadget without model assertion`)

	// setup model assertion
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
	err = assertstate.Add(s.state, model)
	c.Assert(err, IsNil)
	err = auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	c.Assert(err, IsNil)

	err = devicestate.CheckGadgetOrKernel(s.state, gadgetInfo, nil, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install gadget "gadget", model assertion requests "pc"`)

	// install pc gadget
	pcGadgetInfo := snaptest.MockInfo(c, `type: gadget
name: pc`, nil)
	err = devicestate.CheckGadgetOrKernel(s.state, pcGadgetInfo, nil, snapstate.Flags{})
	c.Check(err, IsNil)
}

func (s *deviceMgrSuite) TestCheckKernel(c *C) {
	release.OnClassic = false
	s.state.Lock()
	defer s.state.Unlock()
	// nothing is setup
	kernelInfo := snaptest.MockInfo(c, `type: kernel
name: lnrk`, nil)

	err := devicestate.CheckGadgetOrKernel(s.state, kernelInfo, nil, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install kernel without model assertion`)

	// setup model assertion
	model, err := s.storeSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":       "16",
		"brand-id":     "canonical",
		"model":        "pc",
		"gadget":       "pc",
		"kernel":       "krnl",
		"architecture": "amd64",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, model)
	c.Assert(err, IsNil)
	err = auth.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	c.Assert(err, IsNil)

	err = devicestate.CheckGadgetOrKernel(s.state, kernelInfo, nil, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install kernel "lnrk", model assertion requests "krnl"`)

	// install krnl kernel
	krnlKernelInfo := snaptest.MockInfo(c, `type: kernel
name: krnl`, nil)
	err = devicestate.CheckGadgetOrKernel(s.state, krnlKernelInfo, nil, snapstate.Flags{})
	c.Check(err, IsNil)
}
