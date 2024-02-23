// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/release"
)

func (s *SnapSuite) TestRecoveryHelp(c *C) {
	msg := `Usage:
  snap.test recovery [recovery-OPTIONS]

The recovery command lists the available recovery systems.

With --show-keys it displays recovery keys that can be used to unlock the
encrypted partitions if the device-specific automatic unlocking does not work.

[recovery command options]
      --color=[auto|never|always]     Use a little bit of color to highlight
                                      some things. (default: auto)
      --unicode=[auto|never|always]   Use a little bit of Unicode to improve
                                      legibility. (default: auto)
      --show-keys                     Show recovery keys (if available) to
                                      unlock encrypted partitions.
`
	s.testSubCommandHelp(c, "recovery", msg)
}

func (s *SnapSuite) TestRecovery(c *C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/systems")
			c.Check(r.URL.RawQuery, Equals, "")
			fmt.Fprintln(w, `{"type": "sync", "result": {
        "systems": [
           {
                "current": true,
                "default-recovery-system": true,
                "label": "20200101",
                "model": {
                    "model": "model-id-1",
                    "brand-id": "brand-id-1",
                    "display-name": "Wonky Model"
                },
                "brand": {
                    "id": "brand-id-1",
                    "username": "brand-1",
                    "display-name": "Wonky Publishing"
                },
                "actions": [
                    {"title": "recover", "mode": "recover"},
                    {"title": "reinstall", "mode": "install"}
                ]
           },
           {
                "label": "20200802",
                "model": {
                    "model": "model-id-2",
                    "brand-id": "brand-id-1",
                    "display-name": "Other Model"
                },
                "brand": {
                    "id": "brand-id-2",
                    "username": "brand-2",
                    "display-name": "Other Publishing"
                },
                "actions": [
                    {"title": "recover", "mode": "recover"},
                    {"title": "reinstall", "mode": "install"}
                ]
           }
        ]
}}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"recovery"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, `
Label     Brand    Model       Notes
20200101  brand-1  model-id-1  current,default-recovery-system
20200802  brand-2  model-id-2  -
`[1:])
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestNoRecoverySystems(c *C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/systems")
			c.Check(r.URL.RawQuery, Equals, "")
			fmt.Fprintln(w, `{"type": "sync", "result": {}}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"recovery"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "No recovery systems available.\n")
}

func (s *SnapSuite) TestNoRecoverySystemsError(c *C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/systems")
			c.Check(r.URL.RawQuery, Equals, "")
			fmt.Fprintln(w, `{"type": "error", "result": {"message": "permission denied"}, "status-code": 403}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"recovery"})
	c.Check(err, ErrorMatches, `cannot list recovery systems: permission denied`)
}

func (s *SnapSuite) TestRecoveryShowRecoveryKeyHappy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/system-recovery-keys")
			c.Check(r.URL.RawQuery, Equals, "")
			fmt.Fprintln(w, `{"type": "sync", "result": {"recovery-key": "61665-00531-54469-09783-47273-19035-40077-28287", "reinstall-key":"1234"}}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"recovery", "--show-keys"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, `recovery:   61665-00531-54469-09783-47273-19035-40077-28287
reinstall:  1234
`)
	c.Check(s.Stderr(), Equals, "")
	c.Check(n, Equals, 1)
}

func (s *SnapSuite) TestRecoveryShowRecoveryKeyAloneHappy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/system-recovery-keys")
			c.Check(r.URL.RawQuery, Equals, "")
			fmt.Fprintln(w, `{"type": "sync", "result": {"recovery-key": "61665-00531-54469-09783-47273-19035-40077-28287"}}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"recovery", "--show-keys"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, `recovery:  61665-00531-54469-09783-47273-19035-40077-28287
`)
	c.Check(s.Stderr(), Equals, "")
	c.Check(n, Equals, 1)
}
