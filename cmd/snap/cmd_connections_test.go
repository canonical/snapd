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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	. "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestConnectionsNoneConnectedPlugs(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Connections{
				Plugs: []client.Plug{
					{
						Snap:      "keyboard-lights",
						Name:      "capslock-led",
						Interface: "leds",
					},
				},
			},
		})
	})
	rest, err := Parser(Client()).ParseArgs([]string{"connections"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"no interface connections found\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")

	s.ResetStdStreams()

	// with -a shows unconnected plugs
	rest, err = Parser(Client()).ParseArgs([]string{"connections", "-a"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout = "" +
		"Plug                          Slot  Interface  Notes\n" +
		"keyboard-lights:capslock-led  -     leds       -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestConnectionsNoneConnectedSlots(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Connections{
				Slots: []client.Slot{
					{
						Snap:      "leds-provider",
						Name:      "capslock-led",
						Interface: "leds",
					},
				},
			},
		})
	})
	rest, err := Parser(Client()).ParseArgs([]string{"connections"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"no interface connections found\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")

	s.ResetStdStreams()

	// with -a shows unconnected plugs
	rest, err = Parser(Client()).ParseArgs([]string{"connections", "-a"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout = "" +
		"Plug  Slot                        Interface  Notes\n" +
		"-     leds-provider:capslock-led  leds       -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestConnectionsSomeConnectedSystem(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Connections{
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
							Snap: "system",
							Name: "numlock-led",
						}},
					},
				},
				Slots: []client.Slot{
					{
						Snap:      "system",
						Name:      "capslock-led",
						Interface: "leds",
					},
				},
			},
		})
	})
	rest, err := Parser(Client()).ParseArgs([]string{"connections"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Plug                      Slot                        Interface  Notes\n" +
		"keyboard-lights:capslock  leds-provider:capslock-led  leds       -\n" +
		"keyboard-lights:numlock   :numlock-led                leds       -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")

	s.ResetStdStreams()

	// with -a shows unconnected plugs
	rest, err = Parser(Client()).ParseArgs([]string{"connections", "-a"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout = "" +
		"Plug                      Slot                        Interface  Notes\n" +
		"keyboard-lights:capslock  leds-provider:capslock-led  leds       -\n" +
		"keyboard-lights:numlock   :numlock-led                leds       -\n" +
		"-                         :capslock-led               leds       -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestConnectionsUnsupportedFiltering(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Fatal("should not have been called")
	})
	_, err := Parser(Client()).ParseArgs([]string{"connections", "foo:bar"})
	c.Assert(err, ErrorMatches, "filtering by slot/plug name is not supported")
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestConnectionsFiltering(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Connections{
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
						Snap:      "mouse-buttons",
						Name:      "left-button",
						Interface: "buttons",
						Connections: []client.SlotRef{{
							Snap: "core",
							Name: "mouse-left-button",
						}},
					}, {
						Snap:      "mouse-buttons",
						Name:      "right-button",
						Interface: "buttons",
					},
				},
				Slots: []client.Slot{
					{
						Snap:      "core",
						Name:      "capslock-led",
						Interface: "leds",
					},
				},
			},
		})
	})

	// filter by snap name, show only connected interfaces
	rest, err := Parser(Client()).ParseArgs([]string{"connections", "mouse-buttons"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Plug                       Slot                Interface  Notes\n" +
		"mouse-buttons:left-button  :mouse-left-button  buttons    -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")

	s.ResetStdStreams()

	// filter by snap name, show all interfaces
	rest, err = Parser(Client()).ParseArgs([]string{"connections", "mouse-buttons", "-a"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout = "" +
		"Plug                        Slot                Interface  Notes\n" +
		"mouse-buttons:left-button   :mouse-left-button  buttons    -\n" +
		"mouse-buttons:right-button  -                   buttons    -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")

	s.ResetStdStreams()

	// filter by snap name, snap appearing only on the slot side
	rest, err = Parser(Client()).ParseArgs([]string{"connections", "leds-provider"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout = "" +
		"Plug                      Slot                        Interface  Notes\n" +
		"keyboard-lights:capslock  leds-provider:capslock-led  leds       -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")

	s.ResetStdStreams()

	// filter by system snap alias
	rest, err = Parser(Client()).ParseArgs([]string{"connections", "system"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout = "" +
		"Plug                       Slot                Interface  Notes\n" +
		"keyboard-lights:numlock    :numlock-led        leds       -\n" +
		"mouse-buttons:left-button  :mouse-left-button  buttons    -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")

	s.ResetStdStreams()

	// filter by system snap unaliased name
	rest, err = Parser(Client()).ParseArgs([]string{"connections", "core"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")

	s.ResetStdStreams()

	// filter by system snap alias, show all
	rest, err = Parser(Client()).ParseArgs([]string{"connections", "system", "-a"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout = "" +
		"Plug                       Slot                Interface  Notes\n" +
		"keyboard-lights:numlock    :numlock-led        leds       -\n" +
		"mouse-buttons:left-button  :mouse-left-button  buttons    -\n" +
		"-                          :capslock-led       leds       -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")

	s.ResetStdStreams()
}
