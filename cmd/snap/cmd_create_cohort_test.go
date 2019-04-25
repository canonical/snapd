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
	"fmt"
	"net/http"

	"gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestCreateCohort(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		fmt.Fprintln(w, `{
"type": "sync",
"status-code": 200,
"status": "OK",
"result": {"foo": "what", "bar": "this"}}`)

	})

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"create-cohort", "foo", "bar"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)

	var v map[string]map[string]map[string]string
	c.Assert(yaml.Unmarshal(s.stdout.Bytes(), &v), check.IsNil)
	c.Check(v, check.DeepEquals, map[string]map[string]map[string]string{
		"cohorts": {
			"foo": {"cohort-key": "what"},
			"bar": {"cohort-key": "this"},
		},
	})
}
