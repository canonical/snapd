// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"bytes"
	"fmt"
	"net/http"
	"os"

	"gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/testutil"
)

type reportIssueSuite struct {
	BaseSnapSuite

	xdgOpen *testutil.MockCmd
}

var _ = check.Suite(&reportIssueSuite{})

func (s *reportIssueSuite) SetUpTest(c *check.C) {
	s.BaseSnapSuite.SetUpTest(c)

	preserveEnv := func(evar string) {
		v := os.Getenv(evar)
		if v != "" {
			s.AddCleanup(func() { os.Setenv(evar, v) })
		} else {
			s.AddCleanup(func() { os.Unsetenv(evar) })
		}
	}
	preserveEnv("DISPLAY")
	preserveEnv("WAYLAND_DISPLAY")
	preserveEnv("DESKTOP_SESSION")

	os.Unsetenv("DISPLAY")
	os.Unsetenv("WAYLAND_DISPLAY")
	os.Unsetenv("DESKTOP_SESSION")

	s.xdgOpen = testutil.MockCommand(c, "xdg-open", ``)
	s.AddCleanup(s.xdgOpen.Restore)

}

const mockInfoJSONHelloWithLinks = `
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": [
    {
      "channel": "stable",
      "confinement": "strict",
      "description": "GNU hello prints a friendly greeting. This is part of the snapcraft tour at https://snapcraft.io/",
      "developer": "canonical",
      "publisher": {
         "id": "canonical",
         "username": "canonical",
         "display-name": "Canonical",
         "validation": "verified"
      },
      "download-size": 65536,
      "icon": "",
      "id": "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6",
      "name": "hello",
      "private": false,
      "resource": "/v2/snaps/hello",
      "revision": "1",
      "status": "available",
      "summary": "The GNU Hello snap",
      "store-url": "https://snapcraft.io/hello",
      "type": "app",
      "version": "2.10",
      "license": "MIT",
      "channels": {
        "1/stable": {
          "revision": "1",
          "version": "2.10",
          "channel": "1/stable",
          "size": 65536,
          "released-at": "2018-12-18T15:16:56.723501Z"
        }
      },
      "tracks": ["1"],
      "links": {
        "website": ["https://hello.world", "https://snapcraft.io/foo", "https://hello.world"],
        "sources": ["https://github.com/canonical/hello-snap"],
        "contact": ["mailto:store@hello.world"],
        "issues":  ["https://github.com/canonical/hello-snap/issues"]
      }
    }
  ],
  "sources": [
    "store"
  ],
  "suggested-currency": "GBP"
}
`

const mockInfoJSONHelloLocalNoLinksLegacyStore = `
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": {
      "channel": "stable",
      "confinement": "strict",
      "description": "GNU hello prints a friendly greeting. This is part of the snapcraft tour at https://snapcraft.io/",
      "developer": "canonical",
      "publisher": {
         "id": "canonical",
         "username": "canonical",
         "display-name": "Canonical",
         "validation": "verified"
      },
      "id": "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6",
      "install-date": "2006-01-02T22:04:07.123456789Z",
      "installed-size": 1024,
      "name": "hello",
      "private": false,
      "revision": "100",
      "status": "available",
      "store-url": "https://snapcraft.io/hello",
      "summary": "The GNU Hello snap",
      "type": "app",
      "version": "2.10",
      "license": "",
      "tracking-channel": "beta"
    }
}
`

func (s *reportIssueSuite) testReportIssueHappyNoPrompt(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Logf("hit 1")
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			q := r.URL.Query()
			// asks for the instance snap
			c.Check(q.Get("name"), check.Equals, "hello")
			fmt.Fprint(w, mockInfoJSONHelloWithLinks)
		case 1:
			c.Logf("hit 2")
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/hello")
			fmt.Fprint(w, mockInfoJSONWithStoreURL)
		default:
			c.Fatalf("expected to get 2 requests, now on %d (%v)", n+1, r)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"report-issue", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	// links are unique and sorted, generic snapcraft.io link is dropped
	c.Check(s.Stdout(), check.Equals, `
Publisher of snap "hello" has listed the following points of contact:
  Contact:
    mailto:local@hello.world
    mailto:store@hello.world
  Issue reporting:
    https://github.com/canonical/hello-snap/issues
  Website:
    https://hello.world

Use one of the links listed above to report an issue with the snap.
`[1:])
	c.Check(s.Stderr(), check.Equals, "")

	c.Check(s.xdgOpen.Calls(), check.HasLen, 0)
}

func (s *reportIssueSuite) TestReportIssueHappyNoPrompt(c *check.C) {
	s.testReportIssueHappyNoPrompt(c)
}

func (s *reportIssueSuite) TestReportIssueHappyNoPromptDesktopNoXdgOpen(c *check.C) {
	restore := snap.MockIsStdinTTY(true)
	defer restore()
	os.Setenv("DESKTOP_SESSION", "gnome")

	// override PATH so that even the host's xdg-open cannot be found
	os.Setenv("PATH", "")

	s.testReportIssueHappyNoPrompt(c)
}

func (s *reportIssueSuite) TestReportIssueHappyNoPromptDesktopNotInteractive(c *check.C) {
	restore := snap.MockIsStdinTTY(false)
	defer restore()

	os.Setenv("DESKTOP_SESSION", "gnome")
	s.testReportIssueHappyNoPrompt(c)
}

func (s *reportIssueSuite) testReportIssueHappyOpenLink(c *check.C, input string, opens bool) {
	restore := snap.MockIsStdinTTY(true)
	defer restore()
	os.Setenv("DESKTOP_SESSION", "gnome")

	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Logf("hit 1")
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			q := r.URL.Query()
			// asks for the instance snap
			c.Check(q.Get("name"), check.Equals, "hello")
			fmt.Fprint(w, mockInfoJSONHelloWithLinks)
		case 1:
			c.Logf("hit 2")
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/hello")
			fmt.Fprint(w, mockInfoJSONWithStoreURL)
		default:
			c.Fatalf("expected to get 2 requests, now on %d (%v)", n+1, r)
		}

		n++
	})
	snap.Stdin = bytes.NewBuffer([]byte(input))

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"report-issue", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	// links are unique and sorted, generic snapcraft.io link is dropped
	c.Check(s.Stdout(), check.Equals, `
Publisher of snap "hello" has listed the following points of contact:
  Contact:
    mailto:local@hello.world
    mailto:store@hello.world
  Issue reporting:
    https://github.com/canonical/hello-snap/issues
  Website:
    https://hello.world

Use one of the links listed above to report an issue with the snap.

Would you like to open the first issue tracker link in browser? [Y/n] `[1:])
	c.Check(s.Stderr(), check.Equals, "")
	if opens {
		c.Check(s.xdgOpen.Calls(), check.DeepEquals, [][]string{
			{"xdg-open", "https://github.com/canonical/hello-snap/issues"},
		})
	} else {
		c.Check(s.xdgOpen.Calls(), check.HasLen, 0)
	}
}

func (s *reportIssueSuite) TestReportIssueHappyOpenLinkYes(c *check.C) {
	opensLink := true
	s.testReportIssueHappyOpenLink(c, "Y\n", opensLink)
}

func (s *reportIssueSuite) TestReportIssueHappyOpenLinkYesImplicit(c *check.C) {
	opensLink := true
	s.testReportIssueHappyOpenLink(c, "\n", opensLink)
}

func (s *reportIssueSuite) TestReportIssueHappyOpenLinkNo(c *check.C) {
	opensLink := false
	s.testReportIssueHappyOpenLink(c, "n\n", opensLink)
}

func (s *reportIssueSuite) TestReportIssueNoContact(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			q := r.URL.Query()
			// asks for the instance snap
			c.Check(q.Get("name"), check.Equals, "hello")
			fmt.Fprint(w, mockInfoJSONWithChannels)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/hello")
			fmt.Fprint(w, mockInfoJSONHelloLocalNoLinksLegacyStore)
		default:
			c.Fatalf("expected to get 2 requests, now on %d (%v)", n+1, r)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"report-issue", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	// make sure local and remote info is combined in the output
	c.Check(s.Stdout(), check.Equals, `
Publisher of snap "hello" has not listed any points of contact.
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *reportIssueSuite) TestReportIssueNoLocal(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			q := r.URL.Query()
			// asks for the instance snap
			c.Check(q.Get("name"), check.Equals, "hello")
			fmt.Fprint(w, mockInfoJSONHelloWithLinks)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/hello")
			w.WriteHeader(404)
			fmt.Fprint(w, `{
			"type": "error",
            "status-code": 404,
			"result": {
				"message": "snap not installed",
				"kind": "snap-not-found",
				"value": "hello"
				}}`)
		default:
			c.Fatalf("expected to get 2 requests, now on %d (%v)", n+1, r)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"report-issue", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	// only store links
	c.Check(s.Stdout(), check.Equals, `
Publisher of snap "hello" has listed the following points of contact:
  Contact:
    mailto:store@hello.world
  Issue reporting:
    https://github.com/canonical/hello-snap/issues
  Website:
    https://hello.world

Use one of the links listed above to report an issue with the snap.
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}
