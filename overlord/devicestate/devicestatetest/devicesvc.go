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

package devicestatetest

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/httputil"
)

type DeviceServiceBehavior struct {
	ReqID string

	RequestIDURLPath     string
	SerialURLPath        string
	ExpectedCapabilities string

	Head          func(c *C, bhv *DeviceServiceBehavior, w http.ResponseWriter, r *http.Request)
	PostPreflight func(c *C, bhv *DeviceServiceBehavior, w http.ResponseWriter, r *http.Request)

	SignSerial func(c *C, bhv *DeviceServiceBehavior, headers map[string]interface{}, body []byte) (serial asserts.Assertion, ancillary []asserts.Assertion, err error)
}

// Request IDs for hard-coded behaviors.
const (
	ReqIDFailID501          = "REQID-FAIL-ID-501"
	ReqIDBadRequest         = "REQID-BAD-REQ"
	ReqIDPoll               = "REQID-POLL"
	ReqIDSerialWithBadModel = "REQID-SERIAL-W-BAD-MODEL"
)

const (
	requestIDURLPath = "/api/v1/snaps/auth/request-id"
	serialURLPath    = "/api/v1/snaps/auth/devices"
)

func MockDeviceService(c *C, bhv *DeviceServiceBehavior) *httptest.Server {
	expectedUserAgent := httputil.UserAgent()

	// default URL paths
	if bhv.RequestIDURLPath == "" {
		bhv.RequestIDURLPath = requestIDURLPath
		bhv.SerialURLPath = serialURLPath
	}
	// currently supported
	if bhv.ExpectedCapabilities == "" {
		bhv.ExpectedCapabilities = "serial-stream"
	}

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
			if bhv.Head != nil {
				bhv.Head(c, bhv, w, r)
			}
			w.WriteHeader(200)
			return
		case "POST":
			// carry on
		}

		if bhv.PostPreflight != nil {
			bhv.PostPreflight(c, bhv, w, r)
		}

		switch r.URL.Path {
		default:
			c.Fatalf("unexpected POST request %q", r.URL.String())
		case bhv.RequestIDURLPath:
			if bhv.ReqID == ReqIDFailID501 {
				w.WriteHeader(501)
				return
			}
			w.WriteHeader(200)
			c.Check(r.Header.Get("User-Agent"), Equals, expectedUserAgent)
			io.WriteString(w, fmt.Sprintf(`{"request-id": "%s"}`, bhv.ReqID))
		case bhv.SerialURLPath:
			c.Check(r.Header.Get("User-Agent"), Equals, expectedUserAgent)
			c.Check(r.Header.Get("Snap-Device-Capabilities"), Equals, bhv.ExpectedCapabilities)

			mu.Lock()
			serialNum := 9999 + count
			count++
			mu.Unlock()

			dec := asserts.NewDecoder(r.Body)

			a, err := dec.Decode()
			c.Assert(err, IsNil)
			serialReq, ok := a.(*asserts.SerialRequest)
			c.Assert(ok, Equals, true)
			extra := []asserts.Assertion{}
			for {
				a1, err := dec.Decode()
				if err == io.EOF {
					break
				}
				c.Assert(err, IsNil)
				extra = append(extra, a1)
			}
			err = asserts.SignatureCheck(serialReq, serialReq.DeviceKey())
			c.Assert(err, IsNil)
			brandID := serialReq.BrandID()
			model := serialReq.Model()
			reqID := serialReq.RequestID()
			if reqID == ReqIDBadRequest {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(400)
				w.Write([]byte(`{
  "error_list": [{"message": "bad serial-request"}]
}`))
				return
			}
			if reqID == ReqIDPoll && serialNum != 10002 {
				w.WriteHeader(202)
				return
			}
			serialStr := fmt.Sprintf("%d", serialNum)
			if serialReq.Serial() != "" {
				// use proposed serial
				serialStr = serialReq.Serial()
			}
			if serialReq.HeaderString("original-model") != "" {
				// re-registration
				c.Check(extra, HasLen, 2)
				_, ok := extra[0].(*asserts.Model)
				c.Check(ok, Equals, true)
				origSerial, ok := extra[1].(*asserts.Serial)
				c.Check(ok, Equals, true)
				c.Check(origSerial.DeviceKey(), DeepEquals, serialReq.DeviceKey())
				// TODO: more checks once we have Original* accessors
			} else {
				c.Check(extra, HasLen, 0)
			}
			serial, ancillary, err := bhv.SignSerial(c, bhv, map[string]interface{}{
				"authority-id":        "canonical",
				"brand-id":            brandID,
				"model":               model,
				"serial":              serialStr,
				"device-key":          serialReq.HeaderString("device-key"),
				"device-key-sha3-384": serialReq.SignKeyID(),
				"timestamp":           time.Now().Format(time.RFC3339),
			}, serialReq.Body())
			c.Assert(err, IsNil)
			w.Header().Set("Content-Type", asserts.MediaType)
			w.WriteHeader(200)
			if reqID == ReqIDSerialWithBadModel {
				encoded := asserts.Encode(serial)

				encoded = bytes.Replace(encoded, []byte("model: pc"), []byte("model: bad-model-foo"), 1)
				w.Write(encoded)
				return
			}
			enc := asserts.NewEncoder(w)
			enc.Encode(serial)
			for _, a := range ancillary {
				enc.Encode(a)
			}
		}
	}))
}
