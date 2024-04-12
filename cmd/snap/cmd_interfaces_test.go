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
	"io"
	"net/http"
	"os"

	"github.com/jessevdk/go-flags"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	. "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/testutil"
)

func (s *SnapSuite) TestInterfacesZeroSlotsOnePlug(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Connections{
				Plugs: []client.Plug{
					{
						Snap: "keyboard-lights",
						Name: "capslock-led",
					},
				},
			},
		})
	})
	rest, err := Parser(Client()).ParseArgs([]string{"interfaces"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot  Plug\n" +
		"-     keyboard-lights:capslock-led\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), testutil.EqualsWrapped, InterfacesDeprecationNotice)
}

func (s *SnapSuite) TestInterfacesZeroPlugsOneSlot(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Connections{
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
	rest, err := Parser(Client()).ParseArgs([]string{"interfaces"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot                  Plug\n" +
		"canonical-pi2:pin-13  -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), testutil.EqualsWrapped, InterfacesDeprecationNotice)
}

func (s *SnapSuite) TestInterfacesOneSlotOnePlug(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Connections{
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
	rest, err := Parser(Client()).ParseArgs([]string{"interfaces"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot                  Plug\n" +
		"canonical-pi2:pin-13  keyboard-lights:capslock-led\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), testutil.EqualsWrapped, InterfacesDeprecationNotice)

	s.ResetStdStreams()
	// should be the same
	rest, err = Parser(Client()).ParseArgs([]string{"interfaces", "canonical-pi2"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), testutil.EqualsWrapped, InterfacesDeprecationNotice)
	s.ResetStdStreams()
	// and the same again
	rest, err = Parser(Client()).ParseArgs([]string{"interfaces", "keyboard-lights"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), testutil.EqualsWrapped, InterfacesDeprecationNotice)
}

func (s *SnapSuite) TestInterfacesTwoPlugs(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Connections{
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
	rest, err := Parser(Client()).ParseArgs([]string{"interfaces"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot                  Plug\n" +
		"canonical-pi2:pin-13  keyboard-lights:capslock-led,keyboard-lights:scrollock-led\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), testutil.EqualsWrapped, InterfacesDeprecationNotice)
}

func (s *SnapSuite) TestInterfacesPlugsWithCommonName(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Connections{
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
	rest, err := Parser(Client()).ParseArgs([]string{"interfaces"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot                             Plug\n" +
		"canonical-pi2:network-listening  paste-daemon,time-daemon\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), testutil.EqualsWrapped, InterfacesDeprecationNotice)
}

func (s *SnapSuite) TestInterfacesOsSnapSlots(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Connections{
				Slots: []client.Slot{
					{
						Snap:      "system",
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
								Snap: "system",
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
								Snap: "system",
								Name: "network-listening",
							},
						},
					},
				},
			},
		})
	})
	rest, err := Parser(Client()).ParseArgs([]string{"interfaces"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot                Plug\n" +
		":network-listening  paste-daemon,time-daemon\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), testutil.EqualsWrapped, InterfacesDeprecationNotice)
}

func (s *SnapSuite) TestInterfacesTwoSlotsAndFiltering(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Connections{
				Slots: []client.Slot{
					{
						Snap:      "canonical-pi2",
						Name:      "debug-console",
						Interface: "serial-port",
						Label:     "Serial port on the expansion header",
						Connections: []client.PlugRef{
							{
								Snap: "core",
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
	rest, err := Parser(Client()).ParseArgs([]string{"interfaces", "-i=serial-port"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot                         Plug\n" +
		"canonical-pi2:debug-console  core\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), testutil.EqualsWrapped, InterfacesDeprecationNotice)
}

func (s *SnapSuite) TestInterfacesOfSpecificSnap(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Connections{
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
	rest, err := Parser(Client()).ParseArgs([]string{"interfaces", "wake-up-alarm"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot                  Plug\n" +
		"wake-up-alarm:toggle  -\n" +
		"wake-up-alarm:snooze  -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), testutil.EqualsWrapped, InterfacesDeprecationNotice)
}

func (s *SnapSuite) TestInterfacesOfSystemNicknameSnap(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Connections{
				Slots: []client.Slot{
					{
						Snap:        "system",
						Name:        "core-support",
						Interface:   "some-iface",
						Connections: []client.PlugRef{{Snap: "core", Name: "core-support-plug"}},
					}, {
						Snap:      "foo",
						Name:      "foo-slot",
						Interface: "foo-slot-iface",
					},
				},
				Plugs: []client.Plug{
					{
						Snap:        "core",
						Name:        "core-support-plug",
						Interface:   "some-iface",
						Connections: []client.SlotRef{{Snap: "system", Name: "core-support"}},
					},
				},
			},
		})
	})
	rest, err := Parser(Client()).ParseArgs([]string{"interfaces", "system"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot           Plug\n" +
		":core-support  core:core-support-plug\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), testutil.EqualsWrapped, InterfacesDeprecationNotice)

	s.ResetStdStreams()

	// when called with system nickname we get the same output
	rest, err = Parser(Client()).ParseArgs([]string{"interfaces", "system"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdoutSystem := "" +
		"Slot           Plug\n" +
		":core-support  core:core-support-plug\n"
	c.Assert(s.Stdout(), Equals, expectedStdoutSystem)
	c.Assert(s.Stderr(), testutil.EqualsWrapped, InterfacesDeprecationNotice)
}

func (s *SnapSuite) TestInterfacesOfSpecificSnapAndSlot(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Connections{
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
	rest, err := Parser(Client()).ParseArgs([]string{"interfaces", "wake-up-alarm:snooze"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot                  Plug\n" +
		"wake-up-alarm:snooze  -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), testutil.EqualsWrapped, InterfacesDeprecationNotice)
}

func (s *SnapSuite) TestInterfacesNothingAtAll(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type":   "sync",
			"result": client.Connections{},
		})
	})
	rest, err := Parser(Client()).ParseArgs([]string{"interfaces"})
	c.Assert(err, ErrorMatches, "no interfaces found")
	// XXX: not sure why this is returned, I guess that's what happens when a
	// command Execute returns an error.
	c.Assert(rest, DeepEquals, []string{"interfaces"})
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestInterfacesOfSpecificType(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Connections{
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
	rest, err := Parser(Client()).ParseArgs([]string{"interfaces", "-i", "bool-file"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot                  Plug\n" +
		"cheese:photo-trigger  -\n" +
		"wake-up-alarm:toggle  -\n" +
		"wake-up-alarm:snooze  -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), testutil.EqualsWrapped, InterfacesDeprecationNotice)
}

func (s *SnapSuite) TestInterfacesCompletion(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/connections":
			c.Assert(r.Method, Equals, "GET")
			EncodeResponseBody(c, w, map[string]interface{}{
				"type":   "sync",
				"result": fortestingConnectionList,
			})
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	})
	os.Setenv("GO_FLAGS_COMPLETION", "verbose")
	defer os.Unsetenv("GO_FLAGS_COMPLETION")

	expected := []flags.Completion{}
	parser := Parser(Client())
	parser.CompletionHandler = func(obtained []flags.Completion) {
		c.Check(obtained, DeepEquals, expected)
	}

	expected = []flags.Completion{{Item: "canonical-pi2:"}, {Item: "core:"}, {Item: "keyboard-lights:"}, {Item: "paste-daemon:"}, {Item: "potato:"}, {Item: "wake-up-alarm:"}}
	_, err := parser.ParseArgs([]string{"interfaces", ""})
	c.Assert(err, IsNil)

	expected = []flags.Completion{{Item: "paste-daemon:network-listening", Description: "plug"}}
	_, err = parser.ParseArgs([]string{"interfaces", "pa"})
	c.Assert(err, IsNil)

	expected = []flags.Completion{{Item: "wake-up-alarm:toggle", Description: "slot"}}
	_, err = parser.ParseArgs([]string{"interfaces", "wa"})
	c.Assert(err, IsNil)

	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestInterfacesCoreNicknamedSystem(c *C) {
	s.checkConnectionsSystemCoreRemapping(c, "core", "system")
}

func (s *SnapSuite) TestInterfacesSnapdNicknamedSystem(c *C) {
	s.checkConnectionsSystemCoreRemapping(c, "snapd", "system")
}

func (s *SnapSuite) TestInterfacesSnapdNicknamedCore(c *C) {
	s.checkConnectionsSystemCoreRemapping(c, "snapd", "core")
}

func (s *SnapSuite) TestInterfacesCoreSnap(c *C) {
	s.checkConnectionsSystemCoreRemapping(c, "core", "core")
}

func (s *SnapSuite) checkConnectionsSystemCoreRemapping(c *C, apiSnapName, cliSnapName string) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Connections{
				Slots: []client.Slot{
					{
						Snap: apiSnapName,
						Name: "network",
					},
				},
			},
		})
	})
	rest, err := Parser(Client()).ParseArgs([]string{"interfaces", cliSnapName})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Slot      Plug\n" +
		":network  -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), testutil.EqualsWrapped, InterfacesDeprecationNotice)
}
