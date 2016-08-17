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
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
)

func TestDeviceManager(t *testing.T) { TestingT(t) }

type deviceMgrSuite struct {
	state *state.State
	mgr   *devicestate.DeviceManager

	storeSigning *assertstest.StoreStack
}

var _ = Suite(&deviceMgrSuite{})

func (s *deviceMgrSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	rootPrivKey, _ := assertstest.GenerateKey(1024)
	storePrivKey, _ := assertstest.GenerateKey(752)
	s.storeSigning = assertstest.NewStoreStack("canonical", rootPrivKey, storePrivKey)

	s.state = state.New(nil)
	mgr, err := devicestate.Manager(s.state)
	c.Assert(err, IsNil)

	s.mgr = mgr
}

func (s *deviceMgrSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *deviceMgrSuite) settle() {
	for i := 0; i < 50; i++ {
		s.mgr.Ensure()
		s.mgr.Wait()
	}
}

func (s *deviceMgrSuite) TestFullDeviceRegistrationHappyOnClassic(c *C) {
	r1 := release.MockOnClassic(true)
	defer r1()
	r2 := devicestate.MockKeyLength(752)
	defer r2()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, `{"request-id": "REQID-1"}`)
		case "POST":
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
			serial, err := s.storeSigning.Sign(asserts.SerialType, map[string]interface{}{
				"brand-id":   "canonical",
				"model":      "pc",
				"serial":     "9999",
				"device-key": serialReq.HeaderString("device-key"),
				"timestamp":  time.Now().Format(time.RFC3339),
			}, nil, "")
			c.Assert(err, IsNil)
			w.Header().Set("Content-Type", asserts.MediaType)
			w.WriteHeader(http.StatusOK)
			w.Write(asserts.Encode(serial))
		}
	}))
	defer mockServer.Close()

	r3 := devicestate.MockSerialRequestURL(mockServer.URL)
	defer r3()

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

	devId, err := auth.Device(s.state)
	c.Assert(err, IsNil)
	c.Check(devId.Brand, Equals, "canonical")
	c.Check(devId.Model, Equals, "pc")
	c.Check(devId.Serial, Equals, "9999")

	// TODO: check we now have a device key and serial assertion
}
