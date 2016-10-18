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

package main_test

import (
	"io/ioutil"
	"net/http"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	. "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestInterfacesHelp(c *C) {
	msg := `Usage:
  snap.test [OPTIONS] interfaces [interfaces-OPTIONS] [<snap>:<slot or plug>]

The interfaces command lists interfaces available in the system.

By default all slots and plugs, used and offered by all snaps, are displayed.

$ snap interfaces <snap>:<slot or plug>

Lists only the specified slot or plug.

$ snap interfaces <snap>

Lists the slots offered and plugs used by the specified snap.

$ snap interfaces -i=<interface> [<snap>]

Filters the complete output so only plugs and/or slots matching the provided
details are listed.

Application Options:
      --version                    Print the version and exit

Help Options:
  -h, --help                       Show this help message

[interfaces command options]
      -i=                          Constrain listing to specific interfaces

[interfaces command arguments]
  <snap>:<slot or plug>:           Constrain listing to a specific snap or snap:name
`
	rest, err := Parser().ParseArgs([]string{"interfaces", "--help"})
	c.Assert(err.Error(), Equals, msg)
	c.Assert(rest, DeepEquals, []string{})
}

func (s *SnapSuite) TestInterfacesZeroSlotsOnePlug(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Interfaces{
				Plugs: []client.Plug{
					{
						Snap: "keyboard-lights",
						Name: "capslock-led",
					},
				},
			},
		})
	})
	rest, err := Parser().ParseArgs([]string{"interfaces"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot  Plug\n" +
		"-     keyboard-lights:capslock-led\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestInterfacesZeroPlugsOneSlot(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Interfaces{
				Slots: []client.Slot{
					{
						Snap:      "canonical-pi2",
						Name:      "pin-13",
						Interface: "bool-file",
						Label:     "Pin 13",
					},
				},
			},
		})
	})
	rest, err := Parser().ParseArgs([]string{"interfaces"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot                  Plug\n" +
		"canonical-pi2:pin-13  -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestInterfacesOneSlotOnePlug(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Interfaces{
				Slots: []client.Slot{
					{
						Snap:      "canonical-pi2",
						Name:      "pin-13",
						Interface: "bool-file",
						Label:     "Pin 13",
						Connections: []client.PlugRef{
							{
								Snap: "keyboard-lights",
								Name: "capslock-led",
							},
						},
					},
				},
				Plugs: []client.Plug{
					{
						Snap:      "keyboard-lights",
						Name:      "capslock-led",
						Interface: "bool-file",
						Label:     "Capslock indicator LED",
						Connections: []client.SlotRef{
							{
								Snap: "canonical-pi2",
								Name: "pin-13",
							},
						},
					},
				},
			},
		})
	})
	rest, err := Parser().ParseArgs([]string{"interfaces"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot                  Plug\n" +
		"canonical-pi2:pin-13  keyboard-lights:capslock-led\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")

	s.SetUpTest(c)
	// should be the same
	rest, err = Parser().ParseArgs([]string{"interfaces", "canonical-pi2"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")

	s.SetUpTest(c)
	// and the same again
	rest, err = Parser().ParseArgs([]string{"interfaces", "keyboard-lights"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestInterfacesTwoPlugs(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Interfaces{
				Slots: []client.Slot{
					{
						Snap:      "canonical-pi2",
						Name:      "pin-13",
						Interface: "bool-file",
						Label:     "Pin 13",
						Connections: []client.PlugRef{
							{
								Snap: "keyboard-lights",
								Name: "capslock-led",
							},
							{
								Snap: "keyboard-lights",
								Name: "scrollock-led",
							},
						},
					},
				},
			},
		})
	})
	rest, err := Parser().ParseArgs([]string{"interfaces"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot                  Plug\n" +
		"canonical-pi2:pin-13  keyboard-lights:capslock-led,keyboard-lights:scrollock-led\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestInterfacesPlugsWithCommonName(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Interfaces{
				Slots: []client.Slot{
					{
						Snap:      "canonical-pi2",
						Name:      "network-listening",
						Interface: "network-listening",
						Label:     "Ability to be a network service",
						Connections: []client.PlugRef{
							{
								Snap: "paste-daemon",
								Name: "network-listening",
							},
							{
								Snap: "time-daemon",
								Name: "network-listening",
							},
						},
					},
				},
				Plugs: []client.Plug{
					{
						Snap:      "paste-daemon",
						Name:      "network-listening",
						Interface: "network-listening",
						Label:     "Ability to be a network service",
						Connections: []client.SlotRef{
							{
								Snap: "canonical-pi2",
								Name: "network-listening",
							},
						},
					},
					{
						Snap:      "time-daemon",
						Name:      "network-listening",
						Interface: "network-listening",
						Label:     "Ability to be a network service",
						Connections: []client.SlotRef{
							{
								Snap: "canonical-pi2",
								Name: "network-listening",
							},
						},
					},
				},
			},
		})
	})
	rest, err := Parser().ParseArgs([]string{"interfaces"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot                             Plug\n" +
		"canonical-pi2:network-listening  paste-daemon,time-daemon\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestInterfacesOsSnapSlots(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Interfaces{
				Slots: []client.Slot{
					{
						Snap:      "ubuntu-core",
						Name:      "network-listening",
						Interface: "network-listening",
						Label:     "Ability to be a network service",
						Connections: []client.PlugRef{
							{
								Snap: "paste-daemon",
								Name: "network-listening",
							},
							{
								Snap: "time-daemon",
								Name: "network-listening",
							},
						},
					},
				},
				Plugs: []client.Plug{
					{
						Snap:      "paste-daemon",
						Name:      "network-listening",
						Interface: "network-listening",
						Label:     "Ability to be a network service",
						Connections: []client.SlotRef{
							{
								Snap: "ubuntu-core",
								Name: "network-listening",
							},
						},
					},
					{
						Snap:      "time-daemon",
						Name:      "network-listening",
						Interface: "network-listening",
						Label:     "Ability to be a network service",
						Connections: []client.SlotRef{
							{
								Snap: "ubuntu-core",
								Name: "network-listening",
							},
						},
					},
				},
			},
		})
	})
	rest, err := Parser().ParseArgs([]string{"interfaces"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot                Plug\n" +
		":network-listening  paste-daemon,time-daemon\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestInterfacesTwoSlotsAndFiltering(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Interfaces{
				Slots: []client.Slot{
					{
						Snap:      "canonical-pi2",
						Name:      "debug-console",
						Interface: "serial-port",
						Label:     "Serial port on the expansion header",
						Connections: []client.PlugRef{
							{
								Snap: "ubuntu-core",
								Name: "debug-console",
							},
						},
					},
					{
						Snap:      "canonical-pi2",
						Name:      "pin-13",
						Interface: "bool-file",
						Label:     "Pin 13",
						Connections: []client.PlugRef{
							{
								Snap: "keyboard-lights",
								Name: "capslock-led",
							},
						},
					},
				},
			},
		})
	})
	rest, err := Parser().ParseArgs([]string{"interfaces", "-i=serial-port"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot                         Plug\n" +
		"canonical-pi2:debug-console  ubuntu-core\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestInterfacesOfSpecificSnap(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Interfaces{
				Slots: []client.Slot{
					{
						Snap:      "cheese",
						Name:      "photo-trigger",
						Interface: "bool-file",
						Label:     "Photo trigger",
					},
					{
						Snap:      "wake-up-alarm",
						Name:      "toggle",
						Interface: "bool-file",
						Label:     "Alarm toggle",
					},
					{
						Snap:      "wake-up-alarm",
						Name:      "snooze",
						Interface: "bool-file",
						Label:     "Alarm snooze",
					},
				},
			},
		})
	})
	rest, err := Parser().ParseArgs([]string{"interfaces", "wake-up-alarm"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot                  Plug\n" +
		"wake-up-alarm:toggle  -\n" +
		"wake-up-alarm:snooze  -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestInterfacesOfSpecificSnapAndSlot(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Interfaces{
				Slots: []client.Slot{
					{
						Snap:      "cheese",
						Name:      "photo-trigger",
						Interface: "bool-file",
						Label:     "Photo trigger",
					},
					{
						Snap:      "wake-up-alarm",
						Name:      "toggle",
						Interface: "bool-file",
						Label:     "Alarm toggle",
					},
					{
						Snap:      "wake-up-alarm",
						Name:      "snooze",
						Interface: "bool-file",
						Label:     "Alarm snooze",
					},
				},
			},
		})
	})
	rest, err := Parser().ParseArgs([]string{"interfaces", "wake-up-alarm:snooze"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot                  Plug\n" +
		"wake-up-alarm:snooze  -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestInterfacesNothingAtAll(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type":   "sync",
			"result": client.Interfaces{},
		})
	})
	rest, err := Parser().ParseArgs([]string{"interfaces"})
	c.Assert(err, ErrorMatches, "no interfaces found")
	// XXX: not sure why this is returned, I guess that's what happens when a
	// command Execute returns an error.
	c.Assert(rest, DeepEquals, []string{"interfaces"})
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}
