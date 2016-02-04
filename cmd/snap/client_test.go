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

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/client"
)

type Call struct {
	Fn   string
	Args []interface{}
}

// HighLevelTestClient a client compatible with snap commands.
// It allows observing and faking calls to the Client methods.  It does not use
// a real client.Client() so it is totally detached from the client package.
type HighLevelTestClient struct {
	Calls []Call
}

func NewHighLevelTestClient() *HighLevelTestClient {
	return &HighLevelTestClient{}
}

func (c *HighLevelTestClient) Grant(skillSnapName, skillName, slotSnapName, slotName string) error {
	call := Call{
		Fn:   "Grant",
		Args: []interface{}{skillSnapName, skillName, slotSnapName, slotName},
	}
	c.Calls = append(c.Calls, call)
	return nil
}

// LowLevelTestClient is a client compatible with snap commands.
// It allows observing and faking raw HTTP requests and responses.
// It is using a real client.Client() for all of the Client methods so it has
// the same behavior as a real client would, just with a fake server.
type LowLevelTestClient struct {
	*client.Client
	Request  *http.Request
	Response *http.Response
	err      error
}

func NewLowLevelTestClient() *LowLevelTestClient {
	lltc := &LowLevelTestClient{}
	lltc.Client = client.NewWithTransport(lltc)
	lltc.MakeCannedResponse(`{
		"type": "sync",
		"result": {}
	}`, 200)
	return lltc
}

func (lltc *LowLevelTestClient) RoundTrip(req *http.Request) (response *http.Response, err error) {
	lltc.Request = req
	response = lltc.Response
	err = lltc.err
	return
}

// MakeCannedResponse creates response that will be given by the client
func (lltc *LowLevelTestClient) MakeCannedResponse(body string, code int) {
	lltc.Response = &http.Response{
		Body:       ioutil.NopCloser(strings.NewReader(body)),
		StatusCode: code,
	}
	lltc.err = nil
}

// DecodedRequestBody returns the JSON-decoded body of the request
func (lltc *LowLevelTestClient) DecodedRequestBody(c *C) map[string]interface{} {
	var body map[string]interface{}
	decoder := json.NewDecoder(lltc.Request.Body)
	err := decoder.Decode(&body)
	c.Assert(err, IsNil)
	return body
}
