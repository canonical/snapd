// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type clientSuite struct {
	cli     *client.Client
	req     *http.Request
	reqs    []*http.Request
	rsp     string
	rsps    []string
	err     error
	doCalls int
	header  http.Header
	status  int
}

var _ = Suite(&clientSuite{})

func (cs *clientSuite) SetUpTest(c *C) {
	os.Setenv(client.TestAuthFileEnvKey, filepath.Join(c.MkDir(), "auth.json"))
	cs.cli = client.New(nil)
	cs.cli.SetDoer(cs)
	cs.err = nil
	cs.req = nil
	cs.reqs = nil
	cs.rsp = ""
	cs.rsps = nil
	cs.req = nil
	cs.header = nil
	cs.status = http.StatusOK
	cs.doCalls = 0

	dirs.SetRootDir(c.MkDir())
}

func (cs *clientSuite) TearDownTest(c *C) {
	os.Unsetenv(client.TestAuthFileEnvKey)
}

func (cs *clientSuite) Do(req *http.Request) (*http.Response, error) {
	cs.req = req
	cs.reqs = append(cs.reqs, req)
	body := cs.rsp
	if cs.doCalls < len(cs.rsps) {
		body = cs.rsps[cs.doCalls]
	}
	rsp := &http.Response{
		Body:       ioutil.NopCloser(strings.NewReader(body)),
		Header:     cs.header,
		StatusCode: cs.status,
	}
	cs.doCalls++
	return rsp, cs.err
}

func (cs *clientSuite) TestNewPanics(c *C) {
	c.Assert(func() {
		client.New(&client.Config{BaseURL: ":"})
	}, PanicMatches, `cannot parse server base URL: ":" \(parse :: missing protocol scheme\)`)
}

func (cs *clientSuite) TestClientDoReportsErrors(c *C) {
	restore := client.MockDoRetry(10*time.Millisecond, 100*time.Millisecond)
	defer restore()
	cs.err = errors.New("ouchie")
	err := cs.cli.Do("GET", "/", nil, nil, nil)
	c.Check(err, ErrorMatches, "cannot communicate with server: ouchie")
	if cs.doCalls < 2 {
		c.Fatalf("do did not retry")
	}
}

func (cs *clientSuite) TestClientWorks(c *C) {
	var v []int
	cs.rsp = `[1,2]`
	reqBody := ioutil.NopCloser(strings.NewReader(""))
	err := cs.cli.Do("GET", "/this", nil, reqBody, &v)
	c.Check(err, IsNil)
	c.Check(v, DeepEquals, []int{1, 2})
	c.Assert(cs.req, NotNil)
	c.Assert(cs.req.URL, NotNil)
	c.Check(cs.req.Method, Equals, "GET")
	c.Check(cs.req.Body, Equals, reqBody)
	c.Check(cs.req.URL.Path, Equals, "/this")
}

func (cs *clientSuite) TestClientDefaultsToNoAuthorization(c *C) {
	os.Setenv(client.TestAuthFileEnvKey, filepath.Join(c.MkDir(), "json"))
	defer os.Unsetenv(client.TestAuthFileEnvKey)

	var v string
	_ = cs.cli.Do("GET", "/this", nil, nil, &v)
	c.Assert(cs.req, NotNil)
	authorization := cs.req.Header.Get("Authorization")
	c.Check(authorization, Equals, "")
}

func (cs *clientSuite) TestClientSetsAuthorization(c *C) {
	os.Setenv(client.TestAuthFileEnvKey, filepath.Join(c.MkDir(), "json"))
	defer os.Unsetenv(client.TestAuthFileEnvKey)

	mockUserData := client.User{
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	}
	err := client.TestWriteAuth(mockUserData)
	c.Assert(err, IsNil)

	var v string
	_ = cs.cli.Do("GET", "/this", nil, nil, &v)
	authorization := cs.req.Header.Get("Authorization")
	c.Check(authorization, Equals, `Macaroon root="macaroon", discharge="discharge"`)
}

func (cs *clientSuite) TestClientHonorsDisableAuth(c *C) {
	os.Setenv(client.TestAuthFileEnvKey, filepath.Join(c.MkDir(), "json"))
	defer os.Unsetenv(client.TestAuthFileEnvKey)

	mockUserData := client.User{
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	}
	err := client.TestWriteAuth(mockUserData)
	c.Assert(err, IsNil)

	var v string
	cli := client.New(&client.Config{DisableAuth: true})
	cli.SetDoer(cs)
	_ = cli.Do("GET", "/this", nil, nil, &v)
	authorization := cs.req.Header.Get("Authorization")
	c.Check(authorization, Equals, "")
}

func (cs *clientSuite) TestClientSysInfo(c *C) {
	cs.rsp = `{"type": "sync", "result":
                     {"series": "16",
                      "version": "2",
                      "os-release": {"id": "ubuntu", "version-id": "16.04"},
                      "on-classic": true}}`
	sysInfo, err := cs.cli.SysInfo()
	c.Check(err, IsNil)
	c.Check(sysInfo, DeepEquals, &client.SysInfo{
		Version: "2",
		Series:  "16",
		OSRelease: client.OSRelease{
			ID:        "ubuntu",
			VersionID: "16.04",
		},
		OnClassic: true,
	})
}

func (cs *clientSuite) TestServerVersion(c *C) {
	cs.rsp = `{"type": "sync", "result":
                     {"series": "16",
                      "version": "2",
                      "os-release": {"id": "zyggy", "version-id": "123"}}}`
	version, err := cs.cli.ServerVersion()
	c.Check(err, IsNil)
	c.Check(version, DeepEquals, &client.ServerVersion{
		Version:     "2",
		Series:      "16",
		OSID:        "zyggy",
		OSVersionID: "123",
	})
}

func (cs *clientSuite) TestSnapdClientIntegration(c *C) {
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapdSocket), 0755), IsNil)
	l, err := net.Listen("unix", dirs.SnapdSocket)
	if err != nil {
		c.Fatalf("unable to listen on %q: %v", dirs.SnapdSocket, err)
	}

	f := func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Path, Equals, "/v2/system-info")
		c.Check(r.URL.RawQuery, Equals, "")

		fmt.Fprintln(w, `{"type":"sync", "result":{"series":"42"}}`)
	}

	srv := &httptest.Server{
		Listener: l,
		Config:   &http.Server{Handler: http.HandlerFunc(f)},
	}
	srv.Start()
	defer srv.Close()

	cli := client.New(nil)
	si, err := cli.SysInfo()
	c.Check(err, IsNil)
	c.Check(si.Series, Equals, "42")
}

func (cs *clientSuite) TestSnapClientIntegration(c *C) {
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapSocket), 0755), IsNil)
	l, err := net.Listen("unix", dirs.SnapSocket)
	if err != nil {
		c.Fatalf("unable to listen on %q: %v", dirs.SnapSocket, err)
	}

	f := func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Path, Equals, "/v2/snapctl")
		c.Check(r.URL.RawQuery, Equals, "")

		fmt.Fprintln(w, `{"type":"sync", "result":{"stdout":"test stdout","stderr":"test stderr"}}`)
	}

	srv := &httptest.Server{
		Listener: l,
		Config:   &http.Server{Handler: http.HandlerFunc(f)},
	}
	srv.Start()
	defer srv.Close()

	cli := client.New(nil)
	options := &client.SnapCtlOptions{
		ContextID: "foo",
		Args:      []string{"bar", "--baz"},
	}

	stdout, stderr, err := cli.RunSnapctl(options)
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "test stdout")
	c.Check(string(stderr), Equals, "test stderr")
}

func (cs *clientSuite) TestClientReportsOpError(c *C) {
	cs.rsp = `{"type": "error", "status": "potatoes"}`
	_, err := cs.cli.SysInfo()
	c.Check(err, ErrorMatches, `.*server error: "potatoes"`)
}

func (cs *clientSuite) TestClientReportsOpErrorStr(c *C) {
	cs.rsp = `{
		"result": {},
		"status": "Bad Request",
		"status-code": 400,
		"type": "error"
	}`
	_, err := cs.cli.SysInfo()
	c.Check(err, ErrorMatches, `.*server error: "Bad Request"`)
}

func (cs *clientSuite) TestClientReportsBadType(c *C) {
	cs.rsp = `{"type": "what"}`
	_, err := cs.cli.SysInfo()
	c.Check(err, ErrorMatches, `.*expected sync response, got "what"`)
}

func (cs *clientSuite) TestClientReportsOuterJSONError(c *C) {
	cs.rsp = "this isn't really json is it"
	_, err := cs.cli.SysInfo()
	c.Check(err, ErrorMatches, `.*invalid character .*`)
}

func (cs *clientSuite) TestClientReportsInnerJSONError(c *C) {
	cs.rsp = `{"type": "sync", "result": "this isn't really json is it"}`
	_, err := cs.cli.SysInfo()
	c.Check(err, ErrorMatches, `.*cannot unmarshal.*`)
}

func (cs *clientSuite) TestParseError(c *C) {
	resp := &http.Response{
		Status: "404 Not Found",
	}
	err := client.ParseErrorInTest(resp)
	c.Check(err, ErrorMatches, `server error: "404 Not Found"`)

	h := http.Header{}
	h.Add("Content-Type", "application/json")
	resp = &http.Response{
		Status: "400 Bad Request",
		Header: h,
		Body: ioutil.NopCloser(strings.NewReader(`{
			"status-code": 400,
			"type": "error",
			"result": {
				"message": "invalid"
			}
		}`)),
	}
	err = client.ParseErrorInTest(resp)
	c.Check(err, ErrorMatches, "invalid")

	resp = &http.Response{
		Status: "400 Bad Request",
		Header: h,
		Body:   ioutil.NopCloser(strings.NewReader("{}")),
	}
	err = client.ParseErrorInTest(resp)
	c.Check(err, ErrorMatches, `server error: "400 Bad Request"`)
}

func (cs *clientSuite) TestIsTwoFactor(c *C) {
	c.Check(client.IsTwoFactorError(&client.Error{Kind: client.ErrorKindTwoFactorRequired}), Equals, true)
	c.Check(client.IsTwoFactorError(&client.Error{Kind: client.ErrorKindTwoFactorFailed}), Equals, true)
	c.Check(client.IsTwoFactorError(&client.Error{Kind: "some other kind"}), Equals, false)
	c.Check(client.IsTwoFactorError(errors.New("test")), Equals, false)
	c.Check(client.IsTwoFactorError(nil), Equals, false)
	c.Check(client.IsTwoFactorError((*client.Error)(nil)), Equals, false)
}

func (cs *clientSuite) TestClientCreateUser(c *C) {
	_, err := cs.cli.CreateUser(&client.CreateUserOptions{})
	c.Assert(err, ErrorMatches, "cannot create a user without providing an email")

	cs.rsp = `{
		"type": "sync",
		"result": {
                        "username": "karl",
                        "ssh-keys": ["one", "two"]
		}
	}`
	rsp, err := cs.cli.CreateUser(&client.CreateUserOptions{Email: "one@email.com", Sudoer: true, Known: true})
	c.Assert(cs.req.Method, Equals, "POST")
	c.Assert(cs.req.URL.Path, Equals, "/v2/create-user")
	c.Assert(err, IsNil)

	body, err := ioutil.ReadAll(cs.req.Body)
	c.Assert(err, IsNil)
	c.Assert(string(body), Equals, `{"email":"one@email.com","sudoer":true,"known":true}`)

	c.Assert(rsp, DeepEquals, &client.CreateUserResult{
		Username: "karl",
		SSHKeys:  []string{"one", "two"},
	})
}

var createUsersTests = []struct {
	options   []*client.CreateUserOptions
	bodies    []string
	responses []string
	results   []*client.CreateUserResult
	error     string
}{{
	options: []*client.CreateUserOptions{{}},
	error:   "cannot create user from store details without an email to query for",
}, {
	options: []*client.CreateUserOptions{{
		Email:  "one@example.com",
		Sudoer: true,
	}, {
		Known: true,
	}},
	bodies: []string{
		`{"email":"one@example.com","sudoer":true}`,
		`{"known":true}`,
	},
	responses: []string{
		`{"type": "sync", "result": {"username": "one", "ssh-keys":["a", "b"]}}`,
		`{"type": "sync", "result": [{"username": "two"}, {"username": "three"}]}`,
	},
	results: []*client.CreateUserResult{{
		Username: "one",
		SSHKeys:  []string{"a", "b"},
	}, {
		Username: "two",
	}, {
		Username: "three",
	}},
}}

func (cs *clientSuite) TestClientCreateUsers(c *C) {
	for _, test := range createUsersTests {
		cs.rsps = test.responses

		results, err := cs.cli.CreateUsers(test.options)
		if test.error != "" {
			c.Assert(err, ErrorMatches, test.error)
		}
		c.Assert(results, DeepEquals, test.results)

		var bodies []string
		for _, req := range cs.reqs {
			c.Assert(req.Method, Equals, "POST")
			c.Assert(req.URL.Path, Equals, "/v2/create-user")
			data, err := ioutil.ReadAll(req.Body)
			c.Assert(err, IsNil)
			bodies = append(bodies, string(data))
		}

		c.Assert(bodies, DeepEquals, test.bodies)
	}
}

func (cs *clientSuite) TestClientJSONError(c *C) {
	cs.rsp = `some non-json error message`
	_, err := cs.cli.SysInfo()
	c.Assert(err, ErrorMatches, `cannot obtain system details: cannot decode "some non-json error message": invalid char.*`)
}

func (cs *clientSuite) TestUsers(c *C) {
	cs.rsp = `{"type": "sync", "result":
                     [{"username": "foo","email":"foo@example.com"},
                      {"username": "bar","email":"bar@example.com"}]}`
	sysInfo, err := cs.cli.Users()
	c.Check(err, IsNil)
	c.Check(sysInfo, DeepEquals, []*client.User{
		{Username: "foo", Email: "foo@example.com"},
		{Username: "bar", Email: "bar@example.com"},
	})
}
