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
	"net/http"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestListHelp(c *check.C) {
	msg := `Usage:
  snap.test list [list-OPTIONS] [<snap>...]

The list command displays a summary of snaps installed in the current system.

A green check mark (given color and unicode support) after a publisher name
indicates that the publisher has been verified.

[list command options]
      --all                           Show all revisions
      --color=[auto|never|always]     Use a little bit of color to highlight
                                      some things. (default: auto)
      --unicode=[auto|never|always]   Use a little bit of Unicode to improve
                                      legibility. (default: auto)
`
	s.testSubCommandHelp(c, "list", msg)
}

func (s *SnapSuite) TestList(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			c.Check(r.URL.RawQuery, check.Equals, "")
			fmt.Fprintln(w, `{"type": "sync", "result": [
{
  "name": "foo",
  "status": "active",
  "version": "4.2",
  "developer": "bar",
  "publisher": {"id": "bar-id", "username": "bar", "display-name": "Bar", "validation": "unproven"},
  "health": {"status": "blocked"},
  "revision": 17,
  "tracking-channel": "potatoes"
}]}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"list"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `
Name  Version  Rev  Tracking  Publisher  Notes
foo   4.2      17   potatoes  bar        blocked
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestListAll(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			c.Check(r.URL.RawQuery, check.Equals, "select=all")
			fmt.Fprintln(w, `{"type": "sync", "result": [{"name": "foo", "status": "active", "version": "4.2", "developer": "bar", "publisher": {"id": "bar-id", "username": "bar", "display-name": "Bar", "validation": "unproven"}, "revision":17, "tracking-channel": "stable"}]}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"list", "--all"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `Name +Version +Rev +Tracking +Publisher +Notes
foo +4.2 +17 +stable +bar +-
`)
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestListEmpty(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			fmt.Fprintln(w, `{"type": "sync", "result": []}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"list"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "No snaps are installed yet. Try 'snap install hello-world'.\n")
}

func (s *SnapSuite) TestListEmptyWithQuery(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			fmt.Fprintln(w, `{"type": "sync", "result": []}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"list", "quux"}))
	c.Assert(err, check.ErrorMatches, `no matching snaps installed`)
}

func (s *SnapSuite) TestListWithNoMatchingQuery(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			c.Check(r.URL.Query().Get("snaps"), check.Equals, "quux")
			fmt.Fprintln(w, `{"type": "sync", "result": []}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"list", "quux"}))
	c.Assert(err, check.ErrorMatches, "no matching snaps installed")
}

func (s *SnapSuite) TestListWithQuery(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			c.Check(r.URL.Query().Get("snaps"), check.Equals, "foo")
			fmt.Fprintln(w, `{"type": "sync", "result": [{"name": "foo", "status": "active", "version": "4.2", "developer": "bar", "publisher": {"id": "bar-id", "username": "bar", "display-name": "Bar", "validation": "unproven"}, "revision":17, "tracking-channel": "1.10/stable/fix1234"}]}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"list", "foo"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `Name +Version +Rev +Tracking +Publisher +Notes
foo +4.2 +17 +1\.10/stable/… +bar +-
`)
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, 1)
}

func (s *SnapSuite) TestListWithNotes(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			fmt.Fprintln(w, `{"type": "sync", "result": [
{"name": "foo", "status": "active", "version": "4.2", "developer": "bar", "publisher": {"id": "bar-id", "username": "bar", "display-name": "Bar", "validation": "unproven"}, "revision":17, "trymode": true}
,{"name": "dm1", "status": "active", "version": "5", "revision":1, "devmode": true, "confinement": "devmode"}
,{"name": "dm2", "status": "active", "version": "5", "revision":1, "devmode": true, "confinement": "strict"}
,{"name": "cf1", "status": "active", "version": "6", "revision":2, "confinement": "devmode", "jailmode": true}
,{"name": "br1", "status": "active", "version": "", "revision":2, "publisher": {"id": "bar-id", "username": "bar", "display-name": "Bar", "validation": "unproven"}, "confinement": "strict", "broken": "snap is broken"}
,{"name": "dbr1", "status": "", "version": "", "revision":2, "publisher": {"id": "bar-id", "username": "bar", "display-name": "Bar", "validation": "unproven"}, "confinement": "strict", "broken": "snap is broken"}
]}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"list"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?ms)^Name +Version +Rev +Tracking +Publisher +Notes$`)
	c.Check(s.Stdout(), check.Matches, `(?ms).*^foo +4.2 +17 +- +bar +try$`)
	c.Check(s.Stdout(), check.Matches, `(?ms).*^dm1 +.* +devmode$`)
	c.Check(s.Stdout(), check.Matches, `(?ms).*^dm2 +.* +devmode$`)
	c.Check(s.Stdout(), check.Matches, `(?ms).*^cf1 +.* +jailmode$`)
	c.Check(s.Stdout(), check.Matches, `(?ms).*^br1 +- +2 +- +bar +broken$`)
	c.Check(s.Stdout(), check.Matches, `(?ms).*^dbr1 +- +2 +- +bar +disabled,broken$`)
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestFormatChannel(c *check.C) {
	type tableT struct {
		channel  string
		expected string
	}
	for _, t := range []tableT{
		{"", "-"},
		{"latest/stable", "latest/stable"},
		{"foo/stable", "foo/stable"},
		{"foo/edge", "foo/edge"},
		{"foo/stable/bar", "foo/stable/…"},
		{"foo/edge/bar", "foo/edge/…"},
	} {
		c.Check(snap.FormatChannel(t.channel), check.Equals, t.expected, check.Commentf(t.channel))
	}

	// and some SISO tests just to check it doesn't panic nor return empty string
	// (the former would break scripts)
	for _, ch := range []string{
		"",
		"\x00",
		"/",
		"//",
		"///",
		"////",
		"a/",
		"/b",
		"a//b",
		"/stable",
		"/edge",
	} {
		c.Check(snap.FormatChannel(ch), check.Not(check.Equals), "", check.Commentf(ch))
	}
}
