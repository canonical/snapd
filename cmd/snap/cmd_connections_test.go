// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"io/ioutil"
	"net/http"
	"net/url"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	. "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestConnectionsNoneConnected(c *C) {
	result := client.Connections{}
	query := url.Values{}
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		c.Check(r.URL.Query(), DeepEquals, query)
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type":   "sync",
			"result": result,
		})
	})
	_, err := Parser(Client()).ParseArgs([]string{"connections"})
	c.Check(err, ErrorMatches, "no connections found")
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")

	s.ResetStdStreams()

	query = url.Values{
		"select": []string{"all"},
	}
	_, err = Parser(Client()).ParseArgs([]string{"connections", "--all"})
	c.Check(err, ErrorMatches, "no connections found")
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestConnectionsNoneConnectedPlugs(c *C) {
	query := url.Values{
		"select": []string{"all"},
	}
	result := client.Connections{
		Plugs: []client.Plug{
			{
				Snap:      "keyboard-lights",
				Name:      "capslock-led",
				Interface: "leds",
			},
		},
	}
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		c.Check(r.URL.Query(), DeepEquals, query)
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type":   "sync",
			"result": result,
		})
	})

	rest, err := Parser(Client()).ParseArgs([]string{"connections", "--all"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Plug                          Slot  Interface  Notes\n" +
		"keyboard-lights:capslock-led  -     leds       -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")

	s.ResetStdStreams()

	query = url.Values{
		"select": []string{"all"},
		"snap":   []string{"keyboard-lights"},
	}

	rest, err = Parser(Client()).ParseArgs([]string{"connections", "keyboard-lights"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout = "" +
		"Plug                          Slot  Interface  Notes\n" +
		"keyboard-lights:capslock-led  -     leds       -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestConnectionsNoneConnectedSlots(c *C) {
	result := client.Connections{}
	query := url.Values{}
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		c.Check(r.URL.Query(), DeepEquals, query)
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type":   "sync",
			"result": result,
		})
	})
	rest, err := Parser(Client()).ParseArgs([]string{"connections"})
	c.Check(err, ErrorMatches, "no connections found")
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")

	s.ResetStdStreams()

	query = url.Values{
		"select": []string{"all"},
	}
	result = client.Connections{
		Slots: []client.Slot{
			{
				Snap:      "leds-provider",
				Name:      "capslock-led",
				Interface: "leds",
			},
		},
	}
	rest, err = Parser(Client()).ParseArgs([]string{"connections", "--all"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Plug  Slot                        Interface  Notes\n" +
		"-     leds-provider:capslock-led  leds       -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestConnectionsSomeConnected(c *C) {
	result := client.Connections{
		Established: []client.Connection{
			{
				Plug:      client.PlugRef{Snap: "keyboard-lights", Name: "capslock"},
				Slot:      client.SlotRef{Snap: "leds-provider", Name: "capslock-led"},
				Interface: "leds",
				Gadget:    true,
			}, {
				Plug:      client.PlugRef{Snap: "keyboard-lights", Name: "numlock"},
				Slot:      client.SlotRef{Snap: "core", Name: "numlock-led"},
				Interface: "leds",
				Manual:    true,
			}, {
				Plug:      client.PlugRef{Snap: "keyboard-lights", Name: "scrolllock"},
				Slot:      client.SlotRef{Snap: "core", Name: "scrollock-led"},
				Interface: "leds",
			},
		},
		Plugs: []client.Plug{
			{
				Snap:      "keyboard-lights",
				Name:      "capslock",
				Interface: "leds",
				Connections: []client.SlotRef{{
					Snap: "leds-provider",
					Name: "capslock-led",
				}},
			}, {
				Snap:      "keyboard-lights",
				Name:      "numlock",
				Interface: "leds",
				Connections: []client.SlotRef{{
					Snap: "core",
					Name: "numlock-led",
				}},
			}, {
				Snap:      "keyboard-lights",
				Name:      "scrollock",
				Interface: "leds",
				Connections: []client.SlotRef{{
					Snap: "core",
					Name: "scrollock-led",
				}},
			},
		},
		Slots: []client.Slot{
			{
				Snap:      "core",
				Name:      "numlock-led",
				Interface: "leds",
				Connections: []client.PlugRef{{
					Snap: "keyuboard-lights",
					Name: "numlock",
				}},
			}, {
				Snap:      "core",
				Name:      "scrollock-led",
				Interface: "leds",
				Connections: []client.PlugRef{{
					Snap: "keyuboard-lights",
					Name: "scrollock",
				}},
			}, {
				Snap:      "leds-provider",
				Name:      "capslock-led",
				Interface: "leds",
				Connections: []client.PlugRef{{
					Snap: "keyuboard-lights",
					Name: "capslock",
				}},
			},
		},
	}
	query := url.Values{}
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		c.Check(r.URL.Query(), DeepEquals, query)
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type":   "sync",
			"result": result,
		})
	})
	rest, err := Parser(Client()).ParseArgs([]string{"connections"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Plug                       Slot                        Interface  Notes\n" +
		"keyboard-lights:capslock   leds-provider:capslock-led  leds       gadget\n" +
		"keyboard-lights:numlock    :numlock-led                leds       manual\n" +
		"keyboard-lights:scrollock  :scrollock-led              leds       -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestConnectionsSomeDisconnected(c *C) {
	result := client.Connections{
		Established: []client.Connection{
			{
				Plug:      client.PlugRef{Snap: "keyboard-lights", Name: "scrolllock"},
				Slot:      client.SlotRef{Snap: "core", Name: "scrollock-led"},
				Interface: "leds",
			},
		},
		Undesired: []client.Connection{
			{
				Plug:      client.PlugRef{Snap: "keyboard-lights", Name: "numlock"},
				Slot:      client.SlotRef{Snap: "core", Name: "numlock-led"},
				Interface: "leds",
				Manual:    true,
			},
		},
		Plugs: []client.Plug{
			{
				Snap:      "keyboard-lights",
				Name:      "capslock",
				Interface: "leds",
				Connections: []client.SlotRef{{
					Snap: "leds-provider",
					Name: "capslock-led",
				}},
			}, {
				Snap:      "keyboard-lights",
				Name:      "numlock",
				Interface: "leds",
			}, {
				Snap:      "keyboard-lights",
				Name:      "scrollock",
				Interface: "leds",
				Connections: []client.SlotRef{{
					Snap: "core",
					Name: "scrollock-led",
				}},
			},
		},
		Slots: []client.Slot{
			{
				Snap:      "core",
				Name:      "capslock-led",
				Interface: "leds",
			}, {
				Snap:      "core",
				Name:      "numlock-led",
				Interface: "leds",
			}, {
				Snap:      "core",
				Name:      "scrollock-led",
				Interface: "leds",
				Connections: []client.PlugRef{{
					Snap: "keyuboard-lights",
					Name: "scrollock",
				}},
			}, {
				Snap:      "leds-provider",
				Name:      "capslock-led",
				Interface: "leds",
				Connections: []client.PlugRef{{
					Snap: "keyuboard-lights",
					Name: "capslock",
				}},
			}, {
				Snap:      "leds-provider",
				Name:      "numlock-led",
				Interface: "leds",
			},
		},
	}
	query := url.Values{
		"select": []string{"all"},
	}
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		c.Check(r.URL.Query(), DeepEquals, query)
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type":   "sync",
			"result": result,
		})
	})

	rest, err := Parser(Client()).ParseArgs([]string{"connections", "--all"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Plug                       Slot                        Interface  Notes\n" +
		"keyboard-lights:capslock   leds-provider:capslock-led  leds       -\n" +
		"keyboard-lights:numlock    :numlock-led                leds       disconnected,manual\n" +
		"keyboard-lights:scrollock  :scrollock-led              leds       -\n" +
		"-                          leds-provider:numlock-led   leds       -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")

	s.ResetStdStreams()

	rest, err = Parser(Client()).ParseArgs([]string{"connections", "--disconnected"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout = "" +
		"Plug                     Slot                       Interface  Notes\n" +
		"keyboard-lights:numlock  :numlock-led               leds       disconnected,manual\n" +
		"-                        leds-provider:numlock-led  leds       -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestConnectionsFiltering(c *C) {
	result := client.Connections{}
	query := url.Values{
		"select": []string{"all"},
	}
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		c.Check(r.URL.Query(), DeepEquals, query)
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type":   "sync",
			"result": result,
		})
	})

	query = url.Values{
		"select": []string{"all"},
		"snap":   []string{"mouse-buttons"},
	}
	rest, err := Parser(Client()).ParseArgs([]string{"connections", "mouse-buttons"})
	c.Assert(err, ErrorMatches, "no connections found")
	c.Assert(rest, DeepEquals, []string{"mouse-buttons"})

	result = client.Connections{
		Plugs: []client.Plug{
			{
				Snap:      "mouse-buttons",
				Name:      "left",
				Interface: "buttons",
			},
		},
	}
	query = url.Values{
		"select": []string{"all"},
		"snap":   []string{"mouse-buttons"},
	}
	rest, err = Parser(Client()).ParseArgs([]string{"connections", "mouse-buttons", "--all"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	query = url.Values{
		"select": []string{"all"},
		"snap":   []string{"mouse-buttons"},
	}
	rest, err = Parser(Client()).ParseArgs([]string{"connections", "mouse-buttons", "--disconnected"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
}
