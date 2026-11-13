// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package squashfs

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap/squashfs/blockplan"
	"github.com/snapcore/snapd/snapdtool"
)

// This file implements the support for snap deltas. Currently three formats are
// supported:
//
// - plain xdelta3 diff file on the compressed snaps
// - xdelta3 diff on an uncompressed representation of the snap files defined
//   by squashfs-tools called pseudo-files
// - a plan of instructions over the source snap's own compressed blocks,
//   implemented in the snap/squashfs/blockplan package
//
// The first two are implemented in this file. What the third needs from it is
// its name, its place in the order of preference, and a case in the dispatch
// below: delta files are told apart by their first four bytes, so recognising
// another format is a new case rather than a new field. Its trade-offs are in
// that package's documentation; the short of it is that it does not rebuild the
// target with mksquashfs, so applying it does not recompress the whole snap.
//
// The format supporting pseudo-files has files with a header preceding the
// xdelta3 information. This header is padded to 'deltaHeaderSize' size to
// allow for future fields. Current definition is (bytes in each field are in
// little endian order):
//
// |       32b    |    8b    |    8b   |     16b    |     32b    |     16b     |        16b        |
// | magic number | format v | tools v | delta tool | time stamp | compression | super block flags |
//
// Magic number is "sqpf" in ASCII, then we have the format version and the
// tools version, which are 0x01 at the moment. The tools version identify a
// given bundle of tools included in the snapd snap.
//
// Time stamp, compression and super blog flags are respectively the
// modification_time, compression_id and flags fields of the squashfs header of
// the target file, and are needed to ensure reproducibility of the target
// files, as this information is not included in the pseudo-file definitions.
//
// Optional compression options (included in the squashfs file if the
// COMPRESSOR_OPTIONS flag is set) are currently not supported. If the target
// squashfs is detected to use them, we fallback to plain xdelta.
//
// Delta between two snaps (squashfs files) is generated on the squashfs pseudo
// file definition, which is an uncompressed representation of the content of
// the files. The header data is later used as input parameters to mksquashfs
// when recreating the target squashfs from the reconstructed pseudo file.
//
// Reference for the squashfs superblock: https://dr-emann.github.io/squashfs

const (
	// Identifiers for the store
	xdelta3Format = "xdelta3"
	// This follows compatibility labels conventions. First and second
	// number represent format and tools versions respectively, and could
	// use intervals in the future.
	snapDeltaFormatXdelta3 = "snap-1-1-xdelta3"
	// The block plan format's name comes from the package that implements
	// it, so the label negotiated with the store cannot drift from the
	// deltas that are actually produced and consumed.
	snapDeltaFormatBlockPlan = blockplan.Format
)

const (
	// xdelta3 header, see https://datatracker.ietf.org/doc/html/rfc3284
	xdelta3MagicNumber = uint32(0x00c4c3d6)
	// squashfs magic number ("hsqs")
	squashfsMagicNumber = uint32(0x73717368)
)

// Snap delta format constants
const (
	// Delta Header Configuration
	deltaHeaderSize = 32

	// Magic number for our delta files: "sqpf" in hex
	deltaMagicNumber        = uint32(0x66707173)
	deltaFormatVersion      = uint8(0x01)
	deltaFormatToolsVersion = uint8(0x01)

	// Tool IDs
	DeltaToolXdelta3 = uint16(0x1)
)

// SquashfsSuperblock represents a SquashFS header up to the minor_version field.
// Reference: https://dr-emann.github.io/squashfs
type SquashfsSuperblock struct {
	Magic            uint32
	InodeCount       uint32
	ModificationTime uint32
	BlockSize        uint32
	FragmentEntryCnt uint32
	CompressionId    uint16
	BlockLog         uint16
	Flags            uint16
	IdCount          uint16
	MajorVersion     uint16
	MinorVersion     uint16
}

// SquashfsSuperblock.Flags constants
const (
	flagCheck             uint16 = 0x0004
	flagNoFragments       uint16 = 0x0010
	flagDuplicates        uint16 = 0x0040 // Note: logic is inverted (default is duplicates)
	flagExports           uint16 = 0x0080
	flagNoXattrs          uint16 = 0x0200
	flagCompressorOptions uint16 = 0x0400
)

// Tuning parameters for external tools
var (
	// xdelta3 on compressed files tuning.
	// Default compression level set to 3, no measurable gain between 3 and
	// 9 comp level.
	xdelta3PlainTuning = []string{"-3"}

	// xdelta3 on pseudo-files tuning.
	// The gain in size reduction between 3 and 7 comp level is 10 to 20%.
	// Delta size gains flattens at 7 There is no noticeable gain from
	// changing source window size (-B), bytes input window (-W) or size
	// compression duplicates window (-P)
	xdelta3Tuning = []string{"-7"}

	// unsquashfs tuning.
	// By default unsquashfs would allocate ~2x256 for any size of squashfs image.
	// We need to tame it down and use different tuning for:
	// - generating delta: runs on server side -> no tuning, we have memory
	// - apply delta: maybe low spec systems, limit data and fragment queues sizes
	unsquashfsTuningGenerate = []string{"-da", "128", "-fr", "128"}

	// IO buffer size for efficient piping (1MB)
	copyBufferSize = 1024 * 1024
)

// For testing purposes
var (
	osutilRunManyWithContext                  = osutil.RunManyWithContext
	setupPipes                                = setupPipesImpl
	snapdtoolCommandFromSystemSnapWithContext = snapdtool.CommandFromSystemSnapWithContext
	cmdRun                                    = cmdRunImpl
)

// SnapDeltaHeader is the header wrapping the actual delta stream. See
// description above.
type SnapDeltaHeader struct {
	Magic         uint32
	FormatVersion uint8
	ToolsVersion  uint8
	DeltaTool     uint16
	Timestamp     uint32
	Compression   uint16
	Flags         uint16
}

func cmdRunImpl(cmd *exec.Cmd) error {
	return cmd.Run()
}

// toBytes serialises a delta header to a buffer of deltaHeaderSize length.
func (h *SnapDeltaHeader) toBytes() ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, h); err != nil {
		return nil, fmt.Errorf("cannot write header struct: %w", err)
	}

	// Pad to full size (deltaHeaderSize is 32)
	if buf.Len() < deltaHeaderSize {
		buf.Write(make([]byte, deltaHeaderSize-buf.Len()))
	}

	return buf.Bytes(), nil
}

// newDeltaHeaderFromSnap builds a delta header. It takes modification_time,
// compression_id, and flags from the squashfs superblock of targetSnap and
// writes that in the corresponding delta header fields.
func newDeltaHeaderFromSnap(targetSnap string) (*SnapDeltaHeader, error) {
	// we need to get some basic info from the target snap
	f, err := os.Open(targetSnap)
	if err != nil {
		return nil, fmt.Errorf("cannot open target: %w", err)
	}
	defer f.Close()

	var sb SquashfsSuperblock
	if err := binary.Read(f, binary.LittleEndian, &sb); err != nil {
		return nil, fmt.Errorf("while reading target superblock: %w", err)
	}

	if sb.Magic != squashfsMagicNumber {
		return nil, fmt.Errorf("target is not a squashfs")
	}

	if sb.Flags&flagCompressorOptions != 0 {
		return nil, fmt.Errorf("compression options section present in target, which is unsupported")
	}

	// We expect squashfs 4.0 format
	if sb.MajorVersion != 4 || sb.MinorVersion != 0 {
		return nil, fmt.Errorf("unexpected squashfs version %d.%d", sb.MajorVersion, sb.MinorVersion)
	}

	// Note that currently the only supported delta tool is DeltaToolXdelta3.
	hdr := &SnapDeltaHeader{
		Magic:         deltaMagicNumber,
		FormatVersion: deltaFormatVersion,
		ToolsVersion:  deltaFormatToolsVersion,
		DeltaTool:     DeltaToolXdelta3,
	}
	// Populate some header fields from the parsed struct
	hdr.Timestamp = sb.ModificationTime
	hdr.Compression = sb.CompressionId
	hdr.Flags = sb.Flags

	return hdr, nil
}

// Pool for large IO buffers (1MB) to reduce GC pressure during io.Copy
var ioBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, copyBufferSize)
		return b
	},
}

// Helper to copy using pooled buffers
func copyBuffer(dst io.Writer, src io.Reader) (int64, error) {
	bufPtr := ioBufPool.Get().([]byte)
	defer ioBufPool.Put(bufPtr)
	return io.CopyBuffer(dst, src, bufPtr)
}

type DeltaFormatOpts struct {
	WithSnapDeltaFormat bool
}

// Supported delta formats. The order here is determines the preferred formats,
// with lower indexes being preferred. This might become eventually
// compatibility labels if necessary.
func SupportedDeltaFormats(opts DeltaFormatOpts) []string {
	var formats []string
	if opts.WithSnapDeltaFormat {
		// The block plan comes first: on the pairs it was measured
		// against it is the smallest of the three and by far the
		// cheapest to apply, and where it cannot be used at all it is
		// refused at generation time, so the store never offers one.
		formats = append(formats, snapDeltaFormatBlockPlan, snapDeltaFormatXdelta3)
	}
	formats = append(formats, xdelta3Format)
	return formats
}

// growSnapToMinSize pads a file to minSize with zero bytes if it is smaller.
func growSnapToMinSize(path string, minSize int64) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cannot get size of snap: %w", err)
	}
	if fi.Size() >= minSize {
		return nil
	}
	if err := os.Truncate(path, minSize); err != nil {
		return fmt.Errorf("cannot grow snap to minimum size: %w", err)
	}
	return nil
}

// GenerateDeltaOpts tunes generation. Nil, or a zero value, is what a caller
// with no opinion passes and is what snapd itself uses.
//
// Everything here is read by the snap-2-1-hdiffz format alone: the two xdelta3
// formats have nothing to tune, so passing any of it alongside one of them is
// refused rather than ignored. Refusing matters because the failure it prevents
// is silent -- a sweep that believes it measured a window ratio, on a format
// that has no windows.
type GenerateDeltaOpts struct {
	// Jobs is how many blocks the compressor may work on at once. Zero or
	// less means every core, which is the right default for generation: it
	// happens where snaps are built or published rather than on a device, so
	// there is no memory budget to trade speed against.
	Jobs int

	// MaxRunUSize caps the plaintext one patch run reconstructs, and so caps
	// what applying the delta will demand in memory. Zero takes the format's
	// measured default of 8 MiB. Lowering it is how a delta is made for a
	// device with a tighter ceiling than that, at the cost of a larger delta.
	MaxRunUSize int

	// NoVerify skips the pass that applies the finished delta and compares
	// the result with the target. Generation verifies by default, which is
	// what keeps a delta nothing can apply from being published, and this is
	// here for a measurement that does not want to pay twice.
	NoVerify bool

	// NoPatchRuns ships every changed block as a literal and NoPathMatch
	// anchors runs by source offset alone. Each turns off one half of the
	// size optimisation, so a measurement can attribute what it buys; both
	// produce a correct delta, just a larger one.
	NoPatchRuns bool
	NoPathMatch bool

	// Tuning overrides the patch-run cost model. Nil takes the measured
	// defaults; a caller moving one knob starts from
	// blockplan.DefaultPatchRunTuning so the rest keep their settings.
	Tuning *blockplan.PatchRunTuning

	// HdiffzArgs are extra hdiffz options, merged over the built-in diff
	// tuning. blockplan.ParseHdiffzArgs turns a command line's worth of them
	// into this.
	HdiffzArgs []string

	// RunLog receives one line per patch run considered, and Report a summary
	// of the finished delta. Both are nil in normal use, where what little is
	// worth saying goes to the debug log instead.
	RunLog io.Writer
	Report io.Writer
}

// tunesBlockPlan reports whether anything only the block plan reads is set, so
// that the other formats can refuse it instead of dropping it on the floor.
func (o *GenerateDeltaOpts) tunesBlockPlan() bool {
	if o == nil {
		return false
	}
	return o.Jobs != 0 || o.MaxRunUSize != 0 || o.NoVerify || o.NoPatchRuns ||
		o.NoPathMatch || o.Tuning != nil || len(o.HdiffzArgs) != 0 ||
		o.RunLog != nil || o.Report != nil
}

// ApplyDeltaOpts is how a caller with a memory budget bounds an apply. Nil, or
// a zero value, takes the defaults: every core, and whatever the delta asks for.
//
// As with generation, all of it is read by the snap-2-1-hdiffz format alone.
// The two xdelta3 formats hand the work to xdelta3 and mksquashfs, whose
// resource use this package does not mediate.
type ApplyDeltaOpts struct {
	// Jobs is how many blocks the compressor may work on at once. Zero or
	// less means every core. Each job holds its own encoder state and block
	// buffers, so this is the term of an apply's memory demand that scales
	// with the machine rather than with the delta -- lowering it is how an
	// apply is kept off every core of a device that has other work to do.
	Jobs int

	// MaxRunUSize is the largest patch run this apply will accept. A delta
	// whose runs need more is refused before anything is read, written or
	// forked, which is what lets a device fall back to a full download rather
	// than discover the cost partway through assembling an image. Zero
	// accepts whatever the delta asks for.
	MaxRunUSize int

	// Report receives a summary of what the apply cost, including its peak
	// resident size and peak scratch -- the two numbers the settings above
	// are there to bound. Nil in normal use.
	Report io.Writer
}

// appliesBlockPlan reports whether anything only the block plan reads is set.
func (o *ApplyDeltaOpts) appliesBlockPlan() bool {
	if o == nil {
		return false
	}
	return o.Jobs != 0 || o.MaxRunUSize != 0 || o.Report != nil
}

// errDeltaOptsNotBlockPlan refuses options the named format cannot honour.
// Dropping them silently is the failure worth preventing: a measurement that
// believes it bounded an apply, on a format whose cost this package does not
// mediate at all.
func errDeltaOptsNotBlockPlan(format string) error {
	return fmt.Errorf("delta format %q takes no options, they are read by %s alone",
		format, snapDeltaFormatBlockPlan)
}

// GenerateDelta creates a delta file called delta from sourceSnap and
// targetSnap, using deltaFormat. opts may be nil, and is read by
// snap-2-1-hdiffz alone.
func GenerateDelta(ctx context.Context, sourceSnap, targetSnap, delta string, deltaFormat string, opts *GenerateDeltaOpts) error {
	if deltaFormat != snapDeltaFormatBlockPlan && opts.tunesBlockPlan() {
		return errDeltaOptsNotBlockPlan(deltaFormat)
	}
	switch deltaFormat {
	case xdelta3Format:
		// Plain xdelta3 on compressed files
		return generatePlainXdelta3Delta(ctx, sourceSnap, targetSnap, delta)
	case snapDeltaFormatXdelta3:
		return generateSnapDelta(ctx, sourceSnap, targetSnap, delta)
	case snapDeltaFormatBlockPlan:
		return generateBlockPlanDelta(ctx, sourceSnap, targetSnap, delta, opts)
	default:
		return fmt.Errorf("unsupported delta format %q", deltaFormat)
	}
}

// generateBlockPlanDelta generates a snap-2-1-hdiffz delta, which describes the
// target as a plan over the source's compressed blocks.
//
// It generates with verification unless the caller turns that off: the finished
// delta is applied by the same code the device will run and the result compared
// with the target image, and a delta that does not reproduce it is not kept.
// That doubles the cost of generating one, which is affordable here because
// generation happens where snaps are built or published rather than on a device
// -- and it is the check that keeps a delta nothing can apply from ever being
// published.
func generateBlockPlanDelta(ctx context.Context, sourceSnap, targetSnap, delta string, opts *GenerateDeltaOpts) error {
	if opts == nil {
		opts = &GenerateDeltaOpts{}
	}
	// Generate removes the delta file itself if it cannot finish it, so
	// there is nothing to clean up here.
	stats, err := blockplan.Generate(ctx, sourceSnap, targetSnap, delta,
		&blockplan.GenerateOpts{
			Jobs:        opts.Jobs,
			MaxRunUSize: opts.MaxRunUSize,
			Verify:      !opts.NoVerify,
			NoPatchRuns: opts.NoPatchRuns,
			NoPathMatch: opts.NoPathMatch,
			Tuning:      opts.Tuning,
			HdiffzArgs:  opts.HdiffzArgs,
			RunLog:      opts.RunLog,
		})
	if err != nil {
		return err
	}
	if opts.Report != nil {
		blockplan.WriteGenerateReport(opts.Report, stats, sourceSnap, targetSnap, !opts.NoVerify)
	}
	logger.Debugf("generated %s delta: %d bytes describing %d instructions "+
		"(%d copies, %d literals, %d patch runs), %d of %d target bytes copied verbatim, "+
		"%d of %d uncompressed bytes reused, in %v",
		snapDeltaFormatBlockPlan, stats.DeltaSize, stats.Instructions,
		stats.Copies, stats.Literals, stats.PatchRuns,
		stats.CopiedBytes, stats.TargetDataBytes, stats.ReusedUBytes, stats.TargetUBytes,
		stats.Elapsed.Round(time.Millisecond))
	return nil
}

func generateSnapDelta(ctx context.Context, sourceSnap, targetSnap, delta string) error {
	// Build delta header, using the target header
	hdr, err := newDeltaHeaderFromSnap(targetSnap)
	if err != nil {
		return err
	}

	headerBytes, err := hdr.toBytes()
	if err != nil {
		return fmt.Errorf("cannot build delta header: %w", err)
	}

	// Create delta file and write header
	deltaFile, err := os.OpenFile(delta, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("cannot create delta file: %w", err)
	}
	defer deltaFile.Close()
	if _, err := deltaFile.Write(headerBytes); err != nil {
		return fmt.Errorf("cannot write delta header: %w", err)
	}

	if err := generateXdelta3Delta(ctx, deltaFile, sourceSnap, targetSnap); err != nil {
		deltaFile.Close()
		if err := os.Remove(delta); err != nil {
			logger.Noticef("cannot clean-up delta file: %s", err)
		}
		return err
	}
	return nil
}

// ApplyDelta uses sourceSnap and delta files to generate targetSnap. The format
// is read out of the delta rather than passed in, so a caller never has to know
// which one it received.
//
// opts may be nil, which takes the default threading and memory strategy
// documented on applyBlockPlanDelta below; it is read by snap-2-1-hdiffz alone.
func ApplyDelta(ctx context.Context, sourceSnap, delta, targetSnap string, opts *ApplyDeltaOpts) error {
	deltaFile, err := os.Open(delta)
	if err != nil {
		return fmt.Errorf("cannot open delta: %w", err)
	}
	defer deltaFile.Close()

	// Read a maximum of deltaHeaderSize, then check it to find out the
	// format that we need to decode.
	buf := make([]byte, deltaHeaderSize)
	n, err := io.ReadFull(deltaFile, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return fmt.Errorf("cannot read snap delta file: %w", err)
	}
	if n < 4 {
		return fmt.Errorf("delta file does not contain a header")
	}
	magic := binary.LittleEndian.Uint32(buf[0:4])
	switch magic {
	case xdelta3MagicNumber:
		logger.Debugf("plain xdelta3 detected")
		if opts.appliesBlockPlan() {
			return errDeltaOptsNotBlockPlan(xdelta3Format)
		}
		return applyPlainXdelta3Delta(ctx, sourceSnap, delta, targetSnap)
	case deltaMagicNumber:
		if n < deltaHeaderSize {
			return fmt.Errorf("snap delta header too short (%d bytes read)", n)
		}
		hdr := &SnapDeltaHeader{}
		if err := binary.Read(bytes.NewReader(buf), binary.LittleEndian, hdr); err != nil {
			return fmt.Errorf("cannot decode header: %w", err)
		}
		if opts.appliesBlockPlan() {
			return errDeltaOptsNotBlockPlan(snapDeltaFormatXdelta3)
		}
		return applySnapDelta(ctx, sourceSnap, targetSnap, deltaFile, hdr)
	case blockplan.Magic:
		logger.Debugf("block plan detected")
		return applyBlockPlanDelta(ctx, sourceSnap, targetSnap, deltaFile, opts)
	default:
		return fmt.Errorf("unknown delta file format")
	}
}

// applyBlockPlanDelta applies a snap-2-1-hdiffz delta.
//
// The block plan's header is part of its own stream rather than a wrapper around
// somebody else's, so deltaFile is rewound: the bytes read to find the magic
// number above are bytes the format still needs to parse.
//
// A nil opts applies with the format's default threading and memory strategy:
// the compressor gets one job per core, and whatever peak memory the delta asks
// for is accepted. Both are deliberate defaults rather than the absence of a
// decision. Nothing in this package knows the memory budget of the machine it
// is running on, so a caller that does sets Jobs and MaxRunUSize; the applier
// then refuses a delta it cannot afford before reading, writing or forking
// anything, which is a far better outcome than failing partway through
// assembling an image. Taking the defaults is nonetheless safe because the run
// cap is bounded by the delta rather than by the image: deltas generated by
// this package cap a patch run at 8 MiB, and a run's plaintext plus its source
// window are the only large things an apply holds at once.
func applyBlockPlanDelta(ctx context.Context, sourceSnap, targetSnap string, deltaFile *os.File, opts *ApplyDeltaOpts) error {
	if opts == nil {
		opts = &ApplyDeltaOpts{}
	}
	if _, err := deltaFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("cannot rewind delta: %w", err)
	}
	// Nothing to pad afterwards, unlike the pseudo-file format: the plan
	// reproduces the target file byte for byte, whatever padding it carries
	// included, and checks the result against a digest before returning.
	t0 := time.Now()
	stats, err := blockplan.ApplyToFile(ctx, sourceSnap, deltaFile, targetSnap,
		&blockplan.ApplyOpts{
			Jobs:        opts.Jobs,
			MaxRunUSize: opts.MaxRunUSize,
		})
	if err != nil {
		return err
	}
	if opts.Report != nil {
		blockplan.WriteApplyReport(opts.Report, stats, time.Since(t0))
	}
	logger.Debugf("applied %s delta: %d instructions (%d copies, %d literals, %d patch runs), "+
		"compressed %d bytes in %d blocks, read back %d bytes of source, peak scratch %d bytes",
		snapDeltaFormatBlockPlan, stats.Instructions,
		stats.Copies, stats.Literals, stats.PatchRuns,
		stats.UCompressedBytes, stats.BlocksCompressed, stats.WindowUBytes, stats.PeakScratchBytes)
	return nil
}

func applySnapDelta(ctx context.Context, sourceSnap, targetSnap string, deltaFile *os.File, hdr *SnapDeltaHeader) error {
	if hdr.Magic != deltaMagicNumber {
		return fmt.Errorf("invalid magic 0x%X", hdr.Magic)
	}
	// We require compatibility both with format and tools version
	if hdr.FormatVersion != deltaFormatVersion || hdr.ToolsVersion != deltaFormatToolsVersion {
		return fmt.Errorf("incompatible version %d.%d",
			hdr.FormatVersion, hdr.ToolsVersion)
	}
	if hdr.DeltaTool != DeltaToolXdelta3 {
		return fmt.Errorf("unsupported delta tool %d", hdr.DeltaTool)
	}

	// Prepare mksquashfs arguments by looking at delta header
	var err error
	mksqfsArgs := []string{}
	if mksqfsArgs, err = compIdToMksquashfsArgs(hdr.Compression, mksqfsArgs); err != nil {
		return fmt.Errorf("bad compression id from delta header: %w", err)
	}
	if mksqfsArgs, err = superBlockFlagsToMksquashfsArgs(hdr.Flags, mksqfsArgs); err != nil {
		return fmt.Errorf("bad flags from delta header: %w", err)
	}
	mksqfsArgs = append(mksqfsArgs, "-mkfs-time", strconv.FormatUint(uint64(hdr.Timestamp), 10))

	// run delta apply for given delta tool - DeltaToolXdelta3 is the only supported one atm
	if err := applyXdelta3Delta(ctx, sourceSnap, targetSnap, deltaFile, mksqfsArgs); err != nil {
		return err
	}

	// mksquashfs does not know about snap minimum size requirements, so
	// we need to pad the reconstructed snap to MinimumSnapSize, same as
	// snap pack does in Build(). Without this, small snaps would be
	// shorter than the original target because snap pack pads them.
	return growSnapToMinSize(targetSnap, MinimumSnapSize)
}

// generatePlainXdelta3Delta generates a delta between compressed snaps
func generatePlainXdelta3Delta(ctx context.Context, sourceSnap, targetSnap, delta string) error {
	// Compression level, force overwrite (-f), compress (-e), source (-s <file>), target, delta
	opts := append([]string{}, xdelta3PlainTuning...)
	opts = append(opts, "-f", "-e", "-s", sourceSnap, targetSnap, delta)
	cmd, err := snapdtoolCommandFromSystemSnapWithContext(ctx, "/usr/bin/xdelta3", opts...)
	if err != nil {
		return err
	}

	// cmd is cancellable if ctx is a cancellable context
	return cmdRun(cmd)
}

// applyPlainXdelta3Delta applies a delta between compressed snaps
func applyPlainXdelta3Delta(ctx context.Context, sourceSnap, delta, targetSnap string) error {
	// Force overwrite (-f), decompress (-d), source (-s <file>), target, delta
	cmd, err := snapdtoolCommandFromSystemSnapWithContext(
		ctx, "/usr/bin/xdelta3", "-f", "-d", "-s", sourceSnap, delta, targetSnap)
	if err != nil {
		return err
	}

	// cmd is cancellable if ctx is a cancellable context
	return cmdRun(cmd)
}

// generateXdelta3Delta runs in parallel two instances of unsquashfs (one for
// sourceSnap and the other for targetSnap) and xdelta3. unsquashfs output is
// in pseudo-file format. This is read by xdelta3, which calculates the delta
// between the two files (xdelta3 uses windows to calculate differences so this
// can happen in a stream way). Named pipes are used to feed xdelta3 as it does
// not read from stdin.
//
// The output of xdelta3 is sent to deltaFile, which is an open file where we
// already stored the snap delta header. This diagram summarizes this:
//
//	+-----------------+             +-----------------+
//	|   sourceSnap    |             |   targetSnap    |
//	+-------+---------+             +-------+---------+
//	        |                               |
//	        v                               v
//	+-----------------+             +-----------------+
//	|   unsquashfs    |             |   unsquashfs    |
//	| (pseudo-format) |             | (pseudo-format) |
//	+-------+---------+             +-------+---------+
//	        |                               |
//	        | [named pipe: src-pipe]        | [named pipe: trgt-pipe]
//	        |                               |
//	        +--------------+ +--------------+
//	                       | |
//	                       v v
//	            +-------------------------+
//	            |         xdelta3         |
//	            |    (calculate delta)    |
//	            +------------+------------+
//	                         |
//	                         | (stdout)
//	                         v
//	            +-------------------------+
//	            |       deltaFile         |
//	            | (Appended after header) |
//	            +-------------------------+
func generateXdelta3Delta(ctx context.Context, deltaFile *os.File, sourceSnap, targetSnap string) error {
	// Setup named pipes
	tempDir, pipes, err := setupPipes("src-pipe", "trgt-pipe")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	sourcePipe := pipes[0]
	targetPipe := pipes[1]

	buildPipeline := func(ctx context.Context) ([]*exec.Cmd, []func() error, error) {
		// Output to sourcePipe, -pf stands for pseudo-file representation
		unsquashSrcArg := append([]string{}, unsquashfsTuningGenerate...)
		unsquashSrcArg = append(unsquashSrcArg, "-no-progress", "-pf", sourcePipe, sourceSnap)
		unsquashSrcCmd, err := snapdtoolCommandFromSystemSnapWithContext(
			ctx, "/usr/bin/unsquashfs", unsquashSrcArg...)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot find unsquashfs: %w", err)
		}
		// Output to targetPipe.
		// Leave progress output to show it when we run "snap delta".
		unsquashTrgArg := append([]string{}, unsquashfsTuningGenerate...)
		unsquashTrgArg = append(unsquashTrgArg, "-pf", targetPipe, targetSnap)
		unsquashTrgCmd, err := snapdtoolCommandFromSystemSnapWithContext(
			ctx, "/usr/bin/unsquashfs", unsquashTrgArg...)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot find unsquashfs: %w", err)
		}
		// Compress (-e), force overwrite (-f), no app header (-A), source from sourcePipe (-s)
		xdelta3Arg := append([]string{}, xdelta3Tuning...)
		xdelta3Arg = append(xdelta3Arg, "-e", "-f", "-A", "-s", sourcePipe, targetPipe)
		xdelta3Cmd, err := snapdtoolCommandFromSystemSnapWithContext(
			ctx, "/usr/bin/xdelta3", xdelta3Arg...)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot find xdelta3: %w", err)
		}
		// Output to the file where we already wrote the header
		xdelta3Cmd.Stdout = deltaFile

		return []*exec.Cmd{unsquashSrcCmd, unsquashTrgCmd, xdelta3Cmd}, nil, nil
	}

	return osutilRunManyWithContext(ctx, buildPipeline)
}

// applyXdelta3Delta runs in parallel unsquashfs (to get the pseudo-file from
// sourceSnap), a goroutine (sends the delta information in deltaFile to
// xdelta3), xdelta3 (to apply the delta) and mksquashfs (to re-create
// targetSnap).
//
// Named pipes are used to stream the data to xdelta3. The goroutine is needed
// as we have to remove the snap delta header that is stored before the xdelta3
// data in deltaFile. We can use a regular pipe to stream data between xdelta3
// and mksquashfs, as the former can write to stdout and the latter can read
// the pseudo-file from stdin. This diagram summarizes this:
//
//	+-----------------+
//	|   sourceSnap    | (SquashFS file)
//	+-------+---------+
//	        |
//	        v
//	+-----------------+      [named pipe: srcPipe]      +-----------------+
//	|   unsquashfs    | ------------------------------> |                 |
//	| (pseudo-format) |                                 |     xdelta3     |
//	+-----------------+                                 |  (apply delta)  |
//	                                                    |                 |
//	+-----------------+      [named pipe: deltaPipe]    |                 |
//	|  deltaWriter    | ------------------------------> |                 |
//	|  (goroutine)    | (raw xdelta3 data)              +--------+--------+
//	+-------+---------+                                          |
//	        ^                                                    | (stdout pipe)
//	        |                                                    |
//	+-------+---------+                                          v
//	|    deltaFile    |                                 +-----------------+
//	| (Seek past 32b) |                                 |   mksquashfs    |
//	+-----------------+                                 | (rebuild snap)  |
//	                                                    +--------+--------+
//	                                                             |
//	                                                             v
//	                                                    +-----------------+
//	                                                    |   targetSnap    |
//	                                                    +-----------------+
func applyXdelta3Delta(ctx context.Context, sourceSnap, targetSnap string, deltaFile *os.File, mksqfsHdrArgs []string) error {
	// setup pipes to apply delta
	tempDir, pipes, err := setupPipes("src", "delta")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	srcPipe := pipes[0]
	deltaPipe := pipes[1]

	buildPipeline := func(ctx context.Context) ([]*exec.Cmd, []func() error, error) {
		// Output to srcPipe, -pf stands for pseudo-file representation
		unsquashCmd, err := snapdtoolCommandFromSystemSnapWithContext(
			ctx, "/usr/bin/unsquashfs",
			"-no-progress", "-pf", srcPipe, sourceSnap)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot find unsquashfs: %w", err)
		}
		// Decompress (-d), force overwrite (-f), source from srcPipe (-s),
		// delta from deltaPipe, output is to stdout
		xdelta3Cmd, err := snapdtoolCommandFromSystemSnapWithContext(
			ctx, "/usr/bin/xdelta3",
			"-d", "-f", "-s", srcPipe, deltaPipe)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot find xdelta3: %w", err)
		}
		// Source from stdin (-), create targetSnap, pseudo-file from stdin
		// (-pf -), not append to existing filesystem, quiet, append additional
		// args built from our header.
		mksquashArgs := append([]string{
			"-", targetSnap, "-pf", "-", "-noappend", "-quiet",
		}, mksqfsHdrArgs...)
		mksquashCmd, err := snapdtoolCommandFromSystemSnapWithContext(
			ctx, "/usr/bin/mksquashfs", mksquashArgs...)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot find mksquashfs: %w", err)
		}
		// Shows progress when creating squashfs.
		// TODO make this happen only in "snap apply" command
		mksquashCmd.Stdout = os.Stdout
		// Connect xdelta3 output to mksquashfs input
		mksquashCmd.Stdin, err = xdelta3Cmd.StdoutPipe()
		if err != nil {
			return nil, nil, fmt.Errorf("while connecting xdelta to mksqfs: %w", err)
		}

		// task that writes to deltaPipe named FIFO
		deltaWriter := func() error {
			pf, err := os.OpenFile(deltaPipe, os.O_WRONLY, 0)
			if err != nil {
				return err
			}
			defer pf.Close()
			// seek past header
			if _, err := deltaFile.Seek(deltaHeaderSize, io.SeekStart); err != nil {
				return err
			}
			// If there is an error in one of the processes, all of them
			// will be killed by RunManyWithContext, which will in turn
			// close the named pipe and we will return with error here.
			if _, err := copyBuffer(pf, deltaFile); err != nil {
				return err
			}
			return nil
		}
		return []*exec.Cmd{unsquashCmd, mksquashCmd, xdelta3Cmd}, []func() error{deltaWriter}, nil
	}

	return osutilRunManyWithContext(ctx, buildPipeline)
}

// setupPipes creates a temporary directory and named pipes within it.
func setupPipesImpl(pipeNames ...string) (string, []string, error) {
	tempDir, err := os.MkdirTemp("", "snap-delta-")
	if err != nil {
		return "", nil, fmt.Errorf("cannot create temp dir: %w", err)
	}

	pipePaths := make([]string, 0, len(pipeNames))
	for _, name := range pipeNames {
		pipePath := filepath.Join(tempDir, name)
		if err := syscall.Mkfifo(pipePath, 0600); err != nil {
			os.RemoveAll(tempDir) // cleanup
			return "", nil, fmt.Errorf("cannot create fifo %s: %w", pipePath, err)
		}
		pipePaths = append(pipePaths, pipePath)
	}

	return tempDir, pipePaths, nil
}

// compIdToMksquashfsArgs converts SquashFS compression ID to a name.
func compIdToMksquashfsArgs(id uint16, mksqfsArgs []string) ([]string, error) {
	// compression map from squashfs spec
	m := map[uint16]string{1: "gzip", 2: "lzma", 3: "lzo", 4: "xz", 5: "lz4", 6: "zstd"}
	if s, ok := m[id]; ok {
		return append(mksqfsArgs, "-comp", s), nil
	}
	return nil, fmt.Errorf("unknown compression id: %d", id)
}

// superBlockFlagsToMksquashfsArgs converts SquashFS flags to mksquashfs arguments.
func superBlockFlagsToMksquashfsArgs(flags uint16, mksqfsArgs []string) ([]string, error) {
	// Always unset according to spec
	if (flags & flagCheck) != 0 {
		return nil, fmt.Errorf("unexpected value in superblock flags")
	}
	if (flags & flagNoFragments) != 0 {
		mksqfsArgs = append(mksqfsArgs, "-no-fragments")
	}
	// Note: The flag is "DUPLICATES", so if it's *not* set add -no-duplicates.
	if (flags & flagDuplicates) == 0 {
		mksqfsArgs = append(mksqfsArgs, "-no-duplicates")
	}
	if (flags & flagExports) != 0 {
		mksqfsArgs = append(mksqfsArgs, "-exports")
	}
	if (flags & flagNoXattrs) != 0 {
		mksqfsArgs = append(mksqfsArgs, "-no-xattrs")
	}
	if (flags & flagCompressorOptions) != 0 {
		return nil, fmt.Errorf("compression options was set in target, which is unsupported")
	}

	return mksqfsArgs, nil
}
