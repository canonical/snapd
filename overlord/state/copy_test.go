// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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

package state_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/state"
)

func (ss *stateSuite) TestCopyStateAlreadyExists(c *C) {
	srcStateFile := filepath.Join(c.MkDir(), "src-state.json")

	dstStateFile := filepath.Join(c.MkDir(), "dst-state.json")
	err := os.WriteFile(dstStateFile, nil, 0644)
	c.Assert(err, IsNil)

	err = state.CopyState(srcStateFile, dstStateFile, []string{"some-data"})
	c.Assert(err, ErrorMatches, `cannot copy state: "/.*/dst-state.json" already exists`)
}
func (ss *stateSuite) TestCopyStateNoDataEntriesToCopy(c *C) {
	srcStateFile := filepath.Join(c.MkDir(), "src-state.json")
	dstStateFile := filepath.Join(c.MkDir(), "dst-state.json")

	err := state.CopyState(srcStateFile, dstStateFile, nil)
	c.Assert(err, ErrorMatches, `cannot copy state: must provide at least one data entry to copy`)
}

var srcStateContent = []byte(`
{
    "data": {
        "api-download-tokens-secret": "123",
        "api-download-tokens-secret-time": "2020-02-21T10:32:37.916147296Z",
        "auth": {
            "last-id": 1,
            "users": [
                {
                    "id": 1,
                    "email": "some@user.com",
                    "macaroon": "1234",
                    "store-macaroon": "5678",
                    "store-discharges": [
                        "9012345"
                    ]
                }
            ],
            "device": {
                "brand": "generic",
                "model": "generic-classic",
                "serial": "xxxxx-yyyyy-",
                "key-id": "xxxxxx",
                "session-macaroon": "xxxx"
            },
            "macaroon-key": "xxxx="
        },
        "config": {
        }
    }
}
`)

const stateSuffix = `,"changes":{},"tasks":{},"last-change-id":0,"last-task-id":0,"last-lane-id":0,"last-notice-id":0}`

func (ss *stateSuite) TestCopyStateIntegration(c *C) {
	// create a mock srcState
	srcStateFile := filepath.Join(c.MkDir(), "src-state.json")
	err := os.WriteFile(srcStateFile, srcStateContent, 0644)
	c.Assert(err, IsNil)

	// copy
	dstStateFile := filepath.Join(c.MkDir(), "dst-state.json")
	err = state.CopyState(srcStateFile, dstStateFile, []string{"auth.users", "no-existing-does-not-error", "auth.last-id"})
	c.Assert(err, IsNil)

	// and check that the right bits got copied
	dstContent, err := ioutil.ReadFile(dstStateFile)
	c.Assert(err, IsNil)
	c.Check(string(dstContent), Equals, `{"data":{"auth":{"last-id":1,"users":[{"id":1,"email":"some@user.com","macaroon":"1234","store-macaroon":"5678","store-discharges":["9012345"]}]}}`+stateSuffix)
}

var srcStateContent1 = []byte(`{
    "data": {
        "A": {"B": [{"C": 1}, {"D": 2}]},
        "E": {"F": 2, "G": 3},
        "H": 4,
        "I": null
    }
}`)

func (ss *stateSuite) TestCopyState(c *C) {
	srcStateFile := filepath.Join(c.MkDir(), "src-state.json")
	err := os.WriteFile(srcStateFile, srcStateContent1, 0644)
	c.Assert(err, IsNil)

	dstStateFile := filepath.Join(c.MkDir(), "dst-state.json")
	err = state.CopyState(srcStateFile, dstStateFile, []string{"A.B", "no-existing-does-not-error", "E.F", "E", "I", "E.non-existing"})
	c.Assert(err, IsNil)

	dstContent, err := ioutil.ReadFile(dstStateFile)
	c.Assert(err, IsNil)
	c.Check(string(dstContent), Equals, `{"data":{"A":{"B":[{"C":1},{"D":2}]},"E":{"F":2,"G":3},"I":null}`+stateSuffix)
}

func (ss *stateSuite) TestCopyStateUnmarshalNotMap(c *C) {
	srcStateFile := filepath.Join(c.MkDir(), "src-state.json")
	err := os.WriteFile(srcStateFile, srcStateContent1, 0644)
	c.Assert(err, IsNil)

	dstStateFile := filepath.Join(c.MkDir(), "dst-state.json")
	err = state.CopyState(srcStateFile, dstStateFile, []string{"E.F.subkey-not-in-a-map"})
	c.Assert(err, ErrorMatches, `cannot unmarshal state entry "E.F" with value "2" as a map while trying to copy over "E.F.subkey-not-in-a-map"`)
}

func (ss *stateSuite) TestCopyStateDuplicatesInDataEntriesAreFine(c *C) {
	srcStateFile := filepath.Join(c.MkDir(), "src-state.json")
	err := os.WriteFile(srcStateFile, srcStateContent1, 0644)
	c.Assert(err, IsNil)

	dstStateFile := filepath.Join(c.MkDir(), "dst-state.json")
	err = state.CopyState(srcStateFile, dstStateFile, []string{"E", "E"})
	c.Assert(err, IsNil)

	dstContent, err := ioutil.ReadFile(dstStateFile)
	c.Assert(err, IsNil)
	c.Check(string(dstContent), Equals, `{"data":{"E":{"F":2,"G":3}}`+stateSuffix)
}
