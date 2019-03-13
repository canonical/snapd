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
	"io"
	"net/http"
	"net/http/httptest"
	"sort"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/testutil"
)

type assertsSuite struct {
	d *daemon.Daemon
	o *overlord.Overlord

	storeSigning    *assertstest.StoreStack
	trustedRestorer func()
}

var _ = check.Suite(&assertsSuite{})

func (s *assertsSuite) SetUpTest(c *check.C) {
	dirs.SetRootDir(c.MkDir())

	s.o = overlord.Mock()
	s.d = daemon.NewWithOverlord(s.o)

	// adds an assertion db
	s.storeSigning = assertstest.NewStoreStack("can0nical", nil)
	s.trustedRestorer = sysdb.InjectTrusted(s.storeSigning.Trusted)

	assertstate.Manager(s.o.State(), s.o.TaskRunner())
}

func (s *assertsSuite) TearDownTest(c *check.C) {
	s.trustedRestorer()
	s.o = nil
	s.d = nil
}

func (s *assertsSuite) TestGetAsserts(c *check.C) {
	resp := daemon.GetAssertTypeNames(daemon.AssertsCmd, nil, nil)
	c.Check(resp.Status, check.Equals, 200)
	c.Check(resp.Type, check.Equals, daemon.ResponseTypeSync)
	c.Check(resp.Result, check.DeepEquals, map[string][]string{"types": asserts.TypeNames()})
}

func (s *assertsSuite) TestAssertOK(c *check.C) {
	// add store key
	st := s.o.State()
	daemon.AssertAdd(st, s.storeSigning.StoreAccountKey(""))

	acct := assertstest.NewAccount(s.storeSigning, "developer1", nil, "")
	buf := bytes.NewBuffer(asserts.Encode(acct))
	// Execute
	req, err := http.NewRequest("POST", "/v2/assertions", buf)
	c.Assert(err, check.IsNil)
	rsp := daemon.DoAssert(daemon.AssertsCmd, req, nil)
	// Verify (external)
	c.Check(rsp.Type, check.Equals, daemon.ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, 200)
	// Verify (internal)
	st.Lock()
	defer st.Unlock()
	_, err = assertstate.DB(st).Find(asserts.AccountType, map[string]string{
		"account-id": acct.AccountID(),
	})
	c.Check(err, check.IsNil)
}

func (s *assertsSuite) TestAssertStreamOK(c *check.C) {
	st := s.o.State()

	acct := assertstest.NewAccount(s.storeSigning, "developer1", nil, "")
	buf := &bytes.Buffer{}
	enc := asserts.NewEncoder(buf)
	err := enc.Encode(acct)
	c.Assert(err, check.IsNil)
	err = enc.Encode(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, check.IsNil)

	// Execute
	req, err := http.NewRequest("POST", "/v2/assertions", buf)
	c.Assert(err, check.IsNil)
	rsp := daemon.DoAssert(daemon.AssertsCmd, req, nil)
	// Verify (external)
	c.Check(rsp.Type, check.Equals, daemon.ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, 200)
	// Verify (internal)
	st.Lock()
	defer st.Unlock()
	_, err = assertstate.DB(st).Find(asserts.AccountType, map[string]string{
		"account-id": acct.AccountID(),
	})
	c.Check(err, check.IsNil)
}

func (s *assertsSuite) TestAssertInvalid(c *check.C) {
	// Setup
	buf := bytes.NewBufferString("blargh")
	req, err := http.NewRequest("POST", "/v2/assertions", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	// Execute
	daemon.AssertsCmd.POST(daemon.AssertsCmd, req, nil).ServeHTTP(rec, req)
	// Verify (external)
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rec.Body.String(), testutil.Contains,
		"cannot decode request body into assertions")
}

func (s *assertsSuite) TestAssertError(c *check.C) {
	// Setup
	acct := assertstest.NewAccount(s.storeSigning, "developer1", nil, "")
	buf := bytes.NewBuffer(asserts.Encode(acct))
	req, err := http.NewRequest("POST", "/v2/assertions", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	// Execute
	daemon.AssertsCmd.POST(daemon.AssertsCmd, req, nil).ServeHTTP(rec, req)
	// Verify (external)
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rec.Body.String(), testutil.Contains, "assert failed")
}

func (s *assertsSuite) TestAssertsFindManyAll(c *check.C) {
	// add store key
	st := s.o.State()
	daemon.AssertAdd(st, s.storeSigning.StoreAccountKey(""))

	acct := assertstest.NewAccount(s.storeSigning, "developer1", map[string]interface{}{
		"account-id": "developer1-id",
	}, "")
	daemon.AssertAdd(st, acct)

	// Execute
	req, err := http.NewRequest("POST", "/v2/assertions/account", nil)
	c.Assert(err, check.IsNil)
	defer daemon.MockMuxVars(func(*http.Request) map[string]string {
		return map[string]string{"assertType": "account"}
	})()

	rec := httptest.NewRecorder()
	daemon.AssertsFindManyCmd.GET(daemon.AssertsFindManyCmd, req, nil).ServeHTTP(rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, 200, check.Commentf("body %q", rec.Body))
	c.Check(rec.HeaderMap.Get("Content-Type"), check.Equals, "application/x.ubuntu.assertion; bundle=y")
	c.Check(rec.HeaderMap.Get("X-Ubuntu-Assertions-Count"), check.Equals, "4")
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
	st := s.o.State()
	// add store key
	daemon.AssertAdd(st, s.storeSigning.StoreAccountKey(""))

	acct := assertstest.NewAccount(s.storeSigning, "developer1", nil, "")
	daemon.AssertAdd(st, acct)

	// Execute
	req, err := http.NewRequest("POST", "/v2/assertions/account?username=developer1", nil)
	c.Assert(err, check.IsNil)
	defer daemon.MockMuxVars(func(*http.Request) map[string]string {
		return map[string]string{"assertType": "account"}
	})()

	rec := httptest.NewRecorder()
	daemon.AssertsFindManyCmd.GET(daemon.AssertsFindManyCmd, req, nil).ServeHTTP(rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, 200, check.Commentf("body %q", rec.Body))
	c.Check(rec.HeaderMap.Get("X-Ubuntu-Assertions-Count"), check.Equals, "1")
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
	// add store key
	st := s.o.State()
	daemon.AssertAdd(st, s.storeSigning.StoreAccountKey(""))

	acct := assertstest.NewAccount(s.storeSigning, "developer1", nil, "")
	daemon.AssertAdd(st, acct)

	// Execute
	req, err := http.NewRequest("POST", "/v2/assertions/account?username=xyzzyx", nil)
	c.Assert(err, check.IsNil)
	defer daemon.MockMuxVars(func(*http.Request) map[string]string {
		return map[string]string{"assertType": "account"}
	})()

	rec := httptest.NewRecorder()
	daemon.AssertsFindManyCmd.GET(daemon.AssertsFindManyCmd, req, nil).ServeHTTP(rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, 200, check.Commentf("body %q", rec.Body))
	c.Check(rec.HeaderMap.Get("X-Ubuntu-Assertions-Count"), check.Equals, "0")
	dec := asserts.NewDecoder(rec.Body)
	_, err = dec.Decode()
	c.Check(err, check.Equals, io.EOF)
}

func (s *assertsSuite) TestAssertsInvalidType(c *check.C) {
	// Execute
	req, err := http.NewRequest("POST", "/v2/assertions/foo", nil)
	c.Assert(err, check.IsNil)
	defer daemon.MockMuxVars(func(*http.Request) map[string]string {
		return map[string]string{"assertType": "foo"}
	})()

	rec := httptest.NewRecorder()
	daemon.AssertsFindManyCmd.GET(daemon.AssertsFindManyCmd, req, nil).ServeHTTP(rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rec.Body.String(), testutil.Contains, "invalid assert type")
}
