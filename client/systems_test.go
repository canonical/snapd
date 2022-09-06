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

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/gadget"
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
	                    {"title": "recover", "mode": "recover"},
	                    {"title": "reinstall", "mode": "install"}
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
	                    {"title": "factory-reset", "mode": "install"}
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
				{Title: "recover", Mode: "recover"},
				{Title: "reinstall", Mode: "install"},
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
				{Title: "factory-reset", Mode: "install"},
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

func (cs *clientSuite) TestRequestSystemActionHappy(c *check.C) {
	cs.rsp = `{
	    "type": "sync",
	    "status-code": 200,
	    "result": {}
	}`
	err := cs.cli.DoSystemAction("1234", &client.SystemAction{
		Title: "reinstall",
		Mode:  "install",
	})
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/systems/1234")

	body, err := ioutil.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)
	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	c.Assert(err, check.IsNil)
	c.Assert(req, check.DeepEquals, map[string]interface{}{
		"action": "do",
		"title":  "reinstall",
		"mode":   "install",
	})
}

func (cs *clientSuite) TestRequestSystemActionError(c *check.C) {
	cs.rsp = `{
	    "type": "error",
	    "status-code": 500,
	    "result": {"message": "failed"}
	}`
	err := cs.cli.DoSystemAction("1234", &client.SystemAction{Mode: "install"})
	c.Assert(err, check.ErrorMatches, "cannot request system action: failed")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/systems/1234")
}

func (cs *clientSuite) TestRequestSystemActionInvalid(c *check.C) {
	err := cs.cli.DoSystemAction("", &client.SystemAction{})
	c.Assert(err, check.ErrorMatches, "cannot request an action without the system")
	err = cs.cli.DoSystemAction("1234", nil)
	c.Assert(err, check.ErrorMatches, "cannot request an action without one")
}

func (cs *clientSuite) TestRequestSystemRebootHappy(c *check.C) {
	cs.rsp = `{
	    "type": "sync",
	    "status-code": 200,
	    "result": {}
	}`
	err := cs.cli.RebootToSystem("20201212", "install")
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/systems/20201212")

	body, err := ioutil.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)
	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	c.Assert(err, check.IsNil)
	c.Assert(req, check.DeepEquals, map[string]interface{}{
		"action": "reboot",
		"mode":   "install",
	})
}

func (cs *clientSuite) TestRequestSystemRebootErrorNoSystem(c *check.C) {
	cs.rsp = `{
	    "type": "error",
	    "status-code": 500,
	    "result": {"message": "failed"}
	}`
	err := cs.cli.RebootToSystem("", "install")
	c.Assert(err, check.ErrorMatches, `cannot request system reboot: failed`)
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/systems")
}

func (cs *clientSuite) TestRequestSystemRebootErrorWithSystem(c *check.C) {
	cs.rsp = `{
	    "type": "error",
	    "status-code": 500,
	    "result": {"message": "failed"}
	}`
	err := cs.cli.RebootToSystem("1234", "install")
	c.Assert(err, check.ErrorMatches, `cannot request system reboot into "1234": failed`)
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/systems/1234")
}

func (cs *clientSuite) TestRequestSystemInstallHappy(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"type": "async",
		"status-code": 202,
		"change": "42"
	}`
	vols := map[string][]gadget.Volume{
		"pc": {
			{
				Schema: "dos",
				Bootloader: "mbr",
				ID: "0c",
				// Note that name is not exported as json
				Name: "pc",
			},
		},
	}
	chgID, err := cs.cli.InstallSystem("1234", client.InstallStepFinish, vols)
	c.Assert(err, check.IsNil)
	c.Assert(chgID, check.Equals, "42")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/systems/1234")

	body, err := ioutil.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)
	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	c.Assert(err, check.IsNil)
	c.Assert(req, check.DeepEquals, map[string]interface{}{
		"action": "install",
		"step":   "finish",
		"on-volumes": map[string]interface{}{
			"pc": []interface{}{
				map[string]interface{}{
					"schema":     "dos",
					"bootloader": "mbr",
					"id":         "0c",
					"structure":  nil,
				},
			},
		},
	})
}

func (cs *clientSuite) TestRequestSystemInstallErrorNoSystem(c *check.C) {
	cs.rsp = `{
	    "type": "error",
	    "status-code": 500,
	    "result": {"message": "failed"}
	}`
	_, err := cs.cli.InstallSystem("1234", client.InstallStepFinish, nil)
	c.Assert(err, check.ErrorMatches, `cannot request system install for "1234": failed`)
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/systems/1234")
}
