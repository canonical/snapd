// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	snap "github.com/snapcore/snapd/cmd/snap"
)

type snapOpTestServer struct {
	c *check.C

	checker func(r *http.Request)
	n       int
	total   int
	channel string
}

var _ = check.Suite(&SnapOpSuite{})

func (t *snapOpTestServer) handle(w http.ResponseWriter, r *http.Request) {
	switch t.n {
	case 0:
		t.checker(r)
		t.c.Check(r.Method, check.Equals, "POST")
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintln(w, `{"type":"async", "change": "42", "status-code": 202}`)
	case 1:
		t.c.Check(r.Method, check.Equals, "GET")
		t.c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
		fmt.Fprintln(w, `{"type": "sync", "result": {"status": "Doing"}}`)
	case 2:
		t.c.Check(r.Method, check.Equals, "GET")
		t.c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
		fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done", "data": {"snap-name": "foo"}}}`)
	case 3:
		t.c.Check(r.Method, check.Equals, "GET")
		t.c.Check(r.URL.Path, check.Equals, "/v2/snaps")
		fmt.Fprintf(w, `{"type": "sync", "result": [{"name": "foo", "status": "active", "version": "1.0", "developer": "bar", "revision":42, "channel":"%s"}]}\n`, t.channel)
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
	restore := snap.MockMaxGoneTime(time.Millisecond)
	defer restore()

	// lazy way of getting a URL that won't work nor break stuff
	server := httptest.NewServer(nil)
	snap.ClientConfig.BaseURL = server.URL
	server.Close()

	d := c.MkDir()
	oldStdout := os.Stdout
	stdout, err := ioutil.TempFile(d, "stdout")
	c.Assert(err, check.IsNil)
	defer func() {
		os.Stdout = oldStdout
		stdout.Close()
		os.Remove(stdout.Name())
	}()
	os.Stdout = stdout

	cli := snap.Client()
	chg, err := snap.Wait(cli, "x")
	c.Assert(chg, check.IsNil)
	c.Assert(err, check.NotNil)
	buf, err := ioutil.ReadFile(stdout.Name())
	c.Assert(err, check.IsNil)
	c.Check(string(buf), check.Matches, "(?ms).*Waiting for server to restart.*")
}

func (s *SnapOpSuite) TestWaitRecovers(c *check.C) {
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

	d := c.MkDir()
	oldStdout := os.Stdout
	stdout, err := ioutil.TempFile(d, "stdout")
	c.Assert(err, check.IsNil)
	defer func() {
		os.Stdout = oldStdout
		stdout.Close()
		os.Remove(stdout.Name())
	}()
	os.Stdout = stdout

	cli := snap.Client()
	chg, err := snap.Wait(cli, "x")
	// we got the change
	c.Assert(chg, check.NotNil)
	c.Assert(err, check.IsNil)
	buf, err := ioutil.ReadFile(stdout.Name())
	c.Assert(err, check.IsNil)

	// but only after recovering
	c.Check(string(buf), check.Matches, "(?ms).*Waiting for server to restart.*")
}

func (s *SnapOpSuite) TestInstall(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":  "install",
			"channel": "chan",
		})
		s.srv.channel = "chan"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser().ParseArgs([]string{"install", "--channel", "chan", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo \(chan\) 1.0 from 'bar' installed`)
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
			"channel": "chan",
		})
		s.srv.channel = "chan"
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser().ParseArgs([]string{"install", "--channel", "chan", "--devmode", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo \(chan\) 1.0 from 'bar' installed`)
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

func (s *SnapOpSuite) TestInstallPath(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps")
		postData, err := ioutil.ReadAll(r.Body)
		c.Assert(err, check.IsNil)
		c.Assert(string(postData), check.Matches, "(?s).*\r\nsnap-data\r\n.*")
		c.Assert(string(postData), check.Matches, "(?s).*Content-Disposition: form-data; name=\"action\"\r\n\r\ninstall\r\n.*")
		c.Assert(string(postData), check.Matches, "(?s).*Content-Disposition: form-data; name=\"devmode\"\r\n\r\nfalse\r\n.*")
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
		postData, err := ioutil.ReadAll(r.Body)
		c.Assert(err, check.IsNil)
		c.Assert(string(postData), check.Matches, "(?s).*\r\nsnap-data\r\n.*")
		c.Assert(string(postData), check.Matches, "(?s).*Content-Disposition: form-data; name=\"action\"\r\n\r\ninstall\r\n.*")
		c.Assert(string(postData), check.Matches, "(?s).*Content-Disposition: form-data; name=\"devmode\"\r\n\r\ntrue\r\n.*")
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
		postData, err := ioutil.ReadAll(r.Body)
		c.Assert(err, check.IsNil)
		c.Assert(string(postData), check.Matches, "(?s).*\r\nsnap-data\r\n.*")
		c.Assert(string(postData), check.Matches, "(?s).*Content-Disposition: form-data; name=\"action\"\r\n\r\ninstall\r\n.*")
		c.Assert(string(postData), check.Matches, "(?s).*Content-Disposition: form-data; name=\"classic\"\r\n\r\ntrue\r\n.*")
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
		postData, err := ioutil.ReadAll(r.Body)
		c.Assert(err, check.IsNil)
		c.Assert(string(postData), check.Matches, "(?s).*\r\nsnap-data\r\n.*")
		c.Assert(string(postData), check.Matches, "(?s).*Content-Disposition: form-data; name=\"action\"\r\n\r\ninstall\r\n.*")
		c.Assert(string(postData), check.Matches, "(?s).*Content-Disposition: form-data; name=\"dangerous\"\r\n\r\ntrue\r\n.*")
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

func (s *SnapOpSuite) TestRevert(c *check.C) {
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
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo reverted to 1.0`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}
func (s *SnapSuite) TestRefreshList(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			c.Check(r.URL.Query().Get("select"), check.Equals, "refresh")
			fmt.Fprintln(w, `{"type": "sync", "result": [{"name": "foo", "status": "active", "version": "4.2update1", "developer": "bar", "revision":17,"summary":"some summary"}]}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser().ParseArgs([]string{"refresh", "--list"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `Name +Version +Rev +Developer +Notes
foo +4.2update1 +17 +bar +-.*
`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, 1)
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

func (s *SnapOpSuite) runTryTest(c *check.C, devmode bool) {
	// pass relative path to cmd
	tryDir := "some-dir"

	s.srv.checker = func(r *http.Request) {
		// ensure the client always sends the absolute path
		fullTryDir, err := filepath.Abs(tryDir)
		c.Assert(err, check.IsNil)

		c.Check(r.URL.Path, check.Equals, "/v2/snaps")
		postData, err := ioutil.ReadAll(r.Body)
		c.Assert(err, check.IsNil)
		c.Assert(string(postData), check.Matches, "(?s).*Content-Disposition: form-data; name=\"action\"\r\n\r\ntry\r\n.*")
		c.Assert(string(postData), check.Matches, fmt.Sprintf("(?s).*Content-Disposition: form-data; name=\"snap-path\"\r\n\r\n%s\r\n.*", regexp.QuoteMeta(fullTryDir)))
		c.Assert(string(postData), check.Matches, fmt.Sprintf("(?s).*Content-Disposition: form-data; name=\"devmode\"\r\n\r\n%s\r\n.*", strconv.FormatBool(devmode)))
	}

	s.RedirectClientToTestServer(s.srv.handle)

	cmd := []string{"try", tryDir}
	if devmode {
		cmd = append(cmd, "--devmode")
	}

	rest, err := snap.Parser().ParseArgs(cmd)
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, fmt.Sprintf(`(?sm).*foo 1.0 mounted from .*%s`, tryDir))
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestTryNoDevMode(c *check.C) {
	s.runTryTest(c, false)
}
func (s *SnapOpSuite) TestTryDevMode(c *check.C) {
	s.runTryTest(c, true)
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
			w.WriteHeader(http.StatusAccepted)
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
			w.WriteHeader(http.StatusAccepted)
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
			fmt.Fprintf(w, `{"type": "sync", "result": [{"name": "one", "status": "active", "version": "1.0", "developer": "bar", "revision":42, "channel":"stable"},{"name": "two", "status": "active", "version": "2.0", "developer": "baz", "revision":42, "channel":"edge"}]}\n`)

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
