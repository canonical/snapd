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
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
)

var (
	// TODO: allow to post assertions for UserOK? they are verified anyway
	assertsCmd = &Command{
		Path:        "/v2/assertions",
		GET:         getAssertTypeNames,
		POST:        doAssert,
		ReadAccess:  openAccess{},
		WriteAccess: authenticatedAccess{},
	}

	assertsFindManyCmd = &Command{
		Path:       "/v2/assertions/{assertType}",
		GET:        assertsFindMany,
		ReadAccess: openAccess{},
	}
)

// a helper type for parsing the options specified to /v2/assertions and other
// such endpoints that can either do JSON or assertion depending on the value
// of the the URL query parameters
type daemonAssertOptions struct {
	jsonResult  bool
	headersOnly bool
	remote      bool
	headers     map[string]string
}

// helper for parsing url query options into formatting option vars
func parseHeadersFormatOptionsFromURL(q url.Values) (*daemonAssertOptions, error) {
	res := daemonAssertOptions{}
	res.headers = make(map[string]string)
	for k := range q {
		v := q.Get(k)
		switch k {
		case "remote":
			switch v {
			case "true", "false":
				res.remote, _ = strconv.ParseBool(v)
			default:
				return nil, errors.New(`"remote" query parameter when used must be set to "true" or "false" or left unset`)
			}
		case "json":
			switch v {
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
		default:
			res.headers[k] = v
		}
	}

	return &res, nil
}

func getAssertTypeNames(c *Command, r *http.Request, user *auth.UserState) Response {
	return SyncResponse(map[string][]string{
		"types": asserts.TypeNames(),
	})

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

	if err := assertstate.AddBatch(state, batch, &asserts.CommitOptions{
		Precheck: true,
	}); err != nil {
		return BadRequest("assert failed: %v", err)
	}

	return SyncResponse(nil)
}

func assertsFindOneRemote(c *Command, at *asserts.AssertionType, headers map[string]string, user *auth.UserState) ([]asserts.Assertion, error) {
	primaryKeys, err := asserts.PrimaryKeyFromHeaders(at, headers)
	if err != nil {
		return nil, fmt.Errorf("cannot query remote assertion: %v", err)
	}
	sto := storeFrom(c.d)
	as, err := sto.Assertion(at, primaryKeys, user)
	if err != nil {
		return nil, err
	}

	return []asserts.Assertion{as}, nil
}

func assertsFindManyInState(c *Command, at *asserts.AssertionType, headers map[string]string, opts *daemonAssertOptions) ([]asserts.Assertion, error) {
	state := c.d.overlord.State()
	state.Lock()
	db := assertstate.DB(state)
	state.Unlock()

	return db.FindMany(at, opts.headers)
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

	var assertions []asserts.Assertion
	if opts.remote {
		assertions, err = assertsFindOneRemote(c, assertType, opts.headers, user)
	} else {
		assertions, err = assertsFindManyInState(c, assertType, opts.headers, opts)
	}
	if err != nil && !errors.Is(err, &asserts.NotFoundError{}) {
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
		return SyncResponse(assertsJSON)
	}

	return AssertResponse(assertions, true)
}
