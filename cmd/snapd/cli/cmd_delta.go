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
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/snap/squashfs/blockplan"
	"github.com/snapcore/snapd/strutil"
)

var shortDeltaHelp = i18n.G("Apply/Generate snap delta")

// longDeltaHelp carries the format and option detail itself, flush left and
// within 80 columns, because go-flags renders neither for this command: it
// hides the option list of a hidden command, and it strips the leading
// whitespace of every line of a long description before wrapping it at the
// terminal width. What is written below is what `snap delta --help` prints.
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

Options:

--generate            Generate a delta from --source to --target
--apply               Reconstruct --target from --source and --delta
-s, --source <file>   Source snap package, read by both modes
-t, --target <file>   Target snap: read to generate, written to apply
-d, --delta <file>    Delta file: written to generate, read to apply
-f, --format <name>   Format to generate, one of the names above

--format is required with --generate and has no effect on --apply, which
recognises the format from the delta itself.

Everything below is read by the snap-2-1-hdiffz format alone, and is refused
rather than ignored when passed with another one.

Resource limits:

-j, --jobs <n>
Blocks the compressor may work on at once, 0 meaning one per core. Each job
holds its own encoder state and block buffers, so this is the term of an
apply's memory demand that scales with the machine rather than with the delta.

--max-run <bytes>
Applying: refuse a delta whose patch runs need more than this at once, before
anything is read, written or forked; 0 accepts whatever the delta asks for.
Generating: cap the plaintext one run reconstructs, which is what the delta
then asks of the device; 0 takes the measured 8 MiB default.

--stats
Report what the delta consists of, or what applying it cost -- including peak
resident size and peak scratch, which are the two numbers the limits above are
there to bound.

An apply's memory is those first two between them: --max-run bounds the scratch
one patch run holds, since a run's plaintext and its source window are the only
large things held at once, and --jobs bounds what the compressor holds
alongside it. Lowering either trades time for memory.

Generation tuning:

The defaults were measured over snapcraft and kernel revision pairs, and a knob
left unset keeps its measured setting. These exist to measure the format, and
to fit it to a device that disagrees with those measurements; a delta headed
for the store wants none of them.

--min-saving <bytes>
Fewest bytes a run must save over shipping the same blocks verbatim to be worth
compressing at all.

--min-saving-rate <f>
Fewest delta bytes a run must save per byte of plaintext it makes the device
compress. This is the floor that does the work, because it scales with what is
being asked of the device where --min-saving does not.

--window-ratio <f>
Source window size as a multiple of a run's plaintext. A larger window gives
hdiffz more to match against, up to a knee past which it only costs the device
more decompression.

--window-back <f>
Fraction of a window placed before its anchor rather than after it. Measured at
0: a file's plaintext runs forward from the offset its anchor names, so budget
spent behind the anchor is budget not spent ahead of it.

--no-patch-runs
Ship every changed block as a literal, asking the device for no data-block
compression at all. The largest delta the format can produce, and the baseline
the patch runs are measured against.

--no-path-match
Anchor runs by source offset alone, ignoring which file a block belongs to.
Still correct, just larger.

--no-verify
Skip the pass that applies the finished delta and compares the result with the
target.

--run-log
Log one line per patch run considered to stderr, preceded by a key to the
columns. It is how a run that produced a bad patch is told apart from a run
whose content really changed, which the summary counts cannot do.

--hdiffz-args=<args>
Extra hdiffz options, merged into the built-in diff tuning; one naming an
option the tuning already sets replaces it, because hdiffz refuses an option
given twice. The value starts with a dash, so it has to be attached with an
equals sign: --hdiffz-args="-c-zstd-19-24 -block-0".

Examples:

$ snap delta --generate -f snap-2-1-hdiffz -s old.snap -t new.snap -d d.delta
$ snap delta --generate -f snap-1-1-xdelta3 -s old.snap -t new.snap -d d.delta
$ snap delta --generate -f xdelta3 -s old.snap -t new.snap -d d.delta
$ snap delta --apply -s old.snap -d d.delta -t rebuilt.snap
$ snap delta --apply -j 2 --max-run 4194304 -s old.snap -d d.delta -t new.snap
$ snap delta --apply --stats -s old.snap -d d.delta -t rebuilt.snap
`)

// cmdDelta is the command line. The tuning knobs are pointers so that "not
// given" is distinguishable from "given the zero value": zero is a meaningful
// setting for most of them -- --min-saving=0 removes the floor, --window-back=0
// places the whole window after the anchor -- and a knob nobody named has to
// keep the measured default rather than flatten it.
type cmdDelta struct {
	clientMixin
	Generate bool   `long:"generate"`
	Apply    bool   `long:"apply"`
	Source   string `long:"source" short:"s" required:"yes"`
	Target   string `long:"target" short:"t" required:"yes"`
	Delta    string `long:"delta" short:"d" required:"yes"`
	Format   string `long:"format" short:"f"`

	Jobs   int  `long:"jobs" short:"j"`
	Stats  bool `long:"stats"`
	MaxRun int  `long:"max-run"`

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

// override for testing
var (
	squashfsGenerateDelta = squashfs.GenerateDelta
	squashfsApplyDelta    = squashfs.ApplyDelta
)

func init() {
	cmd := addCommand("delta", shortDeltaHelp, longDeltaHelp, func() flags.Commander { return &cmdDelta{} },
		map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"generate": i18n.G("Generate delta between source and target"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"apply": i18n.G("Apply delta on the source to reconstruct target"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"source": i18n.G("Source snap package"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"target": i18n.G("Target snap package"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"delta": i18n.G("Delta between source and target snap package"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"format": i18n.G("Delta format algorithm, one of: xdelta3, snap-1-1-xdelta3, snap-2-1-hdiffz"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"jobs": i18n.G("Blocks the compressor may work on at once (0 = one per core)"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"stats": i18n.G("Report what the delta consists of, or what applying it cost"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"max-run": i18n.G("Cap the plaintext one patch run reconstructs, in bytes"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"min-saving": i18n.G("Fewest bytes a patch run must save to be worth compressing"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"min-saving-rate": i18n.G("Fewest delta bytes a run must save per byte compressed"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"window-ratio": i18n.G("Source window size as a multiple of a run's plaintext"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"window-back": i18n.G("Fraction of a window placed before its anchor"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"no-patch-runs": i18n.G("Ship every changed block as a literal"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"no-path-match": i18n.G("Anchor runs by source offset alone, ignoring filenames"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"no-verify": i18n.G("Skip the verification pass over the finished delta"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"run-log": i18n.G("Log one line per patch run considered to stderr"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"hdiffz-args": i18n.G("Extra hdiffz options, merged over the built-in tuning"),
		}, nil)
	cmd.hidden = true
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
	case x.Generate && x.Apply:
		return fmt.Errorf(i18n.G("cannot use --generate and --apply together"))
	case !x.Generate && !x.Apply:
		return fmt.Errorf(i18n.G("one of --generate or --apply must be specified"))
	case x.Generate:
		supportedFormats := squashfs.SupportedDeltaFormats(squashfs.DeltaFormatOpts{WithSnapDeltaFormat: true})
		if x.Format == "" {
			return fmt.Errorf(i18n.G("the --format flag is required for --generate, supported formats: %s"),
				strings.Join(supportedFormats, ", "))
		}
		if !strutil.ListContains(supportedFormats, x.Format) {
			return fmt.Errorf(i18n.G("unsupported delta format %q, supported formats: %s"),
				x.Format, strings.Join(supportedFormats, ", "))
		}
		// Refused rather than ignored: the failure this prevents is a
		// measurement that believes it set a window ratio, on a format that
		// has no windows.
		if x.Format != blockplan.Format {
			if given := x.blockPlanFlagsSet(); len(given) > 0 {
				return fmt.Errorf(i18n.G("%s tunes the %s format and is only read with --format %s"),
					given[0], blockplan.Format, blockplan.Format)
			}
		}
		opts, err := x.generateOpts()
		if err != nil {
			return err
		}
		fmt.Fprintf(Stdout, i18n.G("Using snap delta algorithm '%s'\n"), x.Format)
		fmt.Fprintf(Stdout, i18n.G("Generating delta...\n"))
		return squashfsGenerateDelta(ctx, x.Source, x.Target, x.Delta, x.Format, opts)
	case x.Apply:
		// An apply reads the format out of the delta, so there is no format
		// to check a flag against here. What can be checked is the mode: a
		// generation knob passed to --apply was going to do nothing.
		if given := x.generateOnlyFlagsSet(); len(given) > 0 {
			return fmt.Errorf(i18n.G("%s tunes generation and is only read with --generate"), given[0])
		}
		fmt.Fprintf(Stdout, i18n.G("Applying delta...\n"))
		return squashfsApplyDelta(ctx, x.Source, x.Delta, x.Target, x.applyOpts())
	}

	return nil
}

// blockPlanFlagsSet names the options only the block plan format reads, in the
// order the help lists them, limited to those actually given. Order matters
// only so that the same command line always names the same flag.
func (x *cmdDelta) blockPlanFlagsSet() []string {
	var given []string
	if x.Jobs != 0 {
		given = append(given, "--jobs")
	}
	if x.MaxRun != 0 {
		given = append(given, "--max-run")
	}
	if x.Stats {
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
		{"--min-saving", x.MinSaving != nil},
		{"--min-saving-rate", x.MinSavingRate != nil},
		{"--window-ratio", x.WindowRatio != nil},
		{"--window-back", x.WindowBack != nil},
		{"--no-patch-runs", x.NoPatchRuns},
		{"--no-path-match", x.NoPathMatch},
		{"--no-verify", x.NoVerify},
		{"--run-log", x.RunLog},
		{"--hdiffz-args", x.HdiffzArgs != ""},
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
		Jobs:        x.Jobs,
		MaxRunUSize: x.MaxRun,
		NoVerify:    x.NoVerify,
		NoPatchRuns: x.NoPatchRuns,
		NoPathMatch: x.NoPathMatch,
	}
	if x.HdiffzArgs != "" {
		extra, err := blockplan.ParseHdiffzArgs(x.HdiffzArgs)
		if err != nil {
			return nil, err
		}
		opts.HdiffzArgs = extra
	}
	if x.MinSaving != nil || x.MinSavingRate != nil || x.WindowRatio != nil || x.WindowBack != nil {
		tune := blockplan.DefaultPatchRunTuning(x.MaxRun)
		if x.MinSaving != nil {
			tune.MinSaving = *x.MinSaving
		}
		if x.MinSavingRate != nil {
			tune.MinSavingRate = *x.MinSavingRate
		}
		if x.WindowRatio != nil {
			tune.WindowRatio = *x.WindowRatio
		}
		if x.WindowBack != nil {
			tune.WindowBackFrac = *x.WindowBack
		}
		opts.Tuning = &tune
	}
	// The run log is a stream of one line per run and the report is a summary,
	// so they go to different places: the log to stderr, where it can be
	// redirected away from the summary a person is reading.
	if x.RunLog {
		opts.RunLog = Stderr
	}
	if x.Stats {
		opts.Report = Stdout
	}
	return opts, nil
}

// applyOpts turns the command line into apply options. These are the two
// resource limits -- how many cores, and how much memory a patch run may ask
// for -- plus the report that says whether they bit.
func (x *cmdDelta) applyOpts() *squashfs.ApplyDeltaOpts {
	opts := &squashfs.ApplyDeltaOpts{
		Jobs:        x.Jobs,
		MaxRunUSize: x.MaxRun,
	}
	if x.Stats {
		opts.Report = Stdout
	}
	return opts
}
