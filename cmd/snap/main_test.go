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

package main_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	. "gopkg.in/check.v1"

	. "github.com/ubuntu-core/snappy/cmd/snap"
	"github.com/ubuntu-core/snappy/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type SnapSuite struct {
	testutil.BaseTest
	stdout *bytes.Buffer
	stderr *bytes.Buffer
}

var _ = Suite(&SnapSuite{})

func (s *SnapSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.stdout = bytes.NewBuffer(nil)
	s.stderr = bytes.NewBuffer(nil)
	Stdout = s.stdout
	Stderr = s.stderr
}

func (s *SnapSuite) TearDownTest(c *C) {
	Stdout = os.Stdout
	Stderr = os.Stderr
	s.BaseTest.TearDownTest(c)
}

func (s *SnapSuite) Stdout() string {
	return s.stdout.String()
}

func (s *SnapSuite) Stderr() string {
	return s.stderr.String()
}

func (s *SnapSuite) RedirectClientToTestServer(handler func(http.ResponseWriter, *http.Request)) {
	server := httptest.NewServer(http.HandlerFunc(handler))
	s.BaseTest.AddCleanup(func() { server.Close() })
	ClientConfig.BaseURL = server.URL
	s.BaseTest.AddCleanup(func() { ClientConfig.BaseURL = "" })
}

// DecodedRequestBody returns the JSON-decoded body of the request.
func DecodedRequestBody(c *C, r *http.Request) map[string]interface{} {
	var body map[string]interface{}
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&body)
	c.Assert(err, IsNil)
	return body
}

// EncodeResponseBody writes JSON-serialized body to the response writer.
func EncodeResponseBody(c *C, w http.ResponseWriter, body interface{}) {
	encoder := json.NewEncoder(w)
	err := encoder.Encode(body)
	c.Assert(err, IsNil)
}
