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
)

func (s *SnapSuite) TestRecoveryHelp(c *C) {
	msg := `Usage:
  snap.test recovery

The recovery command lists the available recovery systems.
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
                "label": "20200101",
                "model": {
                    "model": "this-is-model-id",
                    "brand-id": "brand-id-1",
                    "display-name": "Wonky Model"
                },
                "brand": {
                    "id": "brand-id-1",
                    "username": "brand",
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
                    "model": "this-is-model-id",
                    "brand-id": "brand-id-1",
                    "display-name": "Other Model"
                },
                "brand": {
                    "id": "brand-id-1",
                    "username": "brand",
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
Label     Brand             Model        Notes
20200101  Wonky Publishing  Wonky Model  current
20200802  Other Publishing  Other Model  -
`[1:])
	c.Check(s.Stderr(), Equals, "")
}
