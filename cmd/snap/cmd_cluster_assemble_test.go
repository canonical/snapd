// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestClusterAssemble(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/cluster":
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.Header.Get("Content-Type"), check.Equals, "application/json")
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type":"async", "change":"42", "status-code": 202}`)
		case "/v2/changes/42":
			c.Check(r.Method, check.Equals, "GET")
			fmt.Fprintln(w, `{"type": "sync", "result": {"id": "42", "kind": "assemble-cluster", "summary": "Assemble cluster", "status": "Done", "tasks": [], "ready": true, "spawn-time": "2024-01-01T00:00:00Z"}}`)
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"cluster", "assemble",
		"--secret=test-secret",
		"--address=192.168.1.100:8080",
		"--expected-size=3",
	})
	c.Assert(err, check.IsNil)
	c.Check(s.Stdout(), check.Equals, "Cluster assembly completed successfully.\n")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestClusterAssembleNoWait(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/cluster":
			c.Check(r.Method, check.Equals, "POST")
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type":"async", "change":"42", "status-code": 202}`)
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"cluster", "assemble",
		"--secret=test-secret",
		"--address=192.168.1.100:8080",
		"--no-wait",
	})
	c.Assert(err, check.IsNil)
	c.Check(s.Stdout(), check.Equals, "42\n")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestClusterAssembleMissingSecret(c *check.C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"cluster", "assemble",
		"--address=192.168.1.100:8080",
	})
	c.Assert(err, check.ErrorMatches, `.*required flag.*--secret.*not specified.*`)
}

func (s *SnapSuite) TestClusterAssembleMissingAddress(c *check.C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"cluster", "assemble",
		"--secret=test-secret",
	})
	c.Assert(err, check.ErrorMatches, `.*required flag.*--address.*not specified.*`)
}

func (s *SnapSuite) TestClusterAssembleExtraArgs(c *check.C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"cluster", "assemble",
		"--secret=test-secret",
		"--address=192.168.1.100:8080",
		"extra-arg",
	})
	c.Assert(err, check.Equals, snap.ErrExtraArgs)
}

func (s *SnapSuite) TestClusterUnknownSubcommand(c *check.C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"cluster", "unknown",
	})
	c.Assert(err, check.ErrorMatches, `.*Unknown command.*unknown.*`)
}
