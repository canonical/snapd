// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

package main_test

import (
	"fmt"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/progress/progresstest"
	"github.com/snapcore/snapd/testutil"
)

type snapOpTestServer struct {
	c *check.C

	checker   func(r *http.Request)
	n         int
	total     int
	channel   string
	rebooting bool
}

var _ = check.Suite(&SnapOpSuite{})

func (t *snapOpTestServer) handle(w http.ResponseWriter, r *http.Request) {
	switch t.n {
	case 0:
		t.checker(r)
		method := "POST"
		if strings.HasSuffix(r.URL.Path, "/conf") {
			method = "PUT"
		}
		t.c.Check(r.Method, check.Equals, method)
		w.WriteHeader(202)
		fmt.Fprintln(w, `{"type":"async", "change": "42", "status-code": 202}`)
	case 1:
		t.c.Check(r.Method, check.Equals, "GET")
		t.c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
		if !t.rebooting {
			fmt.Fprintln(w, `{"type": "sync", "result": {"status": "Doing"}}`)
		} else {
			fmt.Fprintln(w, `{"type": "sync", "result": {"status": "Doing"}, "maintenance": {"kind": "system-restart", "message": "system is restarting"}}}`)
		}
	case 2:
		t.c.Check(r.Method, check.Equals, "GET")
		t.c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
		fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done", "data": {"snap-name": "foo"}}}`)
	case 3:
		t.c.Check(r.Method, check.Equals, "GET")
		t.c.Check(r.URL.Path, check.Equals, "/v2/snaps")
		fmt.Fprintf(w, `{"type": "sync", "result": [{"name": "foo", "status": "active", "version": "1.0", "developer": "bar", "publisher": {"id": "bar-id", "username": "bar", "display-name": "Bar"}, "revision":42, "channel":"%s"}]}\n`, t.channel)
	default:
		t.c.Fatalf("expected to get %d requests, now on %d", t.total, t.n+1)
	}

	t.n++
}

type SnapOpSuite struct {
	BaseSnapSuite

	restoreAll func()
	srv        snapOpTestServer
}

func (s *SnapOpSuite) SetUpTest(c *check.C) {
	s.BaseSnapSuite.SetUpTest(c)

	restoreClientRetry := client.MockDoRetry(time.Millisecond, 10*time.Millisecond)
	restorePollTime := snap.MockPollTime(time.Millisecond)
	s.restoreAll = func() {
		restoreClientRetry()
		restorePollTime()
	}

	s.srv = snapOpTestServer{
		c:     c,
		total: 4,
	}
}

func (s *SnapOpSuite) TearDownTest(c *check.C) {
	s.restoreAll()
	s.BaseSnapSuite.TearDownTest(c)
}

func (s *SnapOpSuite) TestWait(c *check.C) {
	meter := &progresstest.Meter{}
	defer progress.MockMeter(meter)()
	restore := snap.MockMaxGoneTime(time.Millisecond)
	defer restore()

	// lazy way of getting a URL that won't work nor break stuff
	server := httptest.NewServer(nil)
	snap.ClientConfig.BaseURL = server.URL
	server.Close()

	cli := snap.Client()
	chg, err := snap.Wait(cli, "x")
	c.Assert(chg, check.IsNil)
	c.Assert(err, check.NotNil)
	c.Check(meter.Labels, testutil.Contains, "Waiting for server to restart")
}

func (s *SnapOpSuite) TestWaitRecovers(c *check.C) {
	meter := &progresstest.Meter{}
	defer progress.MockMeter(meter)()
	restore := snap.MockMaxGoneTime(time.Millisecond)
	defer restore()

	nah := true
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		if nah {
			nah = false
			return
		}
		fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done"}}`)
	})

	cli := snap.Client()
	chg, err := snap.Wait(cli, "x")
	// we got the change
	c.Assert(chg, check.NotNil)
	c.Assert(err, check.IsNil)

	// but only after recovering
	c.Check(meter.Labels, testutil.Contains, "Waiting for server to restart")
}

func (s *SnapOpSuite) TestWaitRebooting(c *check.C) {
	meter := &progresstest.Meter{}
	defer progress.MockMeter(meter)()
	restore := snap.MockMaxGoneTime(time.Millisecond)
	defer restore()

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "sync",
"result": {
"ready": false,
"status": "Doing",
"tasks": [{"kind": "bar", "summary": "...", "status": "Doing", "progress": {"done": 1, "total": 1}, "log": ["INFO: info"]}]
},
"maintenance": {"kind": "system-restart", "message": "system is restarting"}}`)
	})

	cli := snap.Client()
	chg, err := snap.Wait(cli, "x")
	c.Assert(chg, check.IsNil)
	c.Assert(err, check.DeepEquals, &client.Error{Kind: client.ErrorKindSystemRestart, Message: "system is restarting"})

	// last available info is still displayed
	c.Check(meter.Notices, testutil.Contains, "INFO: info")
}

func (s *SnapOpSuite) TestInstall(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":  "install",
			"channel": "candidate",
		})
		s.srv.channel = "candidate"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser().ParseArgs([]string{"install", "--channel", "candidate", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo \(candidate\) 1.0 from 'bar' installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallFromTrack(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":  "install",
			"channel": "3.4/stable",
		})
		s.srv.channel = "3.4/stable"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	// snap install --channel=3.4 means 3.4/stable, this is what we test here
	rest, err := snap.Parser().ParseArgs([]string{"install", "--channel", "3.4", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo \(3.4/stable\) 1.0 from 'bar' installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallFromBranch(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":  "install",
			"channel": "3.4/hotfix-1",
		})
		s.srv.channel = "3.4/hotfix-1"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser().ParseArgs([]string{"install", "--channel", "3.4/hotfix-1", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo \(3.4/hotfix-1\) 1.0 from 'bar' installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallDevMode(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":  "install",
			"devmode": true,
			"channel": "beta",
		})
		s.srv.channel = "beta"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser().ParseArgs([]string{"install", "--channel", "beta", "--devmode", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo \(beta\) 1.0 from 'bar' installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallClassic(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":  "install",
			"classic": true,
		})
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser().ParseArgs([]string{"install", "--classic", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo 1.0 from 'bar' installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallUnaliased(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":    "install",
			"unaliased": true,
		})
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser().ParseArgs([]string{"install", "--unaliased", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo 1.0 from 'bar' installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallSnapNotFound(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "snap not found", "value": "foo", "kind": "snap-not-found"}, "status-code": 404}`)
	})

	_, err := snap.Parser().ParseArgs([]string{"install", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("error: %v\n", err), check.Equals, `error: snap "foo" not found
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailable(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision given constraints", "value": "foo", "kind": "snap-revision-not-available"}, "status-code": 404}`)
	})

	_, err := snap.Parser().ParseArgs([]string{"install", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("\nerror: %v\n", err), check.Equals, `
error: snap "foo" not found for given constraints. Please use 'snap info foo'
       to list available releases.
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailableOnChannel(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision given constraints", "value": "foo", "kind": "snap-revision-not-available"}, "status-code": 404}`)
	})

	_, err := snap.Parser().ParseArgs([]string{"install", "--channel=mytrack", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("\nerror: %v\n", err), check.Equals, `
error: snap "foo" not found at least on channel "mytrack/stable". Please use
       'snap info foo' to list available releases.
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailableAtRevision(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision given constraints", "value": "foo", "kind": "snap-revision-not-available"}, "status-code": 404}`)
	})

	_, err := snap.Parser().ParseArgs([]string{"install", "--revision=2", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("\nerror: %v\n", err), check.Equals, `
error: snap "foo" not found at least at revision 2. Please use 'snap info foo'
       to list available releases.
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailableForChannelTrackOK(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision given constraints", "value": {
  "snap-name": "foo",
  "action": "install",
  "architecture": "amd64",
  "channel": "stable",
  "releases": [{"architecture": "amd64", "channel": "beta"},
               {"architecture": "amd64", "channel": "edge"}]
}, "kind": "snap-revision-not-available-for-channel"}, "status-code": 404}`)
	})

	_, err := snap.Parser().ParseArgs([]string{"install", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("\nerror: %v\n", err), check.Equals, `
error: snap "foo" is not available on stable but is available to install on the
       following channels:

       beta      snap install --beta foo
       edge      snap install --edge foo

       Please be mindful pre-release channels may include features not
       completely tested or implemented. Get more information with 'snap info
       foo'.
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailableForChannelTrackOKPrerelOK(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision given constraints", "value": {
  "snap-name": "foo",
  "action": "install",
  "architecture": "amd64",
  "channel": "candidate",
  "releases": [{"architecture": "amd64", "channel": "beta"},
               {"architecture": "amd64", "channel": "edge"}]
}, "kind": "snap-revision-not-available-for-channel"}, "status-code": 404}`)
	})

	_, err := snap.Parser().ParseArgs([]string{"install", "--candidate", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("\nerror: %v\n", err), check.Equals, `
error: snap "foo" is not available on candidate but is available to install on
       the following channels:

       beta      snap install --beta foo
       edge      snap install --edge foo

       Get more information with 'snap info foo'.
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailableForChannelTrackOther(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision given constraints", "value": {
  "snap-name": "foo",
  "action": "install",
  "architecture": "amd64",
  "channel": "stable",
  "releases": [{"architecture": "amd64", "channel": "1.0/stable"},
               {"architecture": "amd64", "channel": "2.0/stable"}]
}, "kind": "snap-revision-not-available-for-channel"}, "status-code": 404}`)
	})

	_, err := snap.Parser().ParseArgs([]string{"install", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("\nerror: %v\n", err), check.Equals, `
error: snap "foo" is not available on latest/stable but is available to install
       on the following tracks:

       1.0/stable  snap install --channel=1.0 foo
       2.0/stable  snap install --channel=2.0 foo

       Please be mindful that different tracks may include different features.
       Get more information with 'snap info foo'.
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailableForChannelTrackLatestStable(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision given constraints", "value": {
  "snap-name": "foo",
  "action": "install",
  "architecture": "amd64",
  "channel": "2.0/stable",
  "releases": [{"architecture": "amd64", "channel": "stable"}]
}, "kind": "snap-revision-not-available-for-channel"}, "status-code": 404}`)
	})

	_, err := snap.Parser().ParseArgs([]string{"install", "--channel=2.0/stable", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("\nerror: %v\n", err), check.Equals, `
error: snap "foo" is not available on 2.0/stable but is available to install on
       the following tracks:

       latest/stable  snap install --stable foo

       Please be mindful that different tracks may include different features.
       Get more information with 'snap info foo'.
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailableForChannelTrackAndRiskOther(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision given constraints", "value": {
  "snap-name": "foo",
  "action": "install",
  "architecture": "amd64",
  "channel": "2.0/stable",
  "releases": [{"architecture": "amd64", "channel": "1.0/edge"}]
}, "kind": "snap-revision-not-available-for-channel"}, "status-code": 404}`)
	})

	_, err := snap.Parser().ParseArgs([]string{"install", "--channel=2.0/stable", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("\nerror: %v\n", err), check.Equals, `
error: snap "foo" is not available on 2.0/stable but other tracks exist.

       Please be mindful that different tracks may include different features.
       Get more information with 'snap info foo'.
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailableForArchitectureTrackAndRiskOK(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision given constraints", "value": {
  "snap-name": "foo",
  "action": "install",
  "architecture": "arm64",
  "channel": "stable",
  "releases": [{"architecture": "amd64", "channel": "stable"}]
}, "kind": "snap-revision-not-available-for-architecture"}, "status-code": 404}`)
	})

	_, err := snap.Parser().ParseArgs([]string{"install", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("\nerror: %v\n", err), check.Equals, `
error: snap "foo" is not available on stable for this architecture (arm64) but
       exists on other architectures (amd64).
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailableForArchitectureTrackAndRiskOther(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision given constraints", "value": {
  "snap-name": "foo",
  "action": "install",
  "architecture": "arm64",
  "channel": "1.0/stable",
  "releases": [{"architecture": "amd64", "channel": "stable"}]
}, "kind": "snap-revision-not-available-for-architecture"}, "status-code": 404}`)
	})

	_, err := snap.Parser().ParseArgs([]string{"install", "--channel=1.0/stable", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("\nerror: %v\n", err), check.Equals, `
error: snap "foo" is not available on this architecture (arm64) but exists on
       other architectures (amd64).
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailableInvalidChannel(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision given constraints", "value": {
  "snap-name": "foo",
  "action": "install",
  "architecture": "amd64",
  "channel": "a/b/c/d",
  "releases": [{"architecture": "amd64", "channel": "stable"}]
}, "kind": "snap-revision-not-available-for-channel"}, "status-code": 404}`)
	})

	_, err := snap.Parser().ParseArgs([]string{"install", "--channel=a/b/c/d", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("\nerror: %v\n", err), check.Equals, `
error: requested what looks like an invalid channel for snap "foo". Please use
       'snap info foo' to list available releases.
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailableForChannelNonExistingBranch(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision given constraints", "value": {
  "snap-name": "foo",
  "action": "install",
  "architecture": "amd64",
  "channel": "stable/baz",
  "releases": [{"architecture": "amd64", "channel": "stable"}]
}, "kind": "snap-revision-not-available-for-channel"}, "status-code": 404}`)
	})

	_, err := snap.Parser().ParseArgs([]string{"install", "--channel=a/b/c/d", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("\nerror: %v\n", err), check.Equals, `
error: requested an apparently non-existing branch on latest/stable for snap
       "foo": baz
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func testForm(r *http.Request, c *check.C) *multipart.Form {
	contentType := r.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	c.Assert(err, check.IsNil)
	c.Assert(params["boundary"], check.Matches, ".{10,}")
	c.Check(mediaType, check.Equals, "multipart/form-data")

	form, err := multipart.NewReader(r.Body, params["boundary"]).ReadForm(1 << 20)
	c.Assert(err, check.IsNil)

	return form
}

func formFile(form *multipart.Form, c *check.C) (name, filename string, content []byte) {
	c.Assert(form.File, check.HasLen, 1)

	for name, fheaders := range form.File {
		c.Assert(fheaders, check.HasLen, 1)
		body, err := fheaders[0].Open()
		c.Assert(err, check.IsNil)
		defer body.Close()
		filename = fheaders[0].Filename
		content, err = ioutil.ReadAll(body)
		c.Assert(err, check.IsNil)

		return name, filename, content
	}

	return "", "", nil
}

func (s *SnapOpSuite) TestInstallPath(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps")

		form := testForm(r, c)
		defer form.RemoveAll()

		c.Check(form.Value["action"], check.DeepEquals, []string{"install"})
		c.Check(form.Value["devmode"], check.IsNil)
		c.Check(form.Value["snap-path"], check.NotNil)
		c.Check(form.Value, check.HasLen, 2)

		name, _, body := formFile(form, c)
		c.Check(name, check.Equals, "snap")
		c.Check(string(body), check.Equals, "snap-data")
	}

	snapBody := []byte("snap-data")
	s.RedirectClientToTestServer(s.srv.handle)
	snapPath := filepath.Join(c.MkDir(), "foo.snap")
	err := ioutil.WriteFile(snapPath, snapBody, 0644)
	c.Assert(err, check.IsNil)

	rest, err := snap.Parser().ParseArgs([]string{"install", snapPath})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo 1.0 from 'bar' installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallPathDevMode(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps")
		form := testForm(r, c)
		defer form.RemoveAll()

		c.Check(form.Value["action"], check.DeepEquals, []string{"install"})
		c.Check(form.Value["devmode"], check.DeepEquals, []string{"true"})
		c.Check(form.Value["snap-path"], check.NotNil)
		c.Check(form.Value, check.HasLen, 3)

		name, _, body := formFile(form, c)
		c.Check(name, check.Equals, "snap")
		c.Check(string(body), check.Equals, "snap-data")
	}

	snapBody := []byte("snap-data")
	s.RedirectClientToTestServer(s.srv.handle)
	snapPath := filepath.Join(c.MkDir(), "foo.snap")
	err := ioutil.WriteFile(snapPath, snapBody, 0644)
	c.Assert(err, check.IsNil)

	rest, err := snap.Parser().ParseArgs([]string{"install", "--devmode", snapPath})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo 1.0 from 'bar' installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallPathClassic(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps")
		form := testForm(r, c)
		defer form.RemoveAll()

		c.Check(form.Value["action"], check.DeepEquals, []string{"install"})
		c.Check(form.Value["classic"], check.DeepEquals, []string{"true"})
		c.Check(form.Value["snap-path"], check.NotNil)
		c.Check(form.Value, check.HasLen, 3)

		name, _, body := formFile(form, c)
		c.Check(name, check.Equals, "snap")
		c.Check(string(body), check.Equals, "snap-data")
	}

	snapBody := []byte("snap-data")
	s.RedirectClientToTestServer(s.srv.handle)
	snapPath := filepath.Join(c.MkDir(), "foo.snap")
	err := ioutil.WriteFile(snapPath, snapBody, 0644)
	c.Assert(err, check.IsNil)

	rest, err := snap.Parser().ParseArgs([]string{"install", "--classic", snapPath})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo 1.0 from 'bar' installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallPathDangerous(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps")
		form := testForm(r, c)
		defer form.RemoveAll()

		c.Check(form.Value["action"], check.DeepEquals, []string{"install"})
		c.Check(form.Value["dangerous"], check.DeepEquals, []string{"true"})
		c.Check(form.Value["snap-path"], check.NotNil)
		c.Check(form.Value, check.HasLen, 3)

		name, _, body := formFile(form, c)
		c.Check(name, check.Equals, "snap")
		c.Check(string(body), check.Equals, "snap-data")
	}

	snapBody := []byte("snap-data")
	s.RedirectClientToTestServer(s.srv.handle)
	snapPath := filepath.Join(c.MkDir(), "foo.snap")
	err := ioutil.WriteFile(snapPath, snapBody, 0644)
	c.Assert(err, check.IsNil)

	rest, err := snap.Parser().ParseArgs([]string{"install", "--dangerous", snapPath})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo 1.0 from 'bar' installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestRevertRunthrough(c *check.C) {
	s.srv.total = 4
	s.srv.channel = "potato"
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action": "revert",
		})
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser().ParseArgs([]string{"revert", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	// tracking channel is "" in the test server
	c.Check(s.Stdout(), check.Equals, `foo reverted to 1.0
Channel  for foo is closed; temporarily forwarding to potato.
`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) runRevertTest(c *check.C, opts *client.SnapOptions) {
	modes := []struct {
		enabled bool
		name    string
	}{
		{opts.DevMode, "devmode"},
		{opts.JailMode, "jailmode"},
		{opts.Classic, "classic"},
	}

	s.srv.checker = func(r *http.Request) {

		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		d := DecodedRequestBody(c, r)

		n := 1
		c.Check(d["action"], check.Equals, "revert")

		for _, mode := range modes {
			if mode.enabled {
				n++
				c.Check(d[mode.name], check.Equals, true)
			} else {
				c.Check(d[mode.name], check.IsNil)
			}
		}
		c.Check(d, check.HasLen, n)
	}

	s.RedirectClientToTestServer(s.srv.handle)

	cmd := []string{"revert", "foo"}
	for _, mode := range modes {
		if mode.enabled {
			cmd = append(cmd, "--"+mode.name)
		}
	}

	rest, err := snap.Parser().ParseArgs(cmd)
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, "foo reverted to 1.0\n")
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestRevertNoMode(c *check.C) {
	s.runRevertTest(c, &client.SnapOptions{})
}

func (s *SnapOpSuite) TestRevertDevMode(c *check.C) {
	s.runRevertTest(c, &client.SnapOptions{DevMode: true})
}

func (s *SnapOpSuite) TestRevertJailMode(c *check.C) {
	s.runRevertTest(c, &client.SnapOptions{JailMode: true})
}

func (s *SnapOpSuite) TestRevertClassic(c *check.C) {
	s.runRevertTest(c, &client.SnapOptions{Classic: true})
}

func (s *SnapOpSuite) TestRevertMissingName(c *check.C) {
	_, err := snap.Parser().ParseArgs([]string{"revert"})
	c.Assert(err, check.NotNil)
	c.Assert(err, check.ErrorMatches, "the required argument `<snap>` was not provided")
}

func (s *SnapSuite) TestRefreshList(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			c.Check(r.URL.Query().Get("select"), check.Equals, "refresh")
			fmt.Fprintln(w, `{"type": "sync", "result": [{"name": "foo", "status": "active", "version": "4.2update1", "developer": "bar", "publisher": {"id": "bar-id", "username": "bar", "display-name": "Bar"}, "revision":17,"summary":"some summary"}]}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser().ParseArgs([]string{"refresh", "--list"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `Name +Version +Rev +Publisher +Notes
foo +4.2update1 +17 +bar +-.*
`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, 1)
}

func (s *SnapSuite) TestRefreshLegacyTime(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/system-info")
			fmt.Fprintln(w, `{"type": "sync", "status-code": 200, "result": {"refresh": {"schedule": "00:00-04:59/5:00-10:59/11:00-16:59/17:00-23:59", "last": "2017-04-25T17:35:00+02:00", "next": "2017-04-26T00:58:00+02:00"}}}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser().ParseArgs([]string{"refresh", "--time", "--abs-time"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `schedule: 00:00-04:59/5:00-10:59/11:00-16:59/17:00-23:59
last: 2017-04-25T17:35:00+02:00
next: 2017-04-26T00:58:00+02:00
`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, 1)
}

func (s *SnapSuite) TestRefreshTimer(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/system-info")
			fmt.Fprintln(w, `{"type": "sync", "status-code": 200, "result": {"refresh": {"timer": "0:00-24:00/4", "last": "2017-04-25T17:35:00+02:00", "next": "2017-04-26T00:58:00+02:00"}}}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser().ParseArgs([]string{"refresh", "--time", "--abs-time"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `timer: 0:00-24:00/4
last: 2017-04-25T17:35:00+02:00
next: 2017-04-26T00:58:00+02:00
`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, 1)
}

func (s *SnapSuite) TestRefreshHold(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/system-info")
			fmt.Fprintln(w, `{"type": "sync", "status-code": 200, "result": {"refresh": {"timer": "0:00-24:00/4", "last": "2017-04-25T17:35:00+02:00", "next": "2017-04-26T00:58:00+02:00", "hold": "2017-04-28T00:00:00+02:00"}}}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser().ParseArgs([]string{"refresh", "--time", "--abs-time"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `timer: 0:00-24:00/4
last: 2017-04-25T17:35:00+02:00
hold: 2017-04-28T00:00:00+02:00
next: 2017-04-26T00:58:00+02:00
`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, 1)
}

func (s *SnapSuite) TestRefreshNoTimerNoSchedule(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "GET")
		c.Check(r.URL.Path, check.Equals, "/v2/system-info")
		fmt.Fprintln(w, `{"type": "sync", "status-code": 200, "result": {"refresh": {"last": "2017-04-25T17:35:00+0200", "next": "2017-04-26T00:58:00+0200"}}}`)
	})
	_, err := snap.Parser().ParseArgs([]string{"refresh", "--time"})
	c.Assert(err, check.ErrorMatches, `internal error: both refresh.timer and refresh.schedule are empty`)
}

func (s *SnapSuite) TestRefreshListErr(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser().ParseArgs([]string{"refresh", "--list", "--beta"})
	c.Check(err, check.ErrorMatches, "--list does not take .* flags")
}

func (s *SnapOpSuite) TestRefreshOne(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action": "refresh",
		})
	}
	_, err := snap.Parser().ParseArgs([]string{"refresh", "foo"})
	c.Assert(err, check.IsNil)
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo 1.0 from 'bar' refreshed`)

}

func (s *SnapOpSuite) TestRefreshOneSwitchChannel(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":  "refresh",
			"channel": "beta",
		})
		s.srv.channel = "beta"
	}
	_, err := snap.Parser().ParseArgs([]string{"refresh", "--beta", "foo"})
	c.Assert(err, check.IsNil)
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo \(beta\) 1.0 from 'bar' refreshed`)
}

func (s *SnapOpSuite) TestRefreshOneClassic(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/one")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":  "refresh",
			"classic": true,
		})
	}
	_, err := snap.Parser().ParseArgs([]string{"refresh", "--classic", "one"})
	c.Assert(err, check.IsNil)
}

func (s *SnapOpSuite) TestRefreshOneDevmode(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/one")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":  "refresh",
			"devmode": true,
		})
	}
	_, err := snap.Parser().ParseArgs([]string{"refresh", "--devmode", "one"})
	c.Assert(err, check.IsNil)
}

func (s *SnapOpSuite) TestRefreshOneJailmode(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/one")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":   "refresh",
			"jailmode": true,
		})
	}
	_, err := snap.Parser().ParseArgs([]string{"refresh", "--jailmode", "one"})
	c.Assert(err, check.IsNil)
}

func (s *SnapOpSuite) TestRefreshOneIgnoreValidation(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/one")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":            "refresh",
			"ignore-validation": true,
		})
	}
	_, err := snap.Parser().ParseArgs([]string{"refresh", "--ignore-validation", "one"})
	c.Assert(err, check.IsNil)
}

func (s *SnapOpSuite) TestRefreshOneRebooting(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/core")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action": "refresh",
		})
	}
	s.srv.rebooting = true

	restore := mockArgs("snap", "refresh", "core")
	defer restore()

	err := snap.RunMain()
	c.Check(err, check.IsNil)
	c.Check(s.Stderr(), check.Equals, "snapd is about to reboot the system\n")

}

func (s *SnapOpSuite) TestRefreshOneModeErr(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser().ParseArgs([]string{"refresh", "--jailmode", "--devmode", "one"})
	c.Assert(err, check.ErrorMatches, `cannot use devmode and jailmode flags together`)
}

func (s *SnapOpSuite) TestRefreshOneChanErr(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser().ParseArgs([]string{"refresh", "--beta", "--channel=foo", "one"})
	c.Assert(err, check.ErrorMatches, `Please specify a single channel`)
}

func (s *SnapOpSuite) TestRefreshAllChannel(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser().ParseArgs([]string{"refresh", "--beta"})
	c.Assert(err, check.ErrorMatches, `a single snap name is needed to specify mode or channel flags`)
}

func (s *SnapOpSuite) TestRefreshManyChannel(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser().ParseArgs([]string{"refresh", "--beta", "one", "two"})
	c.Assert(err, check.ErrorMatches, `a single snap name is needed to specify mode or channel flags`)
}

func (s *SnapOpSuite) TestRefreshManyIgnoreValidation(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser().ParseArgs([]string{"refresh", "--ignore-validation", "one", "two"})
	c.Assert(err, check.ErrorMatches, `a single snap name must be specified when ignoring validation`)
}

func (s *SnapOpSuite) TestRefreshAllModeFlags(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser().ParseArgs([]string{"refresh", "--devmode"})
	c.Assert(err, check.ErrorMatches, `a single snap name is needed to specify mode or channel flags`)
}

func (s *SnapOpSuite) TestRefreshOneAmend(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/one")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action": "refresh",
			"amend":  true,
		})
	}
	_, err := snap.Parser().ParseArgs([]string{"refresh", "--amend", "one"})
	c.Assert(err, check.IsNil)
}

func (s *SnapOpSuite) runTryTest(c *check.C, opts *client.SnapOptions) {
	// pass relative path to cmd
	tryDir := "some-dir"

	modes := []struct {
		enabled bool
		name    string
	}{
		{opts.DevMode, "devmode"},
		{opts.JailMode, "jailmode"},
		{opts.Classic, "classic"},
	}

	s.srv.checker = func(r *http.Request) {
		// ensure the client always sends the absolute path
		fullTryDir, err := filepath.Abs(tryDir)
		c.Assert(err, check.IsNil)

		c.Check(r.URL.Path, check.Equals, "/v2/snaps")
		form := testForm(r, c)
		defer form.RemoveAll()

		c.Assert(form.Value["action"], check.HasLen, 1)
		c.Assert(form.Value["snap-path"], check.HasLen, 1)
		c.Check(form.File, check.HasLen, 0)
		c.Check(form.Value["action"][0], check.Equals, "try")
		c.Check(form.Value["snap-path"][0], check.Matches, regexp.QuoteMeta(fullTryDir))

		for _, mode := range modes {
			if mode.enabled {
				c.Assert(form.Value[mode.name], check.HasLen, 1)
				c.Check(form.Value[mode.name][0], check.Equals, "true")
			} else {
				c.Check(form.Value[mode.name], check.IsNil)
			}
		}
	}

	s.RedirectClientToTestServer(s.srv.handle)

	cmd := []string{"try", tryDir}
	for _, mode := range modes {
		if mode.enabled {
			cmd = append(cmd, "--"+mode.name)
		}
	}

	rest, err := snap.Parser().ParseArgs(cmd)
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, fmt.Sprintf(`(?sm).*foo 1.0 mounted from .*%s`, tryDir))
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestTryNoMode(c *check.C) {
	s.runTryTest(c, &client.SnapOptions{})
}

func (s *SnapOpSuite) TestTryDevMode(c *check.C) {
	s.runTryTest(c, &client.SnapOptions{DevMode: true})
}

func (s *SnapOpSuite) TestTryJailMode(c *check.C) {
	s.runTryTest(c, &client.SnapOptions{JailMode: true})
}

func (s *SnapOpSuite) TestTryClassic(c *check.C) {
	s.runTryTest(c, &client.SnapOptions{Classic: true})
}

func (s *SnapOpSuite) TestTryNoSnapDirErrors(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		w.WriteHeader(202)
		fmt.Fprintln(w, `
{
  "type": "error",
  "result": {
    "message":"error from server",
    "kind":"snap-not-a-snap"
  },
  "status-code": 400
}`)
	})

	cmd := []string{"try", "/"}
	_, err := snap.Parser().ParseArgs(cmd)
	c.Assert(err, check.ErrorMatches, `"/" does not contain an unpacked snap.

Try 'snapcraft prime' in your project directory, then 'snap try' again.`)
}

func (s *SnapSuite) TestInstallChannelDuplicationError(c *check.C) {
	_, err := snap.Parser().ParseArgs([]string{"install", "--edge", "--beta", "some-snap"})
	c.Assert(err, check.ErrorMatches, "Please specify a single channel")
}

func (s *SnapSuite) TestRefreshChannelDuplicationError(c *check.C) {
	_, err := snap.Parser().ParseArgs([]string{"refresh", "--edge", "--beta", "some-snap"})
	c.Assert(err, check.ErrorMatches, "Please specify a single channel")
}

func (s *SnapOpSuite) TestInstallFromChannel(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":  "install",
			"channel": "edge",
		})
		s.srv.channel = "edge"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser().ParseArgs([]string{"install", "--edge", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo \(edge\) 1.0 from 'bar' installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestEnable(c *check.C) {
	s.srv.total = 3
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action": "enable",
		})
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser().ParseArgs([]string{"enable", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo enabled`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestDisable(c *check.C) {
	s.srv.total = 3
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action": "disable",
		})
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser().ParseArgs([]string{"disable", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo disabled`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestRemove(c *check.C) {
	s.srv.total = 3
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action": "remove",
		})
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser().ParseArgs([]string{"remove", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo removed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestRemoveManyRevision(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser().ParseArgs([]string{"remove", "--revision=17", "one", "two"})
	c.Assert(err, check.ErrorMatches, `a single snap name is needed to specify the revision`)
}

func (s *SnapOpSuite) TestRemoveMany(c *check.C) {
	total := 3
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
				"action": "remove",
				"snaps":  []interface{}{"one", "two"},
			})

			c.Check(r.Method, check.Equals, "POST")
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type":"async", "change": "42", "status-code": 202}`)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
			fmt.Fprintln(w, `{"type": "sync", "result": {"status": "Doing"}}`)
		case 2:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done", "data": {"snap-names": ["one","two"]}}}`)
		default:
			c.Fatalf("expected to get %d requests, now on %d", total, n+1)
		}

		n++
	})

	rest, err := snap.Parser().ParseArgs([]string{"remove", "one", "two"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*one removed`)
	c.Check(s.Stdout(), check.Matches, `(?sm).*two removed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, total)
}

func (s *SnapOpSuite) TestInstallManyChannel(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser().ParseArgs([]string{"install", "--beta", "one", "two"})
	c.Assert(err, check.ErrorMatches, `a single snap name is needed to specify mode or channel flags`)
}

func (s *SnapOpSuite) TestInstallManyMixFileAndStore(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser().ParseArgs([]string{"install", "store-snap", "./local.snap"})
	c.Assert(err, check.ErrorMatches, `only one snap file can be installed at a time`)
}

func (s *SnapOpSuite) TestInstallMany(c *check.C) {
	total := 4
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
				"action": "install",
				"snaps":  []interface{}{"one", "two"},
			})

			c.Check(r.Method, check.Equals, "POST")
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type":"async", "change": "42", "status-code": 202}`)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
			fmt.Fprintln(w, `{"type": "sync", "result": {"status": "Doing"}}`)
		case 2:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done", "data": {"snap-names": ["one","two"]}}}`)
		case 3:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			fmt.Fprintf(w, `{"type": "sync", "result": [{"name": "one", "status": "active", "version": "1.0", "developer": "bar", "publisher": {"id": "bar-id", "username": "bar", "display-name": "Bar"}, "revision":42, "channel":"stable"},{"name": "two", "status": "active", "version": "2.0", "developer": "baz", "publisher": {"id": "baz-id", "username": "baz", "display-name": "Baz"}, "revision":42, "channel":"edge"}]}\n`)

		default:
			c.Fatalf("expected to get %d requests, now on %d", total, n+1)
		}

		n++
	})

	rest, err := snap.Parser().ParseArgs([]string{"install", "one", "two"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	// note that (stable) is omitted
	c.Check(s.Stdout(), check.Matches, `(?sm).*one 1.0 from 'bar' installed`)
	c.Check(s.Stdout(), check.Matches, `(?sm).*two \(edge\) 2.0 from 'baz' installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, total)
}

func (s *SnapOpSuite) TestNoWait(c *check.C) {
	s.srv.checker = func(r *http.Request) {}

	cmds := [][]string{
		{"remove", "--no-wait", "foo"},
		{"remove", "--no-wait", "foo", "bar"},
		{"install", "--no-wait", "foo"},
		{"install", "--no-wait", "foo", "bar"},
		{"revert", "--no-wait", "foo"},
		{"refresh", "--no-wait", "foo"},
		{"refresh", "--no-wait", "foo", "bar"},
		{"refresh", "--no-wait"},
		{"enable", "--no-wait", "foo"},
		{"disable", "--no-wait", "foo"},
		{"try", "--no-wait", "."},
		{"switch", "--no-wait", "--channel=foo", "bar"},
		// commands that use waitMixin from elsewhere
		{"start", "--no-wait", "foo"},
		{"stop", "--no-wait", "foo"},
		{"restart", "--no-wait", "foo"},
		{"alias", "--no-wait", "foo", "bar"},
		{"unalias", "--no-wait", "foo"},
		{"prefer", "--no-wait", "foo"},
		{"set", "--no-wait", "foo", "bar=baz"},
		{"disconnect", "--no-wait", "foo:bar"},
		{"connect", "--no-wait", "foo:bar"},
	}

	s.RedirectClientToTestServer(s.srv.handle)
	for _, cmd := range cmds {
		rest, err := snap.Parser().ParseArgs(cmd)
		c.Assert(err, check.IsNil, check.Commentf("%v", cmd))
		c.Assert(rest, check.DeepEquals, []string{})
		c.Check(s.Stdout(), check.Matches, "(?sm)42\n")
		c.Check(s.Stderr(), check.Equals, "")
		c.Check(s.srv.n, check.Equals, 1)
		// reset
		s.srv.n = 0
		s.stdout.Reset()
	}
}

func (s *SnapOpSuite) TestNoWaitImmediateError(c *check.C) {

	cmds := [][]string{
		{"remove", "--no-wait", "foo"},
		{"remove", "--no-wait", "foo", "bar"},
		{"install", "--no-wait", "foo"},
		{"install", "--no-wait", "foo", "bar"},
		{"revert", "--no-wait", "foo"},
		{"refresh", "--no-wait", "foo"},
		{"refresh", "--no-wait", "foo", "bar"},
		{"refresh", "--no-wait"},
		{"enable", "--no-wait", "foo"},
		{"disable", "--no-wait", "foo"},
		{"try", "--no-wait", "."},
		{"switch", "--no-wait", "--channel=foo", "bar"},
		// commands that use waitMixin from elsewhere
		{"start", "--no-wait", "foo"},
		{"stop", "--no-wait", "foo"},
		{"restart", "--no-wait", "foo"},
		{"alias", "--no-wait", "foo", "bar"},
		{"unalias", "--no-wait", "foo"},
		{"prefer", "--no-wait", "foo"},
		{"set", "--no-wait", "foo", "bar=baz"},
		{"disconnect", "--no-wait", "foo:bar"},
		{"connect", "--no-wait", "foo:bar"},
	}

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "failure"}}`)
	})

	for _, cmd := range cmds {
		_, err := snap.Parser().ParseArgs(cmd)
		c.Assert(err, check.ErrorMatches, "failure", check.Commentf("%v", cmd))
	}
}

func (s *SnapOpSuite) TestWaitServerError(c *check.C) {
	r := snap.MockMaxGoneTime(0)
	defer r()

	cmds := [][]string{
		{"remove", "foo"},
		{"remove", "foo", "bar"},
		{"install", "foo"},
		{"install", "foo", "bar"},
		{"revert", "foo"},
		{"refresh", "foo"},
		{"refresh", "foo", "bar"},
		{"refresh"},
		{"enable", "foo"},
		{"disable", "foo"},
		{"try", "."},
		{"switch", "--channel=foo", "bar"},
		// commands that use waitMixin from elsewhere
		{"start", "foo"},
		{"stop", "foo"},
		{"restart", "foo"},
		{"alias", "foo", "bar"},
		{"unalias", "foo"},
		{"prefer", "foo"},
		{"set", "foo", "bar=baz"},
		{"disconnect", "foo:bar"},
		{"connect", "foo:bar"},
	}

	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type":"async", "change": "42", "status-code": 202}`)
			return
		}
		if n == 3 {
			fmt.Fprintln(w, `{"type": "error", "result": {"message": "unexpected request"}}`)
			return
		}
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "server error"}}`)
	})

	for _, cmd := range cmds {
		_, err := snap.Parser().ParseArgs(cmd)
		c.Assert(err, check.ErrorMatches, "server error", check.Commentf("%v", cmd))
		// reset
		n = 0
	}
}

func (s *SnapOpSuite) TestSwitchHappy(c *check.C) {
	s.srv.total = 3
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":  "switch",
			"channel": "beta",
		})
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser().ParseArgs([]string{"switch", "--beta", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*"foo" switched to the "beta" channel`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestSwitchUnhappy(c *check.C) {
	_, err := snap.Parser().ParseArgs([]string{"switch"})
	c.Assert(err, check.ErrorMatches, "the required argument `<snap>` was not provided")
}

func (s *SnapOpSuite) TestSwitchAlsoUnhappy(c *check.C) {
	_, err := snap.Parser().ParseArgs([]string{"switch", "foo"})
	c.Assert(err, check.ErrorMatches, `missing --channel=<channel-name> parameter`)
}

func (s *SnapOpSuite) TestSnapOpNetworkTimeoutError(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		w.WriteHeader(202)
		w.Write([]byte(`
{
  "type": "error",
  "result": {
    "message":"Get https://api.snapcraft.io/api/v1/snaps/details/hello?channel=stable&fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha3_384%2Csummary%2Cdescription%2Cdeltas%2Cbinary_filesize%2Cdownload_url%2Cepoch%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Cscreenshot_urls%2Csnap_id%2Clicense%2Cbase%2Csupport_url%2Ccontact%2Ctitle%2Ccontent%2Cversion%2Corigin%2Cdeveloper_id%2Cprivate%2Cconfinement%2Cchannel_maps_list: net/http: request canceled while waiting for connection (Client.Timeout exceeded while awaiting headers)",
    "kind":"network-timeout"
  },
  "status-code": 400
}
`))

	})

	cmd := []string{"install", "hello"}
	_, err := snap.Parser().ParseArgs(cmd)
	c.Assert(err, check.ErrorMatches, `unable to contact snap store`)
}
