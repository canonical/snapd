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

package channel_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/snap/channel"
)

func Test(t *testing.T) { TestingT(t) }

type storeChannelSuite struct{}

var _ = Suite(&storeChannelSuite{})

func (s storeChannelSuite) TestParse(c *C) {
	ch, err := channel.Parse("stable", "")
	c.Assert(err, IsNil)
	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.UbuntuArchitecture(),
		Name:         "stable",
		Track:        "",
		Risk:         "stable",
		Branch:       "",
	})

	ch, err = channel.Parse("latest/stable", "")
	c.Assert(err, IsNil)
	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.UbuntuArchitecture(),
		Name:         "stable",
		Track:        "",
		Risk:         "stable",
		Branch:       "",
	})

	ch, err = channel.Parse("1.0/edge", "")
	c.Assert(err, IsNil)
	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.UbuntuArchitecture(),
		Name:         "1.0/edge",
		Track:        "1.0",
		Risk:         "edge",
		Branch:       "",
	})

	ch, err = channel.Parse("1.0", "")
	c.Assert(err, IsNil)
	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.UbuntuArchitecture(),
		Name:         "1.0/stable",
		Track:        "1.0",
		Risk:         "stable",
		Branch:       "",
	})

	ch, err = channel.Parse("1.0/beta/foo", "")
	c.Assert(err, IsNil)
	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.UbuntuArchitecture(),
		Name:         "1.0/beta/foo",
		Track:        "1.0",
		Risk:         "beta",
		Branch:       "foo",
	})

	ch, err = channel.Parse("candidate/foo", "")
	c.Assert(err, IsNil)
	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.UbuntuArchitecture(),
		Name:         "candidate/foo",
		Track:        "",
		Risk:         "candidate",
		Branch:       "foo",
	})

	ch, err = channel.Parse("candidate/foo", "other-arch")
	c.Assert(err, IsNil)
	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: "other-arch",
		Name:         "candidate/foo",
		Track:        "",
		Risk:         "candidate",
		Branch:       "foo",
	})
}

func mustParse(c *C, channelStr string) channel.Channel {
	ch, err := channel.Parse(channelStr, "")
	c.Assert(err, IsNil)
	return ch
}

func (s storeChannelSuite) TestParseVerbatim(c *C) {
	ch, err := channel.ParseVerbatim("sometrack", "")
	c.Assert(err, IsNil)
	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.UbuntuArchitecture(),
		Track:        "sometrack",
	})
	c.Check(mustParse(c, "sometrack"), DeepEquals, ch.Clean())

	ch, err = channel.ParseVerbatim("latest", "")
	c.Assert(err, IsNil)
	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.UbuntuArchitecture(),
		Track:        "latest",
	})
	c.Check(mustParse(c, "latest"), DeepEquals, ch.Clean())

	ch, err = channel.ParseVerbatim("latest/stable", "")
	c.Assert(err, IsNil)
	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.UbuntuArchitecture(),
		Track:        "latest",
		Risk:         "stable",
	})
	c.Check(mustParse(c, "latest/stable"), DeepEquals, ch.Clean())

	ch, err = channel.ParseVerbatim("latest/stable/foo", "")
	c.Assert(err, IsNil)
	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.UbuntuArchitecture(),
		Track:        "latest",
		Risk:         "stable",
		Branch:       "foo",
	})
	c.Check(mustParse(c, "latest/stable/foo"), DeepEquals, ch.Clean())
}

func (s storeChannelSuite) TestClean(c *C) {
	ch := channel.Channel{
		Architecture: "arm64",
		Track:        "latest",
		Name:         "latest/stable",
		Risk:         "stable",
	}

	cleanedCh := ch.Clean()
	c.Check(cleanedCh, Not(DeepEquals), c)
	c.Check(cleanedCh, DeepEquals, channel.Channel{
		Architecture: "arm64",
		Track:        "",
		Name:         "stable",
		Risk:         "stable",
	})
}

func (s storeChannelSuite) TestParseErrors(c *C) {
	for _, tc := range []struct {
		channel string
		err     string
	}{
		{"", "channel name cannot be empty"},
		{"1.0////", "channel name has too many components: 1.0////"},
		{"1.0/cand", "invalid risk in channel name: 1.0/cand"},
		{"fix//hotfix", "invalid risk in channel name: fix//hotfix"},
		{"/stable/", "invalid track in channel name: /stable/"},
		{"//stable", "invalid risk in channel name: //stable"},
		{"stable/", "invalid branch in channel name: stable/"},
		{"/stable", "invalid track in channel name: /stable"},
	} {
		_, err := channel.Parse(tc.channel, "")
		c.Check(err, ErrorMatches, tc.err)
		_, err = channel.ParseVerbatim(tc.channel, "")
		c.Check(err, ErrorMatches, tc.err)
	}
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
		ch, err := channel.Parse(t.channel, "")
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
		ch, err := channel.Parse(t.channel, "")
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
		req, err := channel.Parse(t.req, reqArch)
		c.Assert(err, IsNil)
		c1, err := channel.Parse(t.c1, c1Arch)
		c.Assert(err, IsNil)

		c.Check(req.Match(&c1).String(), Equals, t.res)
	}
}
