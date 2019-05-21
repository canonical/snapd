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
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	snap "github.com/snapcore/snapd/cmd/snap"
	snaplib "github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/squashfs"
)

var cmdAppInfos = []client.AppInfo{{Name: "app1"}, {Name: "app2"}}
var svcAppInfos = []client.AppInfo{
	{
		Name:    "svc1",
		Daemon:  "simple",
		Enabled: false,
		Active:  true,
	},
	{
		Name:    "svc2",
		Daemon:  "simple",
		Enabled: true,
		Active:  false,
	},
}

var mixedAppInfos = append(append([]client.AppInfo(nil), cmdAppInfos...), svcAppInfos...)

type infoSuite struct {
	BaseSnapSuite
}

var _ = check.Suite(&infoSuite{})

type flushBuffer struct{ bytes.Buffer }

func (*flushBuffer) Flush() error { return nil }

func (s *infoSuite) TestMaybePrintServices(c *check.C) {
	for _, infos := range [][]client.AppInfo{svcAppInfos, mixedAppInfos} {
		var buf flushBuffer
		snap.MaybePrintServices(&buf, &client.Snap{Name: "foo", Apps: infos})

		c.Check(buf.String(), check.Equals, `services:
  foo.svc1:	simple, disabled, active
  foo.svc2:	simple, enabled, inactive
`)
	}
}

func (s *infoSuite) TestMaybePrintServicesNoServices(c *check.C) {
	for _, infos := range [][]client.AppInfo{cmdAppInfos, nil} {
		var buf flushBuffer
		snap.MaybePrintServices(&buf, &client.Snap{Name: "foo", Apps: infos})
		c.Check(buf.String(), check.Equals, "")
	}
}

func (s *infoSuite) TestMaybePrintCommands(c *check.C) {
	for _, infos := range [][]client.AppInfo{cmdAppInfos, mixedAppInfos} {
		var buf flushBuffer
		snap.MaybePrintCommands(&buf, &client.Snap{Name: "foo", Apps: infos})

		c.Check(buf.String(), check.Equals, `commands:
  - foo.app1
  - foo.app2
`)
	}
}

func (s *infoSuite) TestMaybePrintCommandsNoCommands(c *check.C) {
	for _, infos := range [][]client.AppInfo{svcAppInfos, nil} {
		var buf flushBuffer
		snap.MaybePrintCommands(&buf, &client.Snap{Name: "foo", Apps: infos})

		c.Check(buf.String(), check.Equals, "")
	}
}

func (infoSuite) TestPrintType(c *check.C) {
	for from, to := range map[string]string{
		"":            "",
		"app":         "",
		"application": "",
		"gadget":      "type:\tgadget\n",
		"core":        "type:\tcore\n",
		"os":          "type:\tcore\n",
	} {
		var buf flushBuffer
		snap.MaybePrintType(&buf, &client.Snap{Type: from})
		c.Check(buf.String(), check.Equals, to, check.Commentf("%q", from))
	}
}

func (infoSuite) TestPrintSummary(c *check.C) {
	for from, to := range map[string]string{
		"":               `""`,                // empty results in quoted empty
		"foo":            "foo",               // plain text results in unquoted
		"two words":      "two words",         // ...even when multi-word
		"{":              `"{"`,               // but yaml-breaking is quoted
		"very long text": "very long\n  text", // too-long text gets split (TODO: split with tabbed indent to preserve alignment)
	} {
		var buf flushBuffer
		snap.PrintSummary(&buf, &client.Snap{Summary: from})
		c.Check(buf.String(), check.Equals, "summary:\t"+to+"\n", check.Commentf("%q", from))
	}
}

func (s *infoSuite) TestMaybePrintPublisher(c *check.C) {
	acct := &snaplib.StoreAccount{
		Validation:  "verified",
		Username:    "team-potato",
		DisplayName: "Team Potato",
	}

	type T struct {
		diskSnap, theSnap *client.Snap
		expected          string
	}

	for i, t := range []T{
		{&client.Snap{}, nil, ""},                 // nothing output for on-disk snap
		{nil, &client.Snap{}, "publisher:\t--\n"}, // from-snapd snap with no publisher is explicit
		{nil, &client.Snap{Publisher: acct}, "publisher:\tTeam Potato*\n"},
	} {
		var buf flushBuffer
		snap.MaybePrintPublisher(&buf, t.diskSnap, t.theSnap)
		c.Check(buf.String(), check.Equals, t.expected, check.Commentf("%d", i))
	}
}

func (s *infoSuite) TestMaybePrintNotes(c *check.C) {
	var buf flushBuffer
	type T struct {
		localSnap, theSnap *client.Snap
		expected           string
	}

	for i, t := range []T{
		{
			nil,
			&client.Snap{Private: true, Confinement: "devmode"},
			"notes:\t\n" +
				"  private:\ttrue\n" +
				"  confinement:\tdevmode\n",
		}, {
			&client.Snap{Private: true, Confinement: "devmode"},
			&client.Snap{Private: true, Confinement: "devmode"},
			"notes:\t\n" +
				"  private:\ttrue\n" +
				"  confinement:\tdevmode\n" +
				"  devmode:\tfalse\n" +
				"  jailmode:\ttrue\n" +
				"  trymode:\tfalse\n" +
				"  enabled:\tfalse\n" +
				"  broken:\tfalse\n" +
				"  ignore-validation:\tfalse\n",
		}, {
			&client.Snap{Private: true, Confinement: "devmode", Broken: "ouch"},
			&client.Snap{Private: true, Confinement: "devmode"},
			"notes:\t\n" +
				"  private:\ttrue\n" +
				"  confinement:\tdevmode\n" +
				"  devmode:\tfalse\n" +
				"  jailmode:\ttrue\n" +
				"  trymode:\tfalse\n" +
				"  enabled:\tfalse\n" +
				"  broken:\ttrue (ouch)\n" +
				"  ignore-validation:\tfalse\n",
		},
	} {
		buf.Reset()
		snap.MaybePrintNotes(&buf, t.localSnap, t.theSnap, false)
		c.Check(buf.String(), check.Equals, "", check.Commentf("%d/false", i))

		buf.Reset()
		snap.MaybePrintNotes(&buf, t.localSnap, t.theSnap, true)
		c.Check(buf.String(), check.Equals, t.expected, check.Commentf("%d/true", i))
	}
}

func (s *infoSuite) TestMaybePrintStandaloneVersion(c *check.C) {
	var buf flushBuffer

	// no disk snap -> no version
	snap.MaybePrintStandaloneVersion(&buf, nil)
	c.Check(buf.String(), check.Equals, "")

	for version, expected := range map[string]string{
		"":    "--",
		"4.2": "4.2",
	} {
		buf.Reset()
		snap.MaybePrintStandaloneVersion(&buf, &client.Snap{Version: version})
		c.Check(buf.String(), check.Equals, "version:\t"+expected+" -\n", check.Commentf("%q", version))

		buf.Reset()
		snap.MaybePrintStandaloneVersion(&buf, &client.Snap{Version: version, Confinement: "devmode"})
		c.Check(buf.String(), check.Equals, "version:\t"+expected+" devmode\n", check.Commentf("%q", version))
	}
}

func (s *infoSuite) TestMaybePrintBuildDate(c *check.C) {
	var buf flushBuffer
	// some prep
	dir := c.MkDir()
	arbfile := filepath.Join(dir, "arb")
	c.Assert(ioutil.WriteFile(arbfile, nil, 0600), check.IsNil)
	filename := filepath.Join(c.MkDir(), "foo.snap")
	diskSnap := squashfs.New(filename)
	c.Assert(diskSnap.Build(dir, "app"), check.IsNil)
	buildDate := diskSnap.BuildDate().Format(time.Kitchen)

	// no disk snap -> no build date
	snap.MaybePrintBuildDate(&buf, nil, "")
	c.Check(buf.String(), check.Equals, "")

	// path is directory -> no build date
	buf.Reset()
	snap.MaybePrintBuildDate(&buf, &client.Snap{}, dir)
	c.Check(buf.String(), check.Equals, "")

	// not actually a snap -> no build date
	buf.Reset()
	snap.MaybePrintBuildDate(&buf, &client.Snap{}, arbfile)
	c.Check(buf.String(), check.Equals, "")

	// disk snap -> get build date
	buf.Reset()
	snap.MaybePrintBuildDate(&buf, &client.Snap{}, filename)
	c.Check(buf.String(), check.Equals, "build-date:\t"+buildDate+"\n")
}

func (s *infoSuite) TestMaybePrintSum(c *check.C) {
	var buf flushBuffer
	// some prep
	dir := c.MkDir()
	filename := filepath.Join(c.MkDir(), "foo.snap")
	diskSnap := squashfs.New(filename)
	c.Assert(diskSnap.Build(dir, "app"), check.IsNil)

	// no disk snap -> no checksum
	snap.MaybePrintSum(&buf, nil, "", true)
	c.Check(buf.String(), check.Equals, "")

	// path is directory -> no checksum
	buf.Reset()
	snap.MaybePrintSum(&buf, &client.Snap{}, dir, true)
	c.Check(buf.String(), check.Equals, "")

	// disk snap and verbose -> get checksum
	buf.Reset()
	snap.MaybePrintSum(&buf, &client.Snap{}, filename, true)
	c.Check(buf.String(), check.Matches, "sha3-384:\t\\S+\n")

	// disk snap but not verbose -> no checksum
	buf.Reset()
	snap.MaybePrintSum(&buf, &client.Snap{}, filename, false)
	c.Check(buf.String(), check.Equals, "")
}

func (s *infoSuite) TestMaybePrintContact(c *check.C) {
	var buf flushBuffer

	for contact, expected := range map[string]string{
		"": "",
		"mailto:joe@example.com": "contact:\tjoe@example.com\n",
		"foo": "contact:\tfoo\n",
	} {
		buf.Reset()
		snap.MaybePrintContact(&buf, contact)
		c.Check(buf.String(), check.Equals, expected, check.Commentf("%q", contact))
	}
}

func (s *infoSuite) TestMaybePrintBase(c *check.C) {
	var buf flushBuffer

	// no verbose -> no base
	snap.MaybePrintBase(&buf, "xyzzy", false)
	c.Check(buf.String(), check.Equals, "")
	buf.Reset()

	// base + verbose -> base
	snap.MaybePrintBase(&buf, "xyzzy", true)
	c.Check(buf.String(), check.Equals, "base:\txyzzy\n")
	buf.Reset()

	// no base -> no base :)
	snap.MaybePrintBase(&buf, "", true)
	c.Check(buf.String(), check.Equals, "")
	buf.Reset()
}

func (s *infoSuite) TestMaybePrintPath(c *check.C) {
	var buf flushBuffer

	// no path -> no path
	snap.MaybePrintPath(&buf, "")
	c.Check(buf.String(), check.Equals, "")
	buf.Reset()

	// path -> path (quoted!)
	snap.MaybePrintPath(&buf, "xyzzy")
	c.Check(buf.String(), check.Equals, "path:\t\"xyzzy\"\n")
	buf.Reset()
}

func (s *infoSuite) TestInfoPricedNarrowTerminal(c *check.C) {
	defer snap.MockTermSize(func() (int, int) { return 44, 25 })()

	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			fmt.Fprintln(w, findPricedJSON)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/hello")
			fmt.Fprintln(w, "{}")
		default:
			c.Fatalf("expected to get 1 requests, now on %d (%v)", n+1, r)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"info", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `
name:    hello
summary: GNU Hello, the "hello world"
  snap
publisher: Canonical*
license:   Proprietary
price:     1.99GBP
description: |
  GNU hello prints a friendly greeting.
  This is part of the snapcraft tour at
  https://snapcraft.io/
snap-id: mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *infoSuite) TestInfoPriced(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			fmt.Fprintln(w, findPricedJSON)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/hello")
			fmt.Fprintln(w, "{}")
		default:
			c.Fatalf("expected to get 1 requests, now on %d (%v)", n+1, r)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"info", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `name:      hello
summary:   GNU Hello, the "hello world" snap
publisher: Canonical*
license:   Proprietary
price:     1.99GBP
description: |
  GNU hello prints a friendly greeting. This is part of the snapcraft tour at
  https://snapcraft.io/
snap-id: mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6
`)
	c.Check(s.Stderr(), check.Equals, "")
}

const mockInfoJSON = `
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
      "type": "app",
      "version": "2.10",
      "license": "MIT"
    }
  ],
  "sources": [
    "store"
  ],
  "suggested-currency": "GBP"
}
`

const mockInfoJSONWithChannels = `
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
      "tracks": ["1"]
    }
  ],
  "sources": [
    "store"
  ],
  "suggested-currency": "GBP"
}
`

func (s *infoSuite) TestInfoUnquoted(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			fmt.Fprintln(w, mockInfoJSON)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/hello")
			fmt.Fprintln(w, "{}")
		default:
			c.Fatalf("expected to get 2 requests, now on %d (%v)", n+1, r)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"info", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `name:      hello
summary:   The GNU Hello snap
publisher: Canonical*
license:   MIT
description: |
  GNU hello prints a friendly greeting. This is part of the snapcraft tour at
  https://snapcraft.io/
snap-id: mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6
`)
	c.Check(s.Stderr(), check.Equals, "")
}

const mockInfoJSONOtherLicense = `
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
      "resource": "/v2/snaps/hello",
      "revision": "1",
      "status": "available",
      "summary": "The GNU Hello snap",
      "type": "app",
      "version": "2.10",
      "license": "BSD-3",
      "tracking-channel": "beta"
    }
}
`
const mockInfoJSONNoLicense = `
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
      "resource": "/v2/snaps/hello",
      "revision": "100",
      "status": "available",
      "summary": "The GNU Hello snap",
      "type": "app",
      "version": "2.10",
      "license": "",
      "tracking-channel": "beta"
    }
}
`

func (s *infoSuite) TestInfoWithLocalDifferentLicense(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			fmt.Fprintln(w, mockInfoJSON)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/hello")
			fmt.Fprintln(w, mockInfoJSONOtherLicense)
		default:
			c.Fatalf("expected to get 2 requests, now on %d (%v)", n+1, r)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"info", "--abs-time", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `name:      hello
summary:   The GNU Hello snap
publisher: Canonical*
license:   BSD-3
description: |
  GNU hello prints a friendly greeting. This is part of the snapcraft tour at
  https://snapcraft.io/
snap-id:      mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6
tracking:     beta
refresh-date: 2006-01-02T22:04:07Z
installed:    2.10 (1) 1kB disabled
`)
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *infoSuite) TestInfoNotFound(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n % 2 {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/x")
		}
		w.WriteHeader(404)
		fmt.Fprintln(w, `{"type":"error","status-code":404,"status":"Not Found","result":{"message":"No.","kind":"snap-not-found","value":"x"}}`)

		n++
	})
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"info", "--verbose", "/x"})
	c.Check(err, check.ErrorMatches, `no snap found for "/x"`)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *infoSuite) TestInfoWithLocalNoLicense(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			fmt.Fprintln(w, mockInfoJSON)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/hello")
			fmt.Fprintln(w, mockInfoJSONNoLicense)
		default:
			c.Fatalf("expected to get 2 requests, now on %d (%v)", n+1, r)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"info", "--abs-time", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `name:      hello
summary:   The GNU Hello snap
publisher: Canonical*
license:   unset
description: |
  GNU hello prints a friendly greeting. This is part of the snapcraft tour at
  https://snapcraft.io/
snap-id:      mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6
tracking:     beta
refresh-date: 2006-01-02T22:04:07Z
installed:    2.10 (100) 1kB disabled
`)
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *infoSuite) TestInfoWithChannelsAndLocal(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0, 2, 4:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			fmt.Fprintln(w, mockInfoJSONWithChannels)
		case 1, 3, 5:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/hello")
			fmt.Fprintln(w, mockInfoJSONNoLicense)
		default:
			c.Fatalf("expected to get 6 requests, now on %d (%v)", n+1, r)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"info", "--abs-time", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `name:      hello
summary:   The GNU Hello snap
publisher: Canonical*
license:   unset
description: |
  GNU hello prints a friendly greeting. This is part of the snapcraft tour at
  https://snapcraft.io/
snap-id:      mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6
tracking:     beta
refresh-date: 2006-01-02T22:04:07Z
channels:
  1/stable:    2.10 2018-12-18T15:16:56Z   (1) 65kB -
  1/candidate: ^                                    
  1/beta:      ^                                    
  1/edge:      ^                                    
installed:     2.10                      (100)  1kB disabled
`)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(n, check.Equals, 2)

	// now the same but without abs-time
	s.ResetStdStreams()
	rest, err = snap.Parser(snap.Client()).ParseArgs([]string{"info", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `name:      hello
summary:   The GNU Hello snap
publisher: Canonical*
license:   unset
description: |
  GNU hello prints a friendly greeting. This is part of the snapcraft tour at
  https://snapcraft.io/
snap-id:      mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6
tracking:     beta
refresh-date: 2006-01-02
channels:
  1/stable:    2.10 2018-12-18   (1) 65kB -
  1/candidate: ^                          
  1/beta:      ^                          
  1/edge:      ^                          
installed:     2.10            (100)  1kB disabled
`)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(n, check.Equals, 4)

	// now the same but with unicode on
	s.ResetStdStreams()
	rest, err = snap.Parser(snap.Client()).ParseArgs([]string{"info", "--unicode=always", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `name:      hello
summary:   The GNU Hello snap
publisher: Canonical✓
license:   unset
description: |
  GNU hello prints a friendly greeting. This is part of the snapcraft tour at
  https://snapcraft.io/
snap-id:      mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6
tracking:     beta
refresh-date: 2006-01-02
channels:
  1/stable:    2.10 2018-12-18   (1) 65kB -
  1/candidate: ↑                          
  1/beta:      ↑                          
  1/edge:      ↑                          
installed:     2.10            (100)  1kB disabled
`)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(n, check.Equals, 6)
}

func (s *infoSuite) TestInfoHumanTimes(c *check.C) {
	// checks that tiemutil.Human is called when no --abs-time is given
	restore := snap.MockTimeutilHuman(func(time.Time) string { return "TOTALLY NOT A ROBOT" })
	defer restore()

	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			fmt.Fprintln(w, "{}")
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/hello")
			fmt.Fprintln(w, mockInfoJSONNoLicense)
		default:
			c.Fatalf("expected to get 2 requests, now on %d (%v)", n+1, r)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"info", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `name:      hello
summary:   The GNU Hello snap
publisher: Canonical*
license:   unset
description: |
  GNU hello prints a friendly greeting. This is part of the snapcraft tour at
  https://snapcraft.io/
snap-id:      mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6
tracking:     beta
refresh-date: TOTALLY NOT A ROBOT
installed:    2.10 (100) 1kB disabled
`)
	c.Check(s.Stderr(), check.Equals, "")
}

func (infoSuite) TestDescr(c *check.C) {
	for k, v := range map[string]string{
		"": "  \n",
		`one:
 * two three four five six  
   * seven height nine ten
`: `  one:
   * two three four
   five six
     * seven height
     nine ten
`,
		"abcdefghijklm nopqrstuvwxyz ABCDEFGHIJKLMNOPQR STUVWXYZ": `
  abcdefghijklm
  nopqrstuvwxyz
  ABCDEFGHIJKLMNOPQR
  STUVWXYZ
`[1:],
		// not much we can do when it won't fit
		"abcdefghijklmnopqrstuvwxyz ABCDEFGHIJKLMNOPQRSTUVWXYZ": `
  abcdefghijklmnopqr
  stuvwxyz
  ABCDEFGHIJKLMNOPQR
  STUVWXYZ
`[1:],
	} {
		var buf bytes.Buffer
		snap.PrintDescr(&buf, k, 20)
		c.Check(buf.String(), check.Equals, v, check.Commentf("%q", k))
	}
}

func (infoSuite) TestWrapCornerCase(c *check.C) {
	// this particular corner case isn't currently reachable from
	// printDescr nor printSummary, but best to have it covered
	var buf bytes.Buffer
	const s = "This is a paragraph indented with leading spaces that are encoded as multiple bytes. All hail EN SPACE."
	snap.WrapFlow(&buf, []rune(s), "\u2002\u2002", 30)
	c.Check(buf.String(), check.Equals, `
  This is a paragraph indented
  with leading spaces that are
  encoded as multiple bytes.
  All hail EN SPACE.
`[1:])
}

func (infoSuite) TestBug1828425(c *check.C) {
	const s = `This is a description
                                  that has
                                  lines
                                  too deeply
                                  indented.
`
	var buf bytes.Buffer
	err := snap.PrintDescr(&buf, s, 30)
	c.Assert(err, check.IsNil)
	c.Check(buf.String(), check.Equals, `  This is a description
    that has
    lines
    too deeply
    indented.
`)
}
