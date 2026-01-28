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

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

// This file implements the support for snap deltas. Currently two formats are
// supported:
//
// - plain xdelta3 diff file on the compressed snaps
// - xdelta3 diff on an uncompressed representation of the snap files defined
//   by squashfs-tools called pseudo-files
//
// The format supporting pseudo-files has files with a header preceding the
// xdelta3 information. This header is padded to 'deltaHeaderSize' size to
// allow for future fields. Current definition is (bytes in each field are in
// little endian order):
//
// |       32b    |   8b  |   8b  |     16b    |     32b    |     16b     |        16b        |
// | magic number | major | minor | delta tool | time stamp | compression | super block flags |
//
// Magic number is "sqpf" in ASCII, major and minor define the format version
// and are 0x01 at the moment. Delta tool identifies the tools used to
// calculate the deltas, which is 0x01 for xdelta3.
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

type DeltaFormat int

const (
	// Identifiers for the formats in the API
	Xdelta3Format DeltaFormat = iota
	SnapXdelta3Format

	// Identifiers for the store
	xdelta3Format          = "xdelta3"
	snapDeltaFormatXdelta3 = "snap-delta-v1-xdelta3"
)

const (
	// xdelta3 header, see https://datatracker.ietf.org/doc/html/rfc3284
	xdelta3MagicNumber = uint32(0x00c4c3d6)
)

// Snap delta format constants
const (
	// Delta Header Configuration
	deltaHeaderSize = 32

	// Magic number for our delta files: "sqpf" in hex
	deltaMagicNumber        = uint32(0x66707173)
	deltaFormatMajorVersion = uint8(0x01)
	deltaFormatMinorVersion = uint8(0x01)

	// Tool IDs
	DeltaToolXdelta3 = uint16(0x1)
)

// squashfs format constants
const (
	// Offsets in squashfs file
	sqModificationTimeOffset = 8
	sqCompressionIdOffset    = 20
	sqSuperblockFlagsOffset  = 24
	sqMajorVersionOffset     = 28
	sqMinorVersionOffset     = 30
	sqRootInodeRefOffset     = 32
	// SquashFS Superblock Flags
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

var (
	osutilRunManyWithContext = osutil.RunManyWithContext
	osutilRunWithContext     = osutil.RunWithContext
)

// SnapDeltaHeader is the header wrapping the actual delta stream. See
// description above.
type SnapDeltaHeader struct {
	Magic        uint32
	MajorVersion uint8
	MinorVersion uint8
	DeltaTool    uint16
	Timestamp    uint32
	Compression  uint16
	Flags        uint16
}

// fillDeltaHeaderFromSnap reads modification_time, compression_id, and flags
// from a squashfs superblock and writes that in the corresponding delta header
// fields.
func (h *SnapDeltaHeader) fillDeltaHeaderFromSnap(f *os.File) error {
	buf := make([]byte, sqRootInodeRefOffset) // Read enough for all fields
	// ReadAt will return error too if read bytes < len(buf)
	if _, err := f.ReadAt(buf, 0); err != nil {
		return fmt.Errorf("while reading target: %w", err)
	}

	// Timestamp @ offset 8 (u32)
	h.Timestamp = binary.LittleEndian.Uint32(buf[sqModificationTimeOffset : sqModificationTimeOffset+4])
	// Compression @ offset 20 (u16)
	h.Compression = binary.LittleEndian.Uint16(buf[sqCompressionIdOffset : sqCompressionIdOffset+2])
	// Flags @ offset 24 (u16)
	h.Flags = binary.LittleEndian.Uint16(buf[sqSuperblockFlagsOffset : sqSuperblockFlagsOffset+2])
	if h.Flags&flagCompressorOptions != 0 {
		return fmt.Errorf("compression options section present in target, which is unsupported")
	}
	// Major/minor @ offset 28, (2*u16)
	// We expect squashfs 4.0 format
	major := binary.LittleEndian.Uint16(buf[sqMajorVersionOffset : sqMajorVersionOffset+2])
	minor := binary.LittleEndian.Uint16(buf[sqMinorVersionOffset : sqMinorVersionOffset+2])
	if major != 4 || minor != 0 {
		return fmt.Errorf("unexpected squashfs version %d.%d", major, minor)
	}

	return nil
}

// toBytes serialises a delta header to a buffer of deltaHeaderSize length.
func (h *SnapDeltaHeader) toBytes() ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, h.Magic); err != nil {
		return nil, fmt.Errorf("failed to write header magic: %w", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, h.MajorVersion); err != nil {
		return nil, fmt.Errorf("failed to write header major version: %w", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, h.MinorVersion); err != nil {
		return nil, fmt.Errorf("failed to write header minor version: %w", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, h.DeltaTool); err != nil {
		return nil, fmt.Errorf("failed to write header tooling: %w", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, h.Timestamp); err != nil {
		return nil, fmt.Errorf("failed to write header timestamp: %w", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, h.Compression); err != nil {
		return nil, fmt.Errorf("failed to write header compression: %w", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, h.Flags); err != nil {
		return nil, fmt.Errorf("failed to write header flags: %w", err)
	}
	// Pad to full size
	if buf.Len() < deltaHeaderSize {
		buf.Write(make([]byte, deltaHeaderSize-buf.Len()))
	}
	return buf.Bytes(), nil
}

// Pool for large IO buffers (1MB) to reduce GC pressure during io.Copy
var ioBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, copyBufferSize)
		return &b
	},
}

// Helper to copy using pooled buffers
func copyBuffer(dst io.Writer, src io.Reader) (int64, error) {
	bufPtr := ioBufPool.Get().(*[]byte)
	defer ioBufPool.Put(bufPtr)
	return io.CopyBuffer(dst, src, *bufPtr)
}

func formatStoreString(id DeltaFormat) string {
	switch id {
	case Xdelta3Format:
		return xdelta3Format
	case SnapXdelta3Format:
		return snapDeltaFormatXdelta3
	}
	return "unexpected"
}

// Supported delta formats
func SupportedDeltaFormats() []string {
	return []string{formatStoreString(SnapXdelta3Format), formatStoreString(Xdelta3Format)}
}

// GenerateDelta creates a delta file called delta from sourceSnap and
// targetSnap, using deltaFormat.
func GenerateDelta(sourceSnap, targetSnap, delta string, deltaFormat DeltaFormat) error {
	// Context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	switch deltaFormat {
	case Xdelta3Format:
		// Plain xdelta3 on compressed files
		return generatePlainXdelta3Delta(ctx, sourceSnap, targetSnap, delta)
	case SnapXdelta3Format:
		return generateSnapDelta(ctx, sourceSnap, targetSnap, delta)
	default:
		return fmt.Errorf("unsupported delta format %d", deltaFormat)
	}
}

func generateSnapDelta(ctx context.Context, sourceSnap, targetSnap, delta string) error {
	// we need to get some basic info from the target snap
	targetFile, err := os.Open(targetSnap)
	if err != nil {
		return fmt.Errorf("cannot open target: %w", err)
	}
	defer targetFile.Close()

	// Build delta header, using the target header. Note that currently the
	// only supported delta tool is DeltaToolXdelta3.
	hdr := SnapDeltaHeader{
		Magic:        deltaMagicNumber,
		MajorVersion: deltaFormatMajorVersion,
		MinorVersion: deltaFormatMinorVersion,
		DeltaTool:    DeltaToolXdelta3,
	}
	if err := hdr.fillDeltaHeaderFromSnap(targetFile); err != nil {
		return err
	}

	headerBytes, err := hdr.toBytes()
	if err != nil {
		return fmt.Errorf("build delta header: %w", err)
	}

	// Create delta file and write header
	deltaFile, err := os.OpenFile(delta, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("create delta file: %w", err)
	}
	defer deltaFile.Close()
	if _, err := deltaFile.Write(headerBytes); err != nil {
		return fmt.Errorf("write delta header: %w", err)
	}

	return generateXdelta3Delta(ctx, deltaFile, sourceSnap, targetSnap)
}

// ApplyDelta uses sourceSnap and delta files to generate targetSnap.
func ApplyDelta(sourceSnap, delta, targetSnap string) error {
	// Global Context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deltaFile, err := os.Open(delta)
	if err != nil {
		return fmt.Errorf("open delta: %w", err)
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
		return applyPlainXdelta3Delta(ctx, sourceSnap, delta, targetSnap)
	case deltaMagicNumber:
		if n < deltaHeaderSize {
			return fmt.Errorf("snap delta header too short (%d bytes read)", n)
		}
		hdr := &SnapDeltaHeader{}
		if err := binary.Read(bytes.NewReader(buf), binary.LittleEndian, hdr); err != nil {
			return fmt.Errorf("decode header: %w", err)
		}
		return applySnapDelta(ctx, sourceSnap, targetSnap, deltaFile, hdr)
	default:
		return fmt.Errorf("unknown delta file format")
	}
}

func applySnapDelta(ctx context.Context, sourceSnap, targetSnap string, deltaFile *os.File, hdr *SnapDeltaHeader) error {
	if hdr.Magic != deltaMagicNumber {
		return fmt.Errorf("invalid magic 0x%X", hdr.Magic)
	}
	// Note that we consider minor version backwards compatible, and that
	// maybe it introduces new features that might not be supported by the
	// installed snapd.
	if hdr.MajorVersion != deltaFormatMajorVersion {
		return fmt.Errorf("incompatible major version %d (needs to be <= %d)",
			hdr.MajorVersion, deltaFormatMajorVersion)
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

	// run delta apply for given deta tool - DeltaToolXdelta3 is the only supported one atm
	return applyXdelta3Delta(ctx, sourceSnap, targetSnap, deltaFile, mksqfsArgs)
}

// generatePlainXdelta3Delta generates a delta between compressed snaps
func generatePlainXdelta3Delta(ctx context.Context, sourceSnap, targetSnap, delta string) error {
	// Compression level, force overwrite (-f), compress (-e), source (-s <file>), target, delta
	opts := append([]string{}, xdelta3PlainTuning...)
	opts = append(opts, "-f", "-e", "-s", sourceSnap, targetSnap, delta)
	cmd, err := snapdtoolCommandFromSystemSnap("/usr/bin/xdelta3", opts...)
	if err != nil {
		return fmt.Errorf("cannot generate delta: %v", err)
	}

	return osutilRunWithContext(ctx, cmd)
}

// applyPlainXdelta3Delta applies a delta between compressed snaps
func applyPlainXdelta3Delta(ctx context.Context, sourceSnap, delta, targetSnap string) error {
	// Force overwrite (-f), decompress (-d), source (-s <file>), target, delta
	cmd, err := snapdtoolCommandFromSystemSnap("/usr/bin/xdelta3",
		"-f", "-d", "-s", sourceSnap, delta, targetSnap)
	if err != nil {
		return fmt.Errorf("cannot apply delta: %v", err)
	}

	return osutilRunWithContext(ctx, cmd)
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

	// Output to sourcePipe, -pf stands for pseudo-file representation
	unsquashSrcArg := append([]string{}, unsquashfsTuningGenerate...)
	unsquashSrcArg = append(unsquashSrcArg, "-no-progress", "-pf", sourcePipe, sourceSnap)
	unsquashSrcCmd, err := snapdtoolCommandFromSystemSnap("/usr/bin/unsquashfs", unsquashSrcArg...)
	if err != nil {
		return fmt.Errorf("cannot find unsquashfs: %v", err)
	}
	// Output to targetPipe.
	// Leave progress output to show it when we run "snap delta".
	unsquashTrgArg := append([]string{}, unsquashfsTuningGenerate...)
	unsquashTrgArg = append(unsquashTrgArg, "-pf", targetPipe, targetSnap)
	unsquashTrgCmd, err := snapdtoolCommandFromSystemSnap("/usr/bin/unsquashfs", unsquashTrgArg...)
	if err != nil {
		return fmt.Errorf("cannot find unsquashfs: %v", err)
	}
	// Compress (-e), force overwrite (-f), no app header (-A), source from sourcePipe (-s)
	xdelta3Arg := append([]string{}, xdelta3Tuning...)
	xdelta3Arg = append(xdelta3Arg, "-e", "-f", "-A", "-s", sourcePipe, targetPipe)
	xdelta3Cmd, err := snapdtoolCommandFromSystemSnap("/usr/bin/xdelta3", xdelta3Arg...)
	if err != nil {
		return fmt.Errorf("cannot find xdelta3: %v", err)
	}
	// Output to the file where we already wrote the header
	xdelta3Cmd.Stdout = deltaFile

	cmds := []*exec.Cmd{unsquashSrcCmd, unsquashTrgCmd, xdelta3Cmd}
	return osutilRunManyWithContext(ctx, cmds, nil)
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

	// Output to srcPipe, -pf stands for pseudo-file representation
	unsquashCmd, err := snapdtoolCommandFromSystemSnap("/usr/bin/unsquashfs",
		"-no-progress", "-pf", srcPipe, sourceSnap)
	if err != nil {
		return fmt.Errorf("cannot find unsquashfs: %v", err)
	}
	// Decompress (-d), force overwrite (-f), source from srcPipe (-s),
	// delta from deltaPipe, output is to stdout
	xdelta3Cmd, err := snapdtoolCommandFromSystemSnap("/usr/bin/xdelta3",
		"-d", "-f", "-s", srcPipe, deltaPipe)
	if err != nil {
		return fmt.Errorf("cannot find xdelta3: %v", err)
	}
	// Source from stdin (-), create targetSnap, pseudo-file from stdin
	// (-pf -), not append to existing filesystem, quiet, append additional
	// args built from our header.
	mksquashArgs := append([]string{
		"-", targetSnap, "-pf", "-", "-noappend", "-quiet",
	}, mksqfsHdrArgs...)
	mksquashCmd, err := snapdtoolCommandFromSystemSnap("/usr/bin/mksquashfs", mksquashArgs...)
	if err != nil {
		return fmt.Errorf("cannot find mksquashfs: %v", err)
	}
	// Shows progress when creating squashfs.
	// TODO make this happen only in "snap apply" command
	mksquashCmd.Stdout = os.Stdout
	// Connect xdelta3 output to mksquashfs input
	mksquashCmd.Stdin, err = xdelta3Cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("while connecting xdelta → mksqfs: %w", err)
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

	cmds := []*exec.Cmd{unsquashCmd, mksquashCmd, xdelta3Cmd}
	return osutilRunManyWithContext(ctx, cmds, []func() error{deltaWriter})
}

// setupPipes creates a temporary directory and named pipes within it.
func setupPipes(pipeNames ...string) (string, []string, error) {
	tempDir, err := os.MkdirTemp("", "snap-delta-")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	pipePaths := make([]string, 0, len(pipeNames))
	for _, name := range pipeNames {
		pipePath := filepath.Join(tempDir, name)
		if err := syscall.Mkfifo(pipePath, 0600); err != nil {
			os.RemoveAll(tempDir) // cleanup
			return "", nil, fmt.Errorf("failed to create fifo %s: %w", pipePath, err)
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
	if (flags & flagCheck) != 0 {
		return nil, fmt.Errorf("this does not look like Squashfs 4+ superblock flags")
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
