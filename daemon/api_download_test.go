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

package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/snapcore/snapd/store"
	fakestore "github.com/snapcore/snapd/tests/lib/fakestore/store"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&snapDownloadSuite{})

type snapDownloadSuite struct {
	fakeStore *store.Store
}

func (s *snapDownloadSuite) SetUpTest(c *check.C) {
	// s.fakeStore = fakestore.NewStore("", "localhost", false).(*store.Store)
}

func (s *snapDownloadSuite) TestDownloadSnap(c *check.C) {

	fstore := fakestore.NewStore("", "localhost", false)

	type scenario struct {
		data   snapDownloadAction
		status int
		err    string
	}

	for i, scen := range []scenario{
		{
			data: snapDownloadAction{
				Action: "download",
			},
			status: 400,
			err:    "download operation requires at least one snap name",
		},
		{
			data: snapDownloadAction{
				Action: "stream",
				Snaps:  []string{"foo"},
			},
			status: 400,
			err:    `unknown download operation "stream"`,
		},
		{
			data: snapDownloadAction{
				Action: "stream",
				Snaps:  []string{"foo", "bar"},
			},
			status: 400,
			err:    `download operation supports only one snap`,
		},
	} {
		fmt.Printf("runnung test case number %d\n", i)
		data, err := json.Marshal(scen.data)
		c.Check(err, check.IsNil)
		req, err := http.NewRequest("POST", "/v2/download", bytes.NewBuffer(data))
		c.Assert(err, check.IsNil)
		rsp := postSnapDownload(snapDownloadCmd, req, nil).(*resp)
		c.Assert(rsp.Status, check.Equals, scen.status)
		if scen.err != "" {
			c.Check(rsp.Result.(*errorResult).Message, check.Matches, scen.err)
		}
	}

}
