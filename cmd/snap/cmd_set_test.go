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
	"encoding/json"
	"fmt"
	"net/http"

	"gopkg.in/check.v1"

	snapset "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

var validApplyYaml = []byte(`name: snapname
version: 1.0
hooks:
 configure:
`)

func (s *SnapSuite) TestInvalidSetParameters(c *check.C) {
	invalidParameters := []string{"set", "snap-name", "key", "value"}
	_, err := snapset.Parser().ParseArgs(invalidParameters)
	c.Check(err, check.ErrorMatches, ".*invalid configuration:.*(want key=value).*")
}

func (s *SnapSuite) TestSnapSetIntegrationString(c *check.C) {
	// mock installed snap
	snaptest.MockSnap(c, string(validApplyYaml), &snap.SideInfo{
		Revision: snap.R(42),
	})

	// and mock the server
	s.mockSetConfigServer(c, "value")

	// Set a config value for the active snap
	_, err := snapset.Parser().ParseArgs([]string{"set", "snapname", "key=value"})
	c.Assert(err, check.IsNil)
}

func (s *SnapSuite) TestSnapSetIntegrationNumber(c *check.C) {
	// mock installed snap
	snaptest.MockSnap(c, string(validApplyYaml), &snap.SideInfo{
		Revision: snap.R(42),
	})

	// and mock the server
	s.mockSetConfigServer(c, json.Number("1.2"))

	// Set a config value for the active snap
	_, err := snapset.Parser().ParseArgs([]string{"set", "snapname", "key=1.2"})
	c.Assert(err, check.IsNil)
}

func (s *SnapSuite) TestSnapSetIntegrationBigInt(c *check.C) {
	snaptest.MockSnap(c, string(validApplyYaml), &snap.SideInfo{
		Revision: snap.R(42),
	})

	// and mock the server
	s.mockSetConfigServer(c, json.Number("1234567890"))

	// Set a config value for the active snap
	_, err := snapset.Parser().ParseArgs([]string{"set", "snapname", "key=1234567890"})
	c.Assert(err, check.IsNil)
}

func (s *SnapSuite) TestSnapSetIntegrationJson(c *check.C) {
	// mock installed snap
	snaptest.MockSnap(c, string(validApplyYaml), &snap.SideInfo{
		Revision: snap.R(42),
	})

	// and mock the server
	s.mockSetConfigServer(c, map[string]interface{}{"subkey": "value"})

	// Set a config value for the active snap
	_, err := snapset.Parser().ParseArgs([]string{"set", "snapname", `key={"subkey":"value"}`})
	c.Assert(err, check.IsNil)
}

func (s *SnapSuite) mockSetConfigServer(c *check.C, expectedValue interface{}) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/snaps/snapname/conf":
			c.Check(r.Method, check.Equals, "PUT")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
				"key": expectedValue,
			})
			fmt.Fprintln(w, `{"type":"async", "status-code": 202, "change": "zzz"}`)
		case "/v2/changes/zzz":
			c.Check(r.Method, check.Equals, "GET")
			fmt.Fprintln(w, `{"type":"sync", "result":{"ready": true, "status": "Done"}}`)
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	})
}
