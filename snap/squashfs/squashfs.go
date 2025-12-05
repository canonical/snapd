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
	"io"
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

const (
	deltaHeaderSize           = 32 // bytes
	deltaFormatVersion uint16 = 0x101
	deltaMagicNumber   uint32 = 0xF989789C
	xdelta3MagicNumber uint32 = 0x00c4c3d6
	snapDeltaFormat           = "snapDeltaV1"
	xdelta3Format             = "xdelta3"
)

var (
	// default compression level assumed 3
	// plain squashfs to squashfs delta size has no measurable gain between  3 and 9 comp level
	xdelta3PlainTuning = []string{"-3"}
	// gain in delta pseudo file between 3 and 7 comp level is 10 to 20% size reduction
	// delta size gain flattens at 7
	// no noticeable gain from changing source window size(-B) or bytes input window(-W)
	// or size compression duplicates window (-P)
	xdelta3Tuning = []string{"-7"}
)

// SquashFS Superblock Flags
const (
	flagCheck             uint16 = 0x0004
	flagNoFragments       uint16 = 0x0010
	flagNoDuplicates      uint16 = 0x0040 // Note: logic is inverted (default is duplicates)
	flagExports           uint16 = 0x0080
	flagNoXattrs          uint16 = 0x0200
	flagCompressorOptions uint16 = 0x0400
)

// Supported version of the snap delta algorythms
func SupportedDeltaFormats() string {
	return snapDeltaFormat + ";" + xdelta3Format
}

type SquashfsCommand func(args ...string) *exec.Cmd

// Check if all the required tools are actually present
// returns ready to use commands for xdelta3, mksquashfs and unsquashfs
func CheckSupportedDetlaFormats(ctx context.Context) (string, SquashfsCommand, SquashfsCommand, SquashfsCommand, error) {
	// check if we have required tools available
	xdeltaCmd, err := checkForTooling(ctx, "/usr/bin/xdelta3", "xdelta3", "config")
	if err != nil {
		return "", nil, nil, nil, fmt.Errorf("missing snapd delta dependencies: %v", err)
	}
	// from here we can support plain xdelta3
	mksquashfsCmd, err := checkForTooling(ctx, "/usr/bin/mksquashfs", "mksquashfs", "-version")
	if err != nil {
		return xdelta3Format, xdeltaCmd, nil, nil, fmt.Errorf("missing snapd delta dependencies: %v", err)
	}
	// use -help since '-version' does not return 0 error code
	unsquashfsCmd, err := checkForTooling(ctx, "/usr/bin/unsquashfs", "unsquashfs", "-help")
	if err != nil {
		return xdelta3Format, xdeltaCmd, nil, nil, fmt.Errorf("missing snapd delta dependencies: %v", err)
	}
	return snapDeltaFormat + ";" + xdelta3Format, xdeltaCmd, mksquashfsCmd, unsquashfsCmd, nil
}

// helper to check for the presence of the required tools
func checkForTooling(ctx context.Context, toolPath, tool, option string) (SquashfsCommand, error) {
	// // check if the 'tool' 'option' command works from the system snap
	cmd, err := snapdtool.CommandFromSystemSnapWithCtx(ctx, toolPath)
	if err == nil {
		// we have tool in the system snap, use it
		exe := cmd.Path
		args := cmd.Args[:len(cmd.Args)-1]
		env := cmd.Env
		dir := cmd.Dir
		return func(toolArgs ...string) *exec.Cmd {
			return &exec.Cmd{
				Path: exe,
				Args: append(args, toolArgs...),
				Env:  env,
				Dir:  dir,
			}
		}, nil
	}

	// we didn't have one from a system snap or it didn't work, fallback to
	// trying 'tool' from the system
	loc, err := exec.LookPath(tool)
	if err != nil {
		// no 'tool' in the env, so no deltas
		return nil, fmt.Errorf("no host system %s available", tool)
	}

	if err := exec.Command(loc, option).Run(); err != nil {
		// xdelta3 in the env failed to run, so no deltas
		return nil, fmt.Errorf("unable to use host system %s, running '%s' command failed: %v", tool, option, err)
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

// parseCompression converts SquashFS compression ID to a name.
func parseCompression(id uint16, mksqfsArgs []string) ([]string, error) {
	switch id {
	case 1:
		return append(mksqfsArgs, "-comp", "gzip"), nil
	case 2:
		return append(mksqfsArgs, "-comp", "lzma"), nil
	case 3:
		return append(mksqfsArgs, "-comp", "lzo"), nil
	case 4:
		return append(mksqfsArgs, "-comp", "xz"), nil
	case 5:
		return append(mksqfsArgs, "-comp", "lz4"), nil
	case 6:
		return append(mksqfsArgs, "-comp", "zstd"), nil
	default:
		return nil, fmt.Errorf("unknown compression id: %d", id)
	}
}

// parseSuperblockFlags converts SquashFS flags to mksquashfs arguments.
func parseSuperblockFlags(flags uint16, mksqfsArgs []string) ([]string, error) {
	if (flags & flagCheck) != 0 {
		return nil, fmt.Errorf("this does not look like Squashfs 4+ superblock flags")
	}
	if (flags & flagNoFragments) != 0 {
		mksqfsArgs = append(mksqfsArgs, "-no-fragments")
	}
	// Note: The flag is "NO_DUPLICATES", so if it's *not* set, we... wait.
	// The bash script logic is: if 0x0040 is *not* set, add -no-duplicates.
	if (flags & flagNoDuplicates) == 0 {
		mksqfsArgs = append(mksqfsArgs, "-no-duplicates")
	}
	if (flags & flagExports) != 0 {
		mksqfsArgs = append(mksqfsArgs, "-exports")
	}
	if (flags & flagNoXattrs) != 0 {
		mksqfsArgs = append(mksqfsArgs, "-no-xattrs")
	}
	if (flags & flagCompressorOptions) != 0 {
		logger.Noticef("warning: Custom compression options detected, created target snap is likely be different from target snap!")
	}

	return mksqfsArgs, nil
}

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

func generatePlainXdelta3Delta(sourceSnap, targetSnap, delta string) error {
	xdelta3Cmd, err := checkForTooling(nil, "/usr/bin/xdelta3", "xdelta3", "config")
	if err != nil {
		return fmt.Errorf("missing xdelta3 tooling: %v", err)
	}
	xdelta3Args := append(xdelta3PlainTuning, "-f", "-e", "-s", sourceSnap, targetSnap, delta)
	cmd := xdelta3Cmd(xdelta3Args...)
	return cmd.Run()
}

func applyPlainXdelta3Delta(sourceSnap, delta, targetSnap string) error {
	xdelta3Cmd, err := checkForTooling(nil, "/usr/bin/xdelta3", "xdelta3", "config")
	if err != nil {
		return fmt.Errorf("missing xdelta3 tooling: %v", err)
	}
	xdelta3Args := []string{
		"-f", "-d", "-s", sourceSnap, delta, targetSnap,
	}
	cmd := xdelta3Cmd(xdelta3Args...)
	return cmd.Run()
}

// generate delta file.
func GenerateSnapDelta(sourceSnap, targetSnap, delta string) error {
	// Open target snap to read superblock
	targetFile, err := os.Open(targetSnap)
	if err != nil {
		return fmt.Errorf("failed to open target file %s: %w", targetSnap, err)
	}
	defer targetFile.Close()

	// Read superblock flags and check for custom compression options (flag flagCompressorOptions)
	flagsBuf := make([]byte, 2)
	if _, err := targetFile.ReadAt(flagsBuf, 24); err != nil {
		return fmt.Errorf("failed to read flags from target: %w", err)
	}
	targetFlags := binary.LittleEndian.Uint16(flagsBuf)

	if (targetFlags & flagCompressorOptions) != 0 {
		logger.Noticef("warning: Custom compression options detected, falling back to plain xdelta3")
		return generatePlainXdelta3Delta(sourceSnap, targetSnap, delta)
	}

	// --- Write Custom Header ---
	headerBuf := new(bytes.Buffer)

	// Magic number (32b)
	if err := binary.Write(headerBuf, binary.LittleEndian, uint32(deltaMagicNumber)); err != nil {
		return fmt.Errorf("failed to write snap-delta magic: %w", err)
	}

	// Version (16b)
	if err := binary.Write(headerBuf, binary.LittleEndian, uint16(deltaFormatVersion)); err != nil {
		return fmt.Errorf("failed to write snap-delta version: %w", err)
	}

	// Read/Write Timestamp (32b at offset 8)
	tsBuf := make([]byte, 4)
	if _, err := targetFile.ReadAt(tsBuf, 8); err != nil {
		return fmt.Errorf("failed to read target snap timestamp: %w", err)
	}
	headerBuf.Write(tsBuf)

	// Read/Write Compression (16b at offset 20)
	compBuf := make([]byte, 2)
	if _, err := targetFile.ReadAt(compBuf, 20); err != nil {
		return fmt.Errorf("failed to read target snap compression: %w", err)
	}
	headerBuf.Write(compBuf)

	// Super block flags (16b) - read from target (16b at offset 24)
	headerBuf.Write(flagsBuf) // We already read this

	// Padding to deltaHeaderSize
	padding := make([]byte, deltaHeaderSize-headerBuf.Len())
	headerBuf.Write(padding)

	// --- End Header ---

	// Create delta file and write header there
	deltaFile, err := os.OpenFile(delta, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create delta file %s: %w", delta, err)
	}
	defer deltaFile.Close()
	if _, err := deltaFile.Write(headerBuf.Bytes()); err != nil {
		return fmt.Errorf("failed to write header to delta file: %w", err)
	}

	// 1. Setup pipes
	tempDir, pipePaths, err := setupPipes("source-pipe", "target-pipe")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	sourcePipe := pipePaths[0]
	targetPipe := pipePaths[1]

	// Run all as concurrent processes
	var wg sync.WaitGroup
	errChan := make(chan error, 3)

	// cancellable context for all tasks
	ctx, cancel := context.WithCancel(context.Background())
	// Defer cancel to clean up, though fail() will usually call it first
	defer cancel()

	// prepare tooling, make sure we have enough for snapDeltaV1 format
	supportedFormats, xdelta3Cmd, _, unsquashfsCmd, err := CheckSupportedDetlaFormats(ctx)
	if err != nil || !strings.Contains(supportedFormats, snapDeltaFormat) {
		return fmt.Errorf("failed to validate required tooling for snap-delta: %v", err)
	}

	// unsquashfs source -> source-pipe
	unsquashfsSourceArgs := []string{
		"-n", "-pf", sourcePipe, sourceSnap,
	}
	unsquashSourceCmd := unsquashfsCmd(unsquashfsSourceArgs...)

	// unsquashfs target -> target-pipe
	unsquashfsTargetArgs := []string{
		"-n", "-pf", targetPipe, targetSnap,
	}
	unsquashfsTargetCmd := unsquashfsCmd(unsquashfsTargetArgs...)

	// xdelta3 source-pipe, target-pipe -> delta-file (append)
	xdelta3Args := append(xdelta3Tuning, "-e", "-f", "-A", "-s", sourcePipe, targetPipe)
	xdeltaCmd := xdelta3Cmd(xdelta3Args...)
	xdeltaCmd.Stdout = deltaFile // Append to the already open delta file

	// handle fail of any task
	var failOnce sync.Once
	fail := func(err error) {
		failOnce.Do(func() {
			cancel() // <-- This signals all other tasks to stop
			errChan <- err
		})
	}

	// run unsquashfs source
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := osutil.RunWithContext(ctx, unsquashSourceCmd); err != nil {
			if ctx.Err() == nil {
				fail(fmt.Errorf("unsquashfs (source) failed: %w", err))
			}
		}
	}()

	// run unsquashfs target
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := osutil.RunWithContext(ctx, unsquashfsTargetCmd); err != nil {
			if ctx.Err() == nil {
				fail(fmt.Errorf("unsquashfs (target) failed: %w", err))
			}
		}
	}()

	// run xdelta3 source-pipe, target-pipe -> delta-file, appending
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := osutil.RunWithContext(ctx, xdeltaCmd); err != nil {
			if ctx.Err() == nil {
				fail(fmt.Errorf("xdelta3 failed: %w", err))
			}
		}
	}()

	// Wait for all processes and collect errors
	wg.Wait()
	close(errChan)

	var allErrors []string
	for err := range errChan {
		allErrors = append(allErrors, err.Error())
	}

	if len(allErrors) > 0 {
		return fmt.Errorf("delta generation failed:%s", strings.Join(allErrors, "\n"))
	}
	return nil
}

// handleApplyDelta applies the smart delta file.
func ApplySnapDelta(sourceSnap, delta, targetSnap string) error {
	// Open delta file to read header
	deltaFile, err := os.Open(delta)
	if err != nil {
		return fmt.Errorf("failed to open delta file %s: %w", delta, err)
	}
	defer deltaFile.Close()

	// --- Read Custom Header ---
	header := make([]byte, deltaHeaderSize)
	n, err := io.ReadFull(deltaFile, header)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return fmt.Errorf("failed to read delta header: %w", err)
	}
	if n < 14 { // Size of magic + version + time + comp + flags
		return fmt.Errorf("delta file is too small to be valid")
	}

	headerReader := bytes.NewReader(header)
	var magicNumber uint32
	binary.Read(headerReader, binary.LittleEndian, &magicNumber)

	// Check for plain xdelta3
	if magicNumber == xdelta3MagicNumber {
		logger.Noticef("This is a plain xdelta3 diff, falling back to plain xdelta3!!")
		return applyPlainXdelta3Delta(sourceSnap, delta, targetSnap)
	}

	if magicNumber != deltaMagicNumber {
		return fmt.Errorf("wrong magic number! (Expected 0x%X, Got 0x%X)", deltaMagicNumber, magicNumber)
	}

	var versionNumber uint16
	binary.Read(headerReader, binary.LittleEndian, &versionNumber)
	if versionNumber != deltaFormatVersion {
		return fmt.Errorf("mismatch delta version number! (Expected 0x%X, Got 0x%X)", deltaFormatVersion, versionNumber)
	}

	var fstimeint uint32
	var targetCompressionID uint16
	var targetFlags uint16
	binary.Read(headerReader, binary.LittleEndian, &fstimeint)
	binary.Read(headerReader, binary.LittleEndian, &targetCompressionID)
	binary.Read(headerReader, binary.LittleEndian, &targetFlags)

	var mksqfsArgs []string
	mksqfsArgs, err = parseCompression(targetCompressionID, mksqfsArgs)
	if err != nil {
		return fmt.Errorf("failed to parse target compression from snap-delta: %v", err)
	}

	mksqfsArgs, err = parseSuperblockFlags(targetFlags, mksqfsArgs)
	if err != nil {
		return fmt.Errorf("failed to parse target supper block flags from snap-delta:%v", err)
	}

	// prepare pipes
	tempDir, pipePaths, err := setupPipes("source-pipe", "delta-pipe")
	if err != nil {
		return fmt.Errorf("failed to prepare pipes for snap delta: %v", err)
	}
	defer os.RemoveAll(tempDir)
	sourceSnapPipe := pipePaths[0]
	deltaPipe := pipePaths[1]

	// Run concurrent processes
	var wg sync.WaitGroup
	errChan := make(chan error, 4) // unsq, dd, xdelta, mksqfs
	// cancellable context for all tasks
	ctx, cancel := context.WithCancel(context.Background())
	// Defer cancel to clean up, though fail() will usually call it first
	defer cancel()

	// prepare tooling, make sure we have enough for snapDeltaV1 format
	supportedFormats, xdelta3Cmd, mksquashfsCmd, unsquashfsCmd, err := CheckSupportedDetlaFormats(ctx)
	if err != nil || !strings.Contains(supportedFormats, snapDeltaFormat) {
		return fmt.Errorf("failed to validate required tooling for snap-delta: %v", err)
	}

	// Setup command chains
	// xdelta3 between two pipes (source and delta pipes)
	xdelta3Args := []string{
		"-f", "-d", "-s", sourceSnapPipe, deltaPipe,
	}
	xdeltaCmd := xdelta3Cmd(xdelta3Args...)

	// mksquashfs from xdelta3 stream to target snap with correct extra arguments
	mksqfsFullArgs := []string{
		"-", // Source from stdin
		targetSnap,
		"-pf", "-", // Read patch file list from stdin
		"-no-progress",
		"-quiet",
		"-noappend",
		"-mkfs-time", strconv.FormatUint(uint64(fstimeint), 10),
	}
	mksqfsFullArgs = append(mksqfsFullArgs, mksqfsArgs...)
	mksqfsCmd := mksquashfsCmd(mksqfsFullArgs...)

	// Connect xdelta3 stdout to mksquashfs stdin
	mksqfsCmd.Stdin, err = xdeltaCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create pipe between xdelta3 and mksquashfs: %w", err)
	}

	// unsquash source snap into source pipe
	unsquashfsArgs := []string{
		"-n", "-pf", sourceSnapPipe, sourceSnap,
	}
	unsquashSourceCmd := unsquashfsCmd(unsquashfsArgs...)

	// handle fail of any task
	var failOnce sync.Once
	fail := func(err error) {
		failOnce.Do(func() {
			cancel() // <-- This signals all other tasks to stop
			errChan <- err
		})
	}

	// Start target consumers
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := osutil.RunWithContext(ctx, mksqfsCmd); err != nil {
			if ctx.Err() == nil {
				fail(fmt.Errorf("mksquashfs (target) failed: %w", err))
			}
		}
	}()

	// start delta consumers: xdelta3
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := osutil.RunWithContext(ctx, xdeltaCmd); err != nil {
			if ctx.Err() == nil {
				fail(fmt.Errorf("xdelta3 failed: %w", err))
			}
		}
	}()

	// Start source producers: unsquashfs source -> source-pipe
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := osutil.RunWithContext(ctx, unsquashSourceCmd); err != nil {
			fail(fmt.Errorf("unsquashfs (source) failed: %w:", err))
		}
	}()

	// dd (Go copy) delta-body -> delta-pipe
	wg.Add(1)
	go func() {
		defer wg.Done()
		pipeF, err := os.OpenFile(deltaPipe, os.O_WRONLY, 0)
		if err != nil {
			fail(fmt.Errorf("failed to open delta pipe for writing: %w", err))
			return
		}
		defer pipeF.Close()

		// Make io.Copy cancellable by closing its pipe.
		go func() {
			<-ctx.Done()  // Wait for cancellation
			pipeF.Close() // This will force io.Copy to unblock with an error
		}()

		// Seek delta file to start of body
		if _, err := deltaFile.Seek(deltaHeaderSize, io.SeekStart); err != nil {
			// Check context, as this could fail if pipeF was closed
			if ctx.Err() == nil {
				fail(fmt.Errorf("failed to seek delta file: %w", err))
			}
			return
		}

		if _, err := io.Copy(pipeF, deltaFile); err != nil {
			// If context is cancelled, io.Copy will fail.
			// We check if this error was an *expected* cancellation.
			if ctx.Err() == nil {
				fail(fmt.Errorf("failed to copy delta body to pipe: %w", err))
			}
		}
	}()

	// Wait for all processes and collect errors
	wg.Wait()
	close(errChan)

	var allErrors []string
	for err := range errChan {
		allErrors = append(allErrors, err.Error())
	}

	if len(allErrors) > 0 {
		return fmt.Errorf("applying snap delta failed: %s", strings.Join(allErrors, "\n"))
	}
	return nil
}
