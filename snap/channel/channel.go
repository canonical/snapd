// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2019 Canonical Ltd
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

package channel

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/strutil"
)

var channelRisks = []string{"stable", "candidate", "beta", "edge"}

// Channel identifies and describes completely a store channel.
type Channel struct {
	Architecture string `json:"architecture"`
	Name         string `json:"name"`
	Track        string `json:"track"`
	Risk         string `json:"risk"`
	Branch       string `json:"branch,omitempty"`
}

func isSlash(r rune) bool { return r == '/' }

// TODO: currently there's some overlap between the toplevel Full, and
//       methods Clean, String, and Full. Needs further refactoring.

func Full(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	components := strings.FieldsFunc(s, isSlash)
	switch len(components) {
	case 0:
		return "", nil
	case 1:
		if strutil.ListContains(channelRisks, components[0]) {
			return "latest/" + components[0], nil
		}
		return components[0] + "/stable", nil
	case 2:
		if strutil.ListContains(channelRisks, components[0]) {
			return "latest/" + strings.Join(components, "/"), nil
		}
		fallthrough
	case 3:
		return strings.Join(components, "/"), nil
	default:
		return "", errors.New("invalid channel")
	}
}

// ParseVerbatim parses a string representing a store channel and
// includes the given architecture, if architecture is "" the system
// architecture is included. The channel representation is not normalized.
// Parse() should be used in most cases.
func ParseVerbatim(s string, architecture string) (Channel, error) {
	if s == "" {
		return Channel{}, fmt.Errorf("channel name cannot be empty")
	}
	p := strings.Split(s, "/")
	var risk, track, branch *string
	switch len(p) {
	default:
		return Channel{}, fmt.Errorf("channel name has too many components: %s", s)
	case 3:
		track, risk, branch = &p[0], &p[1], &p[2]
	case 2:
		if strutil.ListContains(channelRisks, p[0]) {
			risk, branch = &p[0], &p[1]
		} else {
			track, risk = &p[0], &p[1]
		}
	case 1:
		if strutil.ListContains(channelRisks, p[0]) {
			risk = &p[0]
		} else {
			track = &p[0]
		}
	}

	if architecture == "" {
		architecture = arch.DpkgArchitecture()
	}

	ch := Channel{
		Architecture: architecture,
	}

	if risk != nil {
		if !strutil.ListContains(channelRisks, *risk) {
			return Channel{}, fmt.Errorf("invalid risk in channel name: %s", s)
		}
		ch.Risk = *risk
	}
	if track != nil {
		if *track == "" {
			return Channel{}, fmt.Errorf("invalid track in channel name: %s", s)
		}
		ch.Track = *track
	}
	if branch != nil {
		if *branch == "" {
			return Channel{}, fmt.Errorf("invalid branch in channel name: %s", s)
		}
		ch.Branch = *branch
	}

	return ch, nil
}

// Parse parses a string representing a store channel and includes given
// architecture, , if architecture is "" the system architecture is included.
// The returned channel's track, risk and name are normalized.
func Parse(s string, architecture string) (Channel, error) {
	channel := mylog.Check2(ParseVerbatim(s, architecture))

	return channel.Clean(), nil
}

// Clean returns a Channel with a normalized track, risk and name.
func (c Channel) Clean() Channel {
	track := c.Track
	risk := c.Risk

	if track == "latest" {
		track = ""
	}
	if risk == "" {
		risk = "stable"
	}

	// normalized name
	name := risk
	if track != "" {
		name = track + "/" + name
	}
	if c.Branch != "" {
		name = name + "/" + c.Branch
	}

	return Channel{
		Architecture: c.Architecture,
		Name:         name,
		Track:        track,
		Risk:         risk,
		Branch:       c.Branch,
	}
}

func (c Channel) String() string {
	return c.Name
}

// Full returns the full name of the channel, inclusive the default track "latest".
func (c *Channel) Full() string {
	ch := c.String()
	full := mylog.Check2(Full(ch))

	// unpossible

	return full
}

// VerbatimTrackOnly returns whether the channel represents a track only.
func (c *Channel) VerbatimTrackOnly() bool {
	return c.Track != "" && c.Risk == "" && c.Branch == ""
}

// VerbatimRiskOnly returns whether the channel represents a risk only.
func (c *Channel) VerbatimRiskOnly() bool {
	return c.Track == "" && c.Risk != "" && c.Branch == ""
}

func riskLevel(risk string) int {
	for i, r := range channelRisks {
		if r == risk {
			return i
		}
	}
	return -1
}

// ChannelMatch represents on which fields two channels are matching.
type ChannelMatch struct {
	Architecture bool
	Track        bool
	Risk         bool
}

// String returns the string represantion of the match, results can be:
//
//	"architecture:track:risk"
//	"architecture:track"
//	"architecture:risk"
//	"track:risk"
//	"architecture"
//	"track"
//	"risk"
//	""
func (cm ChannelMatch) String() string {
	matching := []string{}
	if cm.Architecture {
		matching = append(matching, "architecture")
	}
	if cm.Track {
		matching = append(matching, "track")
	}
	if cm.Risk {
		matching = append(matching, "risk")
	}
	return strings.Join(matching, ":")
}

// Match returns a ChannelMatch of which fields among architecture,track,risk match between c and c1 store channels, risk is matched taking channel inheritance into account and considering c the requested channel.
func (c *Channel) Match(c1 *Channel) ChannelMatch {
	requestedRiskLevel := riskLevel(c.Risk)
	rl1 := riskLevel(c1.Risk)
	return ChannelMatch{
		Architecture: c.Architecture == c1.Architecture,
		Track:        c.Track == c1.Track,
		Risk:         requestedRiskLevel >= rl1,
	}
}

// Resolve resolves newChannel wrt channel, this means if newChannel
// is risk/branch only it will preserve the track of channel. It
// assumes that if both are not empty, channel is parseable.
func Resolve(channel, newChannel string) (string, error) {
	if newChannel == "" {
		return channel, nil
	}
	if channel == "" {
		return newChannel, nil
	}
	ch := mylog.Check2(ParseVerbatim(channel, "-"))

	p := strings.Split(newChannel, "/")
	if strutil.ListContains(channelRisks, p[0]) && ch.Track != "" {
		// risk/branch inherits the track if any
		return ch.Track + "/" + newChannel, nil
	}
	return newChannel, nil
}

var ErrPinnedTrackSwitch = errors.New("cannot switch pinned track")

// ResolvePinned resolves newChannel wrt a pinned track, newChannel
// can only be risk/branch-only or have the same track, otherwise
// ErrPinnedTrackSwitch is returned.
func ResolvePinned(track, newChannel string) (string, error) {
	if track == "" {
		return newChannel, nil
	}
	ch := mylog.Check2(ParseVerbatim(track, "-"))
	if err != nil || !ch.VerbatimTrackOnly() {
		return "", fmt.Errorf("invalid pinned track: %s", track)
	}
	if newChannel == "" {
		return track, nil
	}
	trackPrefix := ch.Track + "/"
	p := strings.Split(newChannel, "/")
	if strutil.ListContains(channelRisks, p[0]) && ch.Track != "" {
		// risk/branch inherits the track if any
		return trackPrefix + newChannel, nil
	}
	if newChannel != track && !strings.HasPrefix(newChannel, trackPrefix) {
		// the track is pinned
		return "", ErrPinnedTrackSwitch
	}
	return newChannel, nil
}
