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

package client_test

import (
	"encoding/json"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/client"
)

func (cs *clientSuite) TestClientAllSkillsCallsEndpoint(c *check.C) {
	_, _ = cs.cli.AllSkills()
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/skills")
}

func (cs *clientSuite) TestClientAllSkills(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": [
			{
				"snap": "canonical-pi2",
				"name": "pin-13",
				"type": "bool-file",
				"label": "Pin 13",
				"granted_to": [
					{"snap": "keyboard-lights", "name": "capslock-led"}
				]
			}
		]
	}`
	skills, err := cs.cli.AllSkills()
	c.Assert(err, check.IsNil)
	c.Check(skills, check.DeepEquals, []client.SkillGrants{
		{
			Skill: client.Skill{
				Snap:  "canonical-pi2",
				Name:  "pin-13",
				Type:  "bool-file",
				Label: "Pin 13",
			},
			GrantedTo: []client.Slot{
				{
					Snap: "keyboard-lights",
					Name: "capslock-led",
				},
			},
		},
	})
}

func (cs *clientSuite) TestClientGrantCallsEndpoint(c *check.C) {
	_ = cs.cli.Grant("producer", "skill", "consumer", "slot")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/skills")
}

func (cs *clientSuite) TestClientGrant(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": { }
	}`
	err := cs.cli.Grant("producer", "skill", "consumer", "slot")
	c.Check(err, check.IsNil)
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"action": "grant",
		"skill": map[string]interface{}{
			"snap": "producer",
			"name": "skill",
		},
		"slot": map[string]interface{}{
			"snap": "consumer",
			"name": "slot",
		},
	})
}

func (cs *clientSuite) TestClientRevokeCallsEndpoint(c *check.C) {
	_ = cs.cli.Revoke("producer", "skill", "consumer", "slot")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/skills")
}

func (cs *clientSuite) TestClientRevoke(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": { }
	}`
	err := cs.cli.Revoke("producer", "skill", "consumer", "slot")
	c.Check(err, check.IsNil)
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"action": "revoke",
		"skill": map[string]interface{}{
			"snap": "producer",
			"name": "skill",
		},
		"slot": map[string]interface{}{
			"snap": "consumer",
			"name": "slot",
		},
	})
}

func (cs *clientSuite) TestClientAddSkillCallsEndpoint(c *check.C) {
	_ = cs.cli.AddSkill(&client.Skill{})
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/skills")
}

func (cs *clientSuite) TestClientAddSkill(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": { }
	}`
	err := cs.cli.AddSkill(&client.Skill{
		Snap: "snap",
		Name: "name",
		Type: "type",
		Attrs: map[string]interface{}{
			"attr": "value",
		},
		Apps:  []string{"app"},
		Label: "label",
	})
	c.Check(err, check.IsNil)
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"action": "add-skill",
		"skill": map[string]interface{}{
			"name": "name",
			"snap": "snap",
			"type": "type",
			"attrs": map[string]interface{}{
				"attr": "value",
			},
			"apps":  []interface{}{"app"},
			"label": "label",
		},
	})
}

func (cs *clientSuite) TestClientRemoveSkillCallsEndpoint(c *check.C) {
	_ = cs.cli.RemoveSkill("snap", "name")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/skills")
}

func (cs *clientSuite) TestClientRemoveSkill(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": { }
	}`
	err := cs.cli.RemoveSkill("snap", "name")
	c.Check(err, check.IsNil)
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"action": "remove-skill",
		"skill": map[string]interface{}{
			"name": "name",
			"snap": "snap",
		},
	})
}

func (cs *clientSuite) TestClientAddSlotCallsEndpoint(c *check.C) {
	_ = cs.cli.AddSlot(&client.Slot{})
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/skills")
}

func (cs *clientSuite) TestClientAddSlot(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": { }
	}`
	err := cs.cli.AddSlot(&client.Slot{
		Snap: "snap",
		Name: "name",
		Type: "type",
		Attrs: map[string]interface{}{
			"attr": "value",
		},
		Apps:  []string{"app"},
		Label: "label",
	})
	c.Check(err, check.IsNil)
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"action": "add-slot",
		"slot": map[string]interface{}{
			"name": "name",
			"snap": "snap",
			"type": "type",
			"attrs": map[string]interface{}{
				"attr": "value",
			},
			"apps":  []interface{}{"app"},
			"label": "label",
		},
	})
}

func (cs *clientSuite) TestClientRemoveSlotCallsEndpoint(c *check.C) {
	_ = cs.cli.RemoveSlot("snap", "name")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/skills")
}

func (cs *clientSuite) TestClientRemoveSlot(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": { }
	}`
	err := cs.cli.RemoveSlot("snap", "name")
	c.Check(err, check.IsNil)
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"action": "remove-slot",
		"slot": map[string]interface{}{
			"name": "name",
			"snap": "snap",
		},
	})
}
