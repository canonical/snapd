// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"net/http"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/dirs"
)

const remodelOk = `{
  "type": "async",
  "status-code": 202,
  "status": "OK",
  "change": "101"
}`

const remodelError = `{
  "type": "error",
  "result": {
    "message": "bad snap",
    "kind": "bad snap"
  },
  "status-code": 400
}`

func (s *SnapSuite) TestRemodelOffline(c *C) {
	n := 0

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/v2/model")
		w.WriteHeader(202)

		var req map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&req)
		c.Assert(err, IsNil)

		c.Check(req["offline"], Equals, true)

		fmt.Fprint(w, remodelOk)
		n++
	})

	modelPath := filepath.Join(dirs.GlobalRootDir, "new-model")
	err := os.WriteFile(modelPath, []byte("snap1"), 0644)
	c.Assert(err, IsNil)

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"remodel", "--no-wait", "--offline", modelPath})

	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(n, Equals, 1)

	c.Check(s.Stdout(), Matches, "101\n")
	c.Check(s.Stderr(), Equals, "")

	s.ResetStdStreams()
}

func (s *SnapSuite) TestRemodelLocalSnapsOk(c *C) {
	n := 0

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/v2/model")
		w.WriteHeader(202)
		fmt.Fprint(w, remodelOk)
		n++
	})

	var err error
	modelPath := filepath.Join(dirs.GlobalRootDir, "new-model")
	err = os.WriteFile(modelPath, []byte("snap1"), 0644)
	c.Assert(err, IsNil)
	snapPath := filepath.Join(dirs.GlobalRootDir, "snap1.snap")
	err = os.WriteFile(snapPath, []byte("snap1"), 0644)
	c.Assert(err, IsNil)

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"remodel", "--no-wait", "--snap", snapPath, modelPath})

	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(n, Equals, 1)

	c.Check(s.Stdout(), Matches, "101\n")
	c.Check(s.Stderr(), Equals, "")

	s.ResetStdStreams()
}

func (s *SnapSuite) TestRemodelLocalSnapsError(c *C) {
	n := 0

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/v2/model")
		w.WriteHeader(400)
		fmt.Fprint(w, remodelError)
		n++
	})

	var err error
	modelPath := filepath.Join(dirs.GlobalRootDir, "new-model")
	err = os.WriteFile(modelPath, []byte("snap1"), 0644)
	c.Assert(err, IsNil)
	snapPath := filepath.Join(dirs.GlobalRootDir, "snap1.snap")
	err = os.WriteFile(snapPath, []byte("snap1"), 0644)
	c.Assert(err, IsNil)

	_, err = snap.Parser(snap.Client()).ParseArgs([]string{"remodel", "--no-wait", "--snap", snapPath, modelPath})

	c.Assert(err.Error(), Equals, "cannot do offline remodel: bad snap")
	c.Check(n, Equals, 1)

	c.Check(s.Stdout(), Matches, "")
	c.Check(s.Stderr(), Equals, "")

	s.ResetStdStreams()
}
