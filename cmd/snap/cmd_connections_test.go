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
	"fmt"
	"io"
	"net/http"
	"net/url"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
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
		body := mylog.Check2(io.ReadAll(r.Body))
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type":   "sync",
			"result": result,
		})
	})
	_ := mylog.Check2(Parser(Client()).ParseArgs([]string{"connections"}))
	c.Check(err, IsNil)
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")

	s.ResetStdStreams()

	query = url.Values{
		"select": []string{"all"},
	}
	_ = mylog.Check2(Parser(Client()).ParseArgs([]string{"connections", "--all"}))
	c.Check(err, IsNil)
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestConnectionsNotInstalled(c *C) {
	query := url.Values{
		"snap":   []string{"foo"},
		"select": []string{"all"},
	}
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		c.Check(r.URL.Query(), DeepEquals, query)
		body := mylog.Check2(io.ReadAll(r.Body))
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "not found", "value": "foo", "kind": "snap-not-found"}, "status-code": 404}`)
	})
	_ := mylog.Check2(Parser(Client()).ParseArgs([]string{"connections", "foo"}))
	c.Check(err, ErrorMatches, `not found`)
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
		body := mylog.Check2(io.ReadAll(r.Body))
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type":   "sync",
			"result": result,
		})
	})

	rest := mylog.Check2(Parser(Client()).ParseArgs([]string{"connections", "--all"}))

	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Interface  Plug                          Slot  Notes\n" +
		"leds       keyboard-lights:capslock-led  -     -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")

	s.ResetStdStreams()

	query = url.Values{
		"select": []string{"all"},
		"snap":   []string{"keyboard-lights"},
	}

	rest = mylog.Check2(Parser(Client()).ParseArgs([]string{"connections", "keyboard-lights"}))

	c.Assert(rest, DeepEquals, []string{})
	expectedStdout = "" +
		"Interface  Plug                          Slot  Notes\n" +
		"leds       keyboard-lights:capslock-led  -     -\n"
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
		body := mylog.Check2(io.ReadAll(r.Body))
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type":   "sync",
			"result": result,
		})
	})
	_ := mylog.Check2(Parser(Client()).ParseArgs([]string{"connections"}))
	c.Check(err, IsNil)
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
	rest := mylog.Check2(Parser(Client()).ParseArgs([]string{"connections", "--all"}))

	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Interface  Plug  Slot                        Notes\n" +
		"leds       -     leds-provider:capslock-led  -\n"
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
				Plug:      client.PlugRef{Snap: "keyboard-lights", Name: "scrollock"},
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
		body := mylog.Check2(io.ReadAll(r.Body))
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type":   "sync",
			"result": result,
		})
	})
	rest := mylog.Check2(Parser(Client()).ParseArgs([]string{"connections"}))

	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Interface  Plug                       Slot                        Notes\n" +
		"leds       keyboard-lights:capslock   leds-provider:capslock-led  gadget\n" +
		"leds       keyboard-lights:numlock    :numlock-led                manual\n" +
		"leds       keyboard-lights:scrollock  :scrollock-led              -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestConnectionsSomeDisconnected(c *C) {
	result := client.Connections{
		Established: []client.Connection{
			{
				Plug:      client.PlugRef{Snap: "keyboard-lights", Name: "scrollock"},
				Slot:      client.SlotRef{Snap: "core", Name: "scrollock-led"},
				Interface: "leds",
			}, {
				Plug:      client.PlugRef{Snap: "keyboard-lights", Name: "capslock"},
				Slot:      client.SlotRef{Snap: "leds-provider", Name: "capslock-led"},
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
		body := mylog.Check2(io.ReadAll(r.Body))
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type":   "sync",
			"result": result,
		})
	})

	rest := mylog.Check2(Parser(Client()).ParseArgs([]string{"connections", "--all"}))

	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Interface  Plug                       Slot                        Notes\n" +
		"leds       -                          leds-provider:numlock-led   -\n" +
		"leds       keyboard-lights:capslock   leds-provider:capslock-led  -\n" +
		"leds       keyboard-lights:numlock    -                           -\n" +
		"leds       keyboard-lights:scrollock  :scrollock-led              -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestConnectionsOnlyDisconnected(c *C) {
	result := client.Connections{
		Undesired: []client.Connection{
			{
				Plug:      client.PlugRef{Snap: "keyboard-lights", Name: "numlock"},
				Slot:      client.SlotRef{Snap: "leds-provider", Name: "numlock-led"},
				Interface: "leds",
				Manual:    true,
			},
		},
		Slots: []client.Slot{
			{
				Snap:      "leds-provider",
				Name:      "capslock-led",
				Interface: "leds",
			}, {
				Snap:      "leds-provider",
				Name:      "numlock-led",
				Interface: "leds",
			},
		},
	}
	query := url.Values{
		"snap":   []string{"leds-provider"},
		"select": []string{"all"},
	}
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/connections")
		c.Check(r.URL.Query(), DeepEquals, query)
		body := mylog.Check2(io.ReadAll(r.Body))
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type":   "sync",
			"result": result,
		})
	})

	rest := mylog.Check2(Parser(Client()).ParseArgs([]string{"connections", "leds-provider"}))

	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Interface  Plug  Slot                        Notes\n" +
		"leds       -     leds-provider:capslock-led  -\n" +
		"leds       -     leds-provider:numlock-led   -\n"
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
		body := mylog.Check2(io.ReadAll(r.Body))
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
	rest := mylog.Check2(Parser(Client()).ParseArgs([]string{"connections", "mouse-buttons"}))

	c.Assert(rest, DeepEquals, []string{})

	rest = mylog.Check2(Parser(Client()).ParseArgs([]string{"connections", "mouse-buttons", "--all"}))
	c.Assert(err, ErrorMatches, "cannot use --all with snap name")
	c.Assert(rest, DeepEquals, []string{"--all"})
}

func (s *SnapSuite) TestConnectionsSorting(c *C) {
	result := client.Connections{
		Established: []client.Connection{
			{
				Plug:      client.PlugRef{Snap: "foo", Name: "plug"},
				Slot:      client.SlotRef{Snap: "a-content-provider", Name: "data"},
				Interface: "content",
			}, {
				Plug:      client.PlugRef{Snap: "foo", Name: "plug"},
				Slot:      client.SlotRef{Snap: "b-content-provider", Name: "data"},
				Interface: "content",
			}, {
				Plug:      client.PlugRef{Snap: "foo", Name: "desktop-plug"},
				Slot:      client.SlotRef{Snap: "core", Name: "desktop"},
				Interface: "desktop",
			}, {
				Plug:      client.PlugRef{Snap: "foo", Name: "x11-plug"},
				Slot:      client.SlotRef{Snap: "core", Name: "x11"},
				Interface: "x11",
			}, {
				Plug:      client.PlugRef{Snap: "foo", Name: "a-x11-plug"},
				Slot:      client.SlotRef{Snap: "core", Name: "x11"},
				Interface: "x11",
			}, {
				Plug:      client.PlugRef{Snap: "a-foo", Name: "plug"},
				Slot:      client.SlotRef{Snap: "a-content-provider", Name: "data"},
				Interface: "content",
			}, {
				Plug:      client.PlugRef{Snap: "keyboard-app", Name: "x11"},
				Slot:      client.SlotRef{Snap: "core", Name: "x11"},
				Interface: "x11",
				Manual:    true,
			},
		},
		Undesired: []client.Connection{
			{
				Plug:      client.PlugRef{Snap: "foo", Name: "plug"},
				Slot:      client.SlotRef{Snap: "c-content-provider", Name: "data"},
				Interface: "content",
				Manual:    true,
			},
		},
		Plugs: []client.Plug{
			{
				Snap:      "foo",
				Name:      "plug",
				Interface: "content",
				Connections: []client.SlotRef{{
					Snap: "a-content-provider",
					Name: "data",
				}, {
					Snap: "b-content-provider",
					Name: "data",
				}},
			}, {
				Snap:      "foo",
				Name:      "desktop-plug",
				Interface: "desktop",
				Connections: []client.SlotRef{{
					Snap: "core",
					Name: "desktop",
				}},
			}, {
				Snap:      "foo",
				Name:      "x11-plug",
				Interface: "x11",
				Connections: []client.SlotRef{{
					Snap: "core",
					Name: "x11",
				}},
			}, {
				Snap:      "foo",
				Name:      "a-x11-plug",
				Interface: "x11",
				Connections: []client.SlotRef{{
					Snap: "core",
					Name: "x11",
				}},
			}, {
				Snap:      "a-foo",
				Name:      "plug",
				Interface: "content",
				Connections: []client.SlotRef{{
					Snap: "a-content-provider",
					Name: "data",
				}},
			}, {
				Snap:      "keyboard-app",
				Name:      "x11",
				Interface: "x11",
				Connections: []client.SlotRef{{
					Snap: "core",
					Name: "x11",
				}},
			}, {
				Snap:      "keyboard-lights",
				Name:      "numlock",
				Interface: "leds",
			},
		},
		Slots: []client.Slot{
			{
				Snap:      "c-content-provider",
				Name:      "data",
				Interface: "content",
			}, {
				Snap:      "a-content-provider",
				Name:      "data",
				Interface: "content",
				Connections: []client.PlugRef{{
					Snap: "foo",
					Name: "plug",
				}, {
					Snap: "a-foo",
					Name: "plug",
				}},
			}, {
				Snap:      "b-content-provider",
				Name:      "data",
				Interface: "content",
				Connections: []client.PlugRef{{
					Snap: "foo",
					Name: "plug",
				}},
			}, {
				Snap:      "core",
				Name:      "x11",
				Interface: "x11",
				Connections: []client.PlugRef{{
					Snap: "foo",
					Name: "a-x11-plug",
				}, {
					Snap: "foo",
					Name: "x11-plug",
				}, {
					Snap: "keyboard-app",
					Name: "x11",
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
		body := mylog.Check2(io.ReadAll(r.Body))
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type":   "sync",
			"result": result,
		})
	})

	rest := mylog.Check2(Parser(Client()).ParseArgs([]string{"connections", "--all"}))

	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Interface  Plug                     Slot                       Notes\n" +
		"content    -                        c-content-provider:data    -\n" +
		"content    a-foo:plug               a-content-provider:data    -\n" +
		"content    foo:plug                 a-content-provider:data    -\n" +
		"content    foo:plug                 b-content-provider:data    -\n" +
		"desktop    foo:desktop-plug         :desktop                   -\n" +
		"leds       -                        leds-provider:numlock-led  -\n" +
		"leds       keyboard-lights:numlock  -                          -\n" +
		"x11        foo:a-x11-plug           :x11                       -\n" +
		"x11        foo:x11-plug             :x11                       -\n" +
		"x11        keyboard-app:x11         :x11                       manual\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestConnectionsDefiningAttribute(c *C) {
	result := client.Connections{
		Established: []client.Connection{
			{
				Plug:      client.PlugRef{Snap: "foo", Name: "a-plug"},
				Slot:      client.SlotRef{Snap: "a-content-provider", Name: "data"},
				Interface: "content",
				PlugAttrs: map[string]interface{}{
					"content": "plug-some-data",
					"target":  "$SNAP/foo",
				},
				SlotAttrs: map[string]interface{}{
					"content": "slot-some-data",
					"source": map[string]interface{}{
						"read": []string{"$SNAP/bar"},
					},
				},
			}, {
				Plug:      client.PlugRef{Snap: "foo", Name: "b-plug"},
				Slot:      client.SlotRef{Snap: "b-content-provider", Name: "data"},
				Interface: "content",
				PlugAttrs: map[string]interface{}{
					// no content attribute for plug, falls back to slot
					"target": "$SNAP/foo",
				},
				SlotAttrs: map[string]interface{}{
					"content": "slot-some-data",
					"source": map[string]interface{}{
						"read": []string{"$SNAP/bar"},
					},
				},
			}, {
				Plug:      client.PlugRef{Snap: "foo", Name: "c-plug"},
				Slot:      client.SlotRef{Snap: "c-content-provider", Name: "data"},
				Interface: "content",
				PlugAttrs: map[string]interface{}{
					// no content attribute for plug
					"target": "$SNAP/foo",
				},
				SlotAttrs: map[string]interface{}{
					// no content attribute for slot either
					"source": map[string]interface{}{
						"read": []string{"$SNAP/bar"},
					},
				},
			}, {
				Plug:      client.PlugRef{Snap: "foo", Name: "d-plug"},
				Slot:      client.SlotRef{Snap: "d-content-provider", Name: "data"},
				Interface: "content",
				// no attributes at all
			}, {
				Plug: client.PlugRef{Snap: "foo", Name: "desktop-plug"},
				Slot: client.SlotRef{Snap: "core", Name: "desktop"},
				// desktop interface does not have any defining attributes
				Interface: "desktop",
				PlugAttrs: map[string]interface{}{
					"this-is-ignored": "foo",
				},
				SlotAttrs: map[string]interface{}{
					"this-is-ignored-too": "foo",
				},
			},
		},
		Plugs: []client.Plug{
			{
				Snap:      "foo",
				Name:      "a-plug",
				Interface: "content",
				Connections: []client.SlotRef{{
					Snap: "a-content-provider",
					Name: "data",
				}},
				Attrs: map[string]interface{}{
					"content": "plug-some-data",
					"target":  "$SNAP/foo",
				},
			}, {
				Snap:      "foo",
				Name:      "b-plug",
				Interface: "content",
				Connections: []client.SlotRef{{
					Snap: "b-content-provider",
					Name: "data",
				}},
				Attrs: map[string]interface{}{
					// no content attribute for plug, falls back to slot
					"target": "$SNAP/foo",
				},
			}, {
				Snap:      "foo",
				Name:      "c-plug",
				Interface: "content",
				Connections: []client.SlotRef{{
					Snap: "c-content-provider",
					Name: "data",
				}},
				Attrs: map[string]interface{}{
					// no content attribute for plug
					"target": "$SNAP/foo",
				},
			}, {
				Snap:      "foo",
				Name:      "d-plug",
				Interface: "content",
				Connections: []client.SlotRef{{
					Snap: "d-content-provider",
					Name: "data",
				}},
			}, {
				Snap:      "foo",
				Name:      "desktop-plug",
				Interface: "desktop",
				Connections: []client.SlotRef{{
					Snap: "core",
					Name: "desktop",
				}},
			},
		},
		Slots: []client.Slot{
			{
				Snap:      "a-content-provider",
				Name:      "data",
				Interface: "content",
				Connections: []client.PlugRef{{
					Snap: "foo",
					Name: "a-plug",
				}},
				Attrs: map[string]interface{}{
					"content": "slot-some-data",
					"source": map[string]interface{}{
						"read": []string{"$SNAP/bar"},
					},
				},
			}, {
				Snap:      "b-content-provider",
				Name:      "data",
				Interface: "content",
				Connections: []client.PlugRef{{
					Snap: "foo",
					Name: "a-plug",
				}},
				Attrs: map[string]interface{}{
					"content": "slot-some-data",
					"source": map[string]interface{}{
						"read": []string{"$SNAP/bar"},
					},
				},
			}, {
				Snap:      "c-content-provider",
				Name:      "data",
				Interface: "content",
				Connections: []client.PlugRef{{
					Snap: "foo",
					Name: "a-plug",
				}},
				Attrs: map[string]interface{}{
					"source": map[string]interface{}{
						"read": []string{"$SNAP/bar"},
					},
				},
			}, {
				Snap:      "a-content-provider",
				Name:      "data",
				Interface: "content",
				Connections: []client.PlugRef{{
					Snap: "foo",
					Name: "a-plug",
				}},
			}, {
				Snap:      "core",
				Name:      "desktop",
				Interface: "desktop",
				Connections: []client.PlugRef{{
					Snap: "foo",
					Name: "desktop-plug",
				}},
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
		body := mylog.Check2(io.ReadAll(r.Body))
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type":   "sync",
			"result": result,
		})
	})

	rest := mylog.Check2(Parser(Client()).ParseArgs([]string{"connections", "--all"}))

	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Interface                Plug              Slot                     Notes\n" +
		"content[plug-some-data]  foo:a-plug        a-content-provider:data  -\n" +
		"content[slot-some-data]  foo:b-plug        b-content-provider:data  -\n" +
		"content                  foo:c-plug        c-content-provider:data  -\n" +
		"content                  foo:d-plug        d-content-provider:data  -\n" +
		"desktop                  foo:desktop-plug  :desktop                 -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}
