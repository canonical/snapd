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
	"context"
	"errors"
	"os"
	"syscall"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestDeltaCommandGenerateHappyPath(c *C) {
	var gotSource, gotTarget, gotDelta string
	var gotFormat string

	restore := snap.MockSquashfsGenerateDelta(
		func(_ context.Context, source, target, delta string, format string) error {
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
		"--format", "snap-1-1-xdelta3",
	})
	c.Assert(err, IsNil)
	c.Check(gotSource, Equals, "source.snap")
	c.Check(gotTarget, Equals, "target.snap")
	c.Check(gotDelta, Equals, "out.delta")
	c.Check(gotFormat, Equals, "snap-1-1-xdelta3")
	c.Check(s.Stdout(), Matches, `(?s).*Using snap delta algorithm 'snap-1-1-xdelta3'\n.*`)
	c.Check(s.Stdout(), Matches, `(?s).*Generating delta\.\.\..*`)
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDeltaCommandApplyHappyPath(c *C) {
	var gotSource, gotDelta, gotTarget string

	restore := snap.MockSquashfsApplyDelta(
		func(_ context.Context, source, delta, target string) error {
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
	c.Check(s.Stdout(), Equals, "Applying delta...\n")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDeltaCommandGenerateError(c *C) {
	restore := snap.MockSquashfsGenerateDelta(
		func(_ context.Context, source, target, delta string, format string) error {
			return errors.New("cannot generate delta: xdelta3 not found")
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "generate",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "out.delta",
		"--format", "snap-1-1-xdelta3",
	})
	c.Assert(err, ErrorMatches, "cannot generate delta: xdelta3 not found")
}

func (s *SnapSuite) TestDeltaCommandApplyError(c *C) {
	restore := snap.MockSquashfsApplyDelta(
		func(_ context.Context, source, delta, target string) error {
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

func (s *SnapSuite) TestDeltaCommandMissingFormat(c *C) {
	restore := snap.MockSquashfsGenerateDelta(
		func(_ context.Context, source, target, delta string, format string) error {
			return nil
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "generate",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "out.delta",
	})
	c.Assert(err, ErrorMatches, `the --format flag is required for generate.*`)
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
		func(_ context.Context, source, target, delta string, format string) error {
			return nil
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "generate",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "out.delta",
		"--format", "snap-1-1-xdelta3",
	})
	c.Assert(err, IsNil)
	c.Check(s.Stdout(), Matches, `(?s)Using snap delta algorithm 'snap-1-1-xdelta3'\n.*`)
}

func (s *SnapSuite) TestDeltaCommandIsHidden(c *C) {
	parser := snap.Parser(snap.Client())
	for _, cmd := range parser.Commands() {
		if cmd.Name == "delta" {
			c.Check(cmd.Hidden, Equals, true)
			return
		}
	}
	c.Fatalf("delta command not found in parser")
}

func (s *SnapSuite) TestDeltaCommandShortFlags(c *C) {
	var gotSource, gotTarget, gotDelta string

	restore := snap.MockSquashfsGenerateDelta(
		func(_ context.Context, source, target, delta string, format string) error {
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
		"-f", "snap-1-1-xdelta3",
	})
	c.Assert(err, IsNil)
	c.Check(gotSource, Equals, "source.snap")
	c.Check(gotTarget, Equals, "target.snap")
	c.Check(gotDelta, Equals, "diff.delta")
}

func (s *SnapSuite) TestDeltaCommandGenerateXdelta3Format(c *C) {
	var gotFormat string

	restore := snap.MockSquashfsGenerateDelta(
		func(_ context.Context, source, target, delta string, format string) error {
			gotFormat = format
			return nil
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "generate",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "out.delta",
		"--format", "xdelta3",
	})
	c.Assert(err, IsNil)
	c.Check(gotFormat, Equals, "xdelta3")
	c.Check(s.Stdout(), Matches, `(?s).*Using snap delta algorithm 'xdelta3'\n.*`)
}

func (s *SnapSuite) TestDeltaCommandUnsupportedFormat(c *C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "generate",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "out.delta",
		"--format", "bogus",
	})
	c.Assert(err, ErrorMatches, `unsupported delta format "bogus".*`)
}

func (s *SnapSuite) TestDeltaCommandGenerateListensForSignals(c *C) {
	var gotSignals []os.Signal
	sigCh := make(chan os.Signal, 1)
	restoreSignal := snap.MockSignalNotify(func(sig ...os.Signal) (chan os.Signal, func()) {
		gotSignals = sig
		return sigCh, func() {}
	})
	defer restoreSignal()

	var ctxErrDuringExec error
	restoreGenerate := snap.MockSquashfsGenerateDelta(
		func(ctx context.Context, source, target, delta string, format string) error {
			// Capture ctx.Err() here, before Execute returns and
			// defer cancel() fires.
			ctxErrDuringExec = ctx.Err()
			return nil
		})
	defer restoreGenerate()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "generate",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "out.delta",
		"--format", "snap-1-1-xdelta3",
	})
	c.Assert(err, IsNil)
	c.Check(gotSignals, DeepEquals, []os.Signal{syscall.SIGINT, syscall.SIGTERM})
	// Context should not be cancelled when no signal was sent
	c.Check(ctxErrDuringExec, IsNil)
}

func (s *SnapSuite) TestDeltaCommandGenerateCancelledOnSignal(c *C) {
	sigCh := make(chan os.Signal, 1)
	restoreSignal := snap.MockSignalNotify(func(sig ...os.Signal) (chan os.Signal, func()) {
		return sigCh, func() {}
	})
	defer restoreSignal()

	restoreGenerate := snap.MockSquashfsGenerateDelta(
		func(ctx context.Context, source, target, delta string, format string) error {
			// Simulate SIGINT while the operation is in progress
			sigCh <- syscall.SIGINT
			// Wait for context cancellation
			<-ctx.Done()
			return ctx.Err()
		})
	defer restoreGenerate()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "generate",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "out.delta",
		"--format", "snap-1-1-xdelta3",
	})
	c.Assert(err, ErrorMatches, "context canceled")
}

func (s *SnapSuite) TestDeltaCommandApplyCancelledOnSignal(c *C) {
	sigCh := make(chan os.Signal, 1)
	restoreSignal := snap.MockSignalNotify(func(sig ...os.Signal) (chan os.Signal, func()) {
		return sigCh, func() {}
	})
	defer restoreSignal()

	restoreApply := snap.MockSquashfsApplyDelta(
		func(ctx context.Context, source, delta, target string) error {
			// Simulate SIGTERM while the operation is in progress
			sigCh <- syscall.SIGTERM
			// Wait for context cancellation
			<-ctx.Done()
			return ctx.Err()
		})
	defer restoreApply()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "apply",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "patch.delta",
	})
	c.Assert(err, ErrorMatches, "context canceled")
}
