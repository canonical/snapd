// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"errors"
	"fmt"
	"net/http"
	"net/url"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	snap "github.com/snapcore/snapd/cmd/snap"
)

// only used for /v2/snaps/hello
const mockInfoJSONWithApps = `
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": {
    "id": "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6",
    "title": "hello",
    "summary": "GNU Hello, the \"hello world\" snap",
    "description": "GNU hello prints a friendly greeting. This is part of the snapcraft tour at https://snapcraft.io/",
    "installed-size": 98304,
    "name": "hello",
    "publisher": {
      "id": "canonical",
      "username": "canonical",
      "display-name": "Canonical",
      "validation": "verified"
    },
    "developer": "canonical",
    "status": "active",
    "type": "app",
    "version": "2.10",
    "channel": "stable",
    "tracking-channel": "stable",
    "ignore-validation": false,
    "revision": "38",
    "confinement": "strict",
    "private": false,
    "devmode": false,
    "jailmode": false,
    "apps": [
      {
        "snap": "hello",
        "name": "hello",
        "desktop-file": "/path/to/hello_hello.desktop"
      },
      {
        "snap": "hello",
        "name": "universe",
        "desktop-file": "/path/to/hello_universe.desktop"
      },
      {
        "snap": "hello",
        "name": "common-id",
        "desktop-file": "/path/to/hello_common-id.desktop",
        "common-id": "io.snapcraft.hello.common-id"
      }
    ],
    "contact": "mailto:snaps@canonical.com",
    "mounted-from": "/var/lib/snapd/snaps/hello_38.snap",
    "install-date": "2019-10-11T13:34:15.630955389+08:00"
  }
}
`

func (s *SnapSuite) TestPortalInfo(c *C) {
	restore := snap.MockCgroupSnapNameFromPid(func(pid int) (string, error) {
		c.Check(pid, Equals, 42)
		return "hello", nil
	})
	defer restore()
	restore = snap.MockApparmorSnapAppFromPid(func(pid int) (string, string, string, error) {
		c.Check(pid, Equals, 42)
		return "hello", "universe", "", nil
	})
	defer restore()
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/snaps/hello")
			fmt.Fprint(w, mockInfoJSONWithApps)
		case 1:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/connections")
			c.Check(r.URL.Query(), DeepEquals, url.Values{
				"snap":      []string{"hello"},
				"interface": []string{"network-status"},
			})
			result := client.Connections{
				Established: []client.Connection{
					{
						Slot: client.SlotRef{
							Snap: "core",
							Name: "network-status",
						},
						Plug: client.PlugRef{
							Snap: "hello",
							Name: "network-status",
						},
						Interface: "network-status",
					},
				},
			}
			EncodeResponseBody(c, w, map[string]interface{}{
				"type":   "sync",
				"result": result,
			})
		default:
			c.Fatalf("expected to get 2 requests, now on %d (%v)", n+1, r)
		}
		n++
	})
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"routine", "portal-info", "42"}))

	c.Check(s.Stdout(), Equals, `[Snap Info]
InstanceName=hello
AppName=universe
DesktopFile=hello_universe.desktop
HasNetworkStatus=true
`)
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestPortalInfoCommonID(c *C) {
	restore := snap.MockCgroupSnapNameFromPid(func(pid int) (string, error) {
		c.Check(pid, Equals, 42)
		return "hello", nil
	})
	defer restore()
	restore = snap.MockApparmorSnapAppFromPid(func(pid int) (string, string, string, error) {
		c.Check(pid, Equals, 42)
		return "hello", "common-id", "", nil
	})
	defer restore()
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/snaps/hello")
			fmt.Fprint(w, mockInfoJSONWithApps)
		case 1:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/connections")
			c.Check(r.URL.Query(), DeepEquals, url.Values{
				"snap":      []string{"hello"},
				"interface": []string{"network-status"},
			})
			result := client.Connections{}
			EncodeResponseBody(c, w, map[string]interface{}{
				"type":   "sync",
				"result": result,
			})
		default:
			c.Fatalf("expected to get 2 requests, now on %d (%v)", n+1, r)
		}
		n++
	})
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"routine", "portal-info", "42"}))

	c.Check(s.Stdout(), Equals, `[Snap Info]
InstanceName=hello
AppName=common-id
DesktopFile=hello_common-id.desktop
CommonID=io.snapcraft.hello.common-id
HasNetworkStatus=false
`)
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestPortalInfoNoAppInfo(c *C) {
	restore := snap.MockCgroupSnapNameFromPid(func(pid int) (string, error) {
		c.Check(pid, Equals, 42)
		return "hello", nil
	})
	defer restore()
	restore = snap.MockApparmorSnapAppFromPid(func(pid int) (string, string, string, error) {
		c.Check(pid, Equals, 42)
		return "", "", "", errors.New("no apparmor")
	})
	defer restore()
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/snaps/hello")
			fmt.Fprint(w, mockInfoJSONWithApps)
		case 1:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/connections")
			c.Check(r.URL.Query(), DeepEquals, url.Values{
				"snap":      []string{"hello"},
				"interface": []string{"network-status"},
			})
			result := client.Connections{}
			EncodeResponseBody(c, w, map[string]interface{}{
				"type":   "sync",
				"result": result,
			})
		default:
			c.Fatalf("expected to get 2 requests, now on %d (%v)", n+1, r)
		}
		n++
	})
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"routine", "portal-info", "42"}))

	c.Check(s.Stdout(), Equals, `[Snap Info]
InstanceName=hello
AppName=hello
DesktopFile=hello_hello.desktop
HasNetworkStatus=false
`)
	c.Check(s.Stderr(), Equals, "")
}
