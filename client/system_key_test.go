// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package client_test

import (
	"encoding/json"
	"errors"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
)

type mockSystemKey string

func (m mockSystemKey) String() string { return string(m) }

func (cs *clientSuite) TestSystemKeyMismatchProceed(c *check.C) {
	cs.rsp = `{
		"result": null,
		"status": "OK",
		"status-code": 200,
		"type": "sync"
	}`

	adv, err := cs.cli.SystemKeyMismatchAdvice(mockSystemKey(`{"version":1,"system":"key"}`))
	c.Assert(err, check.IsNil)
	c.Check(adv, check.DeepEquals, &client.AdvisedAction{
		SuggestedAction: "proceed",
	})

	c.Assert(cs.reqs, check.HasLen, 1)
	req := cs.reqs[0]

	c.Check(req.Method, check.Equals, "POST")
	c.Check(req.URL.Query(), check.HasLen, 0)
	c.Check(req.Header.Get("Content-Type"), check.Equals, "application/json")
	var b map[string]any
	d := json.NewDecoder(req.Body)
	c.Assert(d.Decode(&b), check.IsNil)
	c.Check(d.More(), check.Equals, false)

	c.Check(b, check.DeepEquals, map[string]any{
		"action":     "advise-system-key-mismatch",
		"system-key": `{"version":1,"system":"key"}`,
	})
}

func (cs *clientSuite) TestSystemKeyMismatchAPIError(c *check.C) {
	cs.status = 500
	cs.rsp = `{
		"result": {
			"message": "oops fail"
		},
		"status": "Internal Server Error",
		"status-code": 500,
		"type": "error"
	}`

	// actual response from the API
	adv, err := cs.cli.SystemKeyMismatchAdvice(mockSystemKey("system-key"))
	c.Assert(err, check.NotNil)
	var respError *client.Error
	c.Assert(errors.As(err, &respError), check.Equals, true)
	c.Check(adv, check.IsNil)
	c.Assert(cs.reqs, check.HasLen, 1)
}

func (cs *clientSuite) TestSystemKeyLowLevelError(c *check.C) {
	cs.err = errors.New("nope")

	adv, err := cs.cli.SystemKeyMismatchAdvice(mockSystemKey("system-key"))
	c.Assert(err, check.ErrorMatches, "cannot communicate with server: nope")
	var respError *client.Error
	c.Assert(errors.As(err, &respError), check.Equals, false)
	c.Check(adv, check.IsNil)
	c.Assert(cs.reqs, check.HasLen, 1)
}

func (cs *clientSuite) TestSystemKeyMismatchNotSupported(c *check.C) {
	cs.status = 405 // Method Not Allowed

	// actual response from the API
	adv, err := cs.cli.SystemKeyMismatchAdvice(mockSystemKey("system-key"))
	c.Assert(err, check.NotNil)
	// a very specific error
	c.Assert(errors.Is(err, client.ErrMismatchAdviceUnsupported), check.Equals, true)
	c.Check(adv, check.IsNil)
	c.Assert(cs.reqs, check.HasLen, 1)
}

func (cs *clientSuite) TestSystemKeyAwaitChange(c *check.C) {
	cs.rsp = `{
		"result": null,
		"status": "OK",
		"status-code": 202,
		"type": "async",
		"change": "1234"
	}`

	adv, err := cs.cli.SystemKeyMismatchAdvice(mockSystemKey("system-key"))
	c.Assert(err, check.IsNil)
	c.Check(adv, check.DeepEquals, &client.AdvisedAction{
		SuggestedAction: "wait-for-change",
		ChangeID:        "1234",
	})
	c.Assert(cs.reqs, check.HasLen, 1)
}

func (cs *clientSuite) TestSystemKeyMisuse(c *check.C) {
	adv, err := cs.cli.SystemKeyMismatchAdvice(nil)
	c.Assert(err, check.ErrorMatches, "cannot be marshaled as system key")
	c.Check(adv, check.IsNil)
	c.Assert(cs.doCalls, check.Equals, 0)

	adv, err = cs.cli.SystemKeyMismatchAdvice(mockSystemKey(""))
	c.Assert(err, check.ErrorMatches, "no system key provided")
	c.Check(adv, check.IsNil)
	c.Assert(cs.doCalls, check.Equals, 0)
}

func (cs *clientSuite) TestSystemKeyRspType(c *check.C) {
	cs.rsp = `{
		"result": null,
		"status": "OK",
		"status-code": 202,
		"type": "bang"
	}`

	adv, err := cs.cli.SystemKeyMismatchAdvice(mockSystemKey("system-key"))
	c.Assert(err, check.ErrorMatches, `unexpected response for "POST" on "/v2/system-info", got "bang"`)
	c.Check(adv, check.IsNil)
	c.Assert(cs.reqs, check.HasLen, 1)
}
