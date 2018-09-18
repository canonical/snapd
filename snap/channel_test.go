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

package snap_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/snap"
)

type storeChannelSuite struct{}

var _ = Suite(&storeChannelSuite{})

func (s storeChannelSuite) TestParseChannel(c *C) {
	ch, err := snap.ParseChannel("stable", "")
	c.Assert(err, IsNil)
	c.Check(ch, DeepEquals, snap.Channel{
		Architecture: arch.UbuntuArchitecture(),
		Name:         "stable",
		Track:        "",
		Risk:         "stable",
		Branch:       "",
	})

	ch, err = snap.ParseChannel("latest/stable", "")
	c.Assert(err, IsNil)
	c.Check(ch, DeepEquals, snap.Channel{
		Architecture: arch.UbuntuArchitecture(),
		Name:         "stable",
		Track:        "",
		Risk:         "stable",
		Branch:       "",
	})

	ch, err = snap.ParseChannel("1.0/edge", "")
	c.Assert(err, IsNil)
	c.Check(ch, DeepEquals, snap.Channel{
		Architecture: arch.UbuntuArchitecture(),
		Name:         "1.0/edge",
		Track:        "1.0",
		Risk:         "edge",
		Branch:       "",
	})

	ch, err = snap.ParseChannel("1.0", "")
	c.Assert(err, IsNil)
	c.Check(ch, DeepEquals, snap.Channel{
		Architecture: arch.UbuntuArchitecture(),
		Name:         "1.0/stable",
		Track:        "1.0",
		Risk:         "stable",
		Branch:       "",
	})

	ch, err = snap.ParseChannel("1.0/beta/foo", "")
	c.Assert(err, IsNil)
	c.Check(ch, DeepEquals, snap.Channel{
		Architecture: arch.UbuntuArchitecture(),
		Name:         "1.0/beta/foo",
		Track:        "1.0",
		Risk:         "beta",
		Branch:       "foo",
	})

	ch, err = snap.ParseChannel("candidate/foo", "")
	c.Assert(err, IsNil)
	c.Check(ch, DeepEquals, snap.Channel{
		Architecture: arch.UbuntuArchitecture(),
		Name:         "candidate/foo",
		Track:        "",
		Risk:         "candidate",
		Branch:       "foo",
	})

	ch, err = snap.ParseChannel("candidate/foo", "other-arch")
	c.Assert(err, IsNil)
	c.Check(ch, DeepEquals, snap.Channel{
		Architecture: "other-arch",
		Name:         "candidate/foo",
		Track:        "",
		Risk:         "candidate",
		Branch:       "foo",
	})
}

func (s storeChannelSuite) TestClean(c *C) {
	ch := snap.Channel{
		Architecture: "arm64",
		Track:        "latest",
		Name:         "latest/stable",
		Risk:         "stable",
	}

	cleanedCh := ch.Clean()
	c.Check(cleanedCh, Not(DeepEquals), c)
	c.Check(cleanedCh, DeepEquals, snap.Channel{
		Architecture: "arm64",
		Track:        "",
		Name:         "stable",
		Risk:         "stable",
	})
}

func (s storeChannelSuite) TestParseChannelErrors(c *C) {
	_, err := snap.ParseChannel("", "")
	c.Check(err, ErrorMatches, "channel name cannot be empty")

	_, err = snap.ParseChannel("1.0////", "")
	c.Check(err, ErrorMatches, "channel name has too many components: 1.0////")

	_, err = snap.ParseChannel("1.0/cand", "invalid risk in channel name: 1.0/cand")
	c.Check(err, ErrorMatches, "invalid risk in channel name: 1.0/cand")
}

func (s *storeChannelSuite) TestString(c *C) {
	tests := []struct {
		channel string
		str     string
	}{
		{"stable", "stable"},
		{"latest/stable", "stable"},
		{"1.0/edge", "1.0/edge"},
		{"1.0/beta/foo", "1.0/beta/foo"},
		{"1.0", "1.0/stable"},
		{"candidate/foo", "candidate/foo"},
	}

	for _, t := range tests {
		ch, err := snap.ParseChannel(t.channel, "")
		c.Assert(err, IsNil)

		c.Check(ch.String(), Equals, t.str)
	}
}

func (s *storeChannelSuite) TestFull(c *C) {
	tests := []struct {
		channel string
		str     string
	}{
		{"stable", "latest/stable"},
		{"latest/stable", "latest/stable"},
		{"1.0/edge", "1.0/edge"},
		{"1.0/beta/foo", "1.0/beta/foo"},
		{"1.0", "1.0/stable"},
		{"candidate/foo", "latest/candidate/foo"},
	}

	for _, t := range tests {
		ch, err := snap.ParseChannel(t.channel, "")
		c.Assert(err, IsNil)

		c.Check(ch.Full(), Equals, t.str)
	}
}

func (s *storeChannelSuite) TestMatch(c *C) {
	tests := []struct {
		req      string
		c1       string
		sameArch bool
		res      string
	}{
		{"stable", "stable", true, "architecture:track:risk"},
		{"stable", "beta", true, "architecture:track"},
		{"beta", "stable", true, "architecture:track:risk"},
		{"stable", "edge", false, "track"},
		{"edge", "stable", false, "track:risk"},
		{"1.0/stable", "1.0/edge", true, "architecture:track"},
		{"1.0/edge", "stable", true, "architecture:risk"},
		{"1.0/edge", "stable", false, "risk"},
		{"1.0/stable", "stable", false, "risk"},
		{"1.0/stable", "beta", false, ""},
		{"1.0/stable", "2.0/beta", false, ""},
		{"2.0/stable", "2.0/beta", false, "track"},
		{"1.0/stable", "2.0/beta", true, "architecture"},
	}

	for _, t := range tests {
		reqArch := "amd64"
		c1Arch := "amd64"
		if !t.sameArch {
			c1Arch = "arm64"
		}
		req, err := snap.ParseChannel(t.req, reqArch)
		c.Assert(err, IsNil)
		c1, err := snap.ParseChannel(t.c1, c1Arch)
		c.Assert(err, IsNil)

		c.Check(req.Match(&c1).String(), Equals, t.res)
	}
}
