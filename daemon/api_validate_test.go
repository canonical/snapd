// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/state"
)

func mockValidationSetsTracking(st *state.State) {
	st.Set("validation-set-tracking", map[string]interface{}{
		"foo/bar": map[string]interface{}{
			"account-id": "foo",
			"name":       "bar",
			"mode":       assertstate.Enforce,
			"pinned-at":  9,
			"current":    12,
		},
		"foo/baz": map[string]interface{}{
			"account-id": "foo",
			"name":       "baz",
			"mode":       assertstate.Monitor,
			"pinned-at":  0,
			"current":    2,
		},
	})
}

func (s *apiSuite) TestQueryValidationSetsErrors(c *check.C) {
	d := s.daemonWithOverlordMock(c)
	st := d.overlord.State()
	st.Lock()
	mockValidationSetsTracking(st)
	st.Unlock()

	for i, tc := range []struct {
		validationSet string
		// pinAt is normally an int, use string for passing invalid ones.
		pinAt   string
		message string
		status  int
	}{
		{
			validationSet: "ąśćź",
			message:       "invalid validation-set argument",
			status:        400,
		},
		{
			validationSet: "1foo/bar",
			message:       `invalid account name "1foo"`,
			status:        400,
		},
		{
			validationSet: "foo/1abc",
			message:       `invalid name "1abc"`,
			status:        400,
		},
		{
			validationSet: "foo/foo",
			message:       "validation set not found",
			status:        404,
		},
		{
			validationSet: "foo/bar",
			pinAt:         "1999",
			message:       "validation set not found",
			status:        404,
		},
		{
			validationSet: "foo/bar",
			pinAt:         "x",
			message:       "invalid pin-at argument",
			status:        400,
		},
	} {
		q := url.Values{}
		q.Set("validation-set", tc.validationSet)
		if tc.pinAt != "" {
			q.Set("pin-at", tc.pinAt)
		}
		req, err := http.NewRequest("GET", "/v2/validation-sets?"+q.Encode(), nil)
		c.Assert(err, check.IsNil)

		rsp := getValidationSets(validateCmd, req, nil).(*resp)

		c.Check(rsp.Type, check.Equals, ResponseTypeError, check.Commentf("case #%d", i))
		c.Check(rsp.Status, check.Equals, tc.status, check.Commentf("case #%d", i))
		c.Check(rsp.ErrorResult().Message, check.Matches, tc.message)
	}
}

func (s *apiSuite) TestQueryValidationSetsNone(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/validation-sets", nil)
	c.Assert(err, check.IsNil)

	s.daemonWithOverlordMock(c)
	rsp := getValidationSets(validateCmd, req, nil).(*resp)

	c.Assert(rsp.Status, check.Equals, 200)
	res := rsp.Result.([]validationSetResult)
	c.Check(res, check.HasLen, 0)
}

func (s *apiSuite) TestQueryValidationSets(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/validation-sets", nil)
	c.Assert(err, check.IsNil)

	d := s.daemonWithOverlordMock(c)
	st := d.overlord.State()
	st.Lock()
	mockValidationSetsTracking(st)
	st.Unlock()

	rsp := getValidationSets(validateCmd, req, nil).(*resp)

	c.Assert(rsp.Status, check.Equals, 200)
	res := rsp.Result.([]validationSetResult)
	c.Check(res, check.DeepEquals, []validationSetResult{
		{
			ValidationSet: "foo/bar=9",
			Mode:          "enforce",
			Seq:           12,
			Valid:         false,
		},
		{
			ValidationSet: "foo/baz",
			Mode:          "monitor",
			Seq:           2,
			Valid:         false,
		},
	})
}

func (s *apiSuite) TestQueryValidationSingleValidationSet(c *check.C) {
	q := url.Values{}
	q.Set("validation-set", "foo/bar")
	req, err := http.NewRequest("GET", "/v2/validation-sets?"+q.Encode(), nil)
	c.Assert(err, check.IsNil)

	d := s.daemonWithOverlordMock(c)
	st := d.overlord.State()
	st.Lock()
	mockValidationSetsTracking(st)
	st.Unlock()

	rsp := getValidationSets(validateCmd, req, nil).(*resp)

	c.Assert(rsp.Status, check.Equals, 200)
	res := rsp.Result.([]validationSetResult)
	c.Check(res, check.DeepEquals, []validationSetResult{
		{
			ValidationSet: "foo/bar=9",
			Mode:          "enforce",
			Seq:           12,
			Valid:         false,
		},
	})
}

func (s *apiSuite) TestQueryValidationSingleValidationSetPinned(c *check.C) {
	q := url.Values{}
	q.Set("validation-set", "foo/bar")
	q.Set("pin-at", "9")
	req, err := http.NewRequest("GET", "/v2/validation-sets?"+q.Encode(), nil)
	c.Assert(err, check.IsNil)

	d := s.daemonWithOverlordMock(c)
	st := d.overlord.State()
	st.Lock()
	mockValidationSetsTracking(st)
	st.Unlock()

	rsp := getValidationSets(validateCmd, req, nil).(*resp)

	c.Assert(rsp.Status, check.Equals, 200)
	res := rsp.Result.([]validationSetResult)
	c.Check(res, check.DeepEquals, []validationSetResult{
		{
			ValidationSet: "foo/bar=9",
			Mode:          "enforce",
			Seq:           12,
			Valid:         false,
		},
	})
}

func (s *apiSuite) TestQueryValidationSingleValidationSetNotFound(c *check.C) {
	q := url.Values{}
	q.Set("validation-set", "foo/other")
	req, err := http.NewRequest("GET", "/v2/validation-sets?"+q.Encode(), nil)
	c.Assert(err, check.IsNil)

	d := s.daemonWithOverlordMock(c)
	st := d.overlord.State()
	st.Lock()
	mockValidationSetsTracking(st)
	st.Unlock()

	rsp := getValidationSets(validateCmd, req, nil).(*resp)
	c.Assert(rsp.Status, check.Equals, 404)
	res := rsp.Result.(*errorResult)
	c.Assert(res, check.NotNil)
	c.Check(string(res.Kind), check.Equals, "validation-set-not-found")
	c.Check(res.Value, check.DeepEquals, "foo/other")
}

func (s *apiSuite) TestQueryValidationSingleValidationSetPinnedNotFound(c *check.C) {
	q := url.Values{}
	q.Set("validation-set", "foo/bar")
	q.Set("pin-at", "333")
	req, err := http.NewRequest("GET", "/v2/validation-sets?"+q.Encode(), nil)
	c.Assert(err, check.IsNil)

	d := s.daemonWithOverlordMock(c)
	st := d.overlord.State()
	st.Lock()
	mockValidationSetsTracking(st)
	st.Unlock()

	rsp := getValidationSets(validateCmd, req, nil).(*resp)
	c.Assert(rsp.Status, check.Equals, 404)
	res := rsp.Result.(*errorResult)
	c.Assert(res, check.NotNil)
	c.Check(string(res.Kind), check.Equals, "validation-set-not-found")
	c.Check(res.Value, check.DeepEquals, "foo/bar=333")
}

func (s *apiSuite) TestApplyValidationSet(c *check.C) {
	d := s.daemonWithOverlordMock(c)
	st := d.overlord.State()

	for _, tc := range []struct {
		flag  string
		pinAt int
		mode  assertstate.ValidationSetMode
	}{
		{
			flag:  "enforce",
			pinAt: 12,
			mode:  assertstate.Enforce,
		},
		{
			flag:  "monitor",
			pinAt: 99,
			mode:  assertstate.Monitor,
		},
		{
			flag: "enforce",
			mode: assertstate.Enforce,
		},
		{
			flag: "monitor",
			mode: assertstate.Monitor,
		},
	} {
		q := url.Values{}
		q.Set("validation-set", "foo/bar")

		var body string
		if tc.pinAt != 0 {
			body = fmt.Sprintf(`{"flag":"%s", "pin-at":%d}`, tc.flag, tc.pinAt)
		} else {
			body = fmt.Sprintf(`{"flag":"%s"}`, tc.flag)
		}
		req, err := http.NewRequest("POST", "/v2/validation-sets?"+q.Encode(), strings.NewReader(body))
		c.Assert(err, check.IsNil)

		rsp := applyValidationSets(validateCmd, req, nil).(*resp)
		c.Assert(rsp.Status, check.Equals, 200)

		var tr assertstate.ValidationSetTracking

		st.Lock()
		err = assertstate.GetValidationSet(st, "foo", "bar", &tr)
		st.Unlock()
		c.Assert(err, check.IsNil)
		c.Check(tr, check.DeepEquals, assertstate.ValidationSetTracking{
			AccountID: "foo",
			Name:      "bar",
			PinnedAt:  tc.pinAt,
			Mode:      tc.mode,
		})
	}
}

func (s *apiSuite) TestForgetValidationSet(c *check.C) {
	d := s.daemonWithOverlordMock(c)
	st := d.overlord.State()

	for i, pinAt := range []int{0, 9} {
		st.Lock()
		mockValidationSetsTracking(st)
		st.Unlock()

		q := url.Values{}
		q.Set("validation-set", "foo/bar")

		var body string
		if pinAt != 0 {
			body = fmt.Sprintf(`{"flag":"forget", "pin-at":%d}`, pinAt)
		} else {
			body = fmt.Sprintf(`{"flag":"forget"}`)
		}
		req, err := http.NewRequest("POST", "/v2/validation-sets?"+q.Encode(), strings.NewReader(body))
		c.Assert(err, check.IsNil)

		var tr assertstate.ValidationSetTracking

		st.Lock()
		// sanity, it exists before removing
		err = assertstate.GetValidationSet(st, "foo", "bar", &tr)
		st.Unlock()
		c.Assert(err, check.IsNil)
		c.Check(tr.AccountID, check.Equals, "foo")
		c.Check(tr.Name, check.Equals, "bar")

		rsp := applyValidationSets(validateCmd, req, nil).(*resp)
		c.Check(rsp.Status, check.Equals, 200, check.Commentf("case #%d", i))

		// after forget it's removed
		st.Lock()
		err = assertstate.GetValidationSet(st, "foo", "bar", &tr)
		st.Unlock()
		c.Check(err, check.Equals, state.ErrNoState)

		// and forget again fails
		req, err = http.NewRequest("POST", "/v2/validation-sets?"+q.Encode(), strings.NewReader(body))
		c.Assert(err, check.IsNil)
		rsp = applyValidationSets(validateCmd, req, nil).(*resp)
		c.Check(rsp.Status, check.Equals, 404, check.Commentf("case #%d", i))
	}
}

func (s *apiSuite) TestApplyValidationSetsErrors(c *check.C) {
	d := s.daemonWithOverlordMock(c)
	st := d.overlord.State()
	st.Lock()
	mockValidationSetsTracking(st)
	st.Unlock()

	for i, tc := range []struct {
		validationSet string
		flag          string
		// pinAt is normally an int, use string for passing invalid ones.
		pinAt   string
		message string
		status  int
	}{
		{
			validationSet: "ąśćź",
			flag:          "monitor",
			message:       "invalid validation-set argument",
			status:        400,
		},
		{
			validationSet: "1foo/bar",
			flag:          "monitor",
			message:       `invalid account name "1foo"`,
			status:        400,
		},
		{
			validationSet: "foo/1abc",
			flag:          "monitor",
			message:       `invalid name "1abc"`,
			status:        400,
		},
		{
			validationSet: "foo/bar",
			pinAt:         "x",
			message:       "cannot decode request body into validation set action: invalid character 'x' looking for beginning of value",
			status:        400,
		},
		{
			validationSet: "foo/bar",
			flag:          "bad",
			message:       `invalid mode "bad"`,
			status:        400,
		},
	} {
		q := url.Values{}
		q.Set("validation-set", tc.validationSet)
		if tc.pinAt != "" {
			q.Set("pin-at", tc.pinAt)
		}
		var body string
		if tc.pinAt != "" {
			body = fmt.Sprintf(`{"flag":"%s", "pin-at":%s}`, tc.flag, tc.pinAt)
		} else {
			body = fmt.Sprintf(`{"flag":"%s"}`, tc.flag)
		}
		req, err := http.NewRequest("POST", "/v2/validation-sets?"+q.Encode(), strings.NewReader(body))
		c.Assert(err, check.IsNil)
		rsp := applyValidationSets(validateCmd, req, nil).(*resp)

		c.Check(rsp.Type, check.Equals, ResponseTypeError, check.Commentf("case #%d", i))
		c.Check(rsp.Status, check.Equals, tc.status, check.Commentf("case #%d", i))
		c.Check(rsp.ErrorResult().Message, check.Matches, tc.message)
	}
}
