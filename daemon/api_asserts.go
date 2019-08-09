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
	"errors"
	"net/http"
	"net/url"

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

// a helper type for parsing the options specified to /v2/assertions and other
// such endpoints that can either do JSON or assertion depending on the value
// of the the URL query parameters
type daemonAssertFormatOptions struct {
	jsonResult  bool
	headersOnly bool
	headers     map[string]string
}

// helper for parsing url query options into formatting option vars
func parseHeadersFormatOptionsFromURL(q url.Values) (*daemonAssertFormatOptions, error) {
	res := daemonAssertFormatOptions{}
	res.headers = make(map[string]string)
	for k := range q {
		if k == "json" {
			switch q.Get(k) {
			case "false":
				res.jsonResult = false
			case "headers":
				res.headersOnly = true
				fallthrough
			case "true":
				res.jsonResult = true
			default:
				return nil, errors.New(`"json" query parameter when used must be set to "true" or "headers"`)
			}
			continue
		}
		res.headers[k] = q.Get(k)
	}

	return &res, nil
}

func getAssertTypeNames(c *Command, r *http.Request, user *auth.UserState) Response {
	return SyncResponse(map[string][]string{
		"types": asserts.TypeNames(),
	}, nil)
}

func doAssert(c *Command, r *http.Request, user *auth.UserState) Response {
	batch := assertstate.NewBatch()
	_, err := batch.AddStream(r.Body)
	if err != nil {
		return BadRequest("cannot decode request body into assertions: %v", err)
	}

	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()

	if err := batch.Commit(state); err != nil {
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
	opts, err := parseHeadersFormatOptionsFromURL(r.URL.Query())
	if err != nil {
		return BadRequest(err.Error())
	}
	state := c.d.overlord.State()
	state.Lock()
	db := assertstate.DB(state)
	state.Unlock()

	assertions, err := db.FindMany(assertType, opts.headers)
	if err != nil && !asserts.IsNotFound(err) {
		return InternalError("searching assertions failed: %v", err)
	}

	if opts.jsonResult {
		assertsJSON := make([]struct {
			Headers map[string]interface{} `json:"headers,omitempty"`
			Body    string                 `json:"body,omitempty"`
		}, len(assertions))
		for i := range assertions {
			assertsJSON[i].Headers = assertions[i].Headers()
			if !opts.headersOnly {
				assertsJSON[i].Body = string(assertions[i].Body())
			}
		}
		return SyncResponse(assertsJSON, nil)
	}

	return AssertResponse(assertions, true)
}
