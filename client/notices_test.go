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

package client_test

import (
	"encoding/json"
	"io"

	"github.com/snapcore/snapd/client"
	. "gopkg.in/check.v1"
)

func (cs *clientSuite) TestNotify(c *C) {
	cs.rsp = `{"type": "sync", "result": {"id": "7"}}`
	noticeID, err := cs.cli.Notify(&client.NotifyOptions{
		Type: client.SnapRunInhibitNotice,
		Key:  "snap-name",
	})
	c.Assert(err, IsNil)
	c.Check(noticeID, Equals, "7")
	c.Assert(cs.req.URL.Path, Equals, "/v2/notices")

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, IsNil)
	var m map[string]any
	err = json.Unmarshal(body, &m)
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]any{
		"action": "add",
		"type":   "snap-run-inhibit",
		"key":    "snap-name",
	})
}
