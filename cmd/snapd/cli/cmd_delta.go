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

package cli

import (
	"context"
	"fmt"
	"strings"
	"syscall"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/snap/squashfs/blockplan"
	"github.com/snapcore/snapd/strutil"
)

var shortDeltaHelp = i18n.G("Apply/Generate snap delta")

// longDeltaHelp is everything go-flags prints before the option groups, so the
// prose about a group has to precede the group rather than sit beside it. Each
// line is written flush left and within 80 columns because go-flags strips the
// leading whitespace of every line before wrapping it at the terminal width.
var longDeltaHelp = i18n.G(`
Generate / apply 'smart' delta between source and target snaps

Exactly one of --generate or --apply must be specified. A delta records its own
format in its first bytes, so --format picks what to generate and is ignored
when applying.

Formats:

xdelta3
A raw xdelta3 binary diff between the two compressed snaps. It needs no
squashfs tooling on either side, and gains the least from the target
resembling the source, because compression has already scattered that
resemblance across the image.

snap-1-1-xdelta3
An xdelta3 diff over the snap's pseudo-file listing, an uncompressed
representation of the image's contents. Applying one rebuilds the target with
mksquashfs, which recompresses the whole image however little of it changed.

snap-2-1-hdiffz
A plan of instructions over the source snap's own compressed blocks. Unchanged
blocks are copied still compressed and only changed ones are compressed again,
so reassembly does not recompress the whole image. A changed run of blocks is
carried as an hdiffz patch against a window of source plaintext and rebuilt on
the device with hpatchz. Generating one verifies it: the finished delta is
applied by the same code the device will run and the result compared against
the target, and a delta that does not reproduce it is not kept.

--format is required with --generate and has no effect on --apply, which
recognises the format from the delta itself.

The two snap-2-1-hdiffz groups below are read by that format alone, and are
refused rather than ignored when passed with another one. The resource limits
are read by both modes; the generation tuning only by --generate.

The resource limits are what an apply's memory demand is made of: --max-run
bounds the scratch one patch run holds, since a run's plaintext and its source
window are the only large things held at once, and --jobs bounds what the
compressor holds alongside it. Lowering either trades time for memory.

The generation tuning defaults were measured over snapcraft and kernel revision
pairs, and a knob left unset keeps its measured setting. These exist to measure
the format, and to fit it to a device that disagrees with those measurements; a
delta headed for the store wants none of them.

Examples:

$ snap delta --generate -f snap-2-1-hdiffz -s old.snap -t new.snap -d d.delta
$ snap delta --generate -f snap-1-1-xdelta3 -s old.snap -t new.snap -d d.delta
$ snap delta --generate -f xdelta3 -s old.snap -t new.snap -d d.delta
$ snap delta --apply -s old.snap -d d.delta -t rebuilt.snap
$ snap delta --apply -j 2 --max-run 4194304 -s old.snap -d d.delta -t new.snap
$ snap delta --apply --stats -s old.snap -d d.delta -t rebuilt.snap
`)

// The options live in groups rather than directly on cmdDelta because go-flags
// renders a group under its own heading, and because it will not render the
// options of a hidden command at all: Command.Hidden is the embedded Group's
// Hidden, so delta's own group is skipped along with the command. Subgroups
// carry their own Hidden, so these print while the command stays out of the
// command list.

// deltaModeOpts is what every delta takes, whatever its format.
type deltaModeOpts struct {
	Generate bool   `long:"generate"`
	Apply    bool   `long:"apply"`
	Source   string `long:"source" short:"s" required:"yes"`
	Target   string `long:"target" short:"t" required:"yes"`
	Delta    string `long:"delta" short:"d" required:"yes"`
	Format   string `long:"format" short:"f"`
}

// deltaLimitOpts bounds what the work costs. Both modes read these, which is
// why they are one group and not two: --max-run caps the run an apply will
// accept and the run a generate will write into the delta, and those are the
// two ends of the same negotiation.
type deltaLimitOpts struct {
	Jobs   int  `long:"jobs" short:"j"`
	MaxRun int  `long:"max-run"`
	Stats  bool `long:"stats"`
}

// deltaTuningOpts drives the patch-run cost model. The numeric knobs are
// pointers so that "not given" is distinguishable from "given the zero value":
// zero is a meaningful setting for most of them -- --min-saving=0 removes the
// floor, --window-back=0 places the whole window after the anchor -- and a knob
// nobody named has to keep its measured default rather than flatten it.
type deltaTuningOpts struct {
	MinSaving     *int     `long:"min-saving"`
	MinSavingRate *float64 `long:"min-saving-rate"`
	WindowRatio   *float64 `long:"window-ratio"`
	WindowBack    *float64 `long:"window-back"`
	NoPatchRuns   bool     `long:"no-patch-runs"`
	NoPathMatch   bool     `long:"no-path-match"`
	NoVerify      bool     `long:"no-verify"`
	RunLog        bool     `long:"run-log"`
	HdiffzArgs    string   `long:"hdiffz-args"`
}

type cmdDelta struct {
	clientMixin
	Mode   deltaModeOpts   `group:"Mode and files"`
	Limits deltaLimitOpts  `group:"snap-2-1-hdiffz resource limits"`
	Tuning deltaTuningOpts `group:"snap-2-1-hdiffz generation tuning"`
}

// override for testing
var (
	squashfsGenerateDelta = squashfs.GenerateDelta
	squashfsApplyDelta    = squashfs.ApplyDelta
)

func init() {
	cmd := addCommand("delta", shortDeltaHelp, longDeltaHelp, func() flags.Commander { return &cmdDelta{} },
		// The descriptions are set below rather than here: addCommand's map is
		// keyed against the command's own options, and every option of this one
		// belongs to a group instead.
		nil, nil)
	cmd.hidden = true
	cmd.extra = func(cmd *flags.Command) {
		for name, desc := range map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"generate": i18n.G("Generate a delta from --source to --target"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"apply": i18n.G("Reconstruct --target from --source and --delta"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"source": i18n.G("Source snap package, read by both modes"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"target": i18n.G("Target snap: read to generate, written to apply"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"delta": i18n.G("Delta file: written to generate, read to apply"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"format": i18n.G("Format to generate, one of the names above; required with --generate and ignored by --apply"),

			// TRANSLATORS: This should not start with a lowercase letter.
			"jobs": i18n.G("Blocks the compressor may work on at once. Every job holds its own encoder state and block buffers, so peak memory grows about in proportion to this: one job measured 17.1 MiB resident against 38.2 MiB for eight. Default 0, one job per core"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"max-run": i18n.G("Applying, the largest patch run to accept: a delta needing more than this at once is refused before anything is read, written or forked, and 0 accepts whatever it asks for. Generating, the cap written into the delta, which is what it will ask of the device; 0 takes the measured 8 MiB default"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"stats": i18n.G("Report what the delta consists of, or what applying it cost, including peak resident size and peak scratch"),

			// TRANSLATORS: This should not start with a lowercase letter.
			"min-saving": i18n.G("Fewest bytes a run must save over shipping the same blocks verbatim to be worth compressing at all (default 0, no floor)"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"min-saving-rate": i18n.G("Fewest delta bytes a run must save per byte of plaintext it makes the device compress. This is the floor that does the work, because it scales with what is being asked of the device (default 0.02)"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"window-ratio": i18n.G("Source window size as a multiple of a run's plaintext. A larger window gives hdiffz more to match against, up to a knee past which it only costs the device more decompression (default 1.5)"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"window-back": i18n.G("Fraction of a window placed before its anchor rather than after it. A file's plaintext runs forward from the offset its anchor names, so budget spent behind the anchor is budget not spent ahead of it (default 0)"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"no-patch-runs": i18n.G("Ship every changed block as a literal, asking the device for no data-block compression at all. The largest delta the format can produce, and the baseline the patch runs are measured against"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"no-path-match": i18n.G("Anchor runs by source offset alone, ignoring which file a block belongs to. Still correct, just larger"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"no-verify": i18n.G("Skip the pass that applies the finished delta and compares the result with the target"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"run-log": i18n.G("Log one line per patch run considered to stderr, preceded by a key to the columns. It is how a run that produced a bad patch is told apart from a run whose content really changed"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"hdiffz-args": i18n.G(`Extra hdiffz options, merged into the built-in diff tuning; one naming an option the tuning already sets replaces it, because hdiffz refuses an option given twice. The value starts with a dash, so attach it with an equals sign: --hdiffz-args="-c-zstd-19-24 -block-0"`),
		} {
			opt := cmd.FindOptionByLongName(name)
			if opt == nil {
				logger.Panicf("delta has no %s option to describe", name)
			}
			lintDesc("delta", name, desc, opt.Description)
			opt.Description = desc
		}
	}
}

func (x *cmdDelta) Execute(args []string) error {
	// Listen for SIGINT/SIGTERM and cancel the context so that
	// subprocesses (xdelta3, unsquashfs, mksquashfs) are stopped.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh, sigStop := signalNotify(syscall.SIGINT, syscall.SIGTERM)
	defer sigStop()
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	switch {
	case x.Mode.Generate && x.Mode.Apply:
		return fmt.Errorf(i18n.G("cannot use --generate and --apply together"))
	case !x.Mode.Generate && !x.Mode.Apply:
		return fmt.Errorf(i18n.G("one of --generate or --apply must be specified"))
	case x.Mode.Generate:
		supportedFormats := squashfs.SupportedDeltaFormats(squashfs.DeltaFormatOpts{WithSnapDeltaFormat: true})
		if x.Mode.Format == "" {
			return fmt.Errorf(i18n.G("the --format flag is required for --generate, supported formats: %s"),
				strings.Join(supportedFormats, ", "))
		}
		if !strutil.ListContains(supportedFormats, x.Mode.Format) {
			return fmt.Errorf(i18n.G("unsupported delta format %q, supported formats: %s"),
				x.Mode.Format, strings.Join(supportedFormats, ", "))
		}
		// Refused rather than ignored: the failure this prevents is a
		// measurement that believes it set a window ratio, on a format that
		// has no windows.
		if x.Mode.Format != blockplan.Format {
			if given := x.blockPlanFlagsSet(); len(given) > 0 {
				return fmt.Errorf(i18n.G("%s tunes the %s format and is only read with --format %s"),
					given[0], blockplan.Format, blockplan.Format)
			}
		}
		opts, err := x.generateOpts()
		if err != nil {
			return err
		}
		fmt.Fprintf(Stdout, i18n.G("Using snap delta algorithm '%s'\n"), x.Mode.Format)
		fmt.Fprintf(Stdout, i18n.G("Generating delta...\n"))
		return squashfsGenerateDelta(ctx, x.Mode.Source, x.Mode.Target, x.Mode.Delta, x.Mode.Format, opts)
	case x.Mode.Apply:
		// An apply reads the format out of the delta, so there is no format
		// to check a flag against here. What can be checked is the mode: a
		// generation knob passed to --apply was going to do nothing.
		if given := x.generateOnlyFlagsSet(); len(given) > 0 {
			return fmt.Errorf(i18n.G("%s tunes generation and is only read with --generate"), given[0])
		}
		fmt.Fprintf(Stdout, i18n.G("Applying delta...\n"))
		return squashfsApplyDelta(ctx, x.Mode.Source, x.Mode.Delta, x.Mode.Target, x.applyOpts())
	}

	return nil
}

// blockPlanFlagsSet names the options only the block plan format reads, in the
// order the help lists them, limited to those actually given. Order matters
// only so that the same command line always names the same flag.
func (x *cmdDelta) blockPlanFlagsSet() []string {
	var given []string
	if x.Limits.Jobs != 0 {
		given = append(given, "--jobs")
	}
	if x.Limits.MaxRun != 0 {
		given = append(given, "--max-run")
	}
	if x.Limits.Stats {
		given = append(given, "--stats")
	}
	return append(given, x.generateOnlyFlagsSet()...)
}

// generateOnlyFlagsSet names the given options that only generation reads.
func (x *cmdDelta) generateOnlyFlagsSet() []string {
	var given []string
	for _, f := range []struct {
		name string
		set  bool
	}{
		{"--min-saving", x.Tuning.MinSaving != nil},
		{"--min-saving-rate", x.Tuning.MinSavingRate != nil},
		{"--window-ratio", x.Tuning.WindowRatio != nil},
		{"--window-back", x.Tuning.WindowBack != nil},
		{"--no-patch-runs", x.Tuning.NoPatchRuns},
		{"--no-path-match", x.Tuning.NoPathMatch},
		{"--no-verify", x.Tuning.NoVerify},
		{"--run-log", x.Tuning.RunLog},
		{"--hdiffz-args", x.Tuning.HdiffzArgs != ""},
	} {
		if f.set {
			given = append(given, f.name)
		}
	}
	return given
}

// generateOpts turns the command line into generation options.
//
// The cost model is only overridden when a knob was actually named, and the
// override starts from the measured defaults so that naming one knob does not
// reset the others. The run cap is not copied into the tuning here: it is
// negotiated with the applier through the delta header, so the generator takes
// it from MaxRunUSize whatever the tuning says.
func (x *cmdDelta) generateOpts() (*squashfs.GenerateDeltaOpts, error) {
	opts := &squashfs.GenerateDeltaOpts{
		Jobs:        x.Limits.Jobs,
		MaxRunUSize: x.Limits.MaxRun,
		NoVerify:    x.Tuning.NoVerify,
		NoPatchRuns: x.Tuning.NoPatchRuns,
		NoPathMatch: x.Tuning.NoPathMatch,
	}
	if x.Tuning.HdiffzArgs != "" {
		extra, err := blockplan.ParseHdiffzArgs(x.Tuning.HdiffzArgs)
		if err != nil {
			return nil, err
		}
		opts.HdiffzArgs = extra
	}
	if x.Tuning.MinSaving != nil || x.Tuning.MinSavingRate != nil || x.Tuning.WindowRatio != nil || x.Tuning.WindowBack != nil {
		tune := blockplan.DefaultPatchRunTuning(x.Limits.MaxRun)
		if x.Tuning.MinSaving != nil {
			tune.MinSaving = *x.Tuning.MinSaving
		}
		if x.Tuning.MinSavingRate != nil {
			tune.MinSavingRate = *x.Tuning.MinSavingRate
		}
		if x.Tuning.WindowRatio != nil {
			tune.WindowRatio = *x.Tuning.WindowRatio
		}
		if x.Tuning.WindowBack != nil {
			tune.WindowBackFrac = *x.Tuning.WindowBack
		}
		opts.Tuning = &tune
	}
	// The run log is a stream of one line per run and the report is a summary,
	// so they go to different places: the log to stderr, where it can be
	// redirected away from the summary a person is reading.
	if x.Tuning.RunLog {
		opts.RunLog = Stderr
	}
	if x.Limits.Stats {
		opts.Report = Stdout
	}
	return opts, nil
}

// applyOpts turns the command line into apply options. These are the two
// resource limits -- how many cores, and how much memory a patch run may ask
// for -- plus the report that says whether they bit.
func (x *cmdDelta) applyOpts() *squashfs.ApplyDeltaOpts {
	opts := &squashfs.ApplyDeltaOpts{
		Jobs:        x.Limits.Jobs,
		MaxRunUSize: x.Limits.MaxRun,
	}
	if x.Limits.Stats {
		opts.Report = Stdout
	}
	return opts
}
