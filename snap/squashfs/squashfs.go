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
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
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

func (s *Snap) Unpack(src, dstDir string) error {
	usw := newUnsquashfsStderrWriter()

	var output bytes.Buffer
	cmd := exec.Command("unsquashfs", "-n", "-f", "-d", dstDir, s.path, src)
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
	tmpdir, err := ioutil.TempDir("", "read-file")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpdir)

	unpackDir := filepath.Join(tmpdir, "unpack")
	if output, err := exec.Command("unsquashfs", "-n", "-i", "-d", unpackDir, s.path, filePath).CombinedOutput(); err != nil {
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
		content, err = ioutil.ReadFile(p)
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
		fmt.Fprintf(&b, "- ")
		fmt.Fprintf(&b, p)
		fmt.Fprintf(&b, "\n")
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
	if snapType != "os" && snapType != "core" && snapType != "base" {
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
