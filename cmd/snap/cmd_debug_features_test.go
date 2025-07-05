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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/testutil"
	"gopkg.in/check.v1"
)

func (s *SnapSuite) TestFeatures(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/debug")
			c.Check(r.URL.RawQuery, check.Equals, "aspect=features")
			data, err := io.ReadAll(r.Body)
			c.Check(err, check.IsNil)
			c.Check(data, check.HasLen, 0)
			fmt.Fprintln(w, `{"type": "sync", "result": {"tasks": ["example-task"]}}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"debug", "features"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	var cmds struct {
		Commands []string `json:"commands"`
		Tasks    []string `json:"tasks"`
	}
	err = json.NewDecoder(strings.NewReader(s.Stdout())).Decode(&cmds)
	c.Assert(err, check.IsNil)
	c.Check(cmds.Commands, testutil.Contains, "debug features")
	c.Check(cmds.Tasks, check.HasLen, 1)
	c.Check(cmds.Tasks, testutil.Contains, "example-task")
}
