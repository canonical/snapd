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
	"net/http"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	snap "github.com/snapcore/snapd/cmd/snap"
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

func (s *infoSuite) TestMaybePrintServices(c *check.C) {
	for _, infos := range [][]client.AppInfo{svcAppInfos, mixedAppInfos} {
		var buf bytes.Buffer
		snap.MaybePrintServices(&buf, "foo", infos, -1)

		c.Check(buf.String(), check.Equals, `services:
  foo.svc1:	simple, disabled, active
  foo.svc2:	simple, enabled, inactive
`)
	}
}

func (s *infoSuite) TestMaybePrintServicesNoServices(c *check.C) {
	for _, infos := range [][]client.AppInfo{cmdAppInfos, nil} {
		var buf bytes.Buffer
		snap.MaybePrintServices(&buf, "foo", infos, -1)

		c.Check(buf.String(), check.Equals, "")
	}
}

func (s *infoSuite) TestMaybePrintCommands(c *check.C) {
	for _, infos := range [][]client.AppInfo{cmdAppInfos, mixedAppInfos} {
		var buf bytes.Buffer
		snap.MaybePrintCommands(&buf, "foo", infos, -1)

		c.Check(buf.String(), check.Equals, `commands:
  - foo.app1
  - foo.app2
`)
	}
}

func (s *infoSuite) TestMaybePrintCommandsNoCommands(c *check.C) {
	for _, infos := range [][]client.AppInfo{svcAppInfos, nil} {
		var buf bytes.Buffer
		snap.MaybePrintCommands(&buf, "foo", infos, -1)

		c.Check(buf.String(), check.Equals, "")
	}
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
	rest, err := snap.Parser().ParseArgs([]string{"info", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `name:      hello
summary:   GNU Hello, the "hello world" snap
publisher: Canonical✓
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
          "size": 65536
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
	rest, err := snap.Parser().ParseArgs([]string{"info", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `name:      hello
summary:   The GNU Hello snap
publisher: Canonical✓
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
      "revision": "1",
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
	rest, err := snap.Parser().ParseArgs([]string{"info", "--abs-time", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `name:      hello
summary:   The GNU Hello snap
publisher: Canonical✓
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
	rest, err := snap.Parser().ParseArgs([]string{"info", "--abs-time", "hello"})
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
refresh-date: 2006-01-02T22:04:07Z
installed:    2.10 (1) 1kB disabled
`)
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *infoSuite) TestInfoWithChannelsAndLocal(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			fmt.Fprintln(w, mockInfoJSONWithChannels)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/hello")
			fmt.Fprintln(w, mockInfoJSONNoLicense)
		default:
			c.Fatalf("expected to get 2 requests, now on %d (%v)", n+1, r)
		}

		n++
	})
	rest, err := snap.Parser().ParseArgs([]string{"info", "--abs-time", "hello"})
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
refresh-date: 2006-01-02T22:04:07Z
channels:                    
  1/stable:    2.10 (1) 65kB -
  1/candidate: ↑             
  1/beta:      ↑             
  1/edge:      ↑             
installed:     2.10 (1) 1kB  disabled
`)
	c.Check(s.Stderr(), check.Equals, "")
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
	rest, err := snap.Parser().ParseArgs([]string{"info", "hello"})
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
refresh-date: TOTALLY NOT A ROBOT
installed:    2.10 (1) 1kB disabled
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
