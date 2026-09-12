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

package cli_test

import (
	"errors"

	. "gopkg.in/check.v1"

	cmdsnap "github.com/snapcore/snapd/cmd/snapd/cli"
)

// The preseed-hints command is a thin wrapper: all of the capture, validation
// and serialization behaviour is covered by image/preseed. Only the CLI syntax
// and the forwarding to the helper are tested here.
type SnapPreseedHintsSuite struct {
	BaseSnapSuite
}

var _ = Suite(&SnapPreseedHintsSuite{})

func (s *SnapPreseedHintsSuite) TestPreseedHintsMissingArg(c *C) {
	var called bool
	restore := cmdsnap.MockPreseedWritePreseedHints(func(hintsFile string) error {
		called = true
		return nil
	})
	defer restore()

	_, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"preseed-hints"})
	c.Check(err, ErrorMatches, `the required argument `+"`<hints-file>`"+` was not provided`)
	c.Check(called, Equals, false)
}

func (s *SnapPreseedHintsSuite) TestPreseedHintsExtraArgs(c *C) {
	var called bool
	restore := cmdsnap.MockPreseedWritePreseedHints(func(hintsFile string) error {
		called = true
		return nil
	})
	defer restore()

	_, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"preseed-hints", "hints.json", "extra"})
	c.Check(err, ErrorMatches, "too many arguments for command")
	c.Check(called, Equals, false)
}

func (s *SnapPreseedHintsSuite) TestPreseedHintsForwardsHintsFile(c *C) {
	var hintsFiles []string
	restore := cmdsnap.MockPreseedWritePreseedHints(func(hintsFile string) error {
		hintsFiles = append(hintsFiles, hintsFile)
		return nil
	})
	defer restore()

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"preseed-hints", "/tmp/hints.json"})
	c.Assert(err, IsNil)
	c.Check(rest, DeepEquals, []string{})
	c.Check(hintsFiles, DeepEquals, []string{"/tmp/hints.json"})
	// the command itself is quiet, the helper owns the file
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapPreseedHintsSuite) TestPreseedHintsPropagatesError(c *C) {
	restore := cmdsnap.MockPreseedWritePreseedHints(func(hintsFile string) error {
		return errors.New("cannot read directory \"/sys/class/leds\": boom")
	})
	defer restore()

	_, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"preseed-hints", "hints.json"})
	c.Check(err, ErrorMatches, `cannot read directory "/sys/class/leds": boom`)
}
