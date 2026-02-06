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

package main_test

import (
	"errors"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/snap/squashfs"
)

func (s *SnapSuite) TestDeltaCommandGenerateHappyPath(c *C) {
	var gotSource, gotTarget, gotDelta string
	var gotFormat squashfs.DeltaFormat

	restore := snap.MockSquashfsGenerateDelta(
		func(source, target, delta string, format squashfs.DeltaFormat) error {
			gotSource = source
			gotTarget = target
			gotDelta = delta
			gotFormat = format
			return nil
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "generate",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "out.delta",
	})
	c.Assert(err, IsNil)
	c.Check(gotSource, Equals, "source.snap")
	c.Check(gotTarget, Equals, "target.snap")
	c.Check(gotDelta, Equals, "out.delta")
	c.Check(gotFormat, Equals, squashfs.SnapXdelta3Format)
	c.Check(s.Stdout(), Matches, `(?s).*Using snap delta algorithm.*`)
	c.Check(s.Stdout(), Matches, `(?s).*Generating delta\.\.\..*`)
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDeltaCommandApplyHappyPath(c *C) {
	var gotSource, gotDelta, gotTarget string

	restore := snap.MockSquashfsApplyDelta(
		func(source, delta, target string) error {
			gotSource = source
			gotDelta = delta
			gotTarget = target
			return nil
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "apply",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "patch.delta",
	})
	c.Assert(err, IsNil)
	c.Check(gotSource, Equals, "source.snap")
	c.Check(gotDelta, Equals, "patch.delta")
	c.Check(gotTarget, Equals, "target.snap")
	c.Check(s.Stdout(), Matches, `(?s).*Using snap delta algorithm.*`)
	c.Check(s.Stdout(), Matches, `(?s).*Applying delta\.\.\..*`)
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDeltaCommandGenerateError(c *C) {
	restore := snap.MockSquashfsGenerateDelta(
		func(source, target, delta string, format squashfs.DeltaFormat) error {
			return errors.New("cannot generate delta: xdelta3 not found")
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "generate",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "out.delta",
	})
	c.Assert(err, ErrorMatches, "cannot generate delta: xdelta3 not found")
}

func (s *SnapSuite) TestDeltaCommandApplyError(c *C) {
	restore := snap.MockSquashfsApplyDelta(
		func(source, delta, target string) error {
			return errors.New("cannot apply delta: unknown delta file format")
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "apply",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "bad.delta",
	})
	c.Assert(err, ErrorMatches, "cannot apply delta: unknown delta file format")
}

func (s *SnapSuite) TestDeltaCommandUnknownOperation(c *C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "compress",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "out.delta",
	})
	c.Assert(err, ErrorMatches, `unknown operation: compress`)
}

func (s *SnapSuite) TestDeltaCommandMissingSource(c *C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "generate",
		"--target", "target.snap",
		"--delta", "out.delta",
	})
	c.Assert(err, ErrorMatches, `the required flag .*--source.* was not specified`)
}

func (s *SnapSuite) TestDeltaCommandMissingTarget(c *C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "generate",
		"--source", "source.snap",
		"--delta", "out.delta",
	})
	c.Assert(err, ErrorMatches, `the required flag .*--target.* was not specified`)
}

func (s *SnapSuite) TestDeltaCommandMissingDelta(c *C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "generate",
		"--source", "source.snap",
		"--target", "target.snap",
	})
	c.Assert(err, ErrorMatches, `the required flag .*--delta.* was not specified`)
}

func (s *SnapSuite) TestDeltaCommandMissingOperation(c *C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "out.delta",
	})
	c.Assert(err, ErrorMatches, `the required argument .* was not provided`)
}

func (s *SnapSuite) TestDeltaCommandAlgorithmDisplayed(c *C) {
	restore := snap.MockSquashfsGenerateDelta(
		func(source, target, delta string, format squashfs.DeltaFormat) error {
			return nil
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "generate",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "out.delta",
	})
	c.Assert(err, IsNil)
	// formats[1] from SupportedDeltaFormats is "xdelta3"
	c.Check(s.Stdout(), Matches, `(?s)Using snap delta algorithm 'xdelta3'\n.*`)
}

func (s *SnapSuite) TestDeltaCommandShortFlags(c *C) {
	var gotSource, gotTarget, gotDelta string

	restore := snap.MockSquashfsGenerateDelta(
		func(source, target, delta string, format squashfs.DeltaFormat) error {
			gotSource = source
			gotTarget = target
			gotDelta = delta
			return nil
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "generate",
		"-s", "source.snap",
		"-t", "target.snap",
		"-d", "diff.delta",
	})
	c.Assert(err, IsNil)
	c.Check(gotSource, Equals, "source.snap")
	c.Check(gotTarget, Equals, "target.snap")
	c.Check(gotDelta, Equals, "diff.delta")
}
