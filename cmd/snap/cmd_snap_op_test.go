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
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/progress/progresstest"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

type snapOpTestServer struct {
	c *check.C

	checker             func(r *http.Request)
	n                   int
	total               int
	channel             string
	trackingChannel     string
	confinement         string
	restart             string
	snap                string
	components          []string
	onlyComponentChange string
	chgInWaitStatus     bool
}

var _ = check.Suite(&SnapOpSuite{})

func (t *snapOpTestServer) handle(w http.ResponseWriter, r *http.Request) {
	if len(t.components) > 0 && t.onlyComponentChange != "" {
		t.c.Fatalf("components and onlyComponentChange are mutually exclusive")
	}

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
		switch {
		case t.restart != "":
			fmt.Fprintln(w, fmt.Sprintf(`{"type": "sync", "result": {"status": "Doing"}, "maintenance": {"kind": "system-restart", "message": "system is %sing", "value": {"op": %q}}}}`, t.restart, t.restart))
		case t.chgInWaitStatus:
			fmt.Fprintln(w, `{"type": "sync", "result": {"status": "Wait", "id":"42"}}`)
		default:
			fmt.Fprintln(w, `{"type": "sync", "result": {"status": "Doing"}}`)
		}
	case 2:
		t.c.Check(r.Method, check.Equals, "GET")
		t.c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
		var data struct {
			SnapNames  []string            `json:"snap-names,omitempty"`
			Components map[string][]string `json:"components,omitempty"`
		}

		switch {
		case t.components != nil:
			data.Components = map[string][]string{
				t.snap: t.components,
			}
			data.SnapNames = []string{t.snap}
		case t.onlyComponentChange != "":
			data.Components = map[string][]string{
				t.snap: {t.onlyComponentChange},
			}
		default:
			data.SnapNames = []string{t.snap}
		}

		encoded, err := json.Marshal(data)
		t.c.Assert(err, check.IsNil)

		fmt.Fprintf(w, `{"type": "sync", "result": {"ready": true, "status": "Done", "data": %s}}\n`, string(encoded))
	case 3:
		t.c.Check(r.Method, check.Equals, "GET")
		t.c.Check(r.URL.Path, check.Equals, "/v2/snaps")

		compNames := t.components
		if t.onlyComponentChange != "" {
			compNames = []string{t.onlyComponentChange}
		}

		var compsData string
		if len(compNames) > 0 {
			comps := make([]map[string]string, 0, len(compNames))
			for _, comp := range compNames {
				comps = append(comps, map[string]string{"name": comp, "version": "3.2"})
			}

			encoded, err := json.Marshal(comps)
			t.c.Assert(err, check.IsNil)

			compsData = fmt.Sprintf(`, "components": %s`, string(encoded))
		}

		fmt.Fprintf(w, `{"type": "sync", "result": [{"name": "%s", "status": "active", "version": "1.0", "developer": "bar", "publisher": {"id": "bar-id", "username": "bar", "display-name": "Bar", "validation": "unproven"}, "revision":42, "channel":"%s", "tracking-channel": "%s", "confinement": "%s"%s}]}\n`,
			t.snap, t.channel, t.trackingChannel, t.confinement, compsData)
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

	restoreClientRetry := client.MockDoTimings(time.Millisecond, time.Second)
	restorePollTime := snap.MockPollTime(time.Millisecond)
	s.restoreAll = func() {
		restoreClientRetry()
		restorePollTime()
	}

	s.srv = snapOpTestServer{
		c:     c,
		total: 4,
		snap:  "foo",
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

	// should always result in a connection refused error, since port zero isn't
	// valid
	snap.ClientConfig.BaseURL = "http://localhost:0"
	s.BaseTest.AddCleanup(func() { snap.ClientConfig.BaseURL = "" })

	cli := snap.Client()
	chg, err := snap.Wait(cli, "x")
	c.Assert(chg, check.IsNil)
	c.Assert(err, check.NotNil)
	c.Check(meter.Labels, testutil.Contains, "Waiting for server to restart")
}

func (s *SnapOpSuite) TestWaitRecovers(c *check.C) {
	meter := &progresstest.Meter{}
	defer progress.MockMeter(meter)()

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
	c.Check(chg, check.NotNil)
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

func (s *SnapOpSuite) TestWaitDaemonUnavailableWithMaintenance(c *check.C) {
	meter := &progresstest.Meter{}
	defer progress.MockMeter(meter)()
	restore := snap.MockMaxGoneTime(time.Millisecond)
	defer restore()

	dirs.SetRootDir(c.MkDir())
	err := os.MkdirAll(filepath.Dir(dirs.SnapdMaintenanceFile), 0755)
	c.Assert(err, check.IsNil)

	maintErr := client.Error{
		Kind:    client.ErrorKindSystemRestart,
		Message: "system is restarting",
	}
	b, err := json.Marshal(&maintErr)
	c.Assert(err, check.IsNil)

	// write the maintenance json
	os.WriteFile(dirs.SnapdMaintenanceFile, b, 0666)

	// use a port that we can't connect to anyway
	snap.ClientConfig.BaseURL = "http://localhost:0"
	s.BaseTest.AddCleanup(func() { snap.ClientConfig.BaseURL = "" })

	cli := snap.Client()
	chg, err := snap.Wait(cli, "x")
	c.Assert(chg, check.IsNil)
	c.Assert(err, check.ErrorMatches, `system is restarting`)
}

func (s *SnapOpSuite) TestWaitStateShowsLog(c *check.C) {
	meter := &progresstest.Meter{}
	defer progress.MockMeter(meter)()
	restore := snap.MockMaxGoneTime(time.Millisecond)
	defer restore()

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "sync",
"result": {
"ready": false,
"status": "Wait",
"tasks": [{"kind": "check-rerefresh", "summary": "...", "status": "Doing", "progress": {"done": 1, "total": 1}}, {"kind": "install", "summary": "...", "status": "Wait", "progress": {"done": 1, "total": 1}, "log": ["INFO: some info about the wait reason"]}]
}}}`)
	})

	cli := snap.Client()
	// Wait() exists once a change is in "Wait" state
	chg, err := snap.Wait(cli, "x")
	c.Assert(err, check.IsNil)
	c.Check(chg.Ready, check.Equals, false)

	// information from wait task is displayed
	c.Check(meter.Notices, testutil.Contains, "INFO: some info about the wait reason")
}

func (s *SnapOpSuite) TestInstall(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "install",
			"channel":     "candidate",
			"cohort-key":  "what",
			"transaction": string(client.TransactionPerSnap),
		})
		s.srv.channel = "candidate"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--channel", "candidate", "--cohort", "what", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo \(candidate\) 1.0 from Bar installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallWithComponent(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "install",
			"channel":     "candidate",
			"cohort-key":  "what",
			"components":  []interface{}{"comp1", "comp2"},
			"transaction": string(client.TransactionPerSnap),
		})
		s.srv.channel = "candidate"
	}

	s.srv.components = []string{"comp1", "comp2"}
	s.RedirectClientToTestServer(s.srv.handle)

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--channel", "candidate", "--cohort", "what", "foo+comp1+comp2"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})

	c.Check(s.Stdout(), check.Matches, `(?sm).*foo \(candidate\) 1.0 from Bar installed`)
	c.Check(s.Stdout(), check.Matches, `(?sm).*component comp1 3.2 for foo \(candidate\) 1.0 installed`)
	c.Check(s.Stdout(), check.Matches, `(?sm).*component comp2 3.2 for foo \(candidate\) 1.0 installed`)
	c.Check(s.Stderr(), check.Equals, "")

	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallManyWithComponents(c *check.C) {
	total := 4
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
				"action": "install",
				"snaps":  []interface{}{"one", "two", "three"},
				"components": map[string]interface{}{
					"one": []interface{}{"comp1", "comp2"},
					"two": []interface{}{"comp3", "comp4"},
				},
				"transaction": string(client.TransactionPerSnap),
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
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done", "data": {"snap-names": ["one","two", "three"], "components": {"one": ["comp1", "comp2"], "two": ["comp3", "comp4"]}}}}`)
		case 3:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")

			oneComps := `[{"name": "comp1", "version": "3.2"}, {"name": "comp2", "version": "3.2"}]`
			twoComps := `[{"name": "comp3", "version": "3.2"}, {"name": "comp4", "version": "3.2"}]`

			fmt.Fprintf(w, `{"type": "sync", "result": [{"name": "one", "status": "active", "version": "1.0", "developer": "bar", "publisher": {"id": "bar-id", "username": "bar", "display-name": "Bar", "validation": "unproven"}, "revision":42, "channel":"stable", "components": %s},{"name": "two", "status": "active", "version": "2.0", "developer": "baz", "publisher": {"id": "baz-id", "username": "baz", "display-name": "Baz", "validation": "unproven"}, "revision":42, "channel":"edge", "components": %s},{"name": "three", "status": "active", "version": "2.0", "developer": "foo", "publisher": {"id": "foo-id", "username": "foo", "display-name": "Foo", "validation": "unproven"}, "revision":42, "channel":"edge"}]}\n`, oneComps, twoComps)

		default:
			c.Fatalf("expected to get %d requests, now on %d", total, n+1)
		}

		n++
	})

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "one+comp1+comp2", "two+comp3+comp4", "three"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})

	c.Check(s.Stdout(), check.Matches, `(?sm).*one 1\.0 from Bar installed`)
	c.Check(s.Stdout(), check.Matches, `(?sm).*two \(edge\) 2\.0 from Baz installed`)
	c.Check(s.Stdout(), check.Matches, `(?sm).*component comp1 3\.2 for one 1\.0 installed`)
	c.Check(s.Stdout(), check.Matches, `(?sm).*component comp2 3\.2 for one 1\.0 installed`)
	c.Check(s.Stdout(), check.Matches, `(?sm).*component comp3 3\.2 for two \(edge\) 2\.0 installed`)
	c.Check(s.Stdout(), check.Matches, `(?sm).*component comp4 3\.2 for two \(edge\) 2\.0 installed`)
	c.Check(s.Stderr(), check.Equals, "")

	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, total)
}

func (s *SnapOpSuite) TestInstallWithWaitStatus(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
	}

	s.srv.chgInWaitStatus = true
	s.srv.total = 2
	s.RedirectClientToTestServer(s.srv.handle)

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*Change 42 waiting on external action to be completed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestListReportsRestartError(c *check.C) {
	var n int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type":"async", "change": "42", "status-code": 202}`)
		case 1:
			fmt.Fprintf(w, `{"type": "sync", "result": {"ready": true, "status": "Done", "data": {"snap-names": ["foo"]}}}\n`)
		case 2:
			fmt.Fprintf(w, `{"type": "sync", "maintenance": {"kind": "system-restart", "message": "system is restarting", "value": {"op": "system-restart"}}}}, "result": [{"name": "foo", "status": "active", "version": "1.0", "developer": "bar", "publisher": {"id": "bar-id", "username": "bar", "display-name": "Bar", "validation": "unproven"}, "revision":42, "channel":"stable", "tracking-channel": "stable", "confinement": "classic"}]}\n`)
		default:
			errMsg := fmt.Sprintf("expected to get 3 requests, now on %d", n+1)
			c.Error(errMsg)
			w.WriteHeader(500)
			w.Write([]byte(errMsg))
		}
		n++
	})

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "foo"})
	c.Assert(err, check.DeepEquals, &client.Error{Kind: client.ErrorKindSystemRestart, Value: map[string]interface{}{"op": "system-restart"}, Message: "system is restarting"})
	c.Assert(rest, check.DeepEquals, []string{"foo"})
	c.Check(s.Stdout(), check.Matches, "")
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, 3)
}

func (s *SnapOpSuite) TestInstallIgnoreRunning(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":         "install",
			"ignore-running": true,
			"transaction":    string(client.TransactionPerSnap),
		})
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--ignore-running", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo 1.0 from Bar installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallNoPATH(c *check.C) {
	// PATH restored by test tear down
	os.Setenv("PATH", "/bin:/usr/bin:/sbin:/usr/sbin")
	// SUDO_UID env must be unset in this test
	if sudoUidEnv, isSet := os.LookupEnv("SUDO_UID"); isSet {
		os.Unsetenv("SUDO_UID")
		defer os.Setenv("SUDO_UID", sudoUidEnv)
	}

	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "install",
			"channel":     "candidate",
			"transaction": string(client.TransactionPerSnap),
		})
		s.srv.channel = "candidate"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--channel", "candidate", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo \(candidate\) 1.0 from Bar installed`)
	c.Check(s.Stderr(), testutil.MatchesWrapped, `Warning: \S+/bin was not found in your \$PATH.*`)
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallNoPATHMaybeResetBySudo(c *check.C) {
	// PATH restored by test tear down
	os.Setenv("PATH", "/bin:/usr/bin:/sbin:/usr/sbin")
	if old, isset := os.LookupEnv("SUDO_UID"); isset {
		defer os.Setenv("SUDO_UID", old)
	}
	os.Setenv("SUDO_UID", "1234")
	restore := release.MockReleaseInfo(&release.OS{ID: "fedora"})
	defer restore()
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "install",
			"channel":     "candidate",
			"transaction": string(client.TransactionPerSnap),
		})
		s.srv.channel = "candidate"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--channel", "candidate", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo \(candidate\) 1.0 from Bar installed`)
	c.Check(s.Stderr(), check.HasLen, 0)
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallFromTrack(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "install",
			"channel":     "3.4",
			"transaction": string(client.TransactionPerSnap),
		})
		s.srv.channel = "3.4/stable"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	// snap install --channel=3.4 means 3.4/stable, this is what we test here
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--channel", "3.4", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo \(3.4/stable\) 1.0 from Bar installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallFromBranch(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "install",
			"channel":     "3.4/stable/hotfix-1",
			"transaction": string(client.TransactionPerSnap),
		})
		s.srv.channel = "3.4/stable/hotfix-1"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--channel", "3.4/stable/hotfix-1", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo \(3.4/stable/hotfix-1\) 1.0 from Bar installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallSameRiskInTrack(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "install",
			"channel":     "latest/stable",
			"transaction": string(client.TransactionPerSnap),
		})
		s.srv.channel = "stable"
		s.srv.trackingChannel = "latest/stable"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--channel", "latest/stable", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, "foo 1.0 from Bar installed\n")
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallSameRiskInDefaultTrack(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "install",
			"channel":     "stable",
			"transaction": string(client.TransactionPerSnap),
		})
		s.srv.channel = "18/stable"
		s.srv.trackingChannel = "18/stable"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--stable", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, "foo (18/stable) 1.0 from Bar installed\n")
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallRiskChannelClosed(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "install",
			"channel":     "edge",
			"transaction": string(client.TransactionPerSnap),
		})
		s.srv.channel = "stable"
		s.srv.trackingChannel = "edge"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--channel", "edge", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `foo 1.0 from Bar installed
Channel edge for foo is closed; temporarily forwarding to stable.
`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallDevMode(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "install",
			"devmode":     true,
			"channel":     "beta",
			"transaction": string(client.TransactionPerSnap),
		})
		s.srv.channel = "beta"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--channel", "beta", "--devmode", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo \(beta\) 1.0 from Bar installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallQuotaGroup(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "install",
			"channel":     "candidate",
			"quota-group": "test-group",
			"transaction": string(client.TransactionPerSnap),
		})
		s.srv.channel = "candidate"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--channel", "candidate", "--quota-group", "test-group", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo \(candidate\) 1.0 from Bar installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallClassic(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "install",
			"classic":     true,
			"transaction": string(client.TransactionPerSnap),
		})
		s.srv.confinement = "classic"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--classic", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo 1.0 from Bar installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallStrictWithClassicFlag(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "install",
			"classic":     true,
			"transaction": string(client.TransactionPerSnap),
		})
		s.srv.confinement = "strict"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--classic", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo 1.0 from Bar installed`)
	c.Check(s.Stderr(), testutil.MatchesWrapped, `Warning:\s+flag --classic ignored for strictly confined snap foo.*`)
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallUnaliased(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "install",
			"unaliased":   true,
			"transaction": string(client.TransactionPerSnap),
		})
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--unaliased", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo 1.0 from Bar installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallPrefer(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "install",
			"prefer":      true,
			"transaction": string(client.TransactionPerSnap),
		})
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--prefer", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo 1.0 from Bar installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallSnapNotFound(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "snap not found", "value": "foo", "kind": "snap-not-found"}, "status-code": 404}`)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("error: %v\n", err), check.Equals, `error: snap "foo" not found
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailable(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision available as specified", "value": "foo", "kind": "snap-revision-not-available"}, "status-code": 404}`)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("\nerror: %v\n", err), check.Equals, `
error: snap "foo" not available as specified (see 'snap info foo')
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailableOnChannel(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision available as specified", "value": "foo", "kind": "snap-revision-not-available"}, "status-code": 404}`)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--channel=mytrack", "foo"})
	c.Check(err, check.ErrorMatches, `snap "foo" not available on channel "mytrack" \(see 'snap info foo'\)`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailableAtRevision(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision available as specified", "value": "foo", "kind": "snap-revision-not-available"}, "status-code": 404}`)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--revision=2", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("\nerror: %v\n", err), check.Equals, `
error: snap "foo" revision 2 not available (see 'snap info foo')
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailableForChannelTrackOK(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision on specified channel", "value": {
  "snap-name": "foo",
  "action": "install",
  "architecture": "amd64",
  "channel": "stable",
  "releases": [{"architecture": "amd64", "channel": "beta"},
               {"architecture": "amd64", "channel": "edge"}]
}, "kind": "snap-channel-not-available"}, "status-code": 404}`)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("\nerror: %v\n", err), check.Equals, `
error: snap "foo" is not available on stable but is available to install on the
       following channels:

       beta       snap install --beta foo
       edge       snap install --edge foo

       Please be mindful pre-release channels may include features not
       completely tested or implemented. Get more information with 'snap info
       foo'.
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailableForChannelTrackOKPrerelOK(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision on specified channel", "value": {
  "snap-name": "foo",
  "action": "install",
  "architecture": "amd64",
  "channel": "candidate",
  "releases": [{"architecture": "amd64", "channel": "beta"},
               {"architecture": "amd64", "channel": "edge"}]
}, "kind": "snap-channel-not-available"}, "status-code": 404}`)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--candidate", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("\nerror: %v\n", err), check.Equals, `
error: snap "foo" is not available on candidate but is available to install on
       the following channels:

       beta       snap install --beta foo
       edge       snap install --edge foo

       Get more information with 'snap info foo'.
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailableForChannelTrackOther(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision on specified channel", "value": {
  "snap-name": "foo",
  "action": "install",
  "architecture": "amd64",
  "channel": "stable",
  "releases": [{"architecture": "amd64", "channel": "1.0/stable"},
               {"architecture": "amd64", "channel": "2.0/stable"}]
}, "kind": "snap-channel-not-available"}, "status-code": 404}`)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "foo"})
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
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision on specified channel", "value": {
  "snap-name": "foo",
  "action": "install",
  "architecture": "amd64",
  "channel": "2.0/stable",
  "releases": [{"architecture": "amd64", "channel": "stable"}]
}, "kind": "snap-channel-not-available"}, "status-code": 404}`)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--channel=2.0/stable", "foo"})
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
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision on specified channel", "value": {
  "snap-name": "foo",
  "action": "install",
  "architecture": "amd64",
  "channel": "2.0/stable",
  "releases": [{"architecture": "amd64", "channel": "1.0/edge"}]
}, "kind": "snap-channel-not-available"}, "status-code": 404}`)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--channel=2.0/stable", "foo"})
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
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision on specified architecture", "value": {
  "snap-name": "foo",
  "action": "install",
  "architecture": "arm64",
  "channel": "stable",
  "releases": [{"architecture": "amd64", "channel": "stable"},
               {"architecture": "s390x", "channel": "stable"}]
}, "kind": "snap-architecture-not-available"}, "status-code": 404}`)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("\nerror: %v\n", err), check.Equals, `
error: snap "foo" is not available on stable for this architecture (arm64) but
       exists on other architectures (amd64, s390x).
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailableForArchitectureTrackAndRiskOther(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision on specified architecture", "value": {
  "snap-name": "foo",
  "action": "install",
  "architecture": "arm64",
  "channel": "1.0/stable",
  "releases": [{"architecture": "amd64", "channel": "stable"},
               {"architecture": "s390x", "channel": "stable"}]
}, "kind": "snap-architecture-not-available"}, "status-code": 404}`)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--channel=1.0/stable", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("\nerror: %v\n", err), check.Equals, `
error: snap "foo" is not available on this architecture (arm64) but exists on
       other architectures (amd64, s390x).
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailableInvalidChannel(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Fatal("unexpected call to server")
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--channel=a/b/c/d", "foo"})
	c.Assert(err, check.ErrorMatches, "channel name has too many components: a/b/c/d")

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailableForChannelNonExistingBranchOnMainChannel(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision on specified channel", "value": {
  "snap-name": "foo",
  "action": "install",
  "architecture": "amd64",
  "channel": "stable/baz",
  "releases": [{"architecture": "amd64", "channel": "stable"}]
}, "kind": "snap-channel-not-available"}, "status-code": 404}`)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--channel=stable/baz", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("\nerror: %v\n", err), check.Equals, `
error: requested a non-existing branch on latest/stable for snap "foo": baz
`)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallSnapRevisionNotAvailableForChannelNonExistingBranch(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "no snap revision on specified channel", "value": {
  "snap-name": "foo",
  "action": "install",
  "architecture": "amd64",
  "channel": "stable/baz",
  "releases": [{"architecture": "amd64", "channel": "edge"}]
}, "kind": "snap-channel-not-available"}, "status-code": 404}`)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--channel=stable/baz", "foo"})
	c.Assert(err, check.NotNil)
	c.Check(fmt.Sprintf("\nerror: %v\n", err), check.Equals, `
error: requested a non-existing branch for snap "foo": latest/stable/baz
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
		content, err = io.ReadAll(body)
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
		c.Check(form.Value["transaction"], check.NotNil)
		c.Check(form.Value, check.HasLen, 3)

		name, _, body := formFile(form, c)
		c.Check(name, check.Equals, "snap")
		c.Check(string(body), check.Equals, "snap-data")
	}

	snapBody := []byte("snap-data")
	s.RedirectClientToTestServer(s.srv.handle)
	snapPath := filepath.Join(c.MkDir(), "foo.snap")
	err := os.WriteFile(snapPath, snapBody, 0644)
	c.Assert(err, check.IsNil)

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", snapPath})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo 1.0 from Bar installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestComponentInstallPath(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps")

		form := testForm(r, c)
		defer form.RemoveAll()

		c.Check(form.Value["action"], check.DeepEquals, []string{"install"})
		c.Check(form.Value["devmode"], check.IsNil)
		c.Check(form.Value["snap-path"], check.NotNil)
		c.Check(form.Value["transaction"], check.NotNil)
		c.Check(form.Value, check.HasLen, 3)

		name, _, body := formFile(form, c)
		c.Check(name, check.Equals, "snap")
		c.Check(string(body), check.Equals, "component-data")
	}
	s.srv.onlyComponentChange = "mycomp"

	snapBody := []byte("component-data")
	s.RedirectClientToTestServer(s.srv.handle)
	snapPath := filepath.Join(c.MkDir(), "foo.comp")
	err := os.WriteFile(snapPath, snapBody, 0644)
	c.Assert(err, check.IsNil)

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", snapPath})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*component mycomp 3.2 for foo 1.0 installed`)
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
		c.Check(form.Value["transaction"], check.NotNil)
		c.Check(form.Value, check.HasLen, 4)

		name, _, body := formFile(form, c)
		c.Check(name, check.Equals, "snap")
		c.Check(string(body), check.Equals, "snap-data")
	}

	snapBody := []byte("snap-data")
	s.RedirectClientToTestServer(s.srv.handle)
	snapPath := filepath.Join(c.MkDir(), "foo.snap")
	err := os.WriteFile(snapPath, snapBody, 0644)
	c.Assert(err, check.IsNil)

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--devmode", snapPath})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo 1.0 from Bar installed`)
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
		c.Check(form.Value["transaction"], check.NotNil)
		c.Check(form.Value, check.HasLen, 4)

		name, _, body := formFile(form, c)
		c.Check(name, check.Equals, "snap")
		c.Check(string(body), check.Equals, "snap-data")

		s.srv.confinement = "classic"
	}

	snapBody := []byte("snap-data")
	s.RedirectClientToTestServer(s.srv.handle)
	snapPath := filepath.Join(c.MkDir(), "foo.snap")
	err := os.WriteFile(snapPath, snapBody, 0644)
	c.Assert(err, check.IsNil)

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--classic", snapPath})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo 1.0 from Bar installed`)
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
		c.Check(form.Value["transaction"], check.NotNil)
		c.Check(form.Value, check.HasLen, 4)

		name, _, body := formFile(form, c)
		c.Check(name, check.Equals, "snap")
		c.Check(string(body), check.Equals, "snap-data")
	}

	snapBody := []byte("snap-data")
	s.RedirectClientToTestServer(s.srv.handle)
	snapPath := filepath.Join(c.MkDir(), "foo.snap")
	err := os.WriteFile(snapPath, snapBody, 0644)
	c.Assert(err, check.IsNil)

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--dangerous", snapPath})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo 1.0 from Bar installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallPathQuotaGroup(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps")
		form := testForm(r, c)
		defer form.RemoveAll()

		c.Check(form.Value["action"], check.DeepEquals, []string{"install"})
		c.Check(form.Value["quota-group"], check.DeepEquals, []string{"foo"})
		c.Check(form.Value["snap-path"], check.NotNil)
		c.Check(form.Value["transaction"], check.NotNil)
		c.Check(form.Value, check.HasLen, 4)

		name, _, body := formFile(form, c)
		c.Check(name, check.Equals, "snap")
		c.Check(string(body), check.Equals, "snap-data")
	}

	snapBody := []byte("snap-data")
	s.RedirectClientToTestServer(s.srv.handle)
	snapPath := filepath.Join(c.MkDir(), "foo.snap")
	err := os.WriteFile(snapPath, snapBody, 0644)
	c.Assert(err, check.IsNil)

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--quota-group", "foo", snapPath})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo 1.0 from Bar installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallPathManyTransactional(c *check.C) {
	snaps := []string{"foo.snap", "bar.snap"}
	total := 4
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			c.Check(r.Method, check.Equals, "POST")

			form := testForm(r, c)
			defer form.RemoveAll()
			c.Check(form.Value["action"], check.DeepEquals, []string{"install"})
			c.Check(form.Value["transaction"], check.DeepEquals, []string{string(client.TransactionAllSnaps)})
			c.Check(form.Value, check.HasLen, 2)
			names, filenames, bodies := formFiles(form, c)
			for i, name := range names {
				c.Check(name, check.Equals, "snap")
				c.Check(filenames[i], check.Equals, snaps[i])
				c.Assert(string(bodies[i]), check.Equals, "snap-data")
			}
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
			fmt.Fprintf(w, `{"type": "sync", "result": [{"name": "one", "version": "1.0", "developer": "bar", "publisher": {"id": "bar-id", "username": "bar", "display-name": "Bar"}},{"name": "two", "version": "2.0", "developer": "baz", "publisher": {"id": "baz-id", "username": "baz", "display-name": "Baz"}}]}\n`)

		default:
			c.Fatalf("expected to get %d requests, now on %d", total, n+1)
		}

		n++
	})

	args := []string{"install", "--transaction=all-snaps"}
	for _, snap := range snaps {
		path := filepath.Join(c.MkDir(), snap)
		args = append(args, path)
		err := os.WriteFile(path, []byte("snap-data"), 0644)
		c.Assert(err, check.IsNil)
	}

	rest, err := snap.Parser(snap.Client()).ParseArgs(args)
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})

	c.Check(s.Stdout(), check.Matches, `(?sm).*one 1.0 from Bar installed`)
	c.Check(s.Stdout(), check.Matches, `(?sm).*two 2.0 from Baz installed`)
	c.Check(s.Stderr(), check.Equals, "")

	c.Check(n, check.Equals, total)
}

func (s *SnapOpSuite) TestInstallPathInstance(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps")

		form := testForm(r, c)
		defer form.RemoveAll()

		c.Check(form.Value["action"], check.DeepEquals, []string{"install"})
		c.Check(form.Value["name"], check.DeepEquals, []string{"foo_bar"})
		c.Check(form.Value["devmode"], check.IsNil)
		c.Check(form.Value["snap-path"], check.NotNil)
		c.Check(form.Value["transaction"], check.NotNil)
		c.Check(form.Value, check.HasLen, 4)

		name, _, body := formFile(form, c)
		c.Check(name, check.Equals, "snap")
		c.Check(string(body), check.Equals, "snap-data")
	}

	snapBody := []byte("snap-data")
	s.RedirectClientToTestServer(s.srv.handle)
	// instance is named foo_bar
	s.srv.snap = "foo_bar"
	snapPath := filepath.Join(c.MkDir(), "foo.snap")
	err := os.WriteFile(snapPath, snapBody, 0644)
	c.Assert(err, check.IsNil)

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", snapPath, "--name", "foo_bar"})
	c.Assert(rest, check.DeepEquals, []string{})
	c.Assert(err, check.IsNil)
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo_bar 1.0 from Bar installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallPathMany(c *check.C) {
	snaps := []string{"foo.snap", "bar.snap"}
	total := 4
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			c.Check(r.Method, check.Equals, "POST")

			form := testForm(r, c)
			defer form.RemoveAll()
			c.Check(form.Value["action"], check.DeepEquals, []string{"install"})
			c.Check(form.Value["transaction"], check.NotNil)
			c.Check(form.Value, check.HasLen, 2)
			names, filenames, bodies := formFiles(form, c)
			for i, name := range names {
				c.Check(name, check.Equals, "snap")
				c.Check(filenames[i], check.Equals, snaps[i])
				c.Assert(string(bodies[i]), check.Equals, "snap-data")
			}
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
			fmt.Fprintf(w, `{"type": "sync", "result": [{"name": "one", "version": "1.0", "developer": "bar", "publisher": {"id": "bar-id", "username": "bar", "display-name": "Bar"}},{"name": "two", "version": "2.0", "developer": "baz", "publisher": {"id": "baz-id", "username": "baz", "display-name": "Baz"}}]}\n`)

		default:
			c.Fatalf("expected to get %d requests, now on %d", total, n+1)
		}

		n++
	})

	args := []string{"install"}
	for _, snap := range snaps {
		path := filepath.Join(c.MkDir(), snap)
		args = append(args, path)
		err := os.WriteFile(path, []byte("snap-data"), 0644)
		c.Assert(err, check.IsNil)
	}

	rest, err := snap.Parser(snap.Client()).ParseArgs(args)
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})

	c.Check(s.Stdout(), check.Matches, `(?sm).*one 1.0 from Bar installed`)
	c.Check(s.Stdout(), check.Matches, `(?sm).*two 2.0 from Baz installed`)
	c.Check(s.Stderr(), check.Equals, "")

	c.Check(n, check.Equals, total)
}

func formFiles(form *multipart.Form, c *check.C) (names, filenames []string, contents [][]byte) {
	for name, fheaders := range form.File {
		for _, h := range fheaders {
			body, err := h.Open()
			c.Assert(err, check.IsNil)
			defer body.Close()

			content, err := io.ReadAll(body)
			c.Assert(err, check.IsNil)
			contents = append(contents, content)
			filenames = append(filenames, h.Filename)
		}

		names = append(names, name)
	}

	return names, filenames, contents
}

func (s *SnapOpSuite) TestInstallPathManyChannel(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--beta", "one.snap", "two.snap"})
	c.Assert(err, check.ErrorMatches, `a single snap name is needed to specify channel flags`)
}

func (s *SnapOpSuite) TestInstallPathManyPrefer(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--prefer", "one.snap", "two.snap"})
	c.Assert(err, check.ErrorMatches, `a single snap name is needed to specify the prefer flag`)
}

func (s *SnapOpSuite) TestInstallPathManyMode(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--classic", "foo.snap", "bar.snap"})
	// allows mode with many local snaps (err is unrelated)
	c.Assert(err, check.ErrorMatches, `cannot open "foo.snap":.*`)
}

func (s *SnapSuite) TestInstallWithInstanceNoPath(c *check.C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--name", "foo_bar", "some-snap"})
	c.Assert(err, check.ErrorMatches, "cannot use explicit name when installing from store")
}

func (s *SnapSuite) TestInstallManyWithInstance(c *check.C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--name", "foo_bar", "some-snap-1", "some-snap-2"})
	c.Assert(err, check.ErrorMatches, "cannot use instance name when installing multiple snaps")
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
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"revert", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	// tracking channel is "" in the test server
	c.Check(s.Stdout(), check.Equals, `foo reverted to 1.0
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

	rest, err := snap.Parser(snap.Client()).ParseArgs(cmd)
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
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"revert"})
	c.Assert(err, check.NotNil)
	c.Assert(err, check.ErrorMatches, "the required argument `<snap>` was not provided")
}

func (s *SnapSuite) TestRefreshListLessOptions(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Fatal("expected to get 0 requests")
	})

	for _, flag := range []string{"--beta", "--channel=potato", "--classic"} {
		_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--list", flag})
		c.Assert(err, check.ErrorMatches, "--list does not accept additional arguments")

		_, err = snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--list", flag, "some-snap"})
		c.Assert(err, check.ErrorMatches, "--list does not accept additional arguments")
	}
}

func (s *SnapSuite) TestRefreshList(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			c.Check(r.URL.Query().Get("select"), check.Equals, "refresh")
			fmt.Fprintln(w, `{"type": "sync", "result": [{"name": "foo", "status": "active", "version": "4.2update1", "developer": "bar", "download-size": 436375552, "publisher": {"id": "bar-id", "username": "bar", "display-name": "Bar", "validation": "unproven"}, "revision":17,"summary":"some summary"}]}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--list"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `Name +Version +Rev +Size +Publisher +Notes
foo +4.2update1 +17 +436MB +bar +-.*
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
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--time", "--abs-time"})
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
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--time", "--abs-time"})
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

func (s *SnapSuite) TestRefreshTimeShowsHolds(c *check.C) {
	type testcase struct {
		in  string
		out string
	}

	curTime, err := time.Parse(time.RFC3339, "2017-04-27T23:00:00+02:00")
	c.Assert(err, check.IsNil)
	restore := snap.MockTimeNow(func() time.Time {
		return curTime
	})
	defer restore()

	for _, tc := range []testcase{
		{in: "2017-04-28T00:00:00+02:00", out: "2017-04-28T00:00:00+02:00"},
		{in: "2117-04-28T00:00:00+02:00", out: "forever"},
	} {
		n := 0
		s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
			switch n {
			case 0:
				c.Check(r.Method, check.Equals, "GET")
				c.Check(r.URL.Path, check.Equals, "/v2/system-info")
				fmt.Fprintf(w, `{"type": "sync", "status-code": 200, "result": {"refresh": {"timer": "0:00-24:00/4", "last": "2017-04-25T17:35:00+02:00", "next": "2017-04-26T00:58:00+02:00", "hold": %q}}}`, tc.in)
			default:
				errMsg := fmt.Sprintf("expected to get 1 requests, now on %d", n+1)
				c.Error(errMsg)
				w.WriteHeader(500)
				w.Write([]byte(errMsg))
			}

			n++
		})

		rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--time", "--abs-time"})
		c.Assert(err, check.IsNil)
		c.Assert(rest, check.DeepEquals, []string{})
		expectedOutput := fmt.Sprintf(`timer: 0:00-24:00/4
last: 2017-04-25T17:35:00+02:00
hold: %s
next: 2017-04-26T00:58:00+02:00 (but held)
`, tc.out)
		c.Check(s.Stdout(), check.Equals, expectedOutput)
		c.Check(s.Stderr(), check.Equals, "")
		// ensure that the fake server api was actually hit
		c.Check(n, check.Equals, 1)
		s.ResetStdStreams()
	}
}

func (s *SnapSuite) TestRefreshHoldAllForever(c *check.C) {
	var n int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
				"action":     "hold",
				"time":       "forever",
				"hold-level": "auto-refresh",
			})
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type": "async", "change": "42", "status-code": 202}`)

		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
			w.WriteHeader(200)
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done"}}`)

		default:
			c.Errorf("expected to get 2 requests, now on %d", n+1)
			fmt.Fprintln(w, `{"type": "error", "result": {"message": "received too many requests"}, "status-code": 500}`)
		}

		n++
	})

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--hold"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, "Auto-refresh of all snaps held indefinitely\n")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestRefreshHoldManySpecificTime(c *check.C) {
	t, err := time.Parse(time.RFC3339, "3000-01-01T00:00:00Z")
	c.Assert(err, check.IsNil)
	restore := snap.MockTimeNow(func() time.Time {
		return t
	})
	defer restore()

	var n int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
				"action":     "hold",
				"time":       "3000-01-01T07:00:00Z",
				"hold-level": "general",
				"snaps":      []interface{}{"foo", "bar"},
			})
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type": "async", "change": "42", "status-code": 202}`)

		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
			w.WriteHeader(200)
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done"}}`)

		default:
			c.Errorf("expected to get 2 requests, now on %d", n+1)
			fmt.Fprintln(w, `{"type": "error", "result": {"message": "received too many requests"}, "status-code": 500}`)
		}

		n++
	})

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--hold=7h", "foo", "bar"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, "General refreshes of \"foo\", \"bar\" held until 3000-01-01T07:00:00Z\n")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestRefreshHoldSnapUntilSpecificTime(c *check.C) {
	t, err := time.Parse(time.RFC3339, "3000-01-01T00:00:00Z")
	c.Assert(err, check.IsNil)
	restore := snap.MockTimeNow(func() time.Time {
		return t
	})
	defer restore()

	var n int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
				"action":     "hold",
				"hold-level": "general",
				"time":       "3000-01-01T07:00:00Z",
			})
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type": "async", "change": "42", "status-code": 202}`)

		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
			w.WriteHeader(200)
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done"}}`)

		default:
			c.Errorf("expected to get 2 requests, now on %d", n+1)
			fmt.Fprintln(w, `{"type": "error", "result": {"message": "received too many requests"}, "status-code": 500}`)
		}

		n++
	})

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--hold=7h", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, "General refreshes of \"foo\" held until 3000-01-01T07:00:00Z\n")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestRefreshHoldSnapForever(c *check.C) {
	var n int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
				"action":     "hold",
				"time":       "forever",
				"hold-level": "general",
			})
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type": "async", "change": "42", "status-code": 202}`)

		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
			w.WriteHeader(200)
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done"}}`)

		default:
			c.Errorf("expected to get 2 requests, now on %d", n+1)
			fmt.Fprintln(w, `{"type": "error", "result": {"message": "received too many requests"}, "status-code": 500}`)
		}

		n++
	})

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--hold", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, "General refreshes of \"foo\" held indefinitely\n")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestRefreshUnholdAllSnaps(c *check.C) {
	var n int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
				"action": "unhold",
			})
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type": "async", "change": "42", "status-code": 202}`)

		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
			w.WriteHeader(200)
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done"}}`)

		default:
			c.Errorf("expected to get 2 requests, now on %d", n+1)
			fmt.Fprintln(w, `{"type": "error", "result": {"message": "received too many requests"}, "status-code": 500}`)
		}

		n++
	})

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--unhold"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, "Removed auto-refresh hold on all snaps\n")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestRefreshUnholdOneSnap(c *check.C) {
	var n int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
				"action": "unhold",
			})
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type": "async", "change": "42", "status-code": 202}`)

		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
			w.WriteHeader(200)
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done"}}`)

		default:
			c.Errorf("expected to get 2 requests, now on %d", n+1)
			fmt.Fprintln(w, `{"type": "error", "result": {"message": "received too many requests"}, "status-code": 500}`)
		}

		n++
	})

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--unhold", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, "Removed general refresh hold of \"foo\"\n")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestRefreshUnholdManySnaps(c *check.C) {
	var n int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
				"action": "unhold",
				"snaps":  []interface{}{"foo", "bar"},
			})
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type": "async", "change": "42", "status-code": 202}`)

		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
			w.WriteHeader(200)
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done"}}`)

		default:
			c.Errorf("expected to get 2 requests, now on %d", n+1)
			fmt.Fprintln(w, `{"type": "error", "result": {"message": "received too many requests"}, "status-code": 500}`)
		}

		n++
	})

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--unhold", "foo", "bar"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, "Removed general refresh hold of \"foo\", \"bar\"\n")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestRefreshHoldAndUnholdFailWithOtherFlags(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Errorf("unexpected request")
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "received too many requests"}, "status-code": 500}`)
	})

	for _, flag := range []string{"--hold", "--unhold"} {
		rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", flag, "--amend"})
		c.Assert(err, check.ErrorMatches, fmt.Sprintf("cannot use %s with other flags", flag))
		c.Assert(rest, check.DeepEquals, []string{"--amend"})
		c.Check(s.Stdout(), check.Equals, "")
		c.Check(s.Stderr(), check.Equals, "")
	}
}

func (s *SnapSuite) TestRefreshHoldAllowedTimeUnits(c *check.C) {
	now := time.Now()
	restore := snap.MockTimeNow(func() time.Time {
		return now
	})
	defer restore()

	var holdTime string
	var n int

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
				"action":     "hold",
				"time":       holdTime,
				"hold-level": "general",
			})
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type": "async", "change": "42", "status-code": 202}`)

		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
			w.WriteHeader(200)
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done"}}`)

		default:
			c.Errorf("expected to get 2 requests, now on %d", n+1)
			w.WriteHeader(500)
			fmt.Fprintln(w, `{"type": "error", "result": {"message": "received too many requests"}, "status-code": 500}`)
		}

		n++
	})

	for _, holdDuration := range []string{"1s", "999s", "5m", "78h", "8760h", "forever"} {
		var outTime string
		if holdDuration != "forever" {
			offset, err := time.ParseDuration(holdDuration)
			c.Assert(err, check.IsNil)
			holdTime = now.Add(offset).Format(time.RFC3339)
			outTime = fmt.Sprintf("until %s", holdTime)
		} else {
			holdTime = "forever"
			outTime = "indefinitely"
		}

		rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--hold=" + holdDuration, "foo"})
		c.Assert(err, check.IsNil)
		c.Assert(rest, check.DeepEquals, []string{})
		c.Check(s.Stdout(), check.Equals, fmt.Sprintf("General refreshes of \"foo\" held %s\n", outTime))
		c.Check(s.Stderr(), check.Equals, "")
		n = 0
		s.ResetStdStreams()
	}
}

func (s *SnapSuite) TestRefreshHoldBadDuration(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Errorf("unexpected request")
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "received too many requests"}, "status-code": 500}`)
	})

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--hold=1d"})
	c.Assert(err, check.ErrorMatches, "hold value must be a number of hours, minutes or seconds, or \"forever\": time: unknown unit \"?d\"? in duration \"?1d\"?")
	c.Assert(rest, check.DeepEquals, []string{"--hold=1d"})
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestRefreshHoldNegativeDuration(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Errorf("unexpected request")
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "received too many requests"}, "status-code": 500}`)
	})

	for _, dur := range []string{"-5h", "15ns", "999ms"} {
		rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--hold=" + dur})
		c.Assert(err, check.ErrorMatches, "cannot hold refreshes for less than a second: "+dur)
		c.Assert(rest, check.DeepEquals, []string{"--hold=" + dur})
		c.Check(s.Stdout(), check.Equals, "")
		c.Check(s.Stderr(), check.Equals, "")
	}
}

func (s *SnapSuite) TestRefreshNoTimerNoSchedule(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "GET")
		c.Check(r.URL.Path, check.Equals, "/v2/system-info")
		fmt.Fprintln(w, `{"type": "sync", "status-code": 200, "result": {"refresh": {"last": "2017-04-25T17:35:00+0200", "next": "2017-04-26T00:58:00+0200"}}}`)
	})
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--time"})
	c.Assert(err, check.ErrorMatches, `internal error: both refresh.timer and refresh.schedule are empty`)
}

func (s *SnapOpSuite) TestRefreshOne(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "refresh",
			"transaction": string(client.TransactionPerSnap),
		})
	}
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "foo"})
	c.Assert(err, check.IsNil)
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo 1.0 from Bar refreshed`)

}

func (s *SnapOpSuite) TestRefreshOneSwitchChannel(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "refresh",
			"channel":     "beta",
			"transaction": string(client.TransactionPerSnap),
		})
		s.srv.channel = "beta"
	}
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--beta", "foo"})
	c.Assert(err, check.IsNil)
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo \(beta\) 1.0 from Bar refreshed`)
}

func (s *SnapOpSuite) TestRefreshOneSwitchCohort(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "refresh",
			"cohort-key":  "what",
			"transaction": string(client.TransactionPerSnap),
		})
	}
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--cohort=what", "foo"})
	c.Assert(err, check.IsNil)
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo 1.0 from Bar refreshed`)
}

func (s *SnapOpSuite) TestRefreshOneLeaveCohort(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"leave-cohort": true,
			"transaction":  string(client.TransactionPerSnap),
		})
	}
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--leave-cohort", "foo"})
	c.Assert(err, check.IsNil)
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo 1.0 from Bar refreshed`)
}

func (s *SnapOpSuite) TestRefreshOneWithPinnedTrack(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "refresh",
			"channel":     "stable",
			"transaction": string(client.TransactionPerSnap),
		})
		s.srv.channel = "18/stable"
		s.srv.trackingChannel = "18/stable"
	}
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--stable", "foo"})
	c.Assert(err, check.IsNil)
	c.Check(s.Stdout(), check.Equals, "foo (18/stable) 1.0 from Bar refreshed\n")
}

func (s *SnapOpSuite) TestRefreshOneClassic(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/one")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "refresh",
			"classic":     true,
			"transaction": string(client.TransactionPerSnap),
		})
	}
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--classic", "one"})
	c.Assert(err, check.IsNil)
}

func (s *SnapOpSuite) TestRefreshOneDevmode(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/one")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "refresh",
			"devmode":     true,
			"transaction": string(client.TransactionPerSnap),
		})
	}
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--devmode", "one"})
	c.Assert(err, check.IsNil)
}

func (s *SnapOpSuite) TestRefreshOneJailmode(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/one")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "refresh",
			"jailmode":    true,
			"transaction": string(client.TransactionPerSnap),
		})
	}
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--jailmode", "one"})
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
			"transaction":       string(client.TransactionPerSnap),
		})
	}
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--ignore-validation", "one"})
	c.Assert(err, check.IsNil)
}

func (s *SnapOpSuite) TestRefreshOneRebooting(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/core")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "refresh",
			"transaction": string(client.TransactionPerSnap),
		})
	}
	s.srv.restart = "reboot"

	restore := mockArgs("snap", "refresh", "core")
	defer restore()

	err := snap.RunMain()
	c.Check(err, check.IsNil)
	c.Check(s.Stderr(), check.Equals, "snapd is about to reboot the system\n")
}

func (s *SnapOpSuite) TestRefreshOneHalting(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/core")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "refresh",
			"transaction": string(client.TransactionPerSnap),
		})
	}
	s.srv.restart = "halt"

	restore := mockArgs("snap", "refresh", "core")
	defer restore()

	err := snap.RunMain()
	c.Check(err, check.IsNil)
	c.Check(s.Stderr(), check.Equals, "snapd is about to halt the system\n")
}

func (s *SnapOpSuite) TestRefreshOnePoweringOff(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/core")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "refresh",
			"transaction": string(client.TransactionPerSnap),
		})
	}
	s.srv.restart = "poweroff"

	restore := mockArgs("snap", "refresh", "core")
	defer restore()

	err := snap.RunMain()
	c.Check(err, check.IsNil)
	c.Check(s.Stderr(), check.Equals, "snapd is about to power off the system\n")
}

func (s *SnapOpSuite) TestRefreshOneChanDeprecated(c *check.C) {
	var in, out string
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{"action": "refresh", "channel": out, "transaction": string(client.TransactionPerSnap)})
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "snap not found", "value": "foo", "kind": "snap-not-found"}, "status-code": 404}`)
	})

	for in, out = range map[string]string{
		"/foo":            "foo/stable",
		"/stable":         "latest/stable",
		"///foo/stable//": "foo/stable",
	} {
		s.stderr.Reset()
		_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--channel=" + in, "one"})
		c.Assert(err, check.ErrorMatches, "snap \"one\" not found")
		c.Check(s.Stderr(), testutil.EqualsWrapped, `Warning: Specifying a channel "`+in+`" is relying on undefined behaviour. Interpreting it as "`+out+`" for now, but this will be an error later.`)
	}
}

func (s *SnapOpSuite) TestRefreshOneModeErr(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--jailmode", "--devmode", "one"})
	c.Assert(err, check.ErrorMatches, `cannot use devmode and jailmode flags together`)
}

func (s *SnapOpSuite) TestRefreshOneChanErr(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--beta", "--channel=foo", "one"})
	c.Assert(err, check.ErrorMatches, `Please specify a single channel`)
}

func (s *SnapOpSuite) TestRefreshAllChannel(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--beta"})
	c.Assert(err, check.ErrorMatches, `a single snap name is needed to specify mode or channel flags`)
}

func (s *SnapOpSuite) TestRefreshOneIgnoreRunning(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/one")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":         "refresh",
			"ignore-running": true,
			"transaction":    "per-snap",
		})
	}
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--ignore-running", "one"})
	c.Assert(err, check.IsNil)
}

func (s *SnapOpSuite) TestRefreshManyIgnoreRunning(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":         "refresh",
			"snaps":          []interface{}{"one", "two"},
			"ignore-running": true,
			"transaction":    "per-snap",
		})
	}
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--ignore-running", "one", "two"})
	c.Assert(err, check.IsNil)
}

func (s *SnapOpSuite) TestRefreshManyChannel(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--beta", "one", "two"})
	c.Assert(err, check.ErrorMatches, `a single snap name is needed to specify mode or channel flags`)
}

func (s *SnapOpSuite) TestRefreshManyIgnoreValidation(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--ignore-validation", "one", "two"})
	c.Assert(err, check.ErrorMatches, `a single snap name must be specified when ignoring validation`)
}

func (s *SnapOpSuite) TestRefreshAllModeFlags(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--devmode"})
	c.Assert(err, check.ErrorMatches, `a single snap name is needed to specify mode or channel flags`)
}

func (s *SnapOpSuite) TestRefreshOneAmend(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/one")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "refresh",
			"amend":       true,
			"transaction": string(client.TransactionPerSnap),
		})
	}
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--amend", "one"})
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

	rest, err := snap.Parser(snap.Client()).ParseArgs(cmd)
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
	_, err := snap.Parser(snap.Client()).ParseArgs(cmd)
	c.Assert(err, testutil.EqualsWrapped, `
"/" does not contain an unpacked snap.

Try 'snapcraft prime' in your project directory, then 'snap try' again.`)
}

func (s *SnapOpSuite) TestTryMissingOpt(c *check.C) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()
	os.Args = []string{"snap", "try", "./"}
	var kind string

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "POST", check.Commentf("%q", kind))
		w.WriteHeader(400)
		fmt.Fprintf(w, `
{
  "type": "error",
  "result": {
    "message":"error from server",
    "value": "some-snap",
    "kind": %q
  },
  "status-code": 400
}`, kind)
	})

	type table struct {
		kind, expected string
	}

	tests := []table{
		{"snap-needs-classic", "published using classic confinement"},
		{"snap-needs-devmode", "only meant for development"},
	}

	for _, test := range tests {
		kind = test.kind
		c.Check(snap.RunMain(), testutil.ContainsWrapped, test.expected, check.Commentf("%q", kind))
	}
}

func (s *SnapOpSuite) TestInstallConfinedAsClassic(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		w.WriteHeader(400)
		fmt.Fprintf(w, `{
  "type": "error",
  "result": {
    "message":"error from server",
    "value": "some-snap",
    "kind": "snap-not-classic"
  },
  "status-code": 400
}`)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--classic", "some-snap"})
	c.Assert(err, check.ErrorMatches, `snap "some-snap" is not compatible with --classic`)
}

func (s *SnapSuite) TestInstallChannelDuplicationError(c *check.C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--edge", "--beta", "some-snap"})
	c.Assert(err, check.ErrorMatches, "Please specify a single channel")
}

func (s *SnapSuite) TestRefreshChannelDuplicationError(c *check.C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "--edge", "--beta", "some-snap"})
	c.Assert(err, check.ErrorMatches, "Please specify a single channel")
}

func (s *SnapOpSuite) TestNotInstalledError(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{
			"type": "error",
			"result": {
				"message": "Snap was not installed",
				"kind": "snap-not-installed",
				"status-code": 400
				}}`)
	})

	for _, t := range []struct {
		cmd string
		err bool
	}{
		{cmd: "refresh foo", err: true},
		{cmd: "refresh foo bar", err: true},
		{cmd: "install foo", err: true},
		{cmd: "install foo bar", err: true},
		{cmd: "revert foo", err: true},
		{cmd: "switch --channel stable foo", err: true},
		{cmd: "switch --channel stable foo bar", err: true},
		{cmd: "enable foo", err: true},
		{cmd: "enable foo bar", err: true},
		{cmd: "disable foo", err: true},
		{cmd: "disable foo bar", err: true},
		{cmd: "list foo", err: true},
		{cmd: "list foo bar", err: true},
		{cmd: "save foo", err: true},
		{cmd: "save foo bar", err: true},
		{cmd: "remove foo", err: false},
		{cmd: "remove foo bar", err: false},
	} {
		_, err := snap.Parser(snap.Client()).ParseArgs(strings.Fields(t.cmd))
		if t.err {
			c.Check(err, check.ErrorMatches, "Snap was not installed")
		} else {
			c.Check(err, check.IsNil)
		}
	}
}

func (s *SnapOpSuite) TestInstallFromChannel(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":      "install",
			"channel":     "edge",
			"transaction": string(client.TransactionPerSnap),
		})
		s.srv.channel = "edge"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--edge", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo \(edge\) 1.0 from Bar installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallOneIgnoreValidation(c *check.C) {
	s.RedirectClientToTestServer(s.srv.handle)
	s.srv.checker = func(r *http.Request) {
		c.Check(r.Method, check.Equals, "POST")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/one")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":            "install",
			"ignore-validation": true,
			"transaction":       string(client.TransactionPerSnap),
		})
	}
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--ignore-validation", "one"})
	c.Assert(err, check.IsNil)
}

func (s *SnapOpSuite) TestInstallManyIgnoreValidation(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--ignore-validation", "one", "two"})
	c.Assert(err, check.ErrorMatches, `a single snap name must be specified when ignoring validation`)
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
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"enable", "foo"})
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
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"disable", "foo"})
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
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"remove", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo removed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestRemoveWithPurge(c *check.C) {
	s.srv.total = 3
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action": "remove",
			"purge":  true,
		})
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"remove", "--purge", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo removed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestRemoveInsufficientDiskSpace(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{
			"type": "error",
			"result": {
				"message": "disk space error",
				"kind": "insufficient-disk-space",
				"value": {
					"snap-names": ["foo", "bar"],
					"change-kind": "remove"
				},
				"status-code": 507
				}}`)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"remove", "foo"})
	c.Check(err, check.ErrorMatches, `(?sm)cannot remove "foo", "bar" due to low disk space for automatic snapshot,.*use --purge to avoid creating a snapshot`)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestInstallInsufficientDiskSpace(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{
			"type": "error",
			"result": {
				"message": "disk space error",
				"kind": "insufficient-disk-space",
				"value": {
					"snap-names": ["foo"],
					"change-kind": "install"
				},
				"status-code": 507
				}}`)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "foo"})
	c.Check(err, check.ErrorMatches, `cannot install "foo" due to low disk space`)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestRefreshInsufficientDiskSpace(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{
			"type": "error",
			"result": {
				"message": "disk space error",
				"kind": "insufficient-disk-space",
				"value": {
					"snap-names": ["foo"],
					"change-kind": "refresh"
				},
				"status-code": 507
				}}`)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "foo"})
	c.Check(err, check.ErrorMatches, `cannot refresh "foo" due to low disk space`)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapOpSuite) TestRemoveRevision(c *check.C) {
	s.srv.total = 3
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":   "remove",
			"revision": "17",
		})
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"remove", "--revision=17", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo \(revision 17\) removed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestRemoveManyOptions(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"remove", "--revision=17", "one", "two"})
	c.Assert(err, check.ErrorMatches, `cannot use --revision with multiple snap names`)
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

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"remove", "one", "two"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*one removed`)
	c.Check(s.Stdout(), check.Matches, `(?sm).*two removed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, total)
}

func (s *SnapOpSuite) TestRemoveManyPurge(c *check.C) {
	n := 0
	errMsg := "stopping test after creating change successfully"
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
				"action": "remove",
				"snaps":  []interface{}{"one", "two"},
				"purge":  true,
			})

			c.Check(r.Method, check.Equals, "POST")
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type":"async", "change": "42", "status-code": 202}`)
		default:
			w.WriteHeader(500)
			fmt.Fprintf(w, `{"type":"error", "result": {"message":%q, "kind":""}, "status-code": 400}`, errMsg)
		}

		n++
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"remove", "--purge", "one", "two"})
	// purge option was processed, the failure comes after
	c.Assert(err, check.ErrorMatches, errMsg)
}

func (s *SnapOpSuite) TestInstallManyChannel(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--beta", "one", "two"})
	c.Assert(err, check.ErrorMatches, `a single snap name is needed to specify channel flags`)
}

func (s *SnapOpSuite) TestInstallManyPrefer(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--prefer", "one", "two"})
	c.Assert(err, check.ErrorMatches, `a single snap name is needed to specify the prefer flag`)
}

func (s *SnapOpSuite) TestInstallManyMode(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "--classic", "one", "two"})
	c.Assert(err.Error(), check.Equals, `cannot specify mode for multiple store snaps (only for one store snap or several local ones)`)
}

func (s *SnapOpSuite) TestInstallManyMixFileAndStore(c *check.C) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "store-snap", "./local.snap"})
	c.Assert(err, check.ErrorMatches, `cannot install local and store snaps at the same time`)
}

func (s *SnapOpSuite) TestInstallMany(c *check.C) {
	total := 4
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
				"action":      "install",
				"snaps":       []interface{}{"one", "two"},
				"transaction": string(client.TransactionPerSnap),
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
			fmt.Fprintf(w, `{"type": "sync", "result": [{"name": "one", "status": "active", "version": "1.0", "developer": "bar", "publisher": {"id": "bar-id", "username": "bar", "display-name": "Bar", "validation": "unproven"}, "revision":42, "channel":"stable"},{"name": "two", "status": "active", "version": "2.0", "developer": "baz", "publisher": {"id": "baz-id", "username": "baz", "display-name": "Baz", "validation": "unproven"}, "revision":42, "channel":"edge"}]}\n`)

		default:
			c.Fatalf("expected to get %d requests, now on %d", total, n+1)
		}

		n++
	})

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "one", "two"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	// note that (stable) is omitted
	c.Check(s.Stdout(), check.Matches, `(?sm).*one 1.0 from Bar installed`)
	c.Check(s.Stdout(), check.Matches, `(?sm).*two \(edge\) 2.0 from Baz installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, total)
}

func (s *SnapOpSuite) TestInstallManyNoChanges(c *check.C) {
	total := 3
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
				"action":      "install",
				"snaps":       []interface{}{"one", "two"},
				"transaction": string(client.TransactionPerSnap),
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
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done", "data": {}}}`)
		default:
			c.Fatalf("expected to get %d requests, now on %d", total, n+1)
		}

		n++
	})

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"install", "one", "two"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	// note that (stable) is omitted
	c.Check(s.Stdout(), check.Matches, `(?sm).*one already installed`)
	c.Check(s.Stdout(), check.Matches, `(?sm).*two already installed`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, total)
}

func (s *SnapOpSuite) TestRefreshManyNoChanges(c *check.C) {
	total := 3
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
				"action":      "refresh",
				"snaps":       []interface{}{"one", "two"},
				"transaction": string(client.TransactionPerSnap),
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
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done", "data": {}}}`)
		default:
			c.Fatalf("expected to get %d requests, now on %d", total, n+1)
		}

		n++
	})

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"refresh", "one", "two"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	// note that (stable) is omitted
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "All snaps up to date.\n")
	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, total)
}

func (s *SnapOpSuite) TestInstallZeroEmpty(c *check.C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"install"})
	c.Assert(err, check.Not(check.IsNil))
	c.Assert(err.Error(), check.Equals, "the required argument `<snap> (at least 1 argument)` was not provided")
	_, err = snap.Parser(snap.Client()).ParseArgs([]string{"install", ""})
	c.Assert(err, check.ErrorMatches, "cannot install snap with empty name")
	_, err = snap.Parser(snap.Client()).ParseArgs([]string{"install", "", "bar"})
	c.Assert(err, check.ErrorMatches, "cannot install snap with empty name")
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
		rest, err := snap.Parser(snap.Client()).ParseArgs(cmd)
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
		_, err := snap.Parser(snap.Client()).ParseArgs(cmd)
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
		{"debug", "migrate-home", "foo"},
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
		_, err := snap.Parser(snap.Client()).ParseArgs(cmd)
		c.Assert(err, check.ErrorMatches, "server error", check.Commentf("%v", cmd))
		// reset
		n = 0
	}
}

func (s *SnapOpSuite) TestSwitchHappy(c *check.C) {
	s.srv.total = 4
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":  "switch",
			"channel": "beta",
		})
		s.srv.trackingChannel = "beta"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"switch", "--beta", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `"foo" switched to the "beta" channel

`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestSwitchHappyCohort(c *check.C) {
	s.srv.total = 4
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":     "switch",
			"cohort-key": "what",
		})
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"switch", "--cohort=what", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*"foo" switched to the "what" cohort`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestSwitchHappyLeaveCohort(c *check.C) {
	s.srv.total = 4
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":       "switch",
			"leave-cohort": true,
		})
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"switch", "--leave-cohort", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*"foo" left the cohort`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestSwitchHappyChannelAndCohort(c *check.C) {
	s.srv.total = 4
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":     "switch",
			"cohort-key": "what",
			"channel":    "edge",
		})
		s.srv.trackingChannel = "edge"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"switch", "--cohort=what", "--edge", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*"foo" switched to the "edge" channel and the "what" cohort`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestSwitchHappyChannelAndLeaveCohort(c *check.C) {
	s.srv.total = 4
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":       "switch",
			"leave-cohort": true,
			"channel":      "edge",
		})
		s.srv.trackingChannel = "edge"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"switch", "--leave-cohort", "--edge", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*"foo" left the cohort, and switched to the "edge" channel`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestSwitchUnhappy(c *check.C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"switch"})
	c.Assert(err, check.ErrorMatches, "the required argument `<snap>` was not provided")
}

func (s *SnapOpSuite) TestSwitchAlsoUnhappy(c *check.C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"switch", "foo"})
	c.Assert(err, check.ErrorMatches, `nothing to switch.*`)
}

func (s *SnapOpSuite) TestSwitchMoreUnhappy(c *check.C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"switch", "foo", "--cohort=what", "--leave-cohort"})
	c.Assert(err, check.ErrorMatches, `cannot specify both --cohort and --leave-cohort`)
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
	_, err := snap.Parser(snap.Client()).ParseArgs(cmd)
	c.Assert(err, check.ErrorMatches, `unable to contact snap store`)
}

func (s *SnapOpSuite) TestWaitReportsInfoStatus(c *check.C) {
	meter := &progresstest.Meter{}
	defer progress.MockMeter(meter)()
	restore := snap.MockMaxGoneTime(time.Millisecond)
	defer restore()

	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			fmt.Fprintln(w, `{"type": "sync",
"result": {
"ready": false,
"status": "Doing",
"tasks": [{"kind": "bar", "summary": "...", "status": "Wait", "progress": {"done": 1, "total": 1}, "log": ["INFO: Task set to wait until a manual system restart allows to continue"]}]
}}`)
		case 1:
			fmt.Fprintln(w, `{"type": "sync",
"result": {
"ready": true,
"status": "Done",
"tasks": [{"kind": "bar", "summary": "...", "status": "Done", "progress": {"done": 1, "total": 1}, "log": ["INFO: Task set to wait until a manual system restart allows to continue"]}]
}}`)
		default:
			c.Fatalf("unexpected number of API calls")
		}
		n++
	})

	cli := snap.Client()
	chg, err := snap.Wait(cli, "x")
	c.Assert(err, check.IsNil)
	c.Assert(chg, check.NotNil)
	c.Check(meter.Notices, testutil.Contains, "INFO: Task set to wait until a manual system restart allows to continue")
	c.Check(n, check.Equals, 2)
}
