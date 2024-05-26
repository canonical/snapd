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
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snapdenv"
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

func MockDeviceService(c *C, bhv *DeviceServiceBehavior) (mockServer *httptest.Server, extraPemEncodedCerts []byte) {
	expectedUserAgent := snapdenv.UserAgent()

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
	// TODO: extract handler func
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// check.Assert here will produce harder to understand failure
		// modes

		switch r.Method {
		default:
			c.Errorf("unexpected verb %q", r.Method)
			w.WriteHeader(500)
			return
		case "HEAD":
			if r.URL.Path != "/" {
				c.Errorf("unexpected HEAD request %q", r.URL.String())
				w.WriteHeader(500)
				return
			}
			if bhv.Head != nil {
				bhv.Head(c, bhv, w, r)
			} else {
				w.WriteHeader(200)
			}
			return
		case "POST":
			// carry on
		}

		if bhv.PostPreflight != nil {
			bhv.PostPreflight(c, bhv, w, r)
		}

		switch r.URL.Path {
		default:
			c.Errorf("unexpected POST request %q", r.URL.String())
			w.WriteHeader(500)
			return
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

			a := mylog.Check2(dec.Decode())

			serialReq, ok := a.(*asserts.SerialRequest)
			if !ok {
				w.WriteHeader(400)
				w.Write([]byte(`{
  "error_list": [{"message": "expected serial-request"}]
}`))
				return
			}
			extra := []asserts.Assertion{}
			for {
				a1, err := dec.Decode()
				if err == io.EOF {
					break
				}

				extra = append(extra, a1)
			}
			mylog.Check(asserts.SignatureCheck(serialReq, serialReq.DeviceKey()))
			c.Check(err, IsNil)

			// also return response to client

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
				if len(extra) != 2 {
					w.WriteHeader(400)
					w.Write([]byte(`{
  "error_list": [{"message": "expected model and original serial"}]
}`))
					return
				}
				_, ok := extra[0].(*asserts.Model)
				if !ok {
					w.WriteHeader(400)
					w.Write([]byte(`{
  "error_list": [{"message": "expected model"}]
}`))
					return
				}
				origSerial, ok := extra[1].(*asserts.Serial)
				if !ok {
					w.WriteHeader(400)
					w.Write([]byte(`{
  "error_list": [{"message": "expected model"}]
}`))
				}
				c.Check(origSerial.DeviceKey(), DeepEquals, serialReq.DeviceKey())
				// TODO: more checks once we have Original* accessors
			} else {

				mod, ok := extra[0].(*asserts.Model)
				if !ok {
					w.WriteHeader(400)
					w.Write([]byte(`{
  "error_list": [{"message": "expected model"}]
}`))
					return
				}
				c.Check(mod.BrandID(), Equals, brandID)
				c.Check(mod.Model(), Equals, model)
			}
			serial, ancillary := mylog.Check3(bhv.SignSerial(c, bhv, map[string]interface{}{
				"authority-id":        "canonical",
				"brand-id":            brandID,
				"model":               model,
				"serial":              serialStr,
				"device-key":          serialReq.HeaderString("device-key"),
				"device-key-sha3-384": serialReq.SignKeyID(),
				"timestamp":           time.Now().Format(time.RFC3339),
			}, serialReq.Body()))
			c.Check(err, IsNil)

			// also return response to client

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
	pemEncodedCerts := bytes.NewBuffer(nil)
	for _, c1 := range server.TLS.Certificates {
		block := &pem.Block{
			Type:  "CERTIFICATE",
			Bytes: c1.Certificate[0],
		}
		mylog.Check(pem.Encode(pemEncodedCerts, block))

	}
	return server, pemEncodedCerts.Bytes()
}
