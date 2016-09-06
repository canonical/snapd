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

	snapset "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

var basicYaml = []byte(`name: snapname
version: 1.0
`)

func (s *SnapSuite) setupGetTests(c *check.C) {
	snaptest.MockSnap(c, string(basicYaml), &snap.SideInfo{
		Revision: snap.R(42),
	})

	// and mock the server
	s.mockGetConfigServer(c)
}

func (s *SnapSuite) TestInvalidGetParameters(c *check.C) {
	invalidParameters := []string{"get", "snap-name", "--foo"}
	_, err := snapset.Parser().ParseArgs(invalidParameters)
	c.Check(err, check.ErrorMatches, ".*unknown flag.*foo.*")
}

func (s *SnapSuite) TestSnapGetIntegration(c *check.C) {
	// mock installed snap
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("/") }()

	s.setupGetTests(c)

	// Get the config value for the active snap
	_, err := snapset.Parser().ParseArgs([]string{"get", "snapname", "test-key"})
	c.Check(err, check.IsNil)
	c.Check(s.Stdout(), check.Equals, "\"test-value\"\n")
}

func (s *SnapSuite) TestSnapGetIntegrationFullDocument(c *check.C) {
	// mock installed snap
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("/") }()

	s.setupGetTests(c)

	// Get the config value for the active snap
	_, err := snapset.Parser().ParseArgs([]string{"get", "-d", "snapname", "test-key"})
	c.Check(err, check.IsNil)
	c.Check(s.Stdout(), check.Equals, `{
	"test-key": "test-value"
}
`)
}

func (s *SnapSuite) TestSnapGetIntegrationMultipleKeys(c *check.C) {
	// mock installed snap
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("/") }()

	s.setupGetTests(c)

	// Get the config value for the active snap
	_, err := snapset.Parser().ParseArgs([]string{"get", "snapname", "test-key1", "test-key2"})
	c.Check(err, check.IsNil)
	c.Check(s.Stdout(), check.Equals, `{
	"test-key1": "test-value1",
	"test-key2": "test-value2"
}
`)
}

func (s *SnapSuite) TestSnapGetIntegrationMissingKey(c *check.C) {
	// mock installed snap
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("/") }()

	s.setupGetTests(c)

	// Get the config value for the active snap
	_, err := snapset.Parser().ParseArgs([]string{"get", "snapname", "missing-key"})
	c.Check(err, check.IsNil)
	c.Check(s.Stdout(), check.Equals, "")
}

func (s *SnapSuite) mockGetConfigServer(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/snaps/snapname/conf" {
			c.Fatalf("unexpected path %q", r.URL.Path)
		}

		query := r.URL.Query()
		switch query.Get("keys") {
		case "test-key":
			c.Check(r.Method, check.Equals, "GET")
			fmt.Fprintln(w, `{"type":"sync", "status-code": 200, "result": {"test-key":"test-value"}}`)
		case "test-key1,test-key2":
			c.Check(r.Method, check.Equals, "GET")
			fmt.Fprintln(w, `{"type":"sync", "status-code": 200, "result": {"test-key1":"test-value1","test-key2":"test-value2"}}`)
		default:
			c.Fatalf("unexpected keys %q", query.Get("keys"))
		}
	})
}
