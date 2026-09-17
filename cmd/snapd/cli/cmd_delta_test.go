// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !darwin

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
	"context"
	"errors"
	"os"
	"syscall"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snapd/cli"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/snap/squashfs/blockplan"
)

func (s *SnapSuite) TestDeltaCommandGenerateHappyPath(c *C) {
	var gotSource, gotTarget, gotDelta string
	var gotFormat string

	restore := snap.MockSquashfsGenerateDelta(
		func(_ context.Context, source, target, delta string, format string, opts *squashfs.GenerateDeltaOpts) error {
			gotSource = source
			gotTarget = target
			gotDelta = delta
			gotFormat = format
			return nil
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "--generate",
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
		func(_ context.Context, source, delta, target string, opts *squashfs.ApplyDeltaOpts) error {
			gotSource = source
			gotDelta = delta
			gotTarget = target
			return nil
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "--apply",
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
		func(_ context.Context, source, target, delta string, format string, opts *squashfs.GenerateDeltaOpts) error {
			return errors.New("cannot generate delta: xdelta3 not found")
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "--generate",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "out.delta",
		"--format", "snap-1-1-xdelta3",
	})
	c.Assert(err, ErrorMatches, "cannot generate delta: xdelta3 not found")
}

func (s *SnapSuite) TestDeltaCommandApplyError(c *C) {
	restore := snap.MockSquashfsApplyDelta(
		func(_ context.Context, source, delta, target string, opts *squashfs.ApplyDeltaOpts) error {
			return errors.New("cannot apply delta: unknown delta file format")
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "--apply",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "bad.delta",
	})
	c.Assert(err, ErrorMatches, "cannot apply delta: unknown delta file format")
}

func (s *SnapSuite) TestDeltaCommandBothGenerateAndApply(c *C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "--generate", "--apply",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "out.delta",
		"--format", "snap-1-1-xdelta3",
	})
	c.Assert(err, ErrorMatches, `cannot use --generate and --apply together`)
}

func (s *SnapSuite) TestDeltaCommandNeitherGenerateNorApply(c *C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "out.delta",
	})
	c.Assert(err, ErrorMatches, `one of --generate or --apply must be specified`)
}

func (s *SnapSuite) TestDeltaCommandMissingSource(c *C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "--generate",
		"--target", "target.snap",
		"--delta", "out.delta",
	})
	c.Assert(err, ErrorMatches, `the required flag .*--source.* was not specified`)
}

func (s *SnapSuite) TestDeltaCommandMissingTarget(c *C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "--generate",
		"--source", "source.snap",
		"--delta", "out.delta",
	})
	c.Assert(err, ErrorMatches, `the required flag .*--target.* was not specified`)
}

func (s *SnapSuite) TestDeltaCommandMissingDelta(c *C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "--generate",
		"--source", "source.snap",
		"--target", "target.snap",
	})
	c.Assert(err, ErrorMatches, `the required flag .*--delta.* was not specified`)
}

func (s *SnapSuite) TestDeltaCommandMissingFormat(c *C) {
	restore := snap.MockSquashfsGenerateDelta(
		func(_ context.Context, source, target, delta string, format string, opts *squashfs.GenerateDeltaOpts) error {
			return nil
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "--generate",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "out.delta",
	})
	c.Assert(err, ErrorMatches, `the --format flag is required for --generate.*`)
}

func (s *SnapSuite) TestDeltaCommandAlgorithmDisplayed(c *C) {
	restore := snap.MockSquashfsGenerateDelta(
		func(_ context.Context, source, target, delta string, format string, opts *squashfs.GenerateDeltaOpts) error {
			return nil
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "--generate",
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
		func(_ context.Context, source, target, delta string, format string, opts *squashfs.GenerateDeltaOpts) error {
			gotSource = source
			gotTarget = target
			gotDelta = delta
			return nil
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "--generate",
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
		func(_ context.Context, source, target, delta string, format string, opts *squashfs.GenerateDeltaOpts) error {
			gotFormat = format
			return nil
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "--generate",
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
		"delta", "--generate",
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
		func(ctx context.Context, source, target, delta string, format string, opts *squashfs.GenerateDeltaOpts) error {
			// Capture ctx.Err() here, before Execute returns and
			// defer cancel() fires.
			ctxErrDuringExec = ctx.Err()
			return nil
		})
	defer restoreGenerate()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "--generate",
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
		func(ctx context.Context, source, target, delta string, format string, opts *squashfs.GenerateDeltaOpts) error {
			// Simulate SIGINT while the operation is in progress
			sigCh <- syscall.SIGINT
			// Wait for context cancellation
			<-ctx.Done()
			return ctx.Err()
		})
	defer restoreGenerate()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "--generate",
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
		func(ctx context.Context, source, delta, target string, opts *squashfs.ApplyDeltaOpts) error {
			// Simulate SIGTERM while the operation is in progress
			sigCh <- syscall.SIGTERM
			// Wait for context cancellation
			<-ctx.Done()
			return ctx.Err()
		})
	defer restoreApply()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "--apply",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "patch.delta",
	})
	c.Assert(err, ErrorMatches, "context canceled")
}

// TestDeltaCommandGenerateTuningReachesOpts holds the whole generate-side
// plumbing in one place: every knob the help documents has to arrive at
// squashfs.GenerateDelta, and the cost-model overrides have to start from the
// measured defaults so that naming one knob does not flatten the others.
func (s *SnapSuite) TestDeltaCommandGenerateTuningReachesOpts(c *C) {
	var got *squashfs.GenerateDeltaOpts
	restore := snap.MockSquashfsGenerateDelta(
		func(_ context.Context, source, target, delta string, format string, opts *squashfs.GenerateDeltaOpts) error {
			got = opts
			return nil
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "--generate",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "out.delta",
		"--format", "snap-2-1-hdiffz",
		"--jobs", "3",
		"--max-run", "4194304",
		"--window-ratio", "2.5",
		"--no-patch-runs",
		"--no-path-match",
		"--no-verify",
		"--run-log",
		"--stats",
		"--hdiffz-args=-c-zstd-19-24",
	})
	c.Assert(err, IsNil)
	c.Assert(got, NotNil)
	c.Check(got.Jobs, Equals, 3)
	c.Check(got.MaxRunUSize, Equals, 4194304)
	c.Check(got.NoPatchRuns, Equals, true)
	c.Check(got.NoPathMatch, Equals, true)
	c.Check(got.NoVerify, Equals, true)
	c.Check(got.HdiffzArgs, DeepEquals, []string{"-c-zstd-19-24"})
	c.Check(got.RunLog, NotNil)
	c.Check(got.Report, NotNil)

	// The one knob named is changed and the rest are the measured defaults,
	// which is the whole reason the override starts from them.
	c.Assert(got.Tuning, NotNil)
	deflt := blockplan.DefaultPatchRunTuning(4194304)
	c.Check(got.Tuning.WindowRatio, Equals, 2.5)
	c.Check(got.Tuning.MinSaving, Equals, deflt.MinSaving)
	c.Check(got.Tuning.MinSavingRate, Equals, deflt.MinSavingRate)
	c.Check(got.Tuning.WindowBackFrac, Equals, deflt.WindowBackFrac)
	c.Check(got.Tuning.MaxCostRatio, Equals, deflt.MaxCostRatio)
}

// TestDeltaCommandGenerateNoTuningLeavesOptsBare checks that a plain generate
// overrides nothing, so that the package's defaults are what a delta headed for
// the store is built with.
func (s *SnapSuite) TestDeltaCommandGenerateNoTuningLeavesOptsBare(c *C) {
	var got *squashfs.GenerateDeltaOpts
	restore := snap.MockSquashfsGenerateDelta(
		func(_ context.Context, source, target, delta string, format string, opts *squashfs.GenerateDeltaOpts) error {
			got = opts
			return nil
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "--generate",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "out.delta",
		"--format", "snap-2-1-hdiffz",
	})
	c.Assert(err, IsNil)
	c.Assert(got, NotNil)
	c.Check(got.Tuning, IsNil)
	c.Check(got.NoVerify, Equals, false)
	c.Check(got.RunLog, IsNil)
	c.Check(got.Report, IsNil)
	c.Check(got.Jobs, Equals, 0)
	c.Check(got.MaxRunUSize, Equals, 0)
}

// TestDeltaCommandGenerateZeroTuningIsExpressible pins the reason the knobs are
// pointers: zero is a meaningful setting for several of them, and has to be
// distinguishable from "not given".
func (s *SnapSuite) TestDeltaCommandGenerateZeroTuningIsExpressible(c *C) {
	var got *squashfs.GenerateDeltaOpts
	restore := snap.MockSquashfsGenerateDelta(
		func(_ context.Context, source, target, delta string, format string, opts *squashfs.GenerateDeltaOpts) error {
			got = opts
			return nil
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "--generate",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "out.delta",
		"--format", "snap-2-1-hdiffz",
		"--min-saving", "0",
		"--min-saving-rate", "0",
	})
	c.Assert(err, IsNil)
	c.Assert(got, NotNil)
	c.Assert(got.Tuning, NotNil)
	c.Check(got.Tuning.MinSaving, Equals, 0)
	c.Check(got.Tuning.MinSavingRate, Equals, 0.0)
}

// TestDeltaCommandApplyLimitsReachOpts holds the apply-side plumbing: the two
// resource limits and the report are what a device with a budget sets.
func (s *SnapSuite) TestDeltaCommandApplyLimitsReachOpts(c *C) {
	var got *squashfs.ApplyDeltaOpts
	restore := snap.MockSquashfsApplyDelta(
		func(_ context.Context, source, delta, target string, opts *squashfs.ApplyDeltaOpts) error {
			got = opts
			return nil
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "--apply",
		"--source", "source.snap",
		"--delta", "in.delta",
		"--target", "target.snap",
		"--jobs", "2",
		"--max-run", "8388608",
		"--stats",
	})
	c.Assert(err, IsNil)
	c.Assert(got, NotNil)
	c.Check(got.Jobs, Equals, 2)
	c.Check(got.MaxRunUSize, Equals, 8388608)
	c.Check(got.Report, NotNil)
}

// TestDeltaCommandTuningRefusedOnOtherFormats checks that a knob only the block
// plan reads is refused rather than dropped. Dropping it silently is the
// failure worth preventing: a sweep that believes it measured a window ratio,
// on a format that has no windows.
func (s *SnapSuite) TestDeltaCommandTuningRefusedOnOtherFormats(c *C) {
	called := false
	restore := snap.MockSquashfsGenerateDelta(
		func(_ context.Context, source, target, delta string, format string, opts *squashfs.GenerateDeltaOpts) error {
			called = true
			return nil
		})
	defer restore()

	for _, tc := range []struct {
		flag, value string
		err         string
	}{
		{flag: "--window-ratio", value: "2", err: `--window-ratio tunes the snap-2-1-hdiffz format .*`},
		{flag: "--jobs", value: "2", err: `--jobs tunes the snap-2-1-hdiffz format .*`},
		{flag: "--max-run", value: "1024", err: `--max-run tunes the snap-2-1-hdiffz format .*`},
		{flag: "--no-verify", err: `--no-verify tunes the snap-2-1-hdiffz format .*`},
		{flag: "--stats", err: `--stats tunes the snap-2-1-hdiffz format .*`},
	} {
		args := []string{
			"delta", "--generate",
			"--source", "source.snap",
			"--target", "target.snap",
			"--delta", "out.delta",
			"--format", "snap-1-1-xdelta3",
			tc.flag,
		}
		if tc.value != "" {
			args = append(args, tc.value)
		}
		_, err := snap.Parser(snap.Client()).ParseArgs(args)
		c.Check(err, ErrorMatches, tc.err, Commentf("flag %s", tc.flag))
	}
	c.Check(called, Equals, false, Commentf("a refused command must not reach the generator"))
}

// TestDeltaCommandGenerateOnlyFlagsRefusedOnApply checks the other half of the
// same rule. An apply reads its format out of the delta, so there is no format
// to check against here; what can be checked is that a generation knob was
// never going to do anything.
func (s *SnapSuite) TestDeltaCommandGenerateOnlyFlagsRefusedOnApply(c *C) {
	called := false
	restore := snap.MockSquashfsApplyDelta(
		func(_ context.Context, source, delta, target string, opts *squashfs.ApplyDeltaOpts) error {
			called = true
			return nil
		})
	defer restore()

	for _, flag := range []string{"--no-verify", "--no-patch-runs", "--no-path-match", "--run-log"} {
		_, err := snap.Parser(snap.Client()).ParseArgs([]string{
			"delta", "--apply",
			"--source", "source.snap",
			"--delta", "in.delta",
			"--target", "target.snap",
			flag,
		})
		c.Check(err, ErrorMatches, flag+` tunes generation and is only read with --generate`)
	}
	c.Check(called, Equals, false)
}

// TestDeltaCommandBadHdiffzArgs checks that a bare word is refused by the
// command line rather than by a diff that has already run for a while.
func (s *SnapSuite) TestDeltaCommandBadHdiffzArgs(c *C) {
	called := false
	restore := snap.MockSquashfsGenerateDelta(
		func(_ context.Context, source, target, delta string, format string, opts *squashfs.GenerateDeltaOpts) error {
			called = true
			return nil
		})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"delta", "--generate",
		"--source", "source.snap",
		"--target", "target.snap",
		"--delta", "out.delta",
		"--format", "snap-2-1-hdiffz",
		"--hdiffz-args=oops",
	})
	c.Check(err, ErrorMatches, `extra hdiffz options are each a dash and an option name: "oops" is not one`)
	c.Check(called, Equals, false)
}
