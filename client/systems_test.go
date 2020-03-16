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
	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/snap"
)

func (cs *clientSuite) TestListSystemsSome(c *check.C) {
	cs.rsp = `{
	    "type": "sync",
	    "status-code": 200,
	    "result": {
	        "systems": [
	           {
	                "current": true,
	                "label": "20200101",
	                "model": {
	                    "model": "this-is-model-id",
	                    "brand-id": "brand-id-1",
	                    "display-name": "wonky model"
	                },
	                "brand": {
	                    "id": "brand-id-1",
	                    "username": "brand",
	                    "display-name": "wonky publishing"
	                },
	                "actions": [
	                    {"id": "action-1", "title": "recover", "mode": "run"},
	                    {"id": "action-2", "title": "reinstall", "mode": "run"}
	                ]
	           }, {
	                "label": "20200311",
	                "model": {
	                    "model": "different-model-id",
	                    "brand-id": "bulky-brand-id-1",
	                    "display-name": "bulky model"
	                },
	                "brand": {
	                    "id": "bulky-brand-id-1",
	                    "username": "bulky-brand",
	                    "display-name": "bulky publishing"
	                },
	                "actions": [
	                    {"id": "action-1", "title": "factory-reset", "mode": "run"}
	                ]
	            }
	        ]
	    }
	}`
	systems, err := cs.cli.ListSystems()
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/systems")
	c.Check(systems, check.DeepEquals, []client.System{
		{
			Current: true,
			Label:   "20200101",
			Model: client.SystemModelData{
				Model:       "this-is-model-id",
				BrandID:     "brand-id-1",
				DisplayName: "wonky model",
			},
			Brand: snap.StoreAccount{
				ID:          "brand-id-1",
				Username:    "brand",
				DisplayName: "wonky publishing",
			},
			Actions: []client.SystemAction{
				{ID: "action-1", Title: "recover", Mode: "run"},
				{ID: "action-2", Title: "reinstall", Mode: "run"},
			},
		}, {
			Label: "20200311",
			Model: client.SystemModelData{
				Model:       "different-model-id",
				BrandID:     "bulky-brand-id-1",
				DisplayName: "bulky model",
			},
			Brand: snap.StoreAccount{
				ID:          "bulky-brand-id-1",
				Username:    "bulky-brand",
				DisplayName: "bulky publishing",
			},
			Actions: []client.SystemAction{
				{ID: "action-1", Title: "factory-reset", Mode: "run"},
			},
		},
	})
}

func (cs *clientSuite) TestListSystemsNone(c *check.C) {
	cs.rsp = `{
	    "type": "sync",
	    "status-code": 200,
	    "result": {}
	}`
	systems, err := cs.cli.ListSystems()
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/systems")
	c.Check(systems, check.HasLen, 0)
}
