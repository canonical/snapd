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

	"github.com/ubuntu-core/snappy/client"
	. "github.com/ubuntu-core/snappy/cmd/snap"
)

func (s *SnapSuite) TestSkillsHelp(c *C) {
	msg := `Usage:
  snap.test [OPTIONS] skills [skills-OPTIONS] [<snap>:<skill>]

The skills command lists skills available in the system.

By default all skills, used and offered by all snaps, are displayed.

$ snap skills <snap name>:<skill name>

Lists only the specified skill.

$ snap skills <snap name>

Lists the skills offered and used by the specified snap.

$ snap skills --type=<type> [<snap name>]

Lists only skills of the specified type.

Help Options:
  -h, --help                Show this help message

[skills command options]
          --type=           constrain listing to skills of this type
`
	rest, _ := Parser().ParseArgs([]string{"skills", "--help"})
	// TODO: Re-enable this after go-flags is updated.
	msg = msg[:]
	// c.Assert(err.Error(), Equals, msg)
	// NOTE: Updated go-flags returns []string{} here
	c.Assert(rest, DeepEquals, []string{"--help"})
}

func (s *SnapSuite) TestSkillsZeroSlots(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/2.0/skills")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": []client.SkillGrants{
				{
					Skill: client.Skill{
						Snap:  "canonical-pi2",
						Name:  "pin-13",
						Type:  "bool-file",
						Label: "Pin 13",
					},
					GrantedTo: []client.Slot{},
				},
			},
		})
	})
	rest, err := Parser().ParseArgs([]string{"skills"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Skill                Granted To\n" +
		"canonical-pi2:pin-13 \n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestSkillsOneSlot(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/2.0/skills")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": []client.SkillGrants{
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
			},
		})
	})
	rest, err := Parser().ParseArgs([]string{"skills"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Skill                Granted To\n" +
		"canonical-pi2:pin-13 keyboard-lights:capslock-led\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestSkillsTwoSlots(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/2.0/skills")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": []client.SkillGrants{
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
						{
							Snap: "keyboard-lights",
							Name: "scrollock-led",
						},
					},
				},
			},
		})
	})
	rest, err := Parser().ParseArgs([]string{"skills"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Skill                Granted To\n" +
		"canonical-pi2:pin-13 keyboard-lights:capslock-led,keyboard-lights:scrollock-led\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestSkillsSlotsWithCommonName(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/2.0/skills")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": []client.SkillGrants{
				{
					Skill: client.Skill{
						Snap:  "canonical-pi2",
						Name:  "network-listening",
						Type:  "network-listening",
						Label: "Ability to be a network service",
					},
					GrantedTo: []client.Slot{
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
		})
	})
	rest, err := Parser().ParseArgs([]string{"skills"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Skill                           Granted To\n" +
		"canonical-pi2:network-listening paste-daemon,time-daemon\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestSkillsTwoSkillsAndFiltering(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/2.0/skills")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": []client.SkillGrants{
				{
					Skill: client.Skill{
						Snap:  "canonical-pi2",
						Name:  "debug-console",
						Type:  "serial-port",
						Label: "Serial port on the expansion header",
					},
					GrantedTo: []client.Slot{
						{
							Snap: "ubuntu-core",
							Name: "debug-console",
						},
					},
				},
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
			},
		})
	})
	rest, err := Parser().ParseArgs([]string{"skills", "--type=serial-port"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Skill                       Granted To\n" +
		"canonical-pi2:debug-console ubuntu-core\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestSkillsOfSpecificSnap(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/2.0/skills")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": []client.SkillGrants{
				{
					Skill: client.Skill{
						Snap:  "cheese",
						Name:  "photo-trigger",
						Type:  "bool-file",
						Label: "Photo trigger",
					},
				}, {
					Skill: client.Skill{
						Snap:  "wake-up-alarm",
						Name:  "toggle",
						Type:  "bool-file",
						Label: "Alarm toggle",
					},
				}, {
					Skill: client.Skill{
						Snap:  "wake-up-alarm",
						Name:  "snooze",
						Type:  "bool-file",
						Label: "Alarm snooze",
					},
				},
			},
		})
	})
	rest, err := Parser().ParseArgs([]string{"skills", "wake-up-alarm"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Skill                Granted To\n" +
		"wake-up-alarm:toggle \n" +
		"wake-up-alarm:snooze \n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestSkillsOfSpecificSnapAndSkill(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/2.0/skills")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": []client.SkillGrants{
				{
					Skill: client.Skill{
						Snap:  "cheese",
						Name:  "photo-trigger",
						Type:  "bool-file",
						Label: "Photo trigger",
					},
				}, {
					Skill: client.Skill{
						Snap:  "wake-up-alarm",
						Name:  "toggle",
						Type:  "bool-file",
						Label: "Alarm toggle",
					},
				}, {
					Skill: client.Skill{
						Snap:  "wake-up-alarm",
						Name:  "snooze",
						Type:  "bool-file",
						Label: "Alarm snooze",
					},
				},
			},
		})
	})
	rest, err := Parser().ParseArgs([]string{"skills", "wake-up-alarm:snooze"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Skill                Granted To\n" +
		"wake-up-alarm:snooze \n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}
