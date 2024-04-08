// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2024 Canonical Ltd
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
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type clientSuite struct {
	testutil.BaseTest

	cli           *client.Client
	req           *http.Request
	reqs          []*http.Request
	rsp           string
	rsps          []string
	err           error
	doCalls       int
	header        http.Header
	status        int
	contentLength int64

	countingCloser *countingCloser
}

var _ = Suite(&clientSuite{})

func (cs *clientSuite) SetUpTest(c *C) {
	os.Setenv(client.TestAuthFileEnvKey, filepath.Join(c.MkDir(), "auth.json"))
	cs.AddCleanup(func() { os.Unsetenv(client.TestAuthFileEnvKey) })

	cs.cli = client.New(nil)
	cs.cli.SetDoer(cs)
	cs.err = nil
	cs.req = nil
	cs.reqs = nil
	cs.rsp = ""
	cs.rsps = nil
	cs.req = nil
	cs.header = nil
	cs.status = 200
	cs.doCalls = 0
	cs.contentLength = 0
	cs.countingCloser = nil

	dirs.SetRootDir(c.MkDir())
	cs.AddCleanup(func() { dirs.SetRootDir("") })

	cs.AddCleanup(client.MockDoTimings(time.Millisecond, 100*time.Millisecond))
}

type countingCloser struct {
	io.Reader
	closeCalled int
}

func (n *countingCloser) Close() error {
	n.closeCalled++
	return nil
}

func (cs *clientSuite) Do(req *http.Request) (*http.Response, error) {
	cs.req = req
	cs.reqs = append(cs.reqs, req)
	body := cs.rsp
	if cs.doCalls < len(cs.rsps) {
		body = cs.rsps[cs.doCalls]
	}
	cs.countingCloser = &countingCloser{Reader: strings.NewReader(body)}
	rsp := &http.Response{
		Body:          cs.countingCloser,
		Header:        cs.header,
		StatusCode:    cs.status,
		ContentLength: cs.contentLength,
	}
	cs.doCalls++
	return rsp, cs.err
}

func (cs *clientSuite) TestNewPanics(c *C) {
	c.Assert(func() {
		client.New(&client.Config{BaseURL: ":"})
	}, PanicMatches, `cannot parse server base URL: ":" \(parse \"?:\"?: missing protocol scheme\)`)
}

func (cs *clientSuite) TestClientDoReportsErrors(c *C) {
	cs.err = errors.New("ouchie")
	_, err := cs.cli.Do("GET", "/", nil, nil, nil, nil)
	c.Check(err, ErrorMatches, "cannot communicate with server: ouchie")
	if cs.doCalls < 2 {
		c.Fatalf("do did not retry")
	}
}

func (cs *clientSuite) TestClientWorks(c *C) {
	var v []int
	cs.rsp = `[1,2]`
	reqBody := io.NopCloser(strings.NewReader(""))
	statusCode, err := cs.cli.Do("GET", "/this", nil, reqBody, &v, nil)
	c.Check(err, IsNil)
	c.Check(statusCode, Equals, 200)
	c.Check(v, DeepEquals, []int{1, 2})
	c.Assert(cs.req, NotNil)
	c.Assert(cs.req.URL, NotNil)
	c.Check(cs.req.Method, Equals, "GET")
	c.Check(cs.req.Body, Equals, reqBody)
	c.Check(cs.req.URL.Path, Equals, "/this")
}

func makeMaintenanceFile(c *C, b []byte) {
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapdMaintenanceFile), 0755), IsNil)
	c.Assert(os.WriteFile(dirs.SnapdMaintenanceFile, b, 0644), IsNil)
}

func (cs *clientSuite) TestClientSetMaintenanceForMaintenanceJSON(c *C) {
	// write a maintenance.json that says snapd is down for a restart
	maintErr := &client.Error{
		Kind:    client.ErrorKindSystemRestart,
		Message: "system is restarting",
	}
	b, err := json.Marshal(maintErr)
	c.Assert(err, IsNil)
	makeMaintenanceFile(c, b)

	// now after a Do(), we will have maintenance set to what we wrote
	// originally
	_, err = cs.cli.Do("GET", "/this", nil, nil, nil, nil)
	c.Check(err, IsNil)

	returnedErr := cs.cli.Maintenance()
	c.Assert(returnedErr, DeepEquals, maintErr)
}

func (cs *clientSuite) TestClientIgnoresGarbageMaintenanceJSON(c *C) {
	// write a garbage maintenance.json that can't be unmarshalled
	makeMaintenanceFile(c, []byte("blah blah blah not json"))

	// after a Do(), no maintenance set and also no error returned from Do()
	_, err := cs.cli.Do("GET", "/this", nil, nil, nil, nil)
	c.Check(err, IsNil)

	returnedErr := cs.cli.Maintenance()
	c.Assert(returnedErr, IsNil)
}

func (cs *clientSuite) TestClientDoNoTimeoutIgnoresRetry(c *C) {
	var v []int
	cs.rsp = `[1,2]`
	cs.err = fmt.Errorf("borken")
	reqBody := io.NopCloser(strings.NewReader(""))
	doOpts := &client.DoOptions{
		// Timeout is unset, thus 0, and thus we ignore the retry and only run
		// once even though there is an error
		Retry: time.Duration(time.Second),
	}
	_, err := cs.cli.Do("GET", "/this", nil, reqBody, &v, doOpts)
	c.Check(err, ErrorMatches, "cannot communicate with server: borken")
	c.Assert(cs.doCalls, Equals, 1)
}

func (cs *clientSuite) TestClientDoRetryValidation(c *C) {
	var v []int
	cs.rsp = `[1,2]`
	reqBody := io.NopCloser(strings.NewReader(""))
	doOpts := &client.DoOptions{
		Retry:   time.Duration(-1),
		Timeout: time.Duration(time.Minute),
	}
	_, err := cs.cli.Do("GET", "/this", nil, reqBody, &v, doOpts)
	c.Check(err, ErrorMatches, "internal error: retry setting.*invalid")
	c.Assert(cs.req, IsNil)
}

func (cs *clientSuite) TestClientDoRetryWorks(c *C) {
	reqBody := io.NopCloser(strings.NewReader(""))
	cs.err = fmt.Errorf("borken")
	doOpts := &client.DoOptions{
		Retry:   time.Duration(time.Millisecond),
		Timeout: time.Duration(time.Second),
	}
	_, err := cs.cli.Do("GET", "/this", nil, reqBody, nil, doOpts)
	c.Check(err, ErrorMatches, "cannot communicate with server: borken")
	// best effort checking given that execution could be slow
	// on some machines
	c.Assert(cs.doCalls > 100, Equals, true, Commentf("got only %v calls", cs.doCalls))
	c.Assert(cs.doCalls < 1100, Equals, true, Commentf("got %v calls", cs.doCalls))
}

func (cs *clientSuite) TestClientOnlyRetryAppropriateErrors(c *C) {
	reqBody := io.NopCloser(strings.NewReader(""))
	doOpts := &client.DoOptions{
		Retry:   time.Millisecond,
		Timeout: 1 * time.Minute,
	}

	for _, t := range []struct{ error }{
		{client.InternalClientError{Err: fmt.Errorf("boom")}},
		{client.AuthorizationError{Err: fmt.Errorf("boom")}},
	} {
		cs.doCalls = 0
		cs.err = t.error

		_, err := cs.cli.Do("GET", "/this", nil, reqBody, nil, doOpts)
		c.Check(err, ErrorMatches, fmt.Sprintf(".*%s", t.error.Error()))
		c.Assert(cs.doCalls, Equals, 1)
	}
}

func (cs *clientSuite) TestClientUnderstandsStatusCode(c *C) {
	var v []int
	cs.status = 202
	cs.rsp = `[1,2]`
	reqBody := io.NopCloser(strings.NewReader(""))
	statusCode, err := cs.cli.Do("GET", "/this", nil, reqBody, &v, nil)
	c.Check(err, IsNil)
	c.Check(statusCode, Equals, 202)
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
	_, _ = cs.cli.Do("GET", "/this", nil, nil, &v, nil)
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
	_, _ = cs.cli.Do("GET", "/this", nil, nil, &v, nil)
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
	_, _ = cli.Do("GET", "/this", nil, nil, &v, nil)
	authorization := cs.req.Header.Get("Authorization")
	c.Check(authorization, Equals, "")
}

func (cs *clientSuite) TestClientHonorsInteractive(c *C) {
	var v string
	cli := client.New(&client.Config{Interactive: false})
	cli.SetDoer(cs)
	_, _ = cli.Do("GET", "/this", nil, nil, &v, nil)
	interactive := cs.req.Header.Get(client.AllowInteractionHeader)
	c.Check(interactive, Equals, "")

	cli = client.New(&client.Config{Interactive: true})
	cli.SetDoer(cs)
	_, _ = cli.Do("GET", "/this", nil, nil, &v, nil)
	interactive = cs.req.Header.Get(client.AllowInteractionHeader)
	c.Check(interactive, Equals, "true")
}

func (cs *clientSuite) TestClientWhoAmINobody(c *C) {
	email, err := cs.cli.WhoAmI()
	c.Assert(err, IsNil)
	c.Check(email, Equals, "")
}

func (cs *clientSuite) TestClientWhoAmIRubbish(c *C) {
	c.Assert(os.WriteFile(client.TestStoreAuthFilename(os.Getenv("HOME")), []byte("rubbish"), 0644), IsNil)

	email, err := cs.cli.WhoAmI()
	c.Check(err, NotNil)
	c.Check(email, Equals, "")
}

func (cs *clientSuite) TestClientWhoAmISomebody(c *C) {
	mockUserData := client.User{
		Email: "foo@example.com",
	}
	c.Assert(client.TestWriteAuth(mockUserData), IsNil)

	email, err := cs.cli.WhoAmI()
	c.Check(err, IsNil)
	c.Check(email, Equals, "foo@example.com")
}

func (cs *clientSuite) TestClientSysInfo(c *C) {
	cs.rsp = `{
  "type": "sync",
  "result": {
    "series": "16",
    "version": "2",
    "os-release": {"id": "ubuntu", "version-id": "16.04"},
    "on-classic": true,
    "build-id": "1234",
    "confinement": "strict",
    "architecture": "TI-99/4A",
    "virtualization": "MESS",
    "sandbox-features": {"backend": ["feature-1", "feature-2"]},
    "features": {
      "foo": {"supported": false, "unsupported-reason": "too foo", "enabled": false},
      "bar": {"supported": false, "unsupported-reason": "not bar enough", "enabled": true},
      "baz": {"supported": true, "enabled": false},
      "buzz": {"supported": true, "enabled": true}
    }
  }
}`
	sysInfo, err := cs.cli.SysInfo()
	c.Check(err, IsNil)
	c.Check(sysInfo, DeepEquals, &client.SysInfo{
		Version: "2",
		Series:  "16",
		OSRelease: client.OSRelease{
			ID:        "ubuntu",
			VersionID: "16.04",
		},
		OnClassic:   true,
		Confinement: "strict",
		SandboxFeatures: map[string][]string{
			"backend": {"feature-1", "feature-2"},
		},
		BuildID:        "1234",
		Architecture:   "TI-99/4A",
		Virtualization: "MESS",
		Features: map[string]features.FeatureInfo{
			"foo":  {Supported: false, UnsupportedReason: "too foo", Enabled: false},
			"bar":  {Supported: false, UnsupportedReason: "not bar enough", Enabled: true},
			"baz":  {Supported: true, Enabled: false},
			"buzz": {Supported: true, Enabled: true},
		},
	})
}

func (cs *clientSuite) TestServerVersion(c *C) {
	cs.rsp = `{"type": "sync", "result":
                     {"series": "16",
                      "version": "2",
                      "os-release": {"id": "zyggy", "version-id": "123"},
                      "architecture": "m32",
                      "virtualization": "qemu"
}}}`
	version, err := cs.cli.ServerVersion()
	c.Check(err, IsNil)
	c.Check(version, DeepEquals, &client.ServerVersion{
		Version:        "2",
		Series:         "16",
		OSID:           "zyggy",
		OSVersionID:    "123",
		Architecture:   "m32",
		Virtualization: "qemu",
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
	c.Assert(err, IsNil)
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

	cli := client.New(&client.Config{
		Socket: dirs.SnapSocket,
	})
	options := &client.SnapCtlOptions{
		ContextID: "foo",
		Args:      []string{"bar", "--baz"},
	}

	stdout, stderr, err := cli.RunSnapctl(options, nil)
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "test stdout")
	c.Check(string(stderr), Equals, "test stderr")
}

func (cs *clientSuite) TestClientReportsOpError(c *C) {
	cs.status = 500
	cs.rsp = `{"type": "error"}`
	_, err := cs.cli.SysInfo()
	c.Check(err, ErrorMatches, `.*server error: "Internal Server Error"`)
}

func (cs *clientSuite) TestClientReportsOpErrorStr(c *C) {
	cs.status = 400
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

func (cs *clientSuite) TestClientMaintenance(c *C) {
	cs.rsp = `{"type":"sync", "result":{"series":"42"}, "maintenance": {"kind": "system-restart", "message": "system is restarting"}}`
	_, err := cs.cli.SysInfo()
	c.Assert(err, IsNil)
	c.Check(cs.cli.Maintenance().(*client.Error), DeepEquals, &client.Error{
		Kind:    client.ErrorKindSystemRestart,
		Message: "system is restarting",
	})

	cs.rsp = `{"type":"sync", "result":{"series":"42"}}`
	_, err = cs.cli.SysInfo()
	c.Assert(err, IsNil)
	c.Check(cs.cli.Maintenance(), Equals, error(nil))
}

func (cs *clientSuite) TestClientAsyncOpMaintenance(c *C) {
	cs.status = 202
	cs.rsp = `{"type":"async", "status-code": 202, "change": "42", "maintenance": {"kind": "system-restart", "message": "system is restarting"}}`
	_, err := cs.cli.Install("foo", nil)
	c.Assert(err, IsNil)
	c.Check(cs.cli.Maintenance().(*client.Error), DeepEquals, &client.Error{
		Kind:    client.ErrorKindSystemRestart,
		Message: "system is restarting",
	})

	cs.rsp = `{"type":"async", "status-code": 202, "change": "42"}`
	_, err = cs.cli.Install("foo", nil)
	c.Assert(err, IsNil)
	c.Check(cs.cli.Maintenance(), Equals, error(nil))
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
		Body: io.NopCloser(strings.NewReader(`{
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
		Body:   io.NopCloser(strings.NewReader("{}")),
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

func (cs *clientSuite) TestIsRetryable(c *C) {
	// unhappy
	c.Check(client.IsRetryable(nil), Equals, false)
	c.Check(client.IsRetryable(errors.New("some-error")), Equals, false)
	c.Check(client.IsRetryable(&client.Error{Kind: "something-else"}), Equals, false)
	// happy
	c.Check(client.IsRetryable(&client.Error{Kind: client.ErrorKindSnapChangeConflict}), Equals, true)
}

func (cs *clientSuite) TestUserAgent(c *C) {
	cli := client.New(&client.Config{UserAgent: "some-agent/9.87"})
	cli.SetDoer(cs)

	var v string
	_, _ = cli.Do("GET", "/", nil, nil, &v, nil)
	c.Assert(cs.req, NotNil)
	c.Check(cs.req.Header.Get("User-Agent"), Equals, "some-agent/9.87")
}

func (cs *clientSuite) TestDebugEnsureStateSoon(c *C) {
	cs.rsp = `{"type": "sync", "result":true}`
	err := cs.cli.Debug("ensure-state-soon", nil, nil)
	c.Check(err, IsNil)
	c.Check(cs.reqs, HasLen, 1)
	c.Check(cs.reqs[0].Method, Equals, "POST")
	c.Check(cs.reqs[0].URL.Path, Equals, "/v2/debug")
	data, err := io.ReadAll(cs.reqs[0].Body)
	c.Assert(err, IsNil)
	c.Check(data, DeepEquals, []byte(`{"action":"ensure-state-soon"}`))
}

func (cs *clientSuite) TestDebugGeneric(c *C) {
	cs.rsp = `{"type": "sync", "result":["res1","res2"]}`

	var result []string
	err := cs.cli.Debug("do-something", []string{"param1", "param2"}, &result)
	c.Check(err, IsNil)
	c.Check(result, DeepEquals, []string{"res1", "res2"})
	c.Check(cs.reqs, HasLen, 1)
	c.Check(cs.reqs[0].Method, Equals, "POST")
	c.Check(cs.reqs[0].URL.Path, Equals, "/v2/debug")
	data, err := io.ReadAll(cs.reqs[0].Body)
	c.Assert(err, IsNil)
	c.Check(string(data), DeepEquals, `{"action":"do-something","params":["param1","param2"]}`)
}

func (cs *clientSuite) TestDebugGet(c *C) {
	cs.rsp = `{"type": "sync", "result":["res1","res2"]}`

	var result []string
	err := cs.cli.DebugGet("do-something", &result, map[string]string{"foo": "bar"})
	c.Check(err, IsNil)
	c.Check(result, DeepEquals, []string{"res1", "res2"})
	c.Check(cs.reqs, HasLen, 1)
	c.Check(cs.reqs[0].Method, Equals, "GET")
	c.Check(cs.reqs[0].URL.Path, Equals, "/v2/debug")
	c.Check(cs.reqs[0].URL.Query(), DeepEquals, url.Values{"aspect": []string{"do-something"}, "foo": []string{"bar"}})
}

func (cs *clientSuite) TestDebugMigrateHome(c *C) {
	cs.status = 202
	cs.rsp = `{"type": "async", "status-code": 202, "change": "123"}`

	snaps := []string{"foo", "bar"}
	changeID, err := cs.cli.MigrateSnapHome(snaps)
	c.Check(err, IsNil)
	c.Check(changeID, Equals, "123")

	c.Check(cs.reqs, HasLen, 1)
	c.Check(cs.reqs[0].Method, Equals, "POST")
	c.Check(cs.reqs[0].URL.Path, Equals, "/v2/debug")
	data, err := io.ReadAll(cs.reqs[0].Body)
	c.Assert(err, IsNil)
	c.Check(string(data), Equals, `{"action":"migrate-home","snaps":["foo","bar"]}`)
}

type integrationSuite struct{}

var _ = Suite(&integrationSuite{})

func (cs *integrationSuite) TestClientTimeoutLP1837804(c *C) {
	restore := client.MockDoTimings(time.Millisecond, 5*time.Millisecond)
	defer restore()

	testServer := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		time.Sleep(25 * time.Millisecond)
	}))
	defer func() { testServer.Close() }()

	cli := client.New(&client.Config{BaseURL: testServer.URL})
	_, err := cli.Do("GET", "/", nil, nil, nil, nil)
	c.Assert(err, ErrorMatches, `.* timeout exceeded while waiting for response`)

	_, err = cli.Do("POST", "/", nil, nil, nil, nil)
	c.Assert(err, ErrorMatches, `.* timeout exceeded while waiting for response`)
}

func (cs *clientSuite) TestClientSystemRecoveryKeys(c *C) {
	cs.rsp = `{"type":"sync", "result":{"recovery-key":"42"}}`

	var key client.SystemRecoveryKeysResponse
	err := cs.cli.SystemRecoveryKeys(&key)
	c.Assert(err, IsNil)
	c.Check(cs.reqs, HasLen, 1)
	c.Check(cs.reqs[0].Method, Equals, "GET")
	c.Check(cs.reqs[0].URL.Path, Equals, "/v2/system-recovery-keys")
	c.Check(key.RecoveryKey, Equals, "42")
}

func (cs *clientSuite) TestClientDebugEnvVar(c *C) {
	buf, restore := logger.MockLogger()
	defer restore()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `bar`)
	}))
	defer srv.Close()

	debugValue, ok := os.LookupEnv("SNAP_CLIENT_DEBUG_HTTP")
	defer func() {
		if ok {
			os.Setenv("SNAP_CLIENT_DEBUG_HTTP", debugValue)
		} else {
			os.Unsetenv("SNAP_CLIENT_DEBUG_HTTP")
		}
	}()

	os.Setenv("SNAP_CLIENT_DEBUG_HTTP", "7")

	cli := client.New(&client.Config{BaseURL: srv.URL})
	c.Assert(cli, NotNil)
	_, err := cli.Do("GET", "/", nil, strings.NewReader("foo"), nil, nil)
	c.Assert(err, IsNil)

	// check request
	c.Assert(buf.String(), testutil.Contains, `logger.go:67: DEBUG: > "GET`)
	// check response
	c.Assert(buf.String(), testutil.Contains, `logger.go:74: DEBUG: < "HTTP/1.1 200 OK`)
	// check bodies
	c.Assert(buf.String(), testutil.Contains, "foo")
	c.Assert(buf.String(), testutil.Contains, "bar")
}
