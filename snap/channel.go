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

package snap

import (
	"fmt"
	"strings"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/strutil"
)

var channelRisks = []string{"stable", "candidate", "beta", "edge"}

// StoreChannel identifies and describes completely a store channel.
type StoreChannel struct {
	Architecture string `json:"architecture"`
	Name         string `json:"name"`
	Track        string `json:"track"`
	Risk         string `json:"risk"`
	Branch       string `json:"branch,omitempty"`
}

// ParseStoreChannel parses a string representing a store channel and includes the given architecture, if architecture is "" the system architecture is included.
func ParseStoreChannel(s string, architecture string) (StoreChannel, error) {
	if s == "" {
		return StoreChannel{}, fmt.Errorf("channel name cannot be empty")
	}
	p := strings.Split(s, "/")
	var risk, track, branch string
	switch len(p) {
	default:
		return StoreChannel{}, fmt.Errorf("channel name has too many components: %s", s)
	case 3:
		track, risk, branch = p[0], p[1], p[2]
	case 2:
		if strutil.ListContains(channelRisks, p[0]) {
			risk, branch = p[0], p[1]
		} else {
			track, risk = p[0], p[1]
		}
	case 1:
		if strutil.ListContains(channelRisks, p[0]) {
			risk = p[0]
		} else {
			track = p[0]
			risk = "stable"
		}
	}

	if !strutil.ListContains(channelRisks, risk) {
		return StoreChannel{}, fmt.Errorf("invalid risk in channel name: %s", s)
	}

	if architecture == "" {
		architecture = arch.UbuntuArchitecture()
	}

	return StoreChannel{
		Architecture: architecture,
		Track:        track,
		Risk:         risk,
		Branch:       branch,
	}.Clean(), nil
}

// Clean returns a StoreChannel with a normalized track and name.
func (c StoreChannel) Clean() StoreChannel {
	track := c.Track

	if track == "latest" {
		track = ""
	}

	// normalized name
	name := c.Risk
	if track != "" {
		name = track + "/" + name
	}
	if c.Branch != "" {
		name = name + "/" + c.Branch
	}

	return StoreChannel{
		Architecture: c.Architecture,
		Name:         name,
		Track:        track,
		Risk:         c.Risk,
		Branch:       c.Branch,
	}
}

func (c *StoreChannel) String() string {
	return c.Name
}

// Full returns the full name of the channel, inclusive the default track "latest".
func (c *StoreChannel) Full() string {
	if c.Track == "" {
		return "latest/" + c.Name
	}
	return c.String()
}

func riskLevel(risk string) int {
	for i, r := range channelRisks {
		if r == risk {
			return i
		}
	}
	return -1
}

// StoreChannelMatch represents on which fields two channels are matching.
type StoreChannelMatch struct {
	Architecture bool
	Track        bool
	Risk         bool
}

// String returns the string represantion of the match, results can be:
//  "architecture:track:risk"
//  "architecture:track"
//  "architecture:risk"
//  "track:risk"
//  "architecture"
//  "track"
//  "risk"
//  ""
func (cm StoreChannelMatch) String() string {
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

// Match returns a StoreChannelMatch of which fields among architecture,track,risk match between c and c1 store channels, risk is matched taking channel inheritance into account and considering c the requested channel.
func (c *StoreChannel) Match(c1 *StoreChannel) StoreChannelMatch {
	requestedRiskLevel := riskLevel(c.Risk)
	rl1 := riskLevel(c1.Risk)
	return StoreChannelMatch{
		Architecture: c.Architecture == c1.Architecture,
		Track:        c.Track == c1.Track,
		Risk:         requestedRiskLevel >= rl1,
	}
}
