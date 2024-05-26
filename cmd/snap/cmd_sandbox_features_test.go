// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestSandboxFeatures(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "sync", "result": {"sandbox-features": {"apparmor": ["a", "b", "c"], "selinux": ["1", "2", "3"]}}}`)
	})
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"debug", "sandbox-features"}))

	c.Assert(s.Stdout(), Equals, ""+
		"apparmor:  a b c\n"+
		"selinux:   1 2 3\n")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestSandboxFeaturesRequired(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "sync", "result": {"sandbox-features": {"apparmor": ["a", "b", "c"], "selinux": ["1", "2", "3"]}}}`)
	})
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"debug", "sandbox-features", "--required=apparmor:a", "--required=selinux:2"}))

	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestSandboxFeaturesRequiredButMissing(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "sync", "result": {"sandbox-features": {"apparmor": ["a", "b", "c"], "selinux": ["1", "2", "3"]}}}`)
	})
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"debug", "sandbox-features", "--required=magic:thing"}))
	c.Assert(err, ErrorMatches, `sandbox feature not available: "magic:thing"`)
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}
