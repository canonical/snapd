// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2018 Canonical Ltd
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
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/internal"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/strutil"
)

const (
	// https://github.com/plougher/squashfs-tools/blob/master/squashfs-tools/squashfs_fs.h#L289
	superblockSize = 96
)

var (
	// magic is the magic prefix of squashfs snap files.
	magic = []byte{'h', 's', 'q', 's'}

	// for testing
	isRootWritableOverlay = osutil.IsRootWritableOverlay
)

func FileHasSquashfsHeader(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	// a squashfs file would contain at least the superblock + some data
	header := make([]byte, superblockSize+1)
	if _, err := f.ReadAt(header, 0); err != nil {
		return false
	}

	return bytes.HasPrefix(header, magic)
}

// Snap is the squashfs based snap.
type Snap struct {
	path string
}

// Path returns the path of the backing file.
func (s *Snap) Path() string {
	return s.path
}

// New returns a new Squashfs snap.
func New(snapPath string) *Snap {
	return &Snap{path: snapPath}
}

var osLink = os.Link
var snapdtoolCommandFromSystemSnap = snapdtool.CommandFromSystemSnap

// Install installs a squashfs snap file through an appropriate method.
func (s *Snap) Install(targetPath, mountDir string, opts *snap.InstallOptions) (bool, error) {

	// ensure mount-point and blob target dir.
	for _, dir := range []string{mountDir, filepath.Dir(targetPath)} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return false, err
		}
	}

	// This is required so that the tests can simulate a mounted
	// snap when we "install" a squashfs snap in the tests.
	// We can not mount it for real in the tests, so we just unpack
	// it to the location which is good enough for the tests.
	if osutil.GetenvBool("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS") {
		if err := s.Unpack("*", mountDir); err != nil {
			return false, err
		}
	}

	// nothing to do, happens on e.g. first-boot when we already
	// booted with the OS snap but its also in the seed.yaml
	if s.path == targetPath || osutil.FilesAreEqual(s.path, targetPath) {
		didNothing := true
		return didNothing, nil
	}

	overlayRoot, err := isRootWritableOverlay()
	if err != nil {
		logger.Noticef("cannot detect root filesystem on overlay: %v", err)
	}
	// Hard-linking on overlayfs is identical to a full blown
	// copy.  When we are operating on a overlayfs based system (e.g. live
	// installer) use symbolic links.
	// https://bugs.launchpad.net/snapd/+bug/1867415
	if overlayRoot == "" {
		// try to (hard)link the file, but go on to trying to copy it
		// if it fails for whatever reason
		//
		// link(2) returns EPERM on filesystems that don't support
		// hard links (like vfat), so checking the error here doesn't
		// make sense vs just trying to copy it.
		if err := osLink(s.path, targetPath); err == nil {
			return false, nil
		}
	}

	// if the installation must not cross devices, then we should not use
	// symlinks and instead must copy the file entirely, this is the case
	// during seeding on uc20 in run mode for example
	if opts == nil || !opts.MustNotCrossDevices {
		// if the source snap file is in seed, but the hardlink failed, symlinking
		// it saves the copy (which in livecd is expensive) so try that next
		// note that on UC20, the snap file could be in a deep subdir of
		// SnapSeedDir, i.e. /var/lib/snapd/seed/systems/20200521/snaps/<name>.snap
		// so we need to check if it has the prefix of the seed dir
		cleanSrc := filepath.Clean(s.path)
		if strings.HasPrefix(cleanSrc, dirs.SnapSeedDir) {
			if os.Symlink(s.path, targetPath) == nil {
				return false, nil
			}
		}
	}

	return false, osutil.CopyFile(s.path, targetPath, osutil.CopyFlagPreserveAll|osutil.CopyFlagSync)
}

// unsquashfsStderrWriter is a helper that captures errors from
// unsquashfs on stderr. Because unsquashfs will potentially
// (e.g. on out-of-diskspace) report an error on every single
// file we limit the reported error lines to 4.
//
// unsquashfs does not exit with an exit code for write errors
// (e.g. no space left on device). There is an upstream PR
// to fix this https://github.com/plougher/squashfs-tools/pull/46
//
// However in the meantime we can detect errors by looking
// on stderr for "failed" which is pretty consistently used in
// the unsquashfs.c source in case of errors.
type unsquashfsStderrWriter struct {
	strutil.MatchCounter
}

var unsquashfsStderrRegexp = regexp.MustCompile(`(?m).*\b[Ff]ailed\b.*`)

func newUnsquashfsStderrWriter() *unsquashfsStderrWriter {
	return &unsquashfsStderrWriter{strutil.MatchCounter{
		Regexp: unsquashfsStderrRegexp,
		N:      4, // note Err below uses this value
	}}
}

func (u *unsquashfsStderrWriter) Err() error {
	// here we use that our N is 4.
	errors, count := u.Matches()
	switch count {
	case 0:
		return nil
	case 1:
		return fmt.Errorf("failed: %q", errors[0])
	case 2, 3, 4:
		return fmt.Errorf("failed: %s, and %q", strutil.Quoted(errors[:len(errors)-1]), errors[len(errors)-1])
	default:
		// count > len(matches)
		extra := count - len(errors)
		return fmt.Errorf("failed: %s, and %d more", strutil.Quoted(errors), extra)
	}
}

// Unpack unpacks the snap to the given directory.
//
// Extended attributes are not preserved. This affects capabilities granted to specific executables.
func (s *Snap) Unpack(src, dstDir string) error {
	usw := newUnsquashfsStderrWriter()

	var output bytes.Buffer
	cmd := exec.Command("unsquashfs", "-no-xattrs", "-n", "-f", "-d", dstDir, s.path, src)
	cmd.Stderr = io.MultiWriter(&output, usw)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cannot extract %q to %q: %v", src, dstDir, osutil.OutputErr(output.Bytes(), err))
	}
	// older versions of unsquashfs do not report errors via exit code,
	// so we need this extra check.
	if usw.Err() != nil {
		return fmt.Errorf("cannot extract %q to %q: %v", src, dstDir, usw.Err())
	}

	return nil
}

// Size returns the size of a squashfs snap.
func (s *Snap) Size() (size int64, err error) {
	st, err := os.Stat(s.path)
	if err != nil {
		return 0, err
	}

	return st.Size(), nil
}

func (s *Snap) withUnpackedFile(filePath string, f func(p string) error) error {
	tmpdir, err := os.MkdirTemp("", "read-file")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpdir)

	unpackDir := filepath.Join(tmpdir, "unpack")
	if output, err := exec.Command("unsquashfs", "-no-xattrs", "-n", "-i", "-d", unpackDir, s.path, filePath).CombinedOutput(); err != nil {
		return fmt.Errorf("cannot run unsquashfs: %v", osutil.OutputErr(output, err))
	}

	return f(filepath.Join(unpackDir, filePath))
}

// RandomAccessFile returns an implementation to read at any given
// location for a single file inside the squashfs snap plus
// information about the file size.
func (s *Snap) RandomAccessFile(filePath string) (interface {
	io.ReaderAt
	io.Closer
	Size() int64
}, error) {
	var f *os.File
	err := s.withUnpackedFile(filePath, func(p string) (err error) {
		f, err = os.Open(p)
		return
	})
	if err != nil {
		return nil, err
	}
	return internal.NewSizedFile(f)
}

// ReadFile returns the content of a single file inside a squashfs snap.
func (s *Snap) ReadFile(filePath string) (content []byte, err error) {
	err = s.withUnpackedFile(filePath, func(p string) (err error) {
		content, err = os.ReadFile(p)
		return
	})
	if err != nil {
		return nil, err
	}
	return content, nil
}

func (s *Snap) ReadLink(filePath string) (string, error) {
	// XXX: This could be optimized by reading a cached version of
	// unsquashfs raw output where the symlink's target is available.
	// Check -> func fromRaw(raw []byte) (*stat, error)
	var target string
	err := s.withUnpackedFile(filePath, func(p string) (err error) {
		target, err = os.Readlink(p)
		return err
	})
	if err != nil {
		return "", err
	}
	return target, nil
}

func (s *Snap) Lstat(filePath string) (os.FileInfo, error) {
	var fileInfo os.FileInfo

	err := s.Walk(filePath, func(path string, info os.FileInfo, err error) error {
		if filePath == path {
			fileInfo = info
		}
		return err
	})
	if err != nil {
		return nil, err
	}

	if fileInfo == nil {
		return nil, os.ErrNotExist
	}

	return fileInfo, nil
}

// skipper is used to track directories that should be skipped
//
// Given sk := make(skipper), if you sk.Add("foo/bar"), then
// sk.Has("foo/bar") is true, but also sk.Has("foo/bar/baz")
//
// It could also be a map[string]bool, but because it's only supposed
// to be checked through its Has method as above, the small added
// complexity of it being a map[string]struct{} lose to the associated
// space savings.
type skipper map[string]struct{}

func (sk skipper) Add(path string) {
	sk[filepath.Clean(path)] = struct{}{}
}

func (sk skipper) Has(path string) bool {
	for p := filepath.Clean(path); p != "." && p != "/"; p = filepath.Dir(p) {
		if _, ok := sk[p]; ok {
			return true
		}
	}

	return false
}

// pre-4.5 unsquashfs writes a funny header like:
//
//	"Parallel unsquashfs: Using 1 processor"
//	"1 inodes (1 blocks) to write"
//	""   <-- empty line
var maybeHeaderRegex = regexp.MustCompile(`^(Parallel unsquashfs: Using .* processor.*|[0-9]+ inodes .* to write)$`)

// Walk (part of snap.Container) is like filepath.Walk, without the ordering guarantee.
func (s *Snap) Walk(relative string, walkFn filepath.WalkFunc) error {
	relative = filepath.Clean(relative)
	if relative == "" || relative == "/" {
		relative = "."
	} else if relative[0] == '/' {
		// I said relative, darn it :-)
		relative = relative[1:]
	}

	var cmd *exec.Cmd
	if relative == "." {
		cmd = exec.Command("unsquashfs", "-no-progress", "-dest", ".", "-ll", s.path)
	} else {
		cmd = exec.Command("unsquashfs", "-no-progress", "-dest", ".", "-ll", s.path, relative)
	}
	cmd.Env = []string{"TZ=UTC"}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return walkFn(relative, nil, err)
	}
	if err := cmd.Start(); err != nil {
		return walkFn(relative, nil, err)
	}
	defer cmd.Process.Kill()

	scanner := bufio.NewScanner(stdout)
	skipper := make(skipper)
	seenHeader := false
	for scanner.Scan() {
		raw := scanner.Bytes()
		if !seenHeader {
			// try to match the header written by older (pre-4.5)
			// squashfs tools
			if len(scanner.Bytes()) == 0 ||
				maybeHeaderRegex.Match(raw) {
				continue
			} else {
				seenHeader = true
			}
		}
		st, err := fromRaw(raw)
		if err != nil {
			err = walkFn(relative, nil, err)
			if err != nil {
				return err
			}
		} else {
			path := filepath.Join(".", st.Path())
			if skipper.Has(path) {
				continue
			}
			// skip if path is not under given relative path
			if relative != "." && !strings.HasPrefix(path, relative) {
				continue
			}
			err = walkFn(path, st, nil)
			if err != nil {
				if err == filepath.SkipDir && st.IsDir() {
					skipper.Add(path)
				} else {
					return err
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return walkFn(relative, nil, err)
	}

	if err := cmd.Wait(); err != nil {
		return walkFn(relative, nil, err)
	}
	return nil
}

// ListDir returns the content of a single directory inside a squashfs snap.
func (s *Snap) ListDir(dirPath string) ([]string, error) {
	output, stderr, err := osutil.RunSplitOutput(
		"unsquashfs", "-no-progress", "-dest", "_", "-l", s.path, dirPath)
	if err != nil {
		return nil, osutil.OutputErrCombine(output, stderr, err)
	}

	prefixPath := path.Join("_", dirPath)
	pattern, err := regexp.Compile("(?m)^" + regexp.QuoteMeta(prefixPath) + "/([^/\r\n]+)$")
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot compile squashfs list dir regexp for %q: %s", dirPath, err)
	}

	var directoryContents []string
	for _, groups := range pattern.FindAllSubmatch(output, -1) {
		if len(groups) > 1 {
			directoryContents = append(directoryContents, string(groups[1]))
		}
	}

	return directoryContents, nil
}

const maxErrPaths = 10

type errPathsNotReadable struct {
	paths []string
}

func (e *errPathsNotReadable) accumulate(p string, fi os.FileInfo) error {
	if len(e.paths) >= maxErrPaths {
		return e
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		e.paths = append(e.paths, fmt.Sprintf("%s (owner %v:%v mode %#03o)", p, st.Uid, st.Gid, fi.Mode().Perm()))
	} else {
		e.paths = append(e.paths, p)
	}
	return nil
}

func (e *errPathsNotReadable) asErr() error {
	if len(e.paths) > 0 {
		return e
	}
	return nil
}

func (e *errPathsNotReadable) Error() string {
	var b bytes.Buffer

	b.WriteString("cannot access the following locations in the snap source directory:\n")
	for _, p := range e.paths {
		fmt.Fprintf(&b, "- %s\n", p)
	}
	if len(e.paths) == maxErrPaths {
		fmt.Fprintf(&b, "- too many errors, listing first %v entries\n", maxErrPaths)
	}
	return b.String()
}

// verifyContentAccessibleForBuild checks whether the content under source
// directory is usable to the user and can be represented by mksquashfs.
func verifyContentAccessibleForBuild(sourceDir string) error {
	var errPaths errPathsNotReadable

	withSlash := filepath.Clean(sourceDir) + "/"
	err := filepath.Walk(withSlash, func(path string, st os.FileInfo, err error) error {
		if err != nil {
			if !os.IsPermission(err) {
				return err
			}
			// accumulate permission errors
			return errPaths.accumulate(strings.TrimPrefix(path, withSlash), st)
		}
		mode := st.Mode()
		if !mode.IsRegular() && !mode.IsDir() {
			// device nodes are just recreated by mksquashfs
			return nil
		}
		if mode.IsRegular() && st.Size() == 0 {
			// empty files are also recreated
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			if !os.IsPermission(err) {
				return err
			}
			// accumulate permission errors
			if err = errPaths.accumulate(strings.TrimPrefix(path, withSlash), st); err != nil {
				return err
			}
			// workaround for https://github.com/golang/go/issues/21758
			// with pre 1.10 go, explicitly skip directory
			if mode.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		f.Close()
		return nil
	})
	if err != nil {
		return err
	}
	return errPaths.asErr()
}

type MksquashfsError struct {
	msg string
}

func (m MksquashfsError) Error() string {
	return m.msg
}

type BuildOpts struct {
	SnapType     string
	Compression  string
	ExcludeFiles []string
}

// MinimumSnapSize is the smallest size a snap can be. The kernel attempts to read a
// partition table from the snap when a loopback device is created from it. If the snap
// is smaller than this size, some versions of the kernel will print error logs while
// scanning the loopback device for partitions.
// TODO: revisit if necessary, some distros (eg. openSUSE) patch squashfs-tools to pad to 64k but
// kernel should work with this
const MinimumSnapSize int64 = 16384

// Build builds the snap.
func (s *Snap) Build(sourceDir string, opts *BuildOpts) error {
	if opts == nil {
		opts = &BuildOpts{}
	}
	if err := verifyContentAccessibleForBuild(sourceDir); err != nil {
		return err
	}

	fullSnapPath, err := filepath.Abs(s.path)
	if err != nil {
		return err
	}
	// default to xz
	compression := opts.Compression
	if compression == "" {
		// TODO: support other compression options, xz is very
		// slow for certain apps, see
		// https://forum.snapcraft.io/t/squashfs-performance-effect-on-snap-startup-time/13920
		compression = "xz"
	}
	cmd, err := snapdtoolCommandFromSystemSnap("/usr/bin/mksquashfs")
	if err != nil {
		cmd = exec.Command("mksquashfs")
	}
	cmd.Args = append(cmd.Args,
		".", fullSnapPath,
		"-noappend",
		"-comp", compression,
		"-no-fragments",
		"-no-progress",
	)

	if len(opts.ExcludeFiles) > 0 {
		cmd.Args = append(cmd.Args, "-wildcards")
		for _, excludeFile := range opts.ExcludeFiles {
			cmd.Args = append(cmd.Args, "-ef", excludeFile)
		}
	}
	snapType := opts.SnapType
	switch snapType {
	case "os", "core", "base", "snapd":
		// -xattrs is default, but let's be explicit about it
		cmd.Args = append(cmd.Args, "-xattrs")
	default:
		cmd.Args = append(cmd.Args, "-all-root", "-no-xattrs")
	}

	err = osutil.ChDir(sourceDir, func() error {
		output, err := cmd.CombinedOutput()
		if err != nil {
			return MksquashfsError{fmt.Sprintf("mksquashfs call failed: %s", osutil.OutputErr(output, err))}
		}

		return nil
	})
	if err != nil {
		return err
	}

	// Grow the snap if it is smaller than the minimum snap size. See
	// MinimumSnapSize for more details.
	return s.growSnapToMinSize(MinimumSnapSize)
}

// BuildDate returns the "Creation or last append time" as reported by unsquashfs.
func (s *Snap) BuildDate() time.Time {
	return BuildDate(s.path)
}

func (s *Snap) growSnapToMinSize(minSize int64) error {
	size, err := s.Size()
	if err != nil {
		return fmt.Errorf("cannot get size of snap: %w", err)
	}
	if size >= minSize {
		return nil
	}
	if err := os.Truncate(s.path, minSize); err != nil {
		return fmt.Errorf("cannot grow snap to minimum size: %w", err)
	}

	return nil
}

// BuildDate returns the "Creation or last append time" as reported by unsquashfs.
func BuildDate(path string) time.Time {
	var t0 time.Time

	const prefix = "Creation or last append time "
	m := &strutil.MatchCounter{
		Regexp: regexp.MustCompile("(?m)^" + prefix + ".*$"),
		N:      1,
	}

	cmd := exec.Command("unsquashfs", "-n", "-s", path)
	cmd.Env = []string{"TZ=UTC"}
	cmd.Stdout = m
	cmd.Stderr = m
	if err := cmd.Run(); err != nil {
		return t0
	}
	matches, count := m.Matches()
	if count != 1 {
		return t0
	}
	t0, _ = time.Parse(time.ANSIC, matches[0][len(prefix):])
	return t0
}

// snap delta support
// Custom Delta header (padded to 'deltaHeaderSize' size)
// generated delta is using following custom header to capture the delta content
// |       32b    |   16b   |    32b     |     16b     |        16b        |
// | magic number | version | time stamp | compression | super block flags |
// reference squashfs supperblock https://dr-emann.github.io/squashfs
// Optional compressor options are currently not supported, if target squashfs is detected to
// use those, we fallback to plain xdelta
// Delta between two snaps(squashfs) is generated on the squashfs pseudo file definition
// this represents uncompressed content of the squashfs packages, custom header data is
// later used as input parameters to mksquashfs when recreated target squashfs from the
// reconstructed pseudo file definition

// --- Constants & Configuration ---

const (
	// Delta Header Configuration
	deltaHeaderSize    = 32
	deltaFormatVersion = uint16(0x101)
	deltaMagicNumber   = uint32(0xF989789C)
	xdelta3MagicNumber = uint32(0x00c4c3d6)

	// Format Identifiers
	snapDeltaFormatXdelta3 = "snapDeltaV1Xdelta3"
	snapDeltaFormatHdiffz  = "snapDeltaV1Hdiffz"
	xdelta3Format          = "xdelta3"

	// Tool IDs
	detlaToolXdelta3 = uint16(0x1)
	detlaToolHdiffz  = uint16(0x2)
	defaultDeltaTool = detlaToolXdelta3

	// SquashFS Superblock Flags
	flagCheck             uint16 = 0x0004
	flagNoFragments       uint16 = 0x0010
	flagDuplicates        uint16 = 0x0040 // Note: logic is inverted (default is duplicates)
	flagExports           uint16 = 0x0080
	flagNoXattrs          uint16 = 0x0200
	flagCompressorOptions uint16 = 0x0400
)

// Tuning Parameters
var (
	// xdelta3 tuning
	// default compression level assumed 3
	// plain squashfs to squashfs delta size has no measurable gain between  3 and 9 comp level
	xdelta3PlainTuning = []string{"-3"}
	// gain in delta pseudo file between 3 and 7 comp level is 10 to 20% size reduction
	// delta size gain flattens at 7
	// no noticeable gain from changing source window size(-B) or bytes input window(-W)
	// or size compression duplicates window (-P)
	xdelta3Tuning = []string{"-7"}

	// hdiffz tuning
	hdiffzTuning  = []string{"-m-6", "-SD", "-c-zstd-21-24", "-d"}
	hpatchzTuning = []string{"-s-8m"}

	// unsquashfs tuning
	// by default unsquashfs would allocated ~2x256 for any size of squashfs image
	// We need to tame it down use different tuning for:
	// - generating detla: running server side -> no tuning, we have memory
	// - apply delta: possibly low spec systems
	unsquashfsTuningGenerate = []string{"-da", "128", "-fr", "128"}
	unsquashfsTuningApply    = []string{"-da", "8", "-fr", "8"}

	// mksquashfs tuning
	// by default mksquashfs can grab up to 25% of the physical memory
	// limit this as we migh run on contrained systems
	mksquashfsTuningApply = []string{"-mem-percent", "10"}

	// IO buffer size for efficient piping (1MB)
	CopyBufferSize = 1024 * 1024
)

// --- Structs ---

// custom delta format header wrapping actual delta stream
type SnapDeltaHeader struct {
	Magic       uint32
	Version     uint16
	DeltaTool   uint16
	Timestamp   uint32
	Compression uint16
	Flags       uint16
}

// PseudoEntry represents a parsed line from the definition
type PseudoEntry struct {
	FilePath string
	Type     string
	// only we only care about size of offset
	DataSize      int64
	DataOffset    int64
	OriginalIndex int // index to the pseudo definition header
}

// type DeltaToolingCmd func(ctx context.Context, args ...string) *exec.Cmd
type DeltaToolingCmd func(args ...string) *exec.Cmd

// --- Memory Pools ---

// Pool for small bytes.Buffer
var bufferPool = sync.Pool{
	New: func() interface{} { return new(bytes.Buffer) },
}

// Pool for large IO buffers (1MB) to reduce GC pressure during io.Copy
var ioBufPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, CopyBufferSize)
		return &b
	},
}

// Helper to copy using pooled buffers
func copyBuffer(dst io.Writer, src io.Reader) (int64, error) {
	bufPtr := ioBufPool.Get().(*[]byte)
	defer ioBufPool.Put(bufPtr)
	return io.CopyBuffer(dst, src, *bufPtr)
}

func copyNBuffer(dst io.Writer, src io.Reader, n int64) (int64, error) {
	bufPtr := ioBufPool.Get().(*[]byte)
	defer ioBufPool.Put(bufPtr)
	return io.CopyBuffer(dst, io.LimitReader(src, n), *bufPtr)
}

// Supported version of the snap delta algorythms
func SupportedDeltaFormats() string {
	return snapDeltaFormatHdiffz + "," + snapDeltaFormatXdelta3 + "," + xdelta3Format
}

// generate delta file.
func GenerateSnapDelta(sourceSnap, targetSnap, delta string, deltaTool uint16) error {
	logger.Noticef("Generating delta...")

	// we need to get some basic info from the target snap
	f, err := os.Open(targetSnap)
	if err != nil {
		return fmt.Errorf("open target: %w", err)
	}
	defer f.Close()

	// Check compressor options flag
	var flagsBuf [2]byte
	if _, err := f.ReadAt(flagsBuf[:], 24); err != nil {
		return fmt.Errorf("read flags: %w", err)
	}
	if binary.LittleEndian.Uint16(flagsBuf[:])&flagCompressorOptions != 0 {
		logger.Noticef("Custom compression options detected. Falling back to plain xdelta3.")
		return generatePlainXdelta3Delta(ctx, sourceSnap, targetSnap, delta)
	}

	// Build delta header
	hdr := SnapDeltaHeader{
		Magic:     deltaMagicNumber,
		Version:   deltaFormatVersion,
		DeltaTool: deltaTool,
	}

	if err := hdr.loadDeltaHeaderFromSnap(f); err != nil {
		return err
	}

	headerBytes, err := hdr.toBytes()
	if err != nil {
		return fmt.Errorf("build delta header: %w", err)
	}

	// prepare delta file
	deltaFile, err := os.OpenFile(delta, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("create delta file: %w", err)
	}
	defer deltaFile.Close()

	if _, err := deltaFile.Write(headerBytes); err != nil {
		return fmt.Errorf("write delta header: %w", err)
	}

	// run delta producer for given deta tool
	switch deltaTool {
	case detlaToolXdelta3:
		return generateXdelta3Delta(ctx, deltaFile, sourceSnap, targetSnap)
	case detlaToolHdiffz:
		return generateHdiffzDelta(ctx, deltaFile, sourceSnap, targetSnap)
	default:
		return fmt.Errorf("Unknown delta tool requested: 0x%X\n", deltaTool)
	}
}

// --- Apply Delta ---
func ApplySnapDelta(sourceSnap, delta, targetSnap string) error {
	logger.Noticef("Applying delta...")

	deltaFile, err := os.Open(delta)
	if err != nil {
		return fmt.Errorf("open delta: %w", err)
	}
	defer deltaFile.Close()

	// get delta header and check it
	hdr, err := readDeltaHeader(deltaFile)
	if err != nil {
		return err
	}

	if hdr.Magic == xdelta3MagicNumber {
		logger.Noticef("Plain xdelta3 detected; using fallback.")
		return applyPlainXdelta3Delta(ctx, sourceSnap, delta, targetSnap)
	}

	if hdr.Magic != deltaMagicNumber {
		return fmt.Errorf("invalid magic 0x%X", hdr.Magic)
	}
	if hdr.Version != deltaFormatVersion {
		return fmt.Errorf("version mismatch %d!=%d", hdr.Version, deltaFormatVersion)
	}
	if hdr.DeltaTool != detlaToolXdelta3 && hdr.DeltaTool != detlaToolHdiffz {
		return fmt.Errorf("unsupported delta tool %d", hdr.DeltaTool)
	}

	// Prepare mksquashfs arguments from delta header
	mksqfsArgs := []string{}
	if mksqfsArgs, err = parseCompression(hdr.Compression, mksqfsArgs); err != nil {
		return fmt.Errorf("failed to parse compression from delta header:%v", err)
	}
	if mksqfsArgs, err = parseSuperblockFlags(hdr.Flags, mksqfsArgs); err != nil {
		return fmt.Errorf("failed to parse flags from delta header:%v", err)
	}
	// run delta apply for given deta tool
	switch hdr.DeltaTool {
	case detlaToolXdelta3:
		return applyXdelta3Delta(ctx, sourceSnap, targetSnap, deltaFile, hdr, mksqfsArgs)
	case detlaToolHdiffz:
		return applyHdiffzDelta(ctx, sourceSnap, targetSnap, deltaFile, hdr, mksqfsArgs)
	default:
		return fmt.Errorf("Unknown delta tool requested: 0x%X\n", hdr.DeltaTool)
	}

}

func generateXdelta3Delta(ctx context.Context, deltaFile *os.File, sourceSnap, targetSnap string) error {
	// Setup Context & ErrGroup
	g, gctx := errgroup.WithContext(ctx)

	// Ensure we have required tooling
	supportedFormats, xdelta3ToolCmdFn, _, unsquashfsCmdFn, _, _, err := CheckSupportedDetlaFormats(gctx)
	if err != nil || !strings.Contains(supportedFormats, snapDeltaFormatXdelta3) {
		return fmt.Errorf("missing delta tooling for xdelta3: %v", err)
	}

	// Setup all pipes
	tempDir, pipes, err := setupPipes("source-pipe", "target-pipe")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	sourcePipe := pipes[0]
	targetPipe := pipes[1]

	// Prepare all the commands
	// unsquashfs source -> source-pipe
	unsquashSourceArg := append(unsquashfsTuningGenerate, "-n", "-pf", sourcePipe, sourceSnap)
	unsquashSourceCmd := unsquashfsCmdFn(unsquashSourceArg...)
	// unsquashfs target -> target-pipe
	unsquashTargetArg := append(unsquashfsTuningGenerate, "-n", "-pf", targetPipe, targetSnap)
	unsquashTargetCmd := unsquashfsCmdFn(unsquashTargetArg...)
	// xdelta3 source-pipe, target-pipe -> delta-file
	// Note: We use the tuning args and append specific inputs
	xdelta3Args := append(xdelta3Tuning, "-e", "-f", "-A", "-s", sourcePipe, targetPipe)
	xdelta3Cmd := xdelta3ToolCmdFn(xdelta3Args...)
	xdelta3Cmd.Stdout = deltaFile

	// 5. Run Concurrent Processes

	// Run unsquashfs (Source)
	g.Go(func() error {
		return wrapErr(runService(gctx, unsquashSourceCmd), "unsquashfs (source)")
	})

	// Run unsquashfs (Target)
	g.Go(func() error {
		return wrapErr(runService(gctx, unsquashTargetCmd), "unsquashfs (target)")
	})

	// Run xdelta3
	g.Go(func() error {
		return wrapErr(runService(gctx, xdelta3Cmd), "xdelta3")
	})

	// wait: first error cancels all others
	if err := g.Wait(); err != nil {
		return fmt.Errorf("delta generation failed: %w", err)
	}

	return nil
}

func applyXdelta3Delta(ctx context.Context, sourceSnap, targetSnap string, deltaFile *os.File, hdr *SnapDeltaHeader, mksqfsArgs []string) error {
	// Setup Context & ErrGroup
	g, gctx := errgroup.WithContext(ctx)
	// check if we have required tooling to apply delta
	supportedFormats, xdelta3CmdFn, mksquashfsCmdFn, unsquashfsCmdFn, _, _, err := CheckSupportedDetlaFormats(gctx)
	if err != nil {
		return fmt.Errorf("failed to validate required tooling for delta format: %v", err)
	}
	if !strings.Contains(supportedFormats, snapDeltaFormatXdelta3) {
		return fmt.Errorf("failed to validate required tooling for delta format'%s', supported: '%s': %v", supportedFormats, snapDeltaFormatHdiffz)
	}

	// setup pipes to apply delta
	tempDir, pipes, err := setupPipes("src", "delta")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	srcPipe := pipes[0]
	deltaPipe := pipes[1]

	unsq := unsquashfsCmdFn("-n", "-pf", srcPipe, sourceSnap)
	xdelta := xdelta3CmdFn("-f", "-d", "-s", srcPipe, deltaPipe)

	sqfsArgs := append([]string{
		"-", targetSnap,
		"-pf", "-",
		"-noappend",
		"-no-progress", "-quiet",
		"-mkfs-time", strconv.FormatUint(uint64(hdr.Timestamp), 10),
	}, mksqfsArgs...)
	sqfsArgs = append(sqfsArgs, mksquashfsTuningApply...)
	mksqfs := mksquashfsCmdFn(sqfsArgs...)

	// connect xdelta → mksquashfs
	mksqfs.Stdin, err = xdelta.StdoutPipe()
	if err != nil {
		return fmt.Errorf("pipe xdelta→mksqfs: %w", err)
	}

	// unsquash source → src pipe
	g.Go(func() error { return wrapErr(runService(gctx, unsq), "unsquashfs") })

	// xdelta3 filters (src pipe, delta pipe) → output
	g.Go(func() error { return wrapErr(runService(gctx, xdelta), "xdelta3") })

	// mksquashfs builds final snap
	g.Go(func() error { return wrapErr(runService(gctx, mksqfs), "mksquashfs") })

	// delta-body writer ("dd")
	g.Go(func() error {
		pf, err := os.OpenFile(deltaPipe, os.O_WRONLY, 0)
		if err != nil {
			return wrapErr(err, "delta pipe open")
		}
		defer pf.Close()

		// cancel copy cleanly
		go func() {
			<-gctx.Done()
			pf.Close()
		}()

		// seek past header
		if _, err := deltaFile.Seek(deltaHeaderSize, io.SeekStart); err != nil {
			if gctx.Err() == nil {
				return wrapErr(err, "delta seek")
			}
			return nil
		}

		if _, err := copyBuffer(pf, deltaFile); err != nil && gctx.Err() == nil {
			return wrapErr(err, "delta copy")
		}
		return nil
	})

	return g.Wait()
}

// --- Hdiffz Implementations ---
func generateHdiffzDelta(ctx context.Context, deltaFile *os.File, sourceSnap, targetSnap string) error {

	// Ensure we have required tooling
	supportedFormats, _, _, unsquashfsCmdFn, hdiffzCmdFn, _, err := CheckSupportedDetlaFormats(ctx)
	if err != nil || !strings.Contains(supportedFormats, snapDeltaFormatHdiffz) {
		return fmt.Errorf("failed to validate required tooling for snap-delta: %v", err)
	}

	// Setup Pipes
	unsquashSourceArg := append(unsquashfsTuningGenerate, "-n", "-pf", "-", sourceSnap)
	unsquashSourceCmd := unsquashfsCmdFn(unsquashSourceArg...)
	sourcePipe, err := unsquashSourceCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create source pf pipe: %w", err)
	}

	unsquashTargetArg := append(unsquashfsTuningGenerate, "-n", "-pf", "-", targetSnap)
	unsquashTargetCmd := unsquashfsCmdFn(unsquashTargetArg...)
	targetPipe, err := unsquashTargetCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create target pf pipe: %w", err)
	}

	if err := unsquashSourceCmd.Start(); err != nil {
		return fmt.Errorf("failed to start source cmd: %w", err)
	}
	defer unsquashSourceCmd.Process.Kill() // Ensure cleanup

	if err := unsquashTargetCmd.Start(); err != nil {
		return fmt.Errorf("failed to start target cmd: %w", err)
	}
	defer unsquashTargetCmd.Process.Kill()

	sourceReader := bufio.NewReaderSize(sourcePipe, CopyBufferSize)
	targetReader := bufio.NewReaderSize(targetPipe, CopyBufferSize)

	// 2. Parse Headers
	// Parse source into a Map for O(1) lookup
	sourceEntries, sourceHeaderBuff, err := parsePseudoStream(sourceReader)
	if err != nil {
		return fmt.Errorf("failed to parse source header: %w", err)
	}
	// Map for fast lookups: FilePath -> Entry
	sourceMap := make(map[string]*PseudoEntry, len(sourceEntries))
	for i := range sourceEntries {
		sourceEntries[i].OriginalIndex = i
		sourceMap[sourceEntries[i].FilePath] = &sourceEntries[i]
	}

	targetEntries, targetHeaderBuff, err := parsePseudoStream(targetReader)
	if err != nil {
		return fmt.Errorf("failed to parse target header: %w", err)
	}

	// Prepare reusable Processors for diffing
	srcMem, err := NewReusableMemFD("src-seg")
	if err != nil {
		return fmt.Errorf("failed to prepare reusable memFd: %w", err)
	}
	defer srcMem.Close()
	targetMem, err := NewReusableMemFD("target-seg")
	if err != nil {
		return fmt.Errorf("failed to prepare reusable memFd: %w", err)
	}
	defer targetMem.Close()

	diffMem, err := NewReusableMemFD("seg-diff")
	if err != nil {
		return fmt.Errorf("failed to prepare reusable memFd: %w", err)
	}
	defer diffMem.Close()

	// calculate header Delta and write it to the delta stream, use prepare mem processors
	_, err = unix.Write(srcMem.Fd, sourceHeaderBuff.Bytes())
	if err != nil {
		return fmt.Errorf("failed to write source header to memFd: %w", err)
	}
	_, err = unix.Write(targetMem.Fd, targetHeaderBuff.Bytes())
	if err != nil {
		return fmt.Errorf("failed to write target header to memFd: %w", err)
	}
	segmentDeltaSize, err := writeHdiffzToDeltaStream(deltaFile, 0, int64(sourceHeaderBuff.Len()), srcMem, targetMem, diffMem, hdiffzCmdFn)
	if err != nil {
		return fmt.Errorf("failed to calculate delta on headers: %w", err)
	}

	sourceHeaderSize := int64(sourceHeaderBuff.Len())
	bufferPool.Put(sourceHeaderBuff)
	bufferPool.Put(targetHeaderBuff)

	sourceRead := int64(0)
	targetRead := int64(0)
	totalDeltaSize := int64(24 + segmentDeltaSize)

	logger.Noticef("Processing %d target entries against %d source entries\n", len(targetEntries), len(sourceEntries))

	// 5. Main Processing Loop
	for _, e := range targetEntries {
		// Reset MemFDs for reuse
		srcMem.Reset()
		targetMem.Reset()
		diffMem.Reset()

		sourceEntry := sourceMap[e.FilePath]
		sourceSize := int64(0)
		sourceOffset := int64(0)
		lastSourceIndex := 0

		if sourceEntry != nil {
			sourceSize = sourceEntry.DataSize
			sourceOffset = sourceEntry.DataOffset
			lastSourceIndex = sourceEntry.OriginalIndex
		} else {
			// logger.Noticef("\tNo original version for: %s\n", e.FilePath)
		}

		// Handle Source Stream extraction
		// Calculate Source CRC while copying to detect identity without re-reading
		srcCRC := crc32.NewIEEE()
		if sourceSize > 0 {
			toSkip := sourceOffset - sourceRead
			if toSkip > 0 {
				// Efficient skip
				if _, err := copyNBuffer(io.Discard, sourceReader, toSkip); err != nil {
					return fmt.Errorf("failed to skip source stream: %w", err)
				}
				sourceRead += toSkip
			}

			// TeeReader reads from source, writes to srcMem AND srcCRC
			mw := io.MultiWriter(srcMem.File, srcCRC)
			if _, err := copyNBuffer(mw, sourceReader, sourceSize); err != nil {
				return fmt.Errorf("failed to extract source segment: %w", err)
			}
			sourceRead += sourceSize
		}

		// Handle Target Stream extraction
		targetCRC := crc32.NewIEEE()
		mw := io.MultiWriter(targetMem.File, targetCRC)
		if _, err := copyNBuffer(mw, targetReader, e.DataSize); err != nil {
			return fmt.Errorf("failed to extract target segment: %w", err)
		}
		targetRead += e.DataSize

		// Determine Identity
		isIdentical := false
		if sourceEntry != nil && sourceSize == e.DataSize {
			if sourceSize == 0 {
				isIdentical = true
			} else {
				// Compare checksums instead of reading files again
				if srcCRC.Sum32() == targetCRC.Sum32() {
					isIdentical = true
				}
			}
		}

		if isIdentical {
			// Files match, write negative index header
			headerBuf := bufferPool.Get().(*bytes.Buffer)
			headerBuf.Reset()
			binary.Write(headerBuf, binary.LittleEndian, int64(-lastSourceIndex))
			if _, err := deltaFile.Write(headerBuf.Bytes()); err != nil {
				bufferPool.Put(headerBuf)
				return err
			}
			bufferPool.Put(headerBuf)
			totalDeltaSize += 8
		} else {
			// Files differ, run hdiffz and store the delta
			segSize, err := writeHdiffzToDeltaStream(deltaFile, sourceHeaderSize+sourceOffset, sourceSize, srcMem, targetMem, diffMem, hdiffzCmdFn)
			if err != nil {
				return err
			}
			totalDeltaSize += (segSize + 24)
			logger.Noticef("Delta: %s (%d bytes -> %d bytes)\n", e.FilePath, e.DataSize, segSize)
		}
	}

	// Validation
	if b := targetReader.Buffered(); b > 0 {
		return fmt.Errorf("target stream has %d bytes left unconsumed", b)
	}

	logger.Noticef("Delta generation complete. Total size: %d\n", totalDeltaSize)
	return nil
}

func applyHdiffzDelta(ctx context.Context, sourceSnap, targetSnap string, deltaFile *os.File, hdr *SnapDeltaHeader, mksqfsArgs []string) error {
	// check if we have required tooling to apply delta
	supportedFormats, _, mksquashfsCmdFn, unsquashfsCmdFn, _, hpatchzCmdFn, err := CheckSupportedDetlaFormats(ctx)
	if err != nil {
		return fmt.Errorf("failed to validate required tooling for delta format: %v", err)
	}
	if !strings.Contains(supportedFormats, snapDeltaFormatHdiffz) {
		return fmt.Errorf("failed to validate required tooling for delta format'%s', supported: '%s': %v", supportedFormats, snapDeltaFormatHdiffz)
	}

	// Start Source Stream (unsquashfs)
	// We read FROM this pipe
	sourceCmd := unsquashfsCmdFn("-n", "-pf", "-", sourceSnap)
	sourcePipe, err := sourceCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create source pipe: %w", err)
	}
	if err := sourceCmd.Start(); err != nil {
		return fmt.Errorf("failed to start source stream: %w", err)
	}
	defer sourceCmd.Process.Kill()

	// Wrap source in a buffered reader for efficient seeking/skipping
	sourceReader := bufio.NewReaderSize(sourcePipe, CopyBufferSize)

	// Start Target Stream consumer (mksquashfs)
	sqfsArgs := append([]string{
		"-", targetSnap,
		"-pf", "-",
		"-noappend",
		"-no-progress", "-quiet",
		"-mkfs-time", strconv.FormatUint(uint64(hdr.Timestamp), 10),
	}, mksqfsArgs...)
	sqfsArgs = append(sqfsArgs, mksquashfsTuningApply...)
	targetCmd := mksquashfsCmdFn(sqfsArgs...)

	targetStdin, err := targetCmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create target stdin pipe: %w", err)
	}

	// Capture stderr to debug mksquashfs failures
	var targetStderr bytes.Buffer
	targetCmd.Stderr = &targetStderr

	if err := targetCmd.Start(); err != nil {
		return fmt.Errorf("failed to start mksquashfs: %w", err)
	}

	// Parse Source Header
	// We need the source entries to resolve "Identical" file references (negative indices)
	sourceEntries, sourceHeaderBuff, err := parsePseudoStream(sourceReader)
	if err != nil {
		return fmt.Errorf("failed to parse source header: %w", err)
	}
	sourceHeaderSize := int64(sourceHeaderBuff.Len())

	// Reconstruct and Write Target Header
	// The first segment in the delta file is ALWAYS the header patch
	// Read Header Patch Metadata: [Offset (0)][SourceSize][PatchSize]
	var headOffset, headSrcSize, headPatchSize int64
	if err := binary.Read(deltaFile, binary.LittleEndian, &headOffset); err != nil {
		return fmt.Errorf("failed to read header delta offset: %w", err)
	}
	if err := binary.Read(deltaFile, binary.LittleEndian, &headSrcSize); err != nil {
		return fmt.Errorf("failed to read header source size: %w", err)
	}
	if err := binary.Read(deltaFile, binary.LittleEndian, &headPatchSize); err != nil {
		return fmt.Errorf("failed to read header patch size: %w", err)
	}

	// Prepare reusable mem Processors for patch applying
	srcMem, err := NewReusableMemFD("src-seg")
	if err != nil {
		return fmt.Errorf("failed to prepare reusable memFd: %w", err)
	}
	defer srcMem.Close()
	targetMem, err := NewReusableMemFD("target_seg")
	if err != nil {
		return fmt.Errorf("failed to prepare reusable memFd: %w", err)
	}
	defer targetMem.Close()

	patchMem, err := NewReusableMemFD("seg-patch")
	if err != nil {
		return fmt.Errorf("failed to prepare reusable memFd: %w", err)
	}
	defer patchMem.Close()

	// get header patch into memory
	if _, err := copyNBuffer(patchMem.File, deltaFile, headPatchSize); err != nil {
		return fmt.Errorf("failed to read header patch data: %w", err)
	}

	// get source header to the memory
	if _, err := copyNBuffer(srcMem.File, sourceHeaderBuff, sourceHeaderSize); err != nil {
		return fmt.Errorf("failed to copy source header data: %w", err)
	}

	// Apply Patch: Source Header + Patch -> Target Header
	if err := applyPatch(srcMem.Path, patchMem.Path, targetMem.Path, hpatchzCmdFn); err != nil {
		return fmt.Errorf("failed to patch header: %w", err)
	}

	// We don't need the raw source header text anymore, so return to pool
	bufferPool.Put(sourceHeaderBuff)

	// Write Reconstructed Header to mksquashfs
	// This tells mksquashfs what files are coming
	if _, err := copyBuffer(targetStdin, targetMem.File); err != nil {
		return fmt.Errorf("failed to write target header to mksquashfs: %w", err)
	}

	// Parse the *Target* header we just generated so we know the order of files expected
	// We need to rewind the targetMem to parse it
	targetMem.File.Seek(0, 0)
	targetHeadReader := bufio.NewReader(targetMem.File)
	targetEntries, _, err := parsePseudoStream(targetHeadReader)
	if err != nil {
		return fmt.Errorf("failed to parse reconstructed target header: %w", err)
	}
	logger.Noticef("Reconstructing %d entries...\n", len(targetEntries))

	// Process Stream Loop
	// we can process delta stream directly, it has all the information we need
	// but using reconstructed target header as entry for the loop
	// gives us debug info at which file we failed to apply patch
	srcMem.Reset()
	patchMem.Reset()
	targetMem.Reset()
	sourceReadCursor := sourceHeaderSize
	for _, entry := range targetEntries {
		// Read Control Int64
		var controlVal int64
		if err := binary.Read(deltaFile, binary.LittleEndian, &controlVal); err != nil {
			return fmt.Errorf("failed to read control value for %s: %w", entry.FilePath, err)
		}
		if controlVal <= 0 {
			// source file is idential to target file, just stream it
			// control value is negative index to the source header
			sourceIndex := int(-controlVal)
			srcEntry := sourceEntries[sourceIndex]

			// stream can only move forward, do sanity check we haven't advanced allready too far
			neededOffset := srcEntry.DataOffset + sourceHeaderSize
			if sourceReadCursor > neededOffset {
				return fmt.Errorf("critical: source stream cursor (%d) passed needed offset (%d). Generator logic flaw or unsorted input", sourceReadCursor, neededOffset)
			}
			// do we need to skip some data in the source stream?
			skip := neededOffset - sourceReadCursor
			if skip > 0 {
				copyNBuffer(io.Discard, sourceReader, skip)
				sourceReadCursor += skip
			}

			// ready to pump data from source stream to -> mksquashfs
			if _, err := copyNBuffer(targetStdin, sourceReader, srcEntry.DataSize); err != nil {
				return fmt.Errorf("failed to copy source data for %s: %w", entry.FilePath, err)
			}
			sourceReadCursor += srcEntry.DataSize

		} else {
			// source and tatget file differ, apply patch on the source
			// controlVal becomes SourceOffset
			srcOffset := controlVal
			var srcSize, patchSize int64

			if err := binary.Read(deltaFile, binary.LittleEndian, &srcSize); err != nil {
				return err
			}
			if err := binary.Read(deltaFile, binary.LittleEndian, &patchSize); err != nil {
				return err
			}
			// prepare patch file
			patchMem.Reset()
			if _, err := copyNBuffer(patchMem.File, deltaFile, patchSize); err != nil {
				return fmt.Errorf("failed to read patch data: %w", err)
			}

			// Prepare Source Segment
			srcMem.Reset()
			if srcSize > 0 {
				// align source stream to what patch applies to
				// mostl likely files from source are not present in the target
				// !! sourceOffset in delta includes source header size for consistency with header delta which has offset 0
				// offset values in the header start at 0 after the header ends

				if sourceReadCursor > srcOffset {
					return fmt.Errorf("critical: source cursor advanced too far for patch %s", entry.FilePath)
				}

				skip := srcOffset - sourceReadCursor
				if skip > 0 {
					copyNBuffer(io.Discard, sourceReader, skip)
					sourceReadCursor += skip
				}

				// Read from stream to MemFD
				if _, err := copyNBuffer(srcMem.File, sourceReader, srcSize); err != nil {
					return fmt.Errorf("failed to extract source segment for patch: %w", err)
				}
				sourceReadCursor += srcSize
			}

			// 3. Apply Patch
			targetMem.Reset()
			// if srcSize is 0, hpatchz treats it as creating a new file from patch
			if err := applyPatch(srcMem.Path, patchMem.Path, targetMem.Path, hpatchzCmdFn); err != nil {
				return fmt.Errorf("failed to patch file %s: %w", entry.FilePath, err)
			}
			// write reconstructed result to mksquashfs
			// DEBUG: logger.Noticef("%s\t(from %d bytes delta)\n", entry.FilePath, patchSize)
			if _, err := copyBuffer(targetStdin, targetMem.File); err != nil {
				return fmt.Errorf("failed to write patched data to mksquashfs: %w", err)
			}
		}
	}

	targetStdin.Close() // Close stdin to signal EOF to mksquashfs

	if err := targetCmd.Wait(); err != nil {
		return fmt.Errorf("mksquashfs failed: %v\nStderr: %s", err, targetStderr.String())
	}

	return nil
}

// --- Shared Helpers ---

// writeHdiffzToltaStream
func writeHdiffzToDeltaStream(deltaFile *os.File, sourceOffset, sourceSize int64, source, target, diff *ReusableMemFD, hdiffzCmdFn DeltaToolingCmd) (int64, error) {

	// Files differ, run hdiffz, use the /proc paths which remain valid for the reused FDs
	hdiffzArgs := append(hdiffzTuning, "-f", source.Path, target.Path, diff.Path)
	hdiffzCmd := hdiffzCmdFn(hdiffzArgs...)
	if output, err := hdiffzCmd.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("hdiffz failed: %v %s", err, string(output))
	}

	headerBuf := bufferPool.Get().(*bytes.Buffer)
	headerBuf.Reset()
	defer bufferPool.Put(headerBuf)

	// Get segment size
	st, err := diff.File.Stat()
	if err != nil {
		return 0, err
	}
	segSize := st.Size()

	binary.Write(headerBuf, binary.LittleEndian, int64(sourceOffset))
	binary.Write(headerBuf, binary.LittleEndian, int64(sourceSize))
	binary.Write(headerBuf, binary.LittleEndian, int64(segSize))

	if _, err := deltaFile.Write(headerBuf.Bytes()); err != nil {
		return 0, err
	}

	// Rewind segment file
	if _, err := diff.File.Seek(0, 0); err != nil {
		return 0, err
	}

	// Copy data
	if _, err := copyBuffer(deltaFile, diff.File); err != nil {
		return 0, err
	}
	return segSize, nil
}

func applyPatch(oldPath, diffPath, outPath string, hpatchzCmdFn DeltaToolingCmd) error {
	xdhpatchzArgs := append(hpatchzTuning, "-f", oldPath, diffPath, outPath)
	cmd := hpatchzCmdFn(xdhpatchzArgs...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, string(output))
	}
	return nil
}

// unescape: only allocates if backslash is present
func unescape(s string) string {
	if strings.IndexByte(s, '\\') == -1 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && (s[i+1] == ' ' || s[i+1] == '\\') {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func parsePseudoDefinitionLine(line string) []string {
	// find first space not preceded by escape
	splitIdx := -1
	for i := 0; i < len(line); i++ {
		if line[i] == ' ' && (i == 0 || line[i-1] != '\\') {
			splitIdx = i
			break
		}
	}

	if splitIdx == -1 {
		return []string{unescape(line)}
	}

	name := unescape(line[:splitIdx])
	rest := strings.Fields(line[splitIdx+1:])

	// Pre-allocate slice
	out := make([]string, 1, len(rest)+1)
	out[0] = name
	out = append(out, rest...)
	return out
}

// parsePseudoStream encapsulates the logic to read the mixed text/binary stream
func parsePseudoStream(reader *bufio.Reader) ([]PseudoEntry, *bytes.Buffer, error) {

	// 2. Storage for parsed data
	var entries []PseudoEntry
	headerBuffer := bufferPool.Get().(*bytes.Buffer)
	headerBuffer.Reset()
	headerEnd := false

	for {
		// Read until the next newline (Text Mode)
		lineBytes, err := reader.ReadBytes('\n')
		if err != nil && err != io.EOF {
			return nil, nil, fmt.Errorf("Stream read error: %v", err)
		}

		// If we got no bytes and EOF, we are done
		if len(lineBytes) == 0 && err == io.EOF {
			break
		}

		// Store this raw line in our header buffer
		headerBuffer.Write(lineBytes)

		// Trim whitespace for parsing
		lineStr := string(lineBytes)
		trimmed := strings.TrimSpace(lineStr)

		// Skip empty lines or comments
		if len(trimmed) == 0 {
			if err == io.EOF {
				break
			}
		}
		if trimmed[0] == '#' {
			// is this the comment after "# START OF DATA - DO NOT MODIFY"
			if headerEnd {
				break
			}
			// detect if this is end of the header "# START OF DATA - DO NOT MODIFY"
			if trimmed == "# START OF DATA - DO NOT MODIFY" {
				// read one more line and break
				headerEnd = true
				continue
			}
		}

		// Parse the Text Line
		fields := parsePseudoDefinitionLine(trimmed)
		if len(fields) < 3 {
			// Handle malformed lines gracefully
			if err == io.EOF {
				break
			}
			continue
		}

		entry := PseudoEntry{
			FilePath: fields[0],
			Type:     fields[1],
		}

		// Logic to handle different types
		switch entry.Type {
		// ignore all the types without inline data
		// D: Directory, S: Symbolic Link, L: hard link, C: Char device
		// x: extended security capability
		case "D", "S", "L", "C", "x":
			continue

		// R: Regular File with INLINE DATA
		// Format: Path  Type  Time  Mode  UID  GID  Size  Offset  XattrIndex
		case "R":
			var parseErr error
			// Format: FilePath R Time Mode UID GID Size Offset <>
			entry.DataSize, parseErr = strconv.ParseInt(fields[6], 10, 64)
			if parseErr != nil {
				log.Fatalf("Invalid data size: %v", parseErr)
			}
			entry.DataOffset, parseErr = strconv.ParseInt(fields[7], 10, 64)
			if parseErr != nil {
				log.Fatalf("Invalid data offset: %v", parseErr)
			}
		default:
			log.Fatalf("unknown type in pseudo definition!!: %s", trimmed)
		}

		entries = append(entries, entry)

		if err == io.EOF {
			break
		}
	}

	return entries, headerBuffer, nil
}

// --- Infrastructure ---

// setupPipes creates a temporary directory and named pipes within it.
func setupPipes(pipeNames ...string) (string, []string, error) {
	tempDir, err := os.MkdirTemp("", "snap-delta-")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	var pipePaths []string
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

// Check if all the required tools are actually present
// returns ready to use commands for xdelta3, bsdiff, mksquashfs and unsquashfs
func CheckSupportedDetlaFormats(ctx context.Context) (string, DeltaToolingCmd, DeltaToolingCmd, DeltaToolingCmd, DeltaToolingCmd, DeltaToolingCmd, error) {
	// check if we have required tools available
	xdeltaCmd, err := checkForTooling(ctx, xdelta3, "xdelta3", "config")
	if err != nil {
		return "", nil, nil, nil, nil, nil, fmt.Errorf("missing snapd delta dependencies: %v", err)
	}
	// from here we can support plain xdelta3
	mksquashfsCmd, err := checkForTooling(ctx, mksquashfs, "mksquashfs", "-version")
	if err != nil {
		return xdelta3Format, xdeltaCmd, nil, nil, nil, nil, fmt.Errorf("missing snapd delta dependencies: %v", err)
	}
	// use -help since '-version' does not return 0 error code
	unsquashfsCmd, err := checkForTooling(ctx, unsquashfs, "unsquashfs", "-help")
	if err != nil {
		return xdelta3Format, xdeltaCmd, nil, nil, nil, nil, fmt.Errorf("missing snapd delta dependencies: %v", err)
	}
	// from here we support snapDeltaFormatXdelta3 + xdelta3Format
	hdiffzCmd, err := checkForTooling(ctx, hdiffz, "hdiffz", "-v")
	if err != nil {
		return snapDeltaFormatXdelta3 + "," + xdelta3Format, xdeltaCmd, mksquashfsCmd, unsquashfsCmd, nil, nil, fmt.Errorf("missing snapd delta dependencies: %v", err)
	}
	hpatchzCmd, err := checkForTooling(ctx, hpatchz, "hpatchz", "-v")
	if err != nil {
		return snapDeltaFormatXdelta3 + "," + xdelta3Format, xdeltaCmd, mksquashfsCmd, unsquashfsCmd, nil, nil, fmt.Errorf("missing snapd delta dependencies: %v", err)
	}

	return snapDeltaFormatHdiffz + "," + snapDeltaFormatXdelta3 + "," + xdelta3Format, xdeltaCmd, mksquashfsCmd, unsquashfsCmd, hdiffzCmd, hpatchzCmd, nil
}

// helper to check for the presence of the required tools
func checkForTooling(ctx context.Context, toolPath, tool, option string) (DeltaToolingCmd, error) {
	loc := toolPath
	if _, err := os.Stat(toolPath); err != nil {
		if p, err := exec.LookPath(tool); err == nil {
			loc = p
		} else {
			return nil, fmt.Errorf("tool not found")
		}
	}
	// Verify execution
	if err := exec.Command(loc, option).Run(); err != nil {
		return nil, fmt.Errorf("tool verification failed: %v", err)
	}
	// TODO: check minimal required version
	// the 'tool' in the env worked, so use that one
	if ctx == nil {
		return func(toolArgs ...string) *exec.Cmd {
			return exec.Command(loc, toolArgs...)
		}, nil
	} else {
		return func(toolArgs ...string) *exec.Cmd {
			return exec.CommandContext(ctx, loc, toolArgs...)
		}, nil
	}
}

// --- Utils ---

func generatePlainXdelta3Delta(ctx context.Context, sourceSnap, targetSnap, delta string) error {
	cmd, err := checkForTooling(ctx, "/usr/bin/xdelta3", "xdelta3", "config")
	if err != nil {
		return fmt.Errorf("missing xdelta3 tooling: %v", err)
	}
	return cmd(append(xdelta3PlainTuning, "-f", "-e", "-s", sourceSnap, targetSnap, delta)...).Run()
}

func applyPlainXdelta3Delta(ctx context.Context, sourceSnap, delta, targetSnap string) error {
	cmd, err := checkForTooling(ctx, "/usr/bin/xdelta3", "xdelta3", "config")
	if err != nil {
		return fmt.Errorf("missing xdelta3 tooling: %v", err)
	}
	return cmd("-f", "-d", "-s", sourceSnap, delta, targetSnap).Run()
}

// // run command in context, ensure commans is terminated if context is cancelled
func runService(ctx context.Context, cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		if err := cmd.Process.Kill(); err != nil {
			logger.Noticef("Failed to kill process (%s): %v\n", cmd.Path, err)
		}
		<-waitDone
		return ctx.Err()
	case err := <-waitDone:
		return err
	}
}

func wrapErr(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s failed: %w", msg, err)
}

// ReusableMemFD wraps the file descriptor logic to reuse resources
type ReusableMemFD struct {
	Fd   int
	File *os.File
	Path string
}

func NewReusableMemFD(name string) (*ReusableMemFD, error) {
	fd, err := unix.MemfdCreate(name, 0)
	if err != nil {
		return nil, err
	}
	// Wrap in os.File for easy Go IO, but we manage the FD manually mostly
	return &ReusableMemFD{
		Fd:   fd,
		File: os.NewFile(uintptr(fd), name),
		Path: fmt.Sprintf("/proc/self/fd/%d", fd),
	}, nil
}

func (m *ReusableMemFD) Reset() error {
	// Truncate file to 0 size
	if err := unix.Ftruncate(m.Fd, 0); err != nil {
		return err
	}
	// Seek to start
	_, err := m.File.Seek(0, 0)
	return err
}

func (m *ReusableMemFD) Close() {
	m.File.Close() // This closes the FD as well
}

// Loads timestamp, compression, and flags from a squashfs superblock.
func (h *SnapDeltaHeader) loadDeltaHeaderFromSnap(f *os.File) error {
	buf := make([]byte, 26) // Read enough for all fields
	if _, err := f.ReadAt(buf, 8); err != nil {
		return err
	}

	// Timestamp @ offset 8 (u32): 8 -> 0
	h.Timestamp = binary.LittleEndian.Uint32(buf[0:4])
	// Compression @ offset 20 (u16): 20 -> 12
	h.Compression = binary.LittleEndian.Uint16(buf[12:14])
	// Flags @ offset 24 (u16): 24 -> 16
	h.Flags = binary.LittleEndian.Uint16(buf[16:18])
	return nil
}

// Serialises delta header into deltaHeaderSize
func (h *SnapDeltaHeader) toBytes() ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, h.Magic); err != nil {
		return nil, fmt.Errorf("failed to write header magic: %w", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, h.Version); err != nil {
		return nil, fmt.Errorf("failed to write header version: %w", err)
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

func readDeltaHeader(r io.Reader) (*SnapDeltaHeader, error) {
	buf := make([]byte, deltaHeaderSize)
	n, err := io.ReadFull(r, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, fmt.Errorf("read header: %w", err)
	}
	if n < deltaHeaderSize {
		return nil, fmt.Errorf("header too short")
	}

	hdr := &SnapDeltaHeader{}
	if err := binary.Read(bytes.NewReader(buf), binary.LittleEndian, hdr); err != nil {
		return nil, fmt.Errorf("decode header: %w", err)
	}
	return hdr, nil
}

// parseCompression converts SquashFS compression ID to a name.
func parseCompression(id uint16, mksqfsArgs []string) ([]string, error) {
	// compression map from squashfs spec
	m := map[uint16]string{1: "gzip", 2: "lzma", 3: "lzo", 4: "xz", 5: "lz4", 6: "zstd"}
	if s, ok := m[id]; ok {
		return append(mksqfsArgs, "-comp", s), nil
	}
	return nil, fmt.Errorf("unknown compression: %d", id)
}

// parseSuperblockFlags converts SquashFS flags to mksquashfs arguments.
func parseSuperblockFlags(flags uint16, mksqfsArgs []string) ([]string, error) {
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
		log.Println("warning: Custom compression options detected, created target snap is likely be different from target snap!")
	}

	return mksqfsArgs, nil
}
