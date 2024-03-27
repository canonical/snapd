// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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
	"path/filepath"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	snap "github.com/snapcore/snapd/cmd/snap"
	snaplib "github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/timeutil"
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

func isoDateTimeToLocalDate(c *check.C, textualTime string) string {
	t, err := time.Parse(time.RFC3339Nano, textualTime)
	c.Assert(err, check.IsNil)
	return t.Local().Format("2006-01-02")
}

func (s *infoSuite) TestMaybePrintServices(c *check.C) {
	var buf flushBuffer
	iw := snap.NewInfoWriter(&buf)
	for _, infos := range [][]client.AppInfo{svcAppInfos, mixedAppInfos} {
		buf.Reset()
		snap.SetupDiskSnap(iw, "", &client.Snap{Name: "foo", Apps: infos})
		snap.MaybePrintServices(iw)

		c.Check(buf.String(), check.Equals, `services:
  foo.svc1:	simple, disabled, active
  foo.svc2:	simple, enabled, inactive
`)
	}
}

func (s *infoSuite) TestMaybePrintServicesNoServices(c *check.C) {
	var buf flushBuffer
	iw := snap.NewInfoWriter(&buf)
	for _, infos := range [][]client.AppInfo{cmdAppInfos, nil} {
		buf.Reset()
		snap.SetupDiskSnap(iw, "", &client.Snap{Name: "foo", Apps: infos})
		snap.MaybePrintServices(iw)
		c.Check(buf.String(), check.Equals, "")
	}
}

func (s *infoSuite) TestMaybePrintCommands(c *check.C) {
	var buf flushBuffer
	iw := snap.NewInfoWriter(&buf)
	for _, infos := range [][]client.AppInfo{cmdAppInfos, mixedAppInfos} {
		buf.Reset()
		snap.SetupDiskSnap(iw, "", &client.Snap{Name: "foo", Apps: infos})
		snap.MaybePrintCommands(iw)

		c.Check(buf.String(), check.Equals, `commands:
  - foo.app1
  - foo.app2
`)
	}
}

func (s *infoSuite) TestMaybePrintCommandsNoCommands(c *check.C) {
	var buf flushBuffer
	iw := snap.NewInfoWriter(&buf)
	for _, infos := range [][]client.AppInfo{svcAppInfos, nil} {
		buf.Reset()
		snap.SetupDiskSnap(iw, "", &client.Snap{Name: "foo", Apps: infos})
		snap.MaybePrintCommands(iw)

		c.Check(buf.String(), check.Equals, "")
	}
}

func (infoSuite) TestPrintType(c *check.C) {
	var buf flushBuffer
	iw := snap.NewInfoWriter(&buf)
	for from, to := range map[string]string{
		"":            "",
		"app":         "",
		"application": "",
		"gadget":      "type:\tgadget\n",
		"core":        "type:\tcore\n",
		"os":          "type:\tcore\n",
	} {
		buf.Reset()
		snap.SetupDiskSnap(iw, "", &client.Snap{Type: from})
		snap.MaybePrintType(iw)
		c.Check(buf.String(), check.Equals, to, check.Commentf("%q", from))
	}
}

func (infoSuite) TestPrintSummary(c *check.C) {
	var buf flushBuffer
	iw := snap.NewInfoWriter(&buf)
	for from, to := range map[string]string{
		"":               `""`,                // empty results in quoted empty
		"foo":            "foo",               // plain text results in unquoted
		"two words":      "two words",         // ...even when multi-word
		"{":              `"{"`,               // but yaml-breaking is quoted
		"very long text": "very long\n  text", // too-long text gets split (TODO: split with tabbed indent to preserve alignment)
	} {
		buf.Reset()
		snap.SetupDiskSnap(iw, "", &client.Snap{Summary: from})
		snap.PrintSummary(iw)
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
		diskSnap, localSnap *client.Snap
		expected            string
	}

	var buf flushBuffer
	iw := snap.NewInfoWriter(&buf)
	for i, t := range []T{
		{&client.Snap{}, nil, ""},                 // nothing output for on-disk snap
		{nil, &client.Snap{}, "publisher:\t--\n"}, // from-snapd snap with no publisher is explicit
		{nil, &client.Snap{Publisher: acct}, "publisher:\tTeam Potato*\n"},
	} {
		buf.Reset()
		if t.diskSnap == nil {
			snap.SetupSnap(iw, t.localSnap, nil, nil)
		} else {
			snap.SetupDiskSnap(iw, "", t.diskSnap)
		}
		snap.MaybePrintPublisher(iw)
		c.Check(buf.String(), check.Equals, t.expected, check.Commentf("%d", i))
	}
}

func (s *infoSuite) TestMaybePrintNotes(c *check.C) {
	type T struct {
		localSnap, diskSnap *client.Snap
		expected            string
	}

	var buf flushBuffer
	iw := snap.NewInfoWriter(&buf)
	for i, t := range []T{
		{
			nil,
			&client.Snap{Private: true, Confinement: "devmode"},
			"notes:\t\n" +
				"  private:\ttrue\n" +
				"  confinement:\tdevmode\n",
		}, {
			&client.Snap{Private: true, Confinement: "devmode"},
			nil,
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
			nil,
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
		snap.SetVerbose(iw, false)
		if t.diskSnap == nil {
			snap.SetupSnap(iw, t.localSnap, nil, nil)
		} else {
			snap.SetupDiskSnap(iw, "", t.diskSnap)
		}
		snap.MaybePrintNotes(iw)
		c.Check(buf.String(), check.Equals, "", check.Commentf("%d/false", i))

		buf.Reset()
		snap.SetVerbose(iw, true)
		snap.MaybePrintNotes(iw)
		c.Check(buf.String(), check.Equals, t.expected, check.Commentf("%d/true", i))
	}
}

func (s *infoSuite) TestMaybePrintStandaloneVersion(c *check.C) {
	var buf flushBuffer
	iw := snap.NewInfoWriter(&buf)

	// no disk snap -> no version
	snap.MaybePrintStandaloneVersion(iw)
	c.Check(buf.String(), check.Equals, "")

	for version, expected := range map[string]string{
		"":    "--",
		"4.2": "4.2",
	} {
		buf.Reset()
		snap.SetupDiskSnap(iw, "", &client.Snap{Version: version})
		snap.MaybePrintStandaloneVersion(iw)
		c.Check(buf.String(), check.Equals, "version:\t"+expected+" -\n", check.Commentf("%q", version))

		buf.Reset()
		snap.SetupDiskSnap(iw, "", &client.Snap{Version: version, Confinement: "devmode"})
		snap.MaybePrintStandaloneVersion(iw)
		c.Check(buf.String(), check.Equals, "version:\t"+expected+" devmode\n", check.Commentf("%q", version))
	}
}

func (s *infoSuite) TestMaybePrintBuildDate(c *check.C) {
	var buf flushBuffer
	iw := snap.NewInfoWriter(&buf)
	// some prep
	dir := c.MkDir()
	arbfile := filepath.Join(dir, "arb")
	c.Assert(os.WriteFile(arbfile, nil, 0600), check.IsNil)
	filename := filepath.Join(c.MkDir(), "foo.snap")
	diskSnap := squashfs.New(filename)
	c.Assert(diskSnap.Build(dir, nil), check.IsNil)
	buildDate := diskSnap.BuildDate().Format(time.Kitchen)

	// no disk snap -> no build date
	snap.MaybePrintBuildDate(iw)
	c.Check(buf.String(), check.Equals, "")

	// path is directory -> no build date
	buf.Reset()
	snap.SetupDiskSnap(iw, dir, &client.Snap{})
	snap.MaybePrintBuildDate(iw)
	c.Check(buf.String(), check.Equals, "")

	// not actually a snap -> no build date
	buf.Reset()
	snap.SetupDiskSnap(iw, arbfile, &client.Snap{})
	snap.MaybePrintBuildDate(iw)
	c.Check(buf.String(), check.Equals, "")

	// disk snap -> get build date
	buf.Reset()
	snap.SetupDiskSnap(iw, filename, &client.Snap{})
	snap.MaybePrintBuildDate(iw)
	c.Check(buf.String(), check.Equals, "build-date:\t"+buildDate+"\n")
}

func (s *infoSuite) TestMaybePrintSum(c *check.C) {
	var buf flushBuffer
	// some prep
	dir := c.MkDir()
	filename := filepath.Join(c.MkDir(), "foo.snap")
	diskSnap := squashfs.New(filename)
	c.Assert(diskSnap.Build(dir, nil), check.IsNil)
	iw := snap.NewInfoWriter(&buf)
	snap.SetVerbose(iw, true)

	// no disk snap -> no checksum
	snap.MaybePrintSum(iw)
	c.Check(buf.String(), check.Equals, "")

	// path is directory -> no checksum
	buf.Reset()
	snap.SetupDiskSnap(iw, dir, &client.Snap{})
	snap.MaybePrintSum(iw)
	c.Check(buf.String(), check.Equals, "")

	// disk snap and verbose -> get checksum
	buf.Reset()
	snap.SetupDiskSnap(iw, filename, &client.Snap{})
	snap.MaybePrintSum(iw)
	c.Check(buf.String(), check.Matches, "sha3-384:\t\\S+\n")

	// disk snap but not verbose -> no checksum
	buf.Reset()
	snap.SetVerbose(iw, false)
	snap.MaybePrintSum(iw)
	c.Check(buf.String(), check.Equals, "")
}

func (s *infoSuite) TestMaybePrintLinksContact(c *check.C) {
	var buf flushBuffer
	iw := snap.NewInfoWriter(&buf)

	for contact, expected := range map[string]string{
		"mailto:joe@example.com": "contact:\tjoe@example.com\n",
		// gofmt 1.9 being silly
		"foo": "contact:\tfoo\n",
		"":    "",
	} {
		buf.Reset()
		snap.SetupDiskSnap(iw, "", &client.Snap{Contact: contact})
		snap.MaybePrintLinks(iw)
		c.Check(buf.String(), check.Equals, expected, check.Commentf("%q", contact))
	}
}

func (s *infoSuite) TestMaybePrintHoldingInfo(c *check.C) {
	var buf flushBuffer
	iw := snap.NewInfoWriterWithFmtTime(&buf, timeutil.Human)
	instant, err := time.Parse(time.RFC3339, "2000-01-01T00:00:00Z")
	c.Assert(err, check.IsNil)

	restore := snap.MockTimeNow(func() time.Time {
		return instant
	})
	defer restore()

	restore = timeutil.MockTimeNow(func() time.Time {
		return instant
	})
	defer restore()

	// ensure timezone is UTC, otherwise test runs in other timezones would fail
	oldLocal := time.Local
	time.Local = time.UTC
	defer func() {
		time.Local = oldLocal
	}()

	for _, holdKind := range []string{"hold", "hold-by-gating"} {
		for hold, expected := range map[string]string{
			"":                     "",
			"0001-01-01T00:00:00Z": "",
			"1999-01-01T00:00:00Z": "",
			"2000-01-01T11:30:00Z": fmt.Sprintf("%s:\ttoday at 11:30 UTC\n", holdKind),
			"2000-01-02T12:00:00Z": fmt.Sprintf("%s:\ttomorrow at 12:00 UTC\n", holdKind),
			"2000-02-01T00:00:00Z": fmt.Sprintf("%s:\tin 31 days, at 00:00 UTC\n", holdKind),
			"2099-01-01T00:00:00Z": fmt.Sprintf("%s:\t2099-01-01\n", holdKind),
			"2100-01-01T00:00:00Z": fmt.Sprintf("%s:\tforever\n", holdKind),
		} {
			buf.Reset()

			var holdTime *time.Time
			if hold != "" {
				t, err := time.Parse(time.RFC3339, hold)
				c.Assert(err, check.IsNil)
				holdTime = &t
			}

			switch holdKind {
			case "hold":
				snap.SetupSnap(iw, &client.Snap{Hold: holdTime}, nil, nil)
			case "hold-by-gating":
				snap.SetupSnap(iw, &client.Snap{GatingHold: holdTime}, nil, nil)
			default:
				c.Fatalf("unknown hold field: %s", holdKind)
			}

			snap.MaybePrintRefreshInfo(iw)
			iw.Flush()
			cmt := check.Commentf("expected %q but got %q", expected, buf.String())
			c.Assert(buf.String(), check.Equals, expected, cmt)
		}
	}
}

func (s *infoSuite) TestMaybePrintHoldingNonUTCLocalTime(c *check.C) {
	var buf flushBuffer
	iw := snap.NewInfoWriterWithFmtTime(&buf, timeutil.Human)
	instant, err := time.Parse(time.RFC3339, "2000-01-01T00:00:00Z")
	c.Assert(err, check.IsNil)

	restore := snap.MockTimeNow(func() time.Time {
		return instant
	})
	defer restore()

	restore = timeutil.MockTimeNow(func() time.Time {
		return instant
	})
	defer restore()

	hold := "2000-01-05T10:00:00Z"
	holdTime, err := time.Parse(time.RFC3339, hold)
	c.Assert(err, check.IsNil)

	// mock a local timezone other than UTC
	oldLocal := time.Local
	time.Local = time.FixedZone("UTC+4", 4*60*60)
	defer func() {
		time.Local = oldLocal
	}()

	snap.SetupSnap(iw, &client.Snap{Hold: &holdTime}, nil, nil)

	snap.MaybePrintRefreshInfo(iw)
	iw.Flush()
	c.Assert(buf.String(), check.Equals, "hold:\tin 4 days, at 14:00 UTC+4\n")
}

func (s *infoSuite) TestMaybePrintLinksVerbose(c *check.C) {
	var buf flushBuffer
	iw := snap.NewInfoWriter(&buf)
	snap.SetVerbose(iw, true)

	const contact = "mailto:joe@example.com"
	const website1 = "http://example.com/www1"
	const website2 = "http://example.com/www2"
	snap.SetupDiskSnap(iw, "", &client.Snap{
		Links: map[string][]string{
			"contact": {contact},
			"website": {website1, website2},
		},
		Contact: contact,
		Website: website1,
	})

	snap.MaybePrintLinks(iw)
	c.Check(buf.String(), check.Equals, "contact:\tjoe@example.com\n"+
		`links:
  contact:
    - mailto:joe@example.com
  website:
    - http://example.com/www1
    - http://example.com/www2
`)
}

func (s *infoSuite) TestMaybePrintBase(c *check.C) {
	var buf flushBuffer
	iw := snap.NewInfoWriter(&buf)
	dSnap := &client.Snap{}
	snap.SetupDiskSnap(iw, "", dSnap)

	// no verbose -> no base
	snap.SetVerbose(iw, false)
	snap.MaybePrintBase(iw)
	c.Check(buf.String(), check.Equals, "")
	buf.Reset()

	// no base -> no base :)
	snap.SetVerbose(iw, true)
	snap.MaybePrintBase(iw)
	c.Check(buf.String(), check.Equals, "")
	buf.Reset()

	// base + verbose -> base
	dSnap.Base = "xyzzy"
	snap.MaybePrintBase(iw)
	c.Check(buf.String(), check.Equals, "base:\txyzzy\n")
	buf.Reset()
}

func (s *infoSuite) TestMaybePrintPath(c *check.C) {
	var buf flushBuffer
	iw := snap.NewInfoWriter(&buf)
	dSnap := &client.Snap{}

	// no path -> no path
	snap.SetupDiskSnap(iw, "", dSnap)
	snap.MaybePrintPath(iw)
	c.Check(buf.String(), check.Equals, "")
	buf.Reset()

	// path -> path (quoted!)
	snap.SetupDiskSnap(iw, "xyzzy", dSnap)
	snap.MaybePrintPath(iw)
	c.Check(buf.String(), check.Equals, "path:\t\"xyzzy\"\n")
	buf.Reset()
}

func (s *infoSuite) TestClientSnapFromPath(c *check.C) {
	// minimal validity check
	fn := snaptest.MakeTestSnapWithFiles(c, `
name: some-snap
version: 9
`, nil)
	dSnap, err := snap.ClientSnapFromPath(fn)
	c.Assert(err, check.IsNil)
	c.Check(dSnap.Version, check.Equals, "9")
}

func (s *infoSuite) TestInfoPricedNarrowTerminal(c *check.C) {
	defer snap.MockTermSize(func() (int, int) { return 44, 25 })()

	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			fmt.Fprint(w, findPricedJSON)
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
publisher: Canonical**
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
			fmt.Fprint(w, findPricedJSON)
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
publisher: Canonical**
license:   Proprietary
price:     1.99GBP
description: |
  GNU hello prints a friendly greeting. This is part of the snapcraft tour at
  https://snapcraft.io/
snap-id: mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6
`)
	c.Check(s.Stderr(), check.Equals, "")
}

// only used for results on /v2/find
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
			fmt.Fprint(w, mockInfoJSON)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/hello")
			fmt.Fprint(w, "{}")
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
publisher: Canonical**
license:   MIT
description: |
  GNU hello prints a friendly greeting. This is part of the snapcraft tour at
  https://snapcraft.io/
snap-id: mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6
`)
	c.Check(s.Stderr(), check.Equals, "")
}

// only used for /v2/snaps/hello
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
      "health": {"revision": "1", "status": "blocked", "message": "please configure the grawflit", "timestamp": "2019-05-13T16:27:01.475851677+01:00"},
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
			fmt.Fprint(w, mockInfoJSON)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/hello")
			fmt.Fprint(w, mockInfoJSONOtherLicense)
		default:
			c.Fatalf("expected to get 2 requests, now on %d (%v)", n+1, r)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"info", "--abs-time", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `
name:    hello
summary: The GNU Hello snap
health:
  status:   blocked
  message:  please configure the grawflit
  checked:  2019-05-13T16:27:01+01:00
  revision: 1
publisher: Canonical**
license:   BSD-3
description: |
  GNU hello prints a friendly greeting. This is part of the snapcraft tour at
  https://snapcraft.io/
snap-id:      mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6
tracking:     beta
refresh-date: 2006-01-02T22:04:07Z
installed:    2.10 (1) 1kB disabled,blocked
`[1:])
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
			fmt.Fprint(w, mockInfoJSON)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/hello")
			fmt.Fprint(w, mockInfoJSONNoLicense)
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
publisher: Canonical**
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
			fmt.Fprint(w, mockInfoJSONWithChannels)
		case 1, 3, 5:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/hello")
			fmt.Fprint(w, mockInfoJSONNoLicense)
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
publisher: Canonical**
store-url: https://snapcraft.io/hello
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
	refreshDate := isoDateTimeToLocalDate(c, "2006-01-02T22:04:07.123456789Z")
	c.Check(s.Stdout(), check.Equals, fmt.Sprintf(`name:      hello
summary:   The GNU Hello snap
publisher: Canonical**
store-url: https://snapcraft.io/hello
license:   unset
description: |
  GNU hello prints a friendly greeting. This is part of the snapcraft tour at
  https://snapcraft.io/
snap-id:      mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6
tracking:     beta
refresh-date: %s
channels:
  1/stable:    2.10 2018-12-18   (1) 65kB -
  1/candidate: ^
  1/beta:      ^
  1/edge:      ^
installed:     2.10            (100)  1kB disabled
`, refreshDate))
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(n, check.Equals, 4)

	// now the same but with unicode on
	s.ResetStdStreams()
	rest, err = snap.Parser(snap.Client()).ParseArgs([]string{"info", "--unicode=always", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, fmt.Sprintf(`name:      hello
summary:   The GNU Hello snap
publisher: Canonical✓
store-url: https://snapcraft.io/hello
license:   unset
description: |
  GNU hello prints a friendly greeting. This is part of the snapcraft tour at
  https://snapcraft.io/
snap-id:      mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6
tracking:     beta
refresh-date: %s
channels:
  1/stable:    2.10 2018-12-18   (1) 65kB -
  1/candidate: ↑
  1/beta:      ↑
  1/edge:      ↑
installed:     2.10            (100)  1kB disabled
`, refreshDate))
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
			fmt.Fprint(w, mockInfoJSONNoLicense)
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
publisher: Canonical**
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

func (infoSuite) TestMaybePrintCohortKey(c *check.C) {
	type T struct {
		snap     *client.Snap
		verbose  bool
		expected string
	}

	tests := []T{
		{snap: nil, verbose: false, expected: ""},
		{snap: nil, verbose: true, expected: ""},
		{snap: &client.Snap{}, verbose: false, expected: ""},
		{snap: &client.Snap{}, verbose: true, expected: ""},
		{snap: &client.Snap{CohortKey: "some-cohort-key"}, verbose: false, expected: ""},
		{snap: &client.Snap{CohortKey: "some-cohort-key"}, verbose: true, expected: "cohort:\t…-key\n"},
	}

	var buf flushBuffer
	iw := snap.NewInfoWriter(&buf)
	defer snap.MockIsStdoutTTY(true)()

	for i, t := range tests {
		buf.Reset()
		snap.SetupSnap(iw, t.snap, nil, nil)
		snap.SetVerbose(iw, t.verbose)
		snap.MaybePrintCohortKey(iw)
		c.Check(buf.String(), check.Equals, t.expected, check.Commentf("tty:true/%d", i))
	}
	// now the same but without a tty -> the last test should no longer ellipt
	tests[len(tests)-1].expected = "cohort:\tsome-cohort-key\n"
	snap.MockIsStdoutTTY(false)
	for i, t := range tests {
		buf.Reset()
		snap.SetupSnap(iw, t.snap, nil, nil)
		snap.SetVerbose(iw, t.verbose)
		snap.MaybePrintCohortKey(iw)
		c.Check(buf.String(), check.Equals, t.expected, check.Commentf("tty:false/%d", i))
	}
}

func (infoSuite) TestMaybePrintHealth(c *check.C) {
	type T struct {
		snap     *client.Snap
		verbose  bool
		expected string
	}

	goodHealth := &client.SnapHealth{Status: "okay"}
	t0 := time.Date(1970, 1, 1, 10, 24, 0, 0, time.UTC)
	badHealth := &client.SnapHealth{
		Status:    "waiting",
		Message:   "godot should be here any moment now",
		Code:      "godot-is-a-lie",
		Revision:  snaplib.R("42"),
		Timestamp: t0,
	}

	tests := []T{
		{snap: nil, verbose: false, expected: ""},
		{snap: nil, verbose: true, expected: ""},
		{snap: &client.Snap{}, verbose: false, expected: ""},
		{snap: &client.Snap{}, verbose: true, expected: `health:
  status:	unknown
  message:	health
    has not been set
`},
		{snap: &client.Snap{Health: goodHealth}, verbose: false, expected: ``},
		{snap: &client.Snap{Health: goodHealth}, verbose: true, expected: `health:
  status:	okay
`},
		{snap: &client.Snap{Health: badHealth}, verbose: false, expected: `health:
  status:	waiting
  message:	godot
    should be here
    any moment now
  code:	godot-is-a-lie
  checked:	10:24AM
  revision:	42
`},
		{snap: &client.Snap{Health: badHealth}, verbose: true, expected: `health:
  status:	waiting
  message:	godot
    should be here
    any moment now
  code:	godot-is-a-lie
  checked:	10:24AM
  revision:	42
`},
	}

	var buf flushBuffer
	iw := snap.NewInfoWriter(&buf)
	defer snap.MockIsStdoutTTY(false)()

	for i, t := range tests {
		buf.Reset()
		snap.SetupSnap(iw, t.snap, nil, nil)
		snap.SetVerbose(iw, t.verbose)
		snap.MaybePrintHealth(iw)
		c.Check(buf.String(), check.Equals, t.expected, check.Commentf("%d", i))
	}
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

const mockInfoJSONParallelInstance = `
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
      "name": "hello_foo",
      "private": false,
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

func (s *infoSuite) TestInfoParllelInstance(c *check.C) {
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
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/hello_foo")
			fmt.Fprint(w, mockInfoJSONParallelInstance)
		default:
			c.Fatalf("expected to get 2 requests, now on %d (%v)", n+1, r)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"info", "hello_foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	refreshDate := isoDateTimeToLocalDate(c, "2006-01-02T22:04:07.123456789Z")
	// make sure local and remote info is combined in the output
	c.Check(s.Stdout(), check.Equals, fmt.Sprintf(`name:      hello_foo
summary:   The GNU Hello snap
publisher: Canonical**
store-url: https://snapcraft.io/hello
license:   unset
description: |
  GNU hello prints a friendly greeting. This is part of the snapcraft tour at
  https://snapcraft.io/
snap-id:      mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6
tracking:     beta
refresh-date: %s
channels:
  1/stable:    2.10 2018-12-18   (1) 65kB -
  1/candidate: ^
  1/beta:      ^
  1/edge:      ^
installed:     2.10            (100)  1kB disabled
`, refreshDate))
	c.Check(s.Stderr(), check.Equals, "")
}

const mockInfoJSONWithStoreURL = `
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

func (s *infoSuite) TestInfoStoreURL(c *check.C) {
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
			fmt.Fprint(w, mockInfoJSONWithStoreURL)
		default:
			c.Fatalf("expected to get 2 requests, now on %d (%v)", n+1, r)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"info", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	refreshDate := isoDateTimeToLocalDate(c, "2006-01-02T22:04:07.123456789Z")
	// make sure local and remote info is combined in the output
	c.Check(s.Stdout(), check.Equals, fmt.Sprintf(`name:      hello
summary:   The GNU Hello snap
publisher: Canonical**
store-url: https://snapcraft.io/hello
license:   unset
description: |
  GNU hello prints a friendly greeting. This is part of the snapcraft tour at
  https://snapcraft.io/
snap-id:      mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6
tracking:     beta
refresh-date: %s
channels:
  1/stable:    2.10 2018-12-18   (1) 65kB -
  1/candidate: ^
  1/beta:      ^
  1/edge:      ^
installed:     2.10            (100)  1kB disabled
`, refreshDate))
	c.Check(s.Stderr(), check.Equals, "")
}
