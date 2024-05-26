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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/snap/channel"
)

func Test(t *testing.T) { TestingT(t) }

type storeChannelSuite struct{}

var _ = Suite(&storeChannelSuite{})

func (s storeChannelSuite) TestParse(c *C) {
	ch := mylog.Check2(channel.Parse("stable", ""))

	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.DpkgArchitecture(),
		Name:         "stable",
		Track:        "",
		Risk:         "stable",
		Branch:       "",
	})

	ch = mylog.Check2(channel.Parse("latest/stable", ""))

	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.DpkgArchitecture(),
		Name:         "stable",
		Track:        "",
		Risk:         "stable",
		Branch:       "",
	})

	ch = mylog.Check2(channel.Parse("1.0/edge", ""))

	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.DpkgArchitecture(),
		Name:         "1.0/edge",
		Track:        "1.0",
		Risk:         "edge",
		Branch:       "",
	})

	ch = mylog.Check2(channel.Parse("1.0", ""))

	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.DpkgArchitecture(),
		Name:         "1.0/stable",
		Track:        "1.0",
		Risk:         "stable",
		Branch:       "",
	})

	ch = mylog.Check2(channel.Parse("1.0/beta/foo", ""))

	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.DpkgArchitecture(),
		Name:         "1.0/beta/foo",
		Track:        "1.0",
		Risk:         "beta",
		Branch:       "foo",
	})

	ch = mylog.Check2(channel.Parse("candidate/foo", ""))

	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.DpkgArchitecture(),
		Name:         "candidate/foo",
		Track:        "",
		Risk:         "candidate",
		Branch:       "foo",
	})

	ch = mylog.Check2(channel.Parse("candidate/foo", "other-arch"))

	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: "other-arch",
		Name:         "candidate/foo",
		Track:        "",
		Risk:         "candidate",
		Branch:       "foo",
	})
}

func mustParse(c *C, channelStr string) channel.Channel {
	ch := mylog.Check2(channel.Parse(channelStr, ""))

	return ch
}

func (s storeChannelSuite) TestParseVerbatim(c *C) {
	ch := mylog.Check2(channel.ParseVerbatim("sometrack", ""))

	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.DpkgArchitecture(),
		Track:        "sometrack",
	})
	c.Check(ch.VerbatimTrackOnly(), Equals, true)
	c.Check(ch.VerbatimRiskOnly(), Equals, false)
	c.Check(mustParse(c, "sometrack"), DeepEquals, ch.Clean())

	ch = mylog.Check2(channel.ParseVerbatim("latest", ""))

	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.DpkgArchitecture(),
		Track:        "latest",
	})
	c.Check(ch.VerbatimTrackOnly(), Equals, true)
	c.Check(ch.VerbatimRiskOnly(), Equals, false)
	c.Check(mustParse(c, "latest"), DeepEquals, ch.Clean())

	ch = mylog.Check2(channel.ParseVerbatim("edge", ""))

	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.DpkgArchitecture(),
		Risk:         "edge",
	})
	c.Check(ch.VerbatimTrackOnly(), Equals, false)
	c.Check(ch.VerbatimRiskOnly(), Equals, true)
	c.Check(mustParse(c, "edge"), DeepEquals, ch.Clean())

	ch = mylog.Check2(channel.ParseVerbatim("latest/stable", ""))

	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.DpkgArchitecture(),
		Track:        "latest",
		Risk:         "stable",
	})
	c.Check(ch.VerbatimTrackOnly(), Equals, false)
	c.Check(ch.VerbatimRiskOnly(), Equals, false)
	c.Check(mustParse(c, "latest/stable"), DeepEquals, ch.Clean())

	ch = mylog.Check2(channel.ParseVerbatim("latest/stable/foo", ""))

	c.Check(ch, DeepEquals, channel.Channel{
		Architecture: arch.DpkgArchitecture(),
		Track:        "latest",
		Risk:         "stable",
		Branch:       "foo",
	})
	c.Check(ch.VerbatimTrackOnly(), Equals, false)
	c.Check(ch.VerbatimRiskOnly(), Equals, false)
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
		full    string
	}{
		{"", "channel name cannot be empty", ""},
		{"1.0////", "channel name has too many components: 1.0////", "1.0/stable"},
		{"1.0/cand", "invalid risk in channel name: 1.0/cand", ""},
		{"fix//hotfix", "invalid risk in channel name: fix//hotfix", ""},
		{"/stable/", "invalid track in channel name: /stable/", "latest/stable"},
		{"//stable", "invalid risk in channel name: //stable", "latest/stable"},
		{"stable/", "invalid branch in channel name: stable/", "latest/stable"},
		{"/stable", "invalid track in channel name: /stable", "latest/stable"},
	} {
		_ := mylog.Check2(channel.Parse(tc.channel, ""))
		c.Check(err, ErrorMatches, tc.err)
		_ = mylog.Check2(channel.ParseVerbatim(tc.channel, ""))
		c.Check(err, ErrorMatches, tc.err)
		if tc.full != "" {
			// testing Full behavior on the malformed channel
			full := mylog.Check2(channel.Full(tc.channel))
			c.Check(err, IsNil)
			c.Check(full, Equals, tc.full)
		}
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
		ch := mylog.Check2(channel.Parse(t.channel, ""))


		c.Check(ch.String(), Equals, t.str)
	}
}

func (s *storeChannelSuite) TestChannelFull(c *C) {
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
		ch := mylog.Check2(channel.Parse(t.channel, ""))


		c.Check(ch.Full(), Equals, t.str)
	}
}

func (s *storeChannelSuite) TestFuncFull(c *C) {
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
		// store behaviour compat; expect these to fail when we stop accommodating the madness :)
		{"//stable//", "latest/stable"},
		// rather weird corner case
		{"///", ""},
		// empty string is OK
		{"", ""},
	}

	for _, t := range tests {
		can := mylog.Check2(channel.Full(t.channel))

		c.Check(can, Equals, t.str)
	}
}

func (s *storeChannelSuite) TestFuncFullErr(c *C) {
	_ := mylog.Check2(channel.Full("foo/bar/baz/quux"))
	c.Check(err, ErrorMatches, "invalid channel")
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
		req := mylog.Check2(channel.Parse(t.req, reqArch))

		c1 := mylog.Check2(channel.Parse(t.c1, c1Arch))


		c.Check(req.Match(&c1).String(), Equals, t.res)
	}
}

func (s *storeChannelSuite) TestResolve(c *C) {
	tests := []struct {
		channel string
		new     string
		result  string
		expErr  string
	}{
		{"", "", "", ""},
		{"", "edge", "edge", ""},
		{"track/foo", "", "track/foo", ""},
		{"stable", "", "stable", ""},
		{"stable", "edge", "edge", ""},
		{"stable/branch1", "edge/branch2", "edge/branch2", ""},
		{"track", "track", "track", ""},
		{"track", "beta", "track/beta", ""},
		{"track/stable", "beta", "track/beta", ""},
		{"track/stable", "stable/branch", "track/stable/branch", ""},
		{"track/stable", "track/edge/branch", "track/edge/branch", ""},
		{"track/stable", "track/candidate", "track/candidate", ""},
		{"track/stable", "track/stable/branch", "track/stable/branch", ""},
		{"track1/stable", "track2/stable", "track2/stable", ""},
		{"track1/stable", "track2/stable/branch", "track2/stable/branch", ""},
		{"track/foo", "track/stable/branch", "", "invalid risk in channel name: track/foo"},
	}

	for _, t := range tests {
		r := mylog.Check2(channel.Resolve(t.channel, t.new))
		tcomm := Commentf("%#v", t)
		if t.expErr == "" {
			c.Assert(err, IsNil, tcomm)
			c.Check(r, Equals, t.result, tcomm)
		} else {
			c.Assert(err, ErrorMatches, t.expErr, tcomm)
		}
	}
}

func (s *storeChannelSuite) TestResolvePinned(c *C) {
	tests := []struct {
		track  string
		new    string
		result string
		expErr string
	}{
		{"", "", "", ""},
		{"", "anytrack/stable", "anytrack/stable", ""},
		{"track/foo", "", "", "invalid pinned track: track/foo"},
		{"track", "", "track", ""},
		{"track", "track", "track", ""},
		{"track", "beta", "track/beta", ""},
		{"track", "stable/branch", "track/stable/branch", ""},
		{"track", "track/edge/branch", "track/edge/branch", ""},
		{"track", "track/candidate", "track/candidate", ""},
		{"track", "track/stable/branch", "track/stable/branch", ""},
		{"track1", "track2/stable", "track2/stable", "cannot switch pinned track"},
		{"track1", "track2/stable/branch", "track2/stable/branch", "cannot switch pinned track"},
	}
	for _, t := range tests {
		r := mylog.Check2(channel.ResolvePinned(t.track, t.new))
		tcomm := Commentf("%#v", t)
		if t.expErr == "" {
			c.Assert(err, IsNil, tcomm)
			c.Check(r, Equals, t.result, tcomm)
		} else {
			c.Assert(err, ErrorMatches, t.expErr, tcomm)
		}
	}
}
