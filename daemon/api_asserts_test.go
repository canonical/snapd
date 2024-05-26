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

package daemon_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/testutil"
)

type assertsSuite struct {
	apiBaseSuite

	mockAssertionFn func(at *asserts.AssertionType, headers []string, user *auth.UserState) (asserts.Assertion, error)
}

var _ = check.Suite(&assertsSuite{})

func (s *assertsSuite) SetUpTest(c *check.C) {
	s.apiBaseSuite.SetUpTest(c)

	s.mockAssertionFn = nil

	s.daemonWithStore(c, s)
}

func (s *assertsSuite) TestGetAsserts(c *check.C) {
	req := mylog.Check2(http.NewRequest("GET", "/v2/assertions", nil))
	c.Assert(err, check.IsNil)
	resp := s.syncReq(c, req, nil)
	c.Check(resp.Status, check.Equals, 200)
	c.Check(resp.Result, check.DeepEquals, map[string][]string{"types": asserts.TypeNames()})
}

func (s *assertsSuite) addAsserts(assertions ...asserts.Assertion) {
	st := s.d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	assertstatetest.AddMany(st, s.StoreSigning.StoreAccountKey(""))
	assertstatetest.AddMany(st, assertions...)
}

func (s *assertsSuite) TestAssertOK(c *check.C) {
	// add store key
	s.addAsserts()

	st := s.d.Overlord().State()

	acct := assertstest.NewAccount(s.StoreSigning, "developer1", nil, "")
	buf := bytes.NewBuffer(asserts.Encode(acct))
	// Execute
	req := mylog.Check2(http.NewRequest("POST", "/v2/assertions", buf))
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil)
	// Verify (external)
	c.Check(rsp.Status, check.Equals, 200)
	// Verify (internal)
	st.Lock()
	defer st.Unlock()
	_ = mylog.Check2(assertstate.DB(st).Find(asserts.AccountType, map[string]string{
		"account-id": acct.AccountID(),
	}))
	c.Check(err, check.IsNil)
}

func (s *assertsSuite) TestAssertStreamOK(c *check.C) {
	st := s.d.Overlord().State()

	acct := assertstest.NewAccount(s.StoreSigning, "developer1", nil, "")
	buf := &bytes.Buffer{}
	enc := asserts.NewEncoder(buf)
	mylog.Check(enc.Encode(acct))
	c.Assert(err, check.IsNil)
	mylog.Check(enc.Encode(s.StoreSigning.StoreAccountKey("")))
	c.Assert(err, check.IsNil)

	// Execute
	req := mylog.Check2(http.NewRequest("POST", "/v2/assertions", buf))
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil)
	// Verify (external)
	c.Check(rsp.Status, check.Equals, 200)
	// Verify (internal)
	st.Lock()
	defer st.Unlock()
	_ = mylog.Check2(assertstate.DB(st).Find(asserts.AccountType, map[string]string{
		"account-id": acct.AccountID(),
	}))
	c.Check(err, check.IsNil)
}

func (s *assertsSuite) TestAssertInvalid(c *check.C) {
	// Setup
	buf := bytes.NewBufferString("blargh")
	req := mylog.Check2(http.NewRequest("POST", "/v2/assertions", buf))
	c.Assert(err, check.IsNil)
	s.asUserAuth(c, req)

	rec := httptest.NewRecorder()
	// Execute
	s.serveHTTP(c, rec, req)
	// Verify (external)
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rec.Body.String(), testutil.Contains,
		"cannot decode request body into assertions")
}

func (s *assertsSuite) TestAssertError(c *check.C) {
	// Setup
	acct := assertstest.NewAccount(s.StoreSigning, "developer1", nil, "")
	buf := bytes.NewBuffer(asserts.Encode(acct))
	req := mylog.Check2(http.NewRequest("POST", "/v2/assertions", buf))
	c.Assert(err, check.IsNil)
	s.asUserAuth(c, req)

	rec := httptest.NewRecorder()
	// Execute
	s.serveHTTP(c, rec, req)
	// Verify (external)
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rec.Body.String(), testutil.Contains, "assert failed")
}

func (s *assertsSuite) TestAssertsFindManyAll(c *check.C) {
	acct := assertstest.NewAccount(s.StoreSigning, "developer1", map[string]interface{}{
		"account-id": "developer1-id",
	}, "")
	s.addAsserts(acct)

	// Execute
	req, err := http.NewRequest("GET", "/v2/assertions/account", nil)
	c.Assert(err, check.IsNil)
	s.asUserAuth(c, req)

	rec := httptest.NewRecorder()
	s.serveHTTP(c, rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, 200, check.Commentf("body %q", rec.Body))
	c.Check(rec.Header().Get("Content-Type"), check.Equals, "application/x.ubuntu.assertion; bundle=y")
	c.Check(rec.Header().Get("X-Ubuntu-Assertions-Count"), check.Equals, "4")
	dec := asserts.NewDecoder(rec.Body)
	a1, err := dec.Decode()
	c.Assert(err, check.IsNil)
	c.Check(a1.Type(), check.Equals, asserts.AccountType)

	a2, err := dec.Decode()
	c.Assert(err, check.IsNil)

	a3, err := dec.Decode()
	c.Assert(err, check.IsNil)

	a4, err := dec.Decode()
	c.Assert(err, check.IsNil)

	_, err = dec.Decode()
	c.Assert(err, check.Equals, io.EOF)

	ids := []string{a1.(*asserts.Account).AccountID(), a2.(*asserts.Account).AccountID(), a3.(*asserts.Account).AccountID(), a4.(*asserts.Account).AccountID()}
	sort.Strings(ids)
	c.Check(ids, check.DeepEquals, []string{"can0nical", "canonical", "developer1-id", "generic"})
}

func (s *assertsSuite) TestAssertsFindManyFilter(c *check.C) {
	acct := assertstest.NewAccount(s.StoreSigning, "developer1", nil, "")
	s.addAsserts(acct)

	// Execute
	req, err := http.NewRequest("GET", "/v2/assertions/account?username=developer1", nil)
	c.Assert(err, check.IsNil)
	s.asUserAuth(c, req)

	rec := httptest.NewRecorder()
	s.serveHTTP(c, rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, 200, check.Commentf("body %q", rec.Body))
	c.Check(rec.Header().Get("X-Ubuntu-Assertions-Count"), check.Equals, "1")
	dec := asserts.NewDecoder(rec.Body)
	a1, err := dec.Decode()
	c.Assert(err, check.IsNil)
	c.Check(a1.Type(), check.Equals, asserts.AccountType)
	c.Check(a1.(*asserts.Account).Username(), check.Equals, "developer1")
	c.Check(a1.(*asserts.Account).AccountID(), check.Equals, acct.AccountID())
	_, err = dec.Decode()
	c.Check(err, check.Equals, io.EOF)
}

func (s *assertsSuite) TestAssertsFindManyNoResults(c *check.C) {
	acct := assertstest.NewAccount(s.StoreSigning, "developer1", nil, "")
	s.addAsserts(acct)

	// Execute
	req, err := http.NewRequest("GET", "/v2/assertions/account?username=xyzzyx", nil)
	c.Assert(err, check.IsNil)
	s.asUserAuth(c, req)

	rec := httptest.NewRecorder()
	s.serveHTTP(c, rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, 200, check.Commentf("body %q", rec.Body))
	c.Check(rec.Header().Get("X-Ubuntu-Assertions-Count"), check.Equals, "0")
	dec := asserts.NewDecoder(rec.Body)
	_, err = dec.Decode()
	c.Check(err, check.Equals, io.EOF)
}

func (s *assertsSuite) TestAssertsInvalidType(c *check.C) {
	// Execute
	req := mylog.Check2(http.NewRequest("GET", "/v2/assertions/foo", nil))
	c.Assert(err, check.IsNil)
	s.asUserAuth(c, req)

	rec := httptest.NewRecorder()
	s.serveHTTP(c, rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rec.Body.String(), testutil.Contains, "invalid assert type")
}

func (s *assertsSuite) TestAssertsFindManyJSONFilter(c *check.C) {
	s.testAssertsFindManyJSONFilter(c, "/v2/assertions/account?json=true&username=developer1")
}

func (s *assertsSuite) TestAssertsFindManyJSONFilterRemoteIsFalse(c *check.C) {
	// setting "remote=false" is the defalt and should not change anything
	s.testAssertsFindManyJSONFilter(c, "/v2/assertions/account?json=true&username=developer1&remote=false")
}

func (s *assertsSuite) testAssertsFindManyJSONFilter(c *check.C, urlPath string) {
	acct := assertstest.NewAccount(s.StoreSigning, "developer1", nil, "")
	s.addAsserts(acct)

	// Execute
	req := mylog.Check2(http.NewRequest("GET", urlPath, nil))
	c.Assert(err, check.IsNil)
	s.asUserAuth(c, req)

	rec := httptest.NewRecorder()
	s.serveHTTP(c, rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, 200, check.Commentf("body %q", rec.Body))
	c.Check(rec.Header().Get("Content-Type"), check.Equals, "application/json")

	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Assert(err, check.IsNil)
	c.Check(body["result"], check.DeepEquals, []interface{}{
		map[string]interface{}{
			"headers": acct.Headers(),
		},
	})
}

func (s *assertsSuite) TestAssertsFindManyJSONNoResults(c *check.C) {
	acct := assertstest.NewAccount(s.StoreSigning, "developer1", nil, "")
	s.addAsserts(acct)

	// Execute
	req := mylog.Check2(http.NewRequest("GET", "/v2/assertions/account?json=true&username=xyz", nil))
	c.Assert(err, check.IsNil)
	s.asUserAuth(c, req)

	rec := httptest.NewRecorder()
	s.serveHTTP(c, rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, 200, check.Commentf("body %q", rec.Body))
	c.Check(rec.Header().Get("Content-Type"), check.Equals, "application/json")

	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Assert(err, check.IsNil)
	c.Check(body["result"], check.DeepEquals, []interface{}{})
}

func (s *assertsSuite) TestAssertsFindManyJSONWithBody(c *check.C) {
	// add store key
	s.addAsserts()

	// Execute
	req := mylog.Check2(http.NewRequest("GET", "/v2/assertions/account-key?json=true", nil))
	c.Assert(err, check.IsNil)
	s.asUserAuth(c, req)

	rec := httptest.NewRecorder()
	s.serveHTTP(c, rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, 200, check.Commentf("body %q", rec.Body))
	c.Check(rec.Header().Get("Content-Type"), check.Equals, "application/json")

	var got []string
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Assert(err, check.IsNil)
	for _, a := range body["result"].([]interface{}) {
		h := a.(map[string]interface{})["headers"].(map[string]interface{})
		got = append(got, h["account-id"].(string)+"/"+h["name"].(string))
		// check body
		l := mylog.Check2(strconv.Atoi(h["body-length"].(string)))
		c.Assert(err, check.IsNil)
		c.Check(a.(map[string]interface{})["body"], check.HasLen, l)
	}
	sort.Strings(got)
	c.Check(got, check.DeepEquals, []string{"can0nical/root", "can0nical/store", "canonical/root", "generic/models"})
}

func (s *assertsSuite) TestAssertsFindManyJSONHeadersOnly(c *check.C) {
	// add store key
	s.addAsserts()

	// Execute
	req := mylog.Check2(http.NewRequest("GET", "/v2/assertions/account-key?json=headers&account-id=can0nical", nil))
	c.Assert(err, check.IsNil)
	s.asUserAuth(c, req)

	rec := httptest.NewRecorder()
	s.serveHTTP(c, rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, 200, check.Commentf("body %q", rec.Body))
	c.Check(rec.Header().Get("Content-Type"), check.Equals, "application/json")

	var got []string
	var body map[string]interface{}
	mylog.Check(json.Unmarshal(rec.Body.Bytes(), &body))
	c.Assert(err, check.IsNil)
	for _, a := range body["result"].([]interface{}) {
		h := a.(map[string]interface{})["headers"].(map[string]interface{})
		got = append(got, h["account-id"].(string)+"/"+h["name"].(string))
		// check body absent
		_, ok := a.(map[string]interface{})["body"]
		c.Assert(ok, check.Equals, false)
	}
	sort.Strings(got)
	c.Check(got, check.DeepEquals, []string{"can0nical/root", "can0nical/store"})
}

func (s *assertsSuite) TestAssertsFindManyJSONInvalidParam(c *check.C) {
	// add store key
	s.addAsserts()

	// Execute
	req := mylog.Check2(http.NewRequest("GET", "/v2/assertions/account-key?json=header&account-id=can0nical", nil))
	c.Assert(err, check.IsNil)
	s.asUserAuth(c, req)

	rec := httptest.NewRecorder()
	s.serveHTTP(c, rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, 400, check.Commentf("body %q", rec.Body))
	c.Check(rec.Header().Get("Content-Type"), check.Equals, "application/json")

	var rsp daemon.RespJSON
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), check.IsNil)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Type, check.Equals, daemon.ResponseTypeError)
	c.Check(rsp.Result, check.DeepEquals, map[string]interface{}{
		"message": `"json" query parameter when used must be set to "true" or "headers"`,
	})
}

func (s *assertsSuite) TestAssertsFindManyJSONNopFilter(c *check.C) {
	acct := assertstest.NewAccount(s.StoreSigning, "developer1", nil, "")
	s.addAsserts(acct)

	// Execute
	req, err := http.NewRequest("GET", "/v2/assertions/account?json=false&username=developer1", nil)
	c.Assert(err, check.IsNil)
	s.asUserAuth(c, req)

	rec := httptest.NewRecorder()
	s.serveHTTP(c, rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, 200, check.Commentf("body %q", rec.Body))
	c.Check(rec.Header().Get("X-Ubuntu-Assertions-Count"), check.Equals, "1")
	dec := asserts.NewDecoder(rec.Body)
	a1, err := dec.Decode()
	c.Assert(err, check.IsNil)
	c.Check(a1.Type(), check.Equals, asserts.AccountType)
	c.Check(a1.(*asserts.Account).Username(), check.Equals, "developer1")
	c.Check(a1.(*asserts.Account).AccountID(), check.Equals, acct.AccountID())
	_, err = dec.Decode()
	c.Check(err, check.Equals, io.EOF)
}

func (s *assertsSuite) TestAssertsFindManyRemoteInvalidParam(c *check.C) {
	// Execute
	req := mylog.Check2(http.NewRequest("GET", "/v2/assertions/account-key?remote=invalid&account-id=can0nical", nil))
	c.Assert(err, check.IsNil)
	s.asUserAuth(c, req)

	rec := httptest.NewRecorder()
	s.serveHTTP(c, rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, 400, check.Commentf("body %q", rec.Body))
	c.Check(rec.Header().Get("Content-Type"), check.Equals, "application/json")
	var rsp daemon.RespJSON
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), check.IsNil)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Type, check.Equals, daemon.ResponseTypeError)
	c.Check(rsp.Result, check.DeepEquals, map[string]interface{}{
		"message": `"remote" query parameter when used must be set to "true" or "false" or left unset`,
	})
}

func (s *assertsSuite) Assertion(at *asserts.AssertionType, headers []string, user *auth.UserState) (asserts.Assertion, error) {
	return s.mockAssertionFn(at, headers, user)
}

func (s *assertsSuite) TestAssertsFindManyRemote(c *check.C) {
	var assertFnCalled int
	s.mockAssertionFn = func(at *asserts.AssertionType, headers []string, user *auth.UserState) (asserts.Assertion, error) {
		assertFnCalled++
		c.Assert(at.Name, check.Equals, "account")
		c.Assert(headers, check.DeepEquals, []string{"can0nical"})
		return assertstest.NewAccount(s.StoreSigning, "some-developer", nil, ""), nil
	}

	acct := assertstest.NewAccount(s.StoreSigning, "developer1", nil, "")
	s.addAsserts(acct)

	// Execute
	req := mylog.Check2(http.NewRequest("GET", "/v2/assertions/account?remote=true&account-id=can0nical", nil))
	c.Assert(err, check.IsNil)
	s.asUserAuth(c, req)

	rec := httptest.NewRecorder()
	s.serveHTTP(c, rec, req)
	// Verify
	c.Check(assertFnCalled, check.Equals, 1)
	c.Check(rec.Code, check.Equals, 200, check.Commentf("body %q", rec.Body))
	c.Check(rec.Header().Get("Content-Type"), check.Equals, "application/x.ubuntu.assertion; bundle=y")

	data := rec.Body.Bytes()
	c.Check(string(data), check.Matches, `(?ms)type: account
authority-id: can0nical
account-id: [a-zA-Z0-9]+
display-name: Some-developer
timestamp: .*
username: some-developer
validation: unproven
.*
`)
}
