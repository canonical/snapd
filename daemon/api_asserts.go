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

package daemon

import (
	"net/http"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
)

var (
	// TODO: allow to post assertions for UserOK? they are verified anyway
	assertsCmd = &Command{
		Path:   "/v2/assertions",
		UserOK: true,
		GET:    getAssertTypeNames,
		POST:   doAssert,
	}

	assertsFindManyCmd = &Command{
		Path:   "/v2/assertions/{assertType}",
		UserOK: true,
		GET:    assertsFindMany,
	}
)

func getAssertTypeNames(c *Command, r *http.Request, user *auth.UserState) Response {
	return SyncResponse(map[string][]string{
		"types": asserts.TypeNames(),
	}, nil)
}

func doAssert(c *Command, r *http.Request, user *auth.UserState) Response {
	batch := asserts.NewBatch(nil)
	_, err := batch.AddStream(r.Body)
	if err != nil {
		return BadRequest("cannot decode request body into assertions: %v", err)
	}

	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()

	if err := assertstate.AddBatch(state, batch, &assertstate.AddBatchOptions{
		Precheck: true,
	}); err != nil {
		return BadRequest("assert failed: %v", err)
	}
	// TODO: what more info do we want to return on success?
	return &resp{
		Type:   ResponseTypeSync,
		Status: 200,
	}
}

func assertsFindMany(c *Command, r *http.Request, user *auth.UserState) Response {
	assertTypeName := muxVars(r)["assertType"]
	assertType := asserts.Type(assertTypeName)
	if assertType == nil {
		return BadRequest("invalid assert type: %q", assertTypeName)
	}
	jsonResult := false
	headersOnly := false
	headers := map[string]string{}
	q := r.URL.Query()
	for k := range q {
		if k == "json" {
			switch q.Get(k) {
			case "false":
				jsonResult = false
			case "headers":
				headersOnly = true
				fallthrough
			case "true":
				jsonResult = true
			default:
				return BadRequest(`"json" query parameter when used must be set to "true" or "headers"`)
			}
			continue
		}
		headers[k] = q.Get(k)
	}

	state := c.d.overlord.State()
	state.Lock()
	db := assertstate.DB(state)
	state.Unlock()

	assertions, err := db.FindMany(assertType, headers)
	if err != nil && !asserts.IsNotFound(err) {
		return InternalError("searching assertions failed: %v", err)
	}

	if jsonResult {
		assertsJSON := make([]struct {
			Headers map[string]interface{} `json:"headers,omitempty"`
			Body    string                 `json:"body,omitempty"`
		}, len(assertions))
		for i := range assertions {
			assertsJSON[i].Headers = assertions[i].Headers()
			if !headersOnly {
				assertsJSON[i].Body = string(assertions[i].Body())
			}
		}
		return SyncResponse(assertsJSON, nil)
	}

	return AssertResponse(assertions, true)
}
