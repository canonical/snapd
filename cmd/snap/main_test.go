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
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/client"
	. "github.com/ubuntu-core/snappy/cmd/snap"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type SnapSuite struct {
	client   *client.Client
	request  *http.Request
	response *http.Response
	err      error
}

var _ = Suite(&SnapSuite{})

func (s *SnapSuite) SetUpSuite(c *C) {
	s.client = client.NewWithTransport(s)
	SetFakeClient(s.client)
}

func (s *SnapSuite) SetUpTest(c *C) {
	s.MakeCannedResponse(`{
		"type": "sync",
		"result": {}
	}`, 200)
}

func (s *SnapSuite) RoundTrip(req *http.Request) (response *http.Response, err error) {
	s.request = req
	response = s.response
	err = s.err
	return
}

// MakeCannedResponse creates response that will be given by the client
func (s *SnapSuite) MakeCannedResponse(body string, code int) {
	s.response = &http.Response{
		Body:       ioutil.NopCloser(strings.NewReader(body)),
		StatusCode: code,
	}
	s.err = nil
}

// DecodedRequestBody returns the JSON-decoded body of the request
func (s *SnapSuite) DecodedRequestBody(c *C) map[string]interface{} {
	var body map[string]interface{}
	decoder := json.NewDecoder(s.request.Body)
	err := decoder.Decode(&body)
	c.Assert(err, IsNil)
	return body
}

// Execute runs snappy as if invoked on command line
func (s *SnapSuite) Execute(args []string) error {
	_, err := Parser().ParseArgs(args)
	return err
}
