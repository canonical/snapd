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
	"net/url"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
)

func (cs *clientSuite) TestClientConnectionsCallsEndpoint(c *check.C) {
	_, _ = cs.cli.Connections(nil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/connections")
}

func (cs *clientSuite) TestClientConnectionsDefault(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": {
			"established": [
				{
					"slot": {"snap": "keyboard-lights", "slot": "capslock-led"},
					"plug": {"snap": "canonical-pi2", "plug": "pin-13"},
					"interface": "bool-file",
					"gadget": true
                                }
			],
			"plugs": [
				{
					"snap": "canonical-pi2",
					"plug": "pin-13",
					"interface": "bool-file",
					"label": "Pin 13",
					"connections": [
						{"snap": "keyboard-lights", "slot": "capslock-led"}
					]
				}
			],
			"slots": [
				{
					"snap": "keyboard-lights",
					"slot": "capslock-led",
					"interface": "bool-file",
					"label": "Capslock indicator LED",
					"connections": [
						{"snap": "canonical-pi2", "plug": "pin-13"}
					]
				}
			]
		}
	}`
	conns := mylog.Check2(cs.cli.Connections(nil))
	c.Assert(err, check.IsNil)
	c.Check(cs.req.URL.Path, check.Equals, "/v2/connections")
	c.Check(conns, check.DeepEquals, client.Connections{
		Established: []client.Connection{
			{
				Plug:      client.PlugRef{Snap: "canonical-pi2", Name: "pin-13"},
				Slot:      client.SlotRef{Snap: "keyboard-lights", Name: "capslock-led"},
				Interface: "bool-file",
				Gadget:    true,
			},
		},
		Plugs: []client.Plug{
			{
				Snap:      "canonical-pi2",
				Name:      "pin-13",
				Interface: "bool-file",
				Label:     "Pin 13",
				Connections: []client.SlotRef{
					{
						Snap: "keyboard-lights",
						Name: "capslock-led",
					},
				},
			},
		},
		Slots: []client.Slot{
			{
				Snap:      "keyboard-lights",
				Name:      "capslock-led",
				Interface: "bool-file",
				Label:     "Capslock indicator LED",
				Connections: []client.PlugRef{
					{
						Snap: "canonical-pi2",
						Name: "pin-13",
					},
				},
			},
		},
	})
}

func (cs *clientSuite) TestClientConnectionsAll(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": {
			"established": [
				{
					"slot": {"snap": "keyboard-lights", "slot": "capslock-led"},
					"plug": {"snap": "canonical-pi2", "plug": "pin-13"},
					"interface": "bool-file",
					"gadget": true
                                }
			],
			"undesired": [
				{
					"slot": {"snap": "keyboard-lights", "slot": "numlock-led"},
					"plug": {"snap": "canonical-pi2", "plug": "pin-14"},
					"interface": "bool-file",
					"gadget": true,
					"manual": true
                                }
			],
			"plugs": [
				{
					"snap": "canonical-pi2",
					"plug": "pin-13",
					"interface": "bool-file",
					"label": "Pin 13",
					"connections": [
						{"snap": "keyboard-lights", "slot": "capslock-led"}
					]
				},
				{
					"snap": "canonical-pi2",
					"plug": "pin-14",
					"interface": "bool-file",
					"label": "Pin 14"
				}
			],
			"slots": [
				{
					"snap": "keyboard-lights",
					"slot": "capslock-led",
					"interface": "bool-file",
					"label": "Capslock indicator LED",
					"connections": [
						{"snap": "canonical-pi2", "plug": "pin-13"}
					]
				},
				{
					"snap": "keyboard-lights",
					"slot": "numlock-led",
					"interface": "bool-file",
					"label": "Numlock LED"
				}
			]
		}
	}`
	conns := mylog.Check2(cs.cli.Connections(&client.ConnectionOptions{All: true}))
	c.Assert(err, check.IsNil)
	c.Check(cs.req.URL.Path, check.Equals, "/v2/connections")
	c.Check(cs.req.URL.RawQuery, check.Equals, "select=all")
	c.Check(conns, check.DeepEquals, client.Connections{
		Established: []client.Connection{
			{
				Plug:      client.PlugRef{Snap: "canonical-pi2", Name: "pin-13"},
				Slot:      client.SlotRef{Snap: "keyboard-lights", Name: "capslock-led"},
				Interface: "bool-file",
				Gadget:    true,
			},
		},
		Undesired: []client.Connection{
			{
				Plug:      client.PlugRef{Snap: "canonical-pi2", Name: "pin-14"},
				Slot:      client.SlotRef{Snap: "keyboard-lights", Name: "numlock-led"},
				Interface: "bool-file",
				Gadget:    true,
				Manual:    true,
			},
		},
		Plugs: []client.Plug{
			{
				Snap:      "canonical-pi2",
				Name:      "pin-13",
				Interface: "bool-file",
				Label:     "Pin 13",
				Connections: []client.SlotRef{
					{
						Snap: "keyboard-lights",
						Name: "capslock-led",
					},
				},
			},
			{
				Snap:      "canonical-pi2",
				Name:      "pin-14",
				Interface: "bool-file",
				Label:     "Pin 14",
			},
		},
		Slots: []client.Slot{
			{
				Snap:      "keyboard-lights",
				Name:      "capslock-led",
				Interface: "bool-file",
				Label:     "Capslock indicator LED",
				Connections: []client.PlugRef{
					{
						Snap: "canonical-pi2",
						Name: "pin-13",
					},
				},
			},
			{
				Snap:      "keyboard-lights",
				Name:      "numlock-led",
				Interface: "bool-file",
				Label:     "Numlock LED",
			},
		},
	})
}

func (cs *clientSuite) TestClientConnectionsFilter(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": {
			"established": [],
			"plugs": [],
			"slots": []
		}
	}`

	_ := mylog.Check2(cs.cli.Connections(&client.ConnectionOptions{All: true}))
	c.Assert(err, check.IsNil)
	c.Check(cs.req.URL.Path, check.Equals, "/v2/connections")
	c.Check(cs.req.URL.RawQuery, check.Equals, "select=all")

	_ = mylog.Check2(cs.cli.Connections(&client.ConnectionOptions{Snap: "foo"}))
	c.Assert(err, check.IsNil)
	c.Check(cs.req.URL.Path, check.Equals, "/v2/connections")
	c.Check(cs.req.URL.RawQuery, check.Equals, "snap=foo")

	_ = mylog.Check2(cs.cli.Connections(&client.ConnectionOptions{Interface: "test"}))
	c.Assert(err, check.IsNil)
	c.Check(cs.req.URL.Path, check.Equals, "/v2/connections")
	c.Check(cs.req.URL.RawQuery, check.Equals, "interface=test")

	_ = mylog.Check2(cs.cli.Connections(&client.ConnectionOptions{All: true, Snap: "foo", Interface: "test"}))
	c.Assert(err, check.IsNil)
	query := cs.req.URL.Query()
	c.Check(query, check.DeepEquals, url.Values{
		"select":    []string{"all"},
		"interface": []string{"test"},
		"snap":      []string{"foo"},
	})
}
