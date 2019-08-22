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

package client_test

import (
	"encoding/json"
	"io/ioutil"

	. "gopkg.in/check.v1"
)

func (cs *clientSuite) TestClientRemodelEndpoint(c *C) {
	cs.cli.Remodel([]byte(`{"new-model": "some-model"}`))
	c.Check(cs.req.Method, Equals, "POST")
	c.Check(cs.req.URL.Path, Equals, "/v2/model")
}

func (cs *clientSuite) TestClientRemodel(c *C) {
	cs.status = 202
	cs.rsp = `{
		"type": "async",
		"status-code": 202,
                "result": {},
		"change": "d728"
	}`
	remodelJsonData := []byte(`{"new-model": "some-model"}`)
	id, err := cs.cli.Remodel(remodelJsonData)
	c.Assert(err, IsNil)
	c.Check(id, Equals, "d728")
	c.Assert(cs.req.Header.Get("Content-Type"), Equals, "application/json")

	body, err := ioutil.ReadAll(cs.req.Body)
	c.Assert(err, IsNil)
	jsonBody := make(map[string]string)
	err = json.Unmarshal(body, &jsonBody)
	c.Assert(err, IsNil)
	c.Check(jsonBody, HasLen, 1)
	c.Check(jsonBody["new-model"], Equals, string(remodelJsonData))
}
