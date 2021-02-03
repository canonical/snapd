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

package daemon_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/state"
)

var _ = check.Suite(&apiValidationSetsSuite{})

type apiValidationSetsSuite struct {
	apiBaseSuite
}

func (s *apiValidationSetsSuite) SetUpTest(c *check.C) {
	s.apiBaseSuite.SetUpTest(c)
	d := s.daemon(c)
	d.Overlord().Loop()
	s.AddCleanup(func() { d.Overlord().Stop() })
}

func mockValidationSetsTracking(st *state.State) {
	st.Set("validation-sets", map[string]interface{}{
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

func (s *apiValidationSetsSuite) TestQueryValidationSetsErrors(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	mockValidationSetsTracking(st)
	st.Unlock()

	for i, tc := range []struct {
		validationSet string
		// sequence is normally an int, use string for passing invalid ones.
		sequence string
		message  string
		status   int
	}{
		{
			validationSet: "abc/Xfoo",
			message:       `invalid name "Xfoo"`,
			status:        400,
		},
		{
			validationSet: "Xfoo/bar",
			message:       `invalid account ID "Xfoo"`,
			status:        400,
		},
		{
			validationSet: "foo/foo",
			message:       "validation set not found",
			status:        404,
		},
		{
			validationSet: "foo/bar",
			sequence:      "1999",
			message:       "validation set not found",
			status:        404,
		},
		{
			validationSet: "foo/bar",
			sequence:      "x",
			message:       "invalid sequence argument",
			status:        400,
		},
		{
			validationSet: "foo/bar",
			sequence:      "-2",
			message:       "invalid sequence argument: -2",
			status:        400,
		},
	} {
		q := url.Values{}
		if tc.sequence != "" {
			q.Set("sequence", tc.sequence)
		}
		req, err := http.NewRequest("GET", fmt.Sprintf("/v2/validation-sets/%s?%s", tc.validationSet, q.Encode()), nil)
		c.Assert(err, check.IsNil)
		rsp := s.req(c, req, nil).(*daemon.Resp)
		c.Assert(rsp.Type, check.Equals, daemon.ResponseTypeError, check.Commentf("case #%d", i))
		c.Check(rsp.Status, check.Equals, tc.status, check.Commentf("case #%d", i))
		c.Check(rsp.ErrorResult().Message, check.Matches, tc.message)
	}
}

func (s *apiValidationSetsSuite) TestGetValidationSetsNone(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/validation-sets", nil)
	c.Assert(err, check.IsNil)

	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(rsp.Status, check.Equals, 200)
	res := rsp.Result.([]daemon.ValidationSetResult)
	c.Check(res, check.HasLen, 0)
}

func (s *apiValidationSetsSuite) TestListValidationSets(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/validation-sets", nil)
	c.Assert(err, check.IsNil)

	st := s.d.Overlord().State()
	st.Lock()
	mockValidationSetsTracking(st)
	st.Unlock()

	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(rsp.Status, check.Equals, 200)
	res := rsp.Result.([]daemon.ValidationSetResult)
	c.Check(res, check.DeepEquals, []daemon.ValidationSetResult{
		{
			AccountID: "foo",
			Name:      "bar",
			PinnedAt:  9,
			Mode:      "enforce",
			Sequence:  12,
			Valid:     false,
		},
		{
			AccountID: "foo",
			Name:      "baz",
			Mode:      "monitor",
			Sequence:  2,
			Valid:     false,
		},
	})
}

func (s *apiValidationSetsSuite) TestGetValidationSetOne(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/validation-sets/foo/bar", nil)
	c.Assert(err, check.IsNil)

	st := s.d.Overlord().State()
	st.Lock()
	mockValidationSetsTracking(st)
	st.Unlock()

	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(rsp.Status, check.Equals, 200)
	res := rsp.Result.(daemon.ValidationSetResult)
	c.Check(res, check.DeepEquals, daemon.ValidationSetResult{
		AccountID: "foo",
		Name:      "bar",
		PinnedAt:  9,
		Mode:      "enforce",
		Sequence:  12,
		Valid:     false,
	})
}

func (s *apiValidationSetsSuite) TestGetValidationSetPinned(c *check.C) {
	q := url.Values{}
	q.Set("sequence", "9")
	req, err := http.NewRequest("GET", "/v2/validation-sets/foo/bar?"+q.Encode(), nil)
	c.Assert(err, check.IsNil)

	st := s.d.Overlord().State()
	st.Lock()
	mockValidationSetsTracking(st)
	st.Unlock()

	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(rsp.Status, check.Equals, 200)
	res := rsp.Result.(daemon.ValidationSetResult)
	c.Check(res, check.DeepEquals, daemon.ValidationSetResult{
		AccountID: "foo",
		Name:      "bar",
		PinnedAt:  9,
		Mode:      "enforce",
		Sequence:  12,
		Valid:     false,
	})
}

func (s *apiValidationSetsSuite) TestGetValidationSetNotFound(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/validation-sets/foo/other", nil)
	c.Assert(err, check.IsNil)

	st := s.d.Overlord().State()
	st.Lock()
	mockValidationSetsTracking(st)
	st.Unlock()

	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(rsp.Status, check.Equals, 404)
	res := rsp.Result.(*daemon.ErrorResult)
	c.Assert(res, check.NotNil)
	c.Check(string(res.Kind), check.Equals, "validation-set-not-found")
	c.Check(res.Value, check.DeepEquals, map[string]interface{}{
		"account-id": "foo",
		"name":       "other",
	})
}

func (s *apiValidationSetsSuite) TestGetValidationSetPinnedNotFound(c *check.C) {
	q := url.Values{}
	q.Set("sequence", "333")
	req, err := http.NewRequest("GET", "/v2/validation-sets/foo/bar?"+q.Encode(), nil)
	c.Assert(err, check.IsNil)

	st := s.d.Overlord().State()
	st.Lock()
	mockValidationSetsTracking(st)
	st.Unlock()

	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(rsp.Status, check.Equals, 404)
	res := rsp.Result.(*daemon.ErrorResult)
	c.Assert(res, check.NotNil)
	c.Check(string(res.Kind), check.Equals, "validation-set-not-found")
	c.Check(res.Value, check.DeepEquals, map[string]interface{}{
		"account-id": "foo",
		"name":       "bar",
		"sequence":   333,
	})
}

func (s *apiValidationSetsSuite) TestApplyValidationSet(c *check.C) {
	st := s.d.Overlord().State()

	for _, tc := range []struct {
		mode         string
		sequence     int
		expectedMode assertstate.ValidationSetMode
	}{
		{
			mode:         "enforce",
			sequence:     12,
			expectedMode: assertstate.Enforce,
		},
		{
			mode:         "monitor",
			sequence:     99,
			expectedMode: assertstate.Monitor,
		},
		{
			mode:         "enforce",
			expectedMode: assertstate.Enforce,
		},
		{
			mode:         "monitor",
			expectedMode: assertstate.Monitor,
		},
	} {
		var body string
		if tc.sequence != 0 {
			body = fmt.Sprintf(`{"action":"apply","mode":"%s", "sequence":%d}`, tc.mode, tc.sequence)
		} else {
			body = fmt.Sprintf(`{"action":"apply","mode":"%s"}`, tc.mode)
		}

		req, err := http.NewRequest("POST", "/v2/validation-sets/foo/bar", strings.NewReader(body))
		c.Assert(err, check.IsNil)

		rsp := s.req(c, req, nil).(*daemon.Resp)
		c.Assert(rsp.Status, check.Equals, 200)

		var tr assertstate.ValidationSetTracking

		st.Lock()
		err = assertstate.GetValidationSet(st, "foo", "bar", &tr)
		st.Unlock()
		c.Assert(err, check.IsNil)
		c.Check(tr, check.DeepEquals, assertstate.ValidationSetTracking{
			AccountID: "foo",
			Name:      "bar",
			PinnedAt:  tc.sequence,
			Mode:      tc.expectedMode,
		})
	}
}

func (s *apiValidationSetsSuite) TestForgetValidationSet(c *check.C) {
	st := s.d.Overlord().State()

	for i, sequence := range []int{0, 9} {
		st.Lock()
		mockValidationSetsTracking(st)
		st.Unlock()

		var body string
		if sequence != 0 {
			body = fmt.Sprintf(`{"action":"forget", "sequence":%d}`, sequence)
		} else {
			body = fmt.Sprintf(`{"action":"forget"}`)
		}

		var tr assertstate.ValidationSetTracking

		st.Lock()
		// sanity, it exists before removing
		err := assertstate.GetValidationSet(st, "foo", "bar", &tr)
		st.Unlock()
		c.Assert(err, check.IsNil)
		c.Check(tr.AccountID, check.Equals, "foo")
		c.Check(tr.Name, check.Equals, "bar")

		req, err := http.NewRequest("POST", "/v2/validation-sets/foo/bar", strings.NewReader(body))
		c.Assert(err, check.IsNil)
		rsp := s.req(c, req, nil).(*daemon.Resp)
		c.Assert(rsp.Status, check.Equals, 200, check.Commentf("case #%d", i))

		// after forget it's removed
		st.Lock()
		err = assertstate.GetValidationSet(st, "foo", "bar", &tr)
		st.Unlock()
		c.Assert(err, check.Equals, state.ErrNoState)

		// and forget again fails
		req, err = http.NewRequest("POST", "/v2/validation-sets/foo/bar", strings.NewReader(body))
		c.Assert(err, check.IsNil)
		rsp = s.req(c, req, nil).(*daemon.Resp)
		c.Assert(rsp.Status, check.Equals, 404, check.Commentf("case #%d", i))
	}
}

func (s *apiValidationSetsSuite) TestApplyValidationSetsErrors(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	mockValidationSetsTracking(st)
	st.Unlock()

	for i, tc := range []struct {
		validationSet string
		mode          string
		// sequence is normally an int, use string for passing invalid ones.
		sequence string
		message  string
		status   int
	}{
		{
			validationSet: "0/zzz",
			mode:          "monitor",
			message:       `invalid account ID "0"`,
			status:        400,
		},
		{
			validationSet: "Xfoo/bar",
			mode:          "monitor",
			message:       `invalid account ID "Xfoo"`,
			status:        400,
		},
		{
			validationSet: "foo/Xabc",
			mode:          "monitor",
			message:       `invalid name "Xabc"`,
			status:        400,
		},
		{
			validationSet: "foo/bar",
			sequence:      "x",
			message:       "cannot decode request body into validation set action: invalid character 'x' looking for beginning of value",
			status:        400,
		},
		{
			validationSet: "foo/bar",
			mode:          "bad",
			message:       `invalid mode "bad"`,
			status:        400,
		},
		{
			validationSet: "foo/bar",
			sequence:      "-1",
			mode:          "monitor",
			message:       `invalid sequence argument: -1`,
			status:        400,
		},
	} {
		var body string
		if tc.sequence != "" {
			body = fmt.Sprintf(`{"action":"apply","mode":"%s", "sequence":%s}`, tc.mode, tc.sequence)
		} else {
			body = fmt.Sprintf(`{"action":"apply","mode":"%s"}`, tc.mode)
		}
		req, err := http.NewRequest("POST", fmt.Sprintf("/v2/validation-sets/%s", tc.validationSet), strings.NewReader(body))
		c.Assert(err, check.IsNil)
		rsp := s.req(c, req, nil).(*daemon.Resp)

		c.Check(rsp.Type, check.Equals, daemon.ResponseTypeError, check.Commentf("case #%d", i))
		c.Check(rsp.Status, check.Equals, tc.status, check.Commentf("case #%d", i))
		c.Check(rsp.ErrorResult().Message, check.Matches, tc.message)
	}
}

func (s *apiValidationSetsSuite) TestApplyValidationSetUnsupportedAction(c *check.C) {
	body := fmt.Sprintf(`{"action":"baz","mode":"monitor"}`)

	req, err := http.NewRequest("POST", "/v2/validation-sets/foo/bar", strings.NewReader(body))
	c.Assert(err, check.IsNil)

	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Check(rsp.Type, check.Equals, daemon.ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.ErrorResult().Message, check.Matches, `unsupported action "baz"`)
}
