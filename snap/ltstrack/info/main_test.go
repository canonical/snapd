// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package main

import (
	"encoding/json"
	"strings"
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type infoSuite struct{}

var _ = Suite(&infoSuite{})

// parseInfoLineValue strips the "SNAPD_LTS_TRACKS=" prefix and surrounding
// single quotes, mirroring how snap.parseSnapdLTSTracks handles the value.
func parseInfoLineValue(line string) (map[string]map[string]string, error) {
	const prefix = "SNAPD_LTS_TRACKS="
	raw := strings.TrimPrefix(line, prefix)
	raw = strings.Trim(raw, "'")
	var out map[string]map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *infoSuite) TestRenderInfoLineEmpty(c *C) {
	line := renderInfoLine(map[int]map[string]string{})
	c.Check(line, Equals, "SNAPD_LTS_TRACKS='{}'")

	parsed, err := parseInfoLineValue(line)
	c.Assert(err, IsNil)
	c.Check(parsed, HasLen, 0)
}

func (s *infoSuite) TestRenderInfoLineNilSameAsEmpty(c *C) {
	// The package-level snapdLTSTracks is an empty (non-nil) map by design;
	// confirm the renderer would handle a nil map identically.
	line := renderInfoLine(nil)
	c.Check(line, Equals, "SNAPD_LTS_TRACKS='null'")
	// Note: 'null' parses fine to nil; the runtime parser
	// snap.parseSnapdLTSTracks treats absence and empty as nil, so both
	// shapes converge at runtime.
}

func (s *infoSuite) TestRenderInfoLineRoundTrip(c *C) {
	tracks := map[int]map[string]string{
		18: {
			"latest":       "18",
			"fips-updates": "18-fips",
			"18":           "18",
			"18-fips":      "18-fips",
		},
		20: {
			"latest": "20",
			"20":     "20",
		},
	}
	line := renderInfoLine(tracks)
	c.Assert(strings.HasPrefix(line, "SNAPD_LTS_TRACKS='"), Equals, true,
		Commentf("got: %s", line))
	c.Assert(strings.HasSuffix(line, "'"), Equals, true,
		Commentf("got: %s", line))

	parsed, err := parseInfoLineValue(line)
	c.Assert(err, IsNil)
	c.Assert(parsed, HasLen, 2)
	c.Check(parsed["18"], DeepEquals, map[string]string{
		"latest":       "18",
		"fips-updates": "18-fips",
		"18":           "18",
		"18-fips":      "18-fips",
	})
	c.Check(parsed["20"], DeepEquals, map[string]string{
		"latest": "20",
		"20":     "20",
	})
}

func (s *infoSuite) TestPackageDefaultIsEmpty(c *C) {
	// Until a UC version is onboarded the policy must ship empty.
	c.Check(snapdLTSTracks, HasLen, 0)
}
