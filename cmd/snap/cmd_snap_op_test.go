// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !integrationcoverage

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
	"path/filepath"
	"regexp"
	"strconv"

	"gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

type snapOpTestServer struct {
	c *check.C

	checker func(r *http.Request)
	n       int
	total   int
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
		fmt.Fprintln(w, `{"type": "sync", "result": [{"name": "foo", "status": "active", "version": "1.0", "developer": "bar", "revision":42}]}`)
	default:
		t.c.Fatalf("expected to get %d requests, now on %d", t.total, t.n+1)
	}

	t.n++
}

type SnapOpSuite struct {
	SnapSuite

	srv snapOpTestServer
}

func (s *SnapOpSuite) SetUpTest(c *check.C) {
	s.SnapSuite.SetUpTest(c)

	s.srv = snapOpTestServer{
		c:     c,
		total: 4,
	}
}

func (s *SnapOpSuite) TestInstall(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":  "install",
			"name":    "foo",
			"channel": "chan",
		})
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser().ParseArgs([]string{"install", "--channel", "chan", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo\s+1.0\s+42\s+bar.*`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(s.srv.n, check.Equals, s.srv.total)
}

func (s *SnapOpSuite) TestInstallDevMode(c *check.C) {
	s.srv.checker = func(r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo")
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"action":  "install",
			"name":    "foo",
			"devmode": true,
			"channel": "chan",
		})
	}

	s.RedirectClientToTestServer(s.srv.handle)
	rest, err := snap.Parser().ParseArgs([]string{"install", "--channel", "chan", "--devmode", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo\s+1.0\s+42\s+bar.*`)
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
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo\s+1.0\s+42\s+bar.*`)
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
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo\s+1.0\s+42\s+bar.*`)
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
	c.Check(s.Stdout(), check.Matches, `Name +Version +Developer +Notes +Summary
foo +4.2update1 +bar +- +some summary
`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, 1)
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
	c.Check(s.Stdout(), check.Matches, `(?sm).*foo\s+1.0\s+42\s+bar.*`)
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
