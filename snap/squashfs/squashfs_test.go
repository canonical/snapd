// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package squashfs_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapdir"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

func makeSnap(c *C, manifest, data string) *squashfs.Snap {
	cur, _ := os.Getwd()
	return makeSnapInDir(c, cur, manifest, data)
}

func makeSnapContents(c *C, manifest, data string) string {
	tmp := c.MkDir()
	err := os.MkdirAll(filepath.Join(tmp, "meta", "hooks", "dir"), 0755)
	c.Assert(err, IsNil)

	// our regular snap.yaml
	err = os.WriteFile(filepath.Join(tmp, "meta", "snap.yaml"), []byte(manifest), 0644)
	c.Assert(err, IsNil)

	// some hooks
	err = os.WriteFile(filepath.Join(tmp, "meta", "hooks", "foo-hook"), nil, 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(tmp, "meta", "hooks", "bar-hook"), nil, 0755)
	c.Assert(err, IsNil)
	// And a file in another directory in there, just for testing (not a valid
	// hook)
	err = os.WriteFile(filepath.Join(tmp, "meta", "hooks", "dir", "baz"), nil, 0755)
	c.Assert(err, IsNil)

	// some empty directories
	err = os.MkdirAll(filepath.Join(tmp, "food", "bard", "bazd"), 0755)
	c.Assert(err, IsNil)

	err = os.Symlink("target", filepath.Join(tmp, "symlink"))
	c.Assert(err, IsNil)

	// some data
	err = os.WriteFile(filepath.Join(tmp, "data.bin"), []byte(data), 0644)
	c.Assert(err, IsNil)

	return tmp
}

func makeSnapInDir(c *C, dir, manifest, data string) *squashfs.Snap {
	snapType := "app"
	var m struct {
		Type string `yaml:"type"`
	}
	if err := yaml.Unmarshal([]byte(manifest), &m); err == nil && m.Type != "" {
		snapType = m.Type
	}

	tmp := makeSnapContents(c, manifest, data)
	// build it
	sn := squashfs.New(filepath.Join(dir, "foo.snap"))
	err := sn.Build(tmp, &squashfs.BuildOpts{SnapType: snapType})
	c.Assert(err, IsNil)

	return sn
}

type SquashfsTestSuite struct {
	testutil.BaseTest

	oldStdout, oldStderr, outf *os.File
}

var _ = Suite(&SquashfsTestSuite{})

func (s *SquashfsTestSuite) SetUpTest(c *C) {
	d := c.MkDir()
	dirs.SetRootDir(d)
	s.AddCleanup(func() { dirs.SetRootDir("") })
	err := os.Chdir(d)
	c.Assert(err, IsNil)

	restore := osutil.MockMountInfo("")
	s.AddCleanup(restore)

	s.outf, err = os.CreateTemp(c.MkDir(), "")
	c.Assert(err, IsNil)
	s.oldStdout, s.oldStderr = os.Stdout, os.Stderr
	os.Stdout, os.Stderr = s.outf, s.outf
}

func (s *SquashfsTestSuite) TearDownTest(c *C) {
	os.Stdout, os.Stderr = s.oldStdout, s.oldStderr

	// this ensures things were quiet
	_, err := s.outf.Seek(0, 0)
	c.Assert(err, IsNil)
	outbuf, err := io.ReadAll(s.outf)
	c.Assert(err, IsNil)
	c.Check(string(outbuf), Equals, "")
}

func (s *SquashfsTestSuite) TestFileHasSquashfsHeader(c *C) {
	sn := makeSnap(c, "name: test", "")
	c.Check(squashfs.FileHasSquashfsHeader(sn.Path()), Equals, true)
}

func (s *SquashfsTestSuite) TestNotFileHasSquashfsHeader(c *C) {
	data := []string{
		"hsqs",
		"hsqs\x00",
		"hsqs" + strings.Repeat("\x00", squashfs.SuperblockSize-4),
		"hsqt" + strings.Repeat("\x00", squashfs.SuperblockSize-4+1),
		"not a snap",
	}

	for _, d := range data {
		err := os.WriteFile("not-a-snap", []byte(d), 0644)
		c.Assert(err, IsNil)

		c.Check(squashfs.FileHasSquashfsHeader("not-a-snap"), Equals, false)
	}
}

func (s *SquashfsTestSuite) TestInstallSimpleNoCp(c *C) {
	// mock cp but still cp
	cmd := testutil.MockCommand(c, "cp", `#!/bin/sh
exec /bin/cp "$@"
`)
	defer cmd.Restore()
	// mock link but still link
	linked := 0
	r := squashfs.MockLink(func(a, b string) error {
		linked++
		return os.Link(a, b)
	})
	defer r()

	sn := makeSnap(c, "name: test", "")
	targetPath := filepath.Join(c.MkDir(), "target.snap")
	mountDir := c.MkDir()
	didNothing, err := sn.Install(targetPath, mountDir, nil)
	c.Assert(err, IsNil)
	c.Assert(didNothing, Equals, false)
	c.Check(osutil.FileExists(targetPath), Equals, true)
	c.Check(linked, Equals, 1)
	c.Check(cmd.Calls(), HasLen, 0)
}

func (s *SquashfsTestSuite) TestInstallSimpleOnOverlayfs(c *C) {
	cmd := testutil.MockCommand(c, "cp", "")
	defer cmd.Restore()

	// mock link but still link
	linked := 0
	r := squashfs.MockLink(func(a, b string) error {
		linked++
		return os.Link(a, b)
	})
	defer r()

	// pretend we are on overlayfs
	restore := squashfs.MockIsRootWritableOverlay(func() (string, error) {
		return "/upper", nil
	})
	defer restore()

	c.Assert(os.MkdirAll(dirs.SnapSeedDir, 0755), IsNil)
	sn := makeSnapInDir(c, dirs.SnapSeedDir, "name: test2", "")
	targetPath := filepath.Join(c.MkDir(), "target.snap")
	_, err := os.Lstat(targetPath)
	c.Check(os.IsNotExist(err), Equals, true)

	didNothing, err := sn.Install(targetPath, c.MkDir(), nil)
	c.Assert(err, IsNil)
	c.Assert(didNothing, Equals, false)
	// symlink in place
	c.Check(osutil.IsSymlink(targetPath), Equals, true)
	// no link / no cp
	c.Check(linked, Equals, 0)
	c.Check(cmd.Calls(), HasLen, 0)
}

func noLink() func() {
	return squashfs.MockLink(func(string, string) error { return errors.New("no.") })
}

func (s *SquashfsTestSuite) TestInstallNotCopyTwice(c *C) {
	// first, disable os.Link
	defer noLink()()

	// then, mock cp but still cp
	cmd := testutil.MockCommand(c, "cp", `#!/bin/sh
exec /bin/cp "$@"
`)
	defer cmd.Restore()

	sn := makeSnap(c, "name: test2", "")
	targetPath := filepath.Join(c.MkDir(), "target.snap")
	mountDir := c.MkDir()
	didNothing, err := sn.Install(targetPath, mountDir, nil)
	c.Assert(err, IsNil)
	c.Assert(didNothing, Equals, false)
	c.Check(cmd.Calls(), HasLen, 1)

	didNothing, err = sn.Install(targetPath, mountDir, nil)
	c.Assert(err, IsNil)
	c.Assert(didNothing, Equals, true)
	c.Check(cmd.Calls(), HasLen, 1) // and not 2 \o/
}

func (s *SquashfsTestSuite) TestInstallSeedNoLink(c *C) {
	defer noLink()()

	c.Assert(os.MkdirAll(dirs.SnapSeedDir, 0755), IsNil)
	sn := makeSnapInDir(c, dirs.SnapSeedDir, "name: test2", "")
	targetPath := filepath.Join(c.MkDir(), "target.snap")
	_, err := os.Lstat(targetPath)
	c.Check(os.IsNotExist(err), Equals, true)

	didNothing, err := sn.Install(targetPath, c.MkDir(), nil)
	c.Assert(err, IsNil)
	c.Assert(didNothing, Equals, false)
	c.Check(osutil.IsSymlink(targetPath), Equals, true) // \o/
}

func (s *SquashfsTestSuite) TestInstallUC20SeedNoLink(c *C) {
	defer noLink()()

	systemSnapsDir := filepath.Join(dirs.SnapSeedDir, "systems", "20200521", "snaps")
	c.Assert(os.MkdirAll(systemSnapsDir, 0755), IsNil)
	snap := makeSnapInDir(c, systemSnapsDir, "name: test2", "")
	targetPath := filepath.Join(c.MkDir(), "target.snap")
	_, err := os.Lstat(targetPath)
	c.Check(os.IsNotExist(err), Equals, true)

	didNothing, err := snap.Install(targetPath, c.MkDir(), nil)
	c.Assert(err, IsNil)
	c.Assert(didNothing, Equals, false)
	c.Check(osutil.IsSymlink(targetPath), Equals, true) // \o/
}

func (s *SquashfsTestSuite) TestInstallMustNotCrossDevices(c *C) {
	defer noLink()()

	c.Assert(os.MkdirAll(dirs.SnapSeedDir, 0755), IsNil)
	sn := makeSnapInDir(c, dirs.SnapSeedDir, "name: test2", "")
	targetPath := filepath.Join(c.MkDir(), "target.snap")
	_, err := os.Lstat(targetPath)
	c.Check(os.IsNotExist(err), Equals, true)

	didNothing, err := sn.Install(targetPath, c.MkDir(), &snap.InstallOptions{MustNotCrossDevices: true})
	c.Assert(err, IsNil)
	c.Assert(didNothing, Equals, false)
	c.Check(osutil.IsSymlink(targetPath), Equals, false)
}

func (s *SquashfsTestSuite) TestInstallNothingToDo(c *C) {
	sn := makeSnap(c, "name: test2", "")

	targetPath := filepath.Join(c.MkDir(), "foo.snap")
	c.Assert(os.Symlink(sn.Path(), targetPath), IsNil)

	didNothing, err := sn.Install(targetPath, c.MkDir(), nil)
	c.Assert(err, IsNil)
	c.Check(didNothing, Equals, true)
}

func (s *SquashfsTestSuite) TestPath(c *C) {
	p := "/path/to/foo.snap"
	sn := squashfs.New("/path/to/foo.snap")
	c.Assert(sn.Path(), Equals, p)
}

func (s *SquashfsTestSuite) TestReadFile(c *C) {
	sn := makeSnap(c, "name: foo", "")

	content, err := sn.ReadFile("meta/snap.yaml")
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "name: foo")
}

func (s *SquashfsTestSuite) TestReadFileFail(c *C) {
	mockUnsquashfs := testutil.MockCommand(c, "unsquashfs", `echo boom; exit 1`)
	defer mockUnsquashfs.Restore()

	sn := makeSnap(c, "name: foo", "")
	_, err := sn.ReadFile("meta/snap.yaml")
	c.Assert(err, ErrorMatches, "cannot run unsquashfs: boom")
}

func (s *SquashfsTestSuite) TestReadlink(c *C) {
	sn := makeSnap(c, "name: foo", "")

	target, err := sn.ReadLink("symlink")
	c.Assert(err, IsNil)
	c.Assert(target, Equals, "target")
}

func (s *SquashfsTestSuite) TestReadlinkFail(c *C) {
	sn := makeSnap(c, "name: foo", "")

	target, err := sn.ReadLink("meta/snap.yaml")
	c.Assert(err, ErrorMatches, "readlink .*/meta/snap.yaml: invalid argument")
	c.Assert(target, Equals, "")
}

func (s *SquashfsTestSuite) TestLstat(c *C) {
	sn := makeSnap(c, "name: foo", "")

	base := c.MkDir()
	c.Assert(sn.Unpack("*", base), IsNil)

	for _, file := range []string{
		"symlink",
		"meta",
		"meta/snap.yaml",
		"meta/hooks/dir",
	} {
		expectedInfo, err := os.Lstat(filepath.Join(base, file))
		c.Assert(err, IsNil)
		info, err := sn.Lstat(file)
		c.Assert(err, IsNil)

		c.Check(info.Name(), Equals, expectedInfo.Name())
		c.Check(info.Mode(), Equals, expectedInfo.Mode())
		// sometimes 4096 bytes is the smallest allocation unit for some
		// filesystems. let's just skip size check for directories.
		if !expectedInfo.IsDir() {
			c.Check(info.Size(), Equals, expectedInfo.Size())
		}
	}
}

func (s *SquashfsTestSuite) TestLstatErrNotExist(c *C) {
	sn := makeSnap(c, "name: foo", "")

	_, err := sn.Lstat("meta/non-existent")
	c.Check(errors.Is(err, os.ErrNotExist), Equals, true)
}

func (s *SquashfsTestSuite) TestRandomAccessFile(c *C) {
	sn := makeSnap(c, "name: foo", "")

	r, err := sn.RandomAccessFile("meta/snap.yaml")
	c.Assert(err, IsNil)
	defer r.Close()

	c.Assert(r.Size(), Equals, int64(9))

	b := make([]byte, 4)
	n, err := r.ReadAt(b, 4)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 4)
	c.Check(string(b), Equals, ": fo")
}

func (s *SquashfsTestSuite) TestListDir(c *C) {
	sn := makeSnap(c, "name: foo", "")

	fileNames, err := sn.ListDir("meta/hooks")
	c.Assert(err, IsNil)
	c.Assert(len(fileNames), Equals, 3)
	c.Check(fileNames[0], Equals, "bar-hook")
	c.Check(fileNames[1], Equals, "dir")
	c.Check(fileNames[2], Equals, "foo-hook")
}

func (s *SquashfsTestSuite) TestWalkNative(c *C) {
	sub := "."
	sn := makeSnap(c, "name: foo", "")
	sqw := map[string]os.FileInfo{}
	sn.Walk(sub, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == "food" {
			return filepath.SkipDir
		}
		sqw[path] = info
		return nil
	})

	base := c.MkDir()
	c.Assert(sn.Unpack("*", base), IsNil)

	sdw := map[string]os.FileInfo{}
	snapdir.New(base).Walk(sub, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == "food" {
			return filepath.SkipDir
		}
		sdw[path] = info
		return nil
	})

	fpw := map[string]os.FileInfo{}
	filepath.Walk(filepath.Join(base, sub), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		path, err = filepath.Rel(base, path)
		if err != nil {
			return err
		}
		if path == "food" {
			return filepath.SkipDir
		}
		fpw[path] = info
		return nil
	})

	for k := range fpw {
		squashfs.Alike(sqw[k], fpw[k], c, Commentf(k))
		squashfs.Alike(sdw[k], fpw[k], c, Commentf(k))
	}

	for k := range sqw {
		squashfs.Alike(fpw[k], sqw[k], c, Commentf(k))
		squashfs.Alike(sdw[k], sqw[k], c, Commentf(k))
	}

	for k := range sdw {
		squashfs.Alike(fpw[k], sdw[k], c, Commentf(k))
		squashfs.Alike(sqw[k], sdw[k], c, Commentf(k))
	}

}

func (s *SquashfsTestSuite) TestWalkRelativeSingleFile(c *C) {
	sn := makeSnap(c, "name: foo", "")

	cnt := 0
	found := false
	err := sn.Walk("meta/snap.yaml", func(path string, info os.FileInfo, err error) error {
		if path == "meta/snap.yaml" {
			found = true
		}
		cnt++
		return nil
	})
	c.Assert(err, IsNil)

	c.Check(found, Equals, true)
	c.Check(cnt, Equals, 1)
}

func (s *SquashfsTestSuite) TestWalkRelativeDirectory(c *C) {
	sn := makeSnap(c, "name: foo", "")

	cnt := 0
	found := map[string]bool{}
	err := sn.Walk("food", func(path string, info os.FileInfo, err error) error {
		found[path] = true
		cnt++
		return nil
	})
	c.Assert(err, IsNil)

	c.Check(found["food"], Equals, true)
	c.Check(found["food/bard"], Equals, true)
	c.Check(found["food/bard/bazd"], Equals, true)
	c.Check(cnt, Equals, 3)
}

func (s *SquashfsTestSuite) testWalkMockedUnsquashfs(c *C) {
	expectingNames := []string{
		".",
		"data.bin",
		"food",
		"meta",
		"meta/hooks",
		"meta/hooks/bar-hook",
		"meta/hooks/dir",
		"meta/hooks/dir/baz",
		"meta/hooks/foo-hook",
		"meta/snap.yaml",
	}
	sub := "."
	sn := makeSnap(c, "name: foo", "")
	var seen []string
	sn.Walk(sub, func(path string, info os.FileInfo, err error) error {
		c.Logf("got %v", path)
		if err != nil {
			return err
		}
		seen = append(seen, path)
		if path == "food" {
			return filepath.SkipDir
		}
		return nil
	})
	c.Assert(len(seen), Equals, len(expectingNames))
	for idx, name := range seen {
		c.Check(name, Equals, expectingNames[idx])
	}
}

func (s *SquashfsTestSuite) TestWalkMockedUnsquashfs45(c *C) {
	// mock behavior of squashfs-tools 4.5 and later
	mockUnsquashfs := testutil.MockCommand(c, "unsquashfs", `
cat <<EOF
drwx------ root/root                55 2021-07-27 13:31 .
-rw-r--r-- root/root                 0 2021-07-27 13:31 ./data.bin
drwxr-xr-x root/root                27 2021-07-27 13:31 ./food
drwxr-xr-x root/root                27 2021-07-27 13:31 ./food/bard
drwxr-xr-x root/root                 3 2021-07-27 13:31 ./food/bard/bazd
drwxr-xr-x root/root                45 2021-07-27 13:31 ./meta
drwxr-xr-x root/root                58 2021-07-27 13:31 ./meta/hooks
-rwxr-xr-x root/root                 0 2021-07-27 13:31 ./meta/hooks/bar-hook
drwxr-xr-x root/root                26 2021-07-27 13:31 ./meta/hooks/dir
-rwxr-xr-x root/root                 0 2021-07-27 13:31 ./meta/hooks/dir/baz
-rwxr-xr-x root/root                 0 2021-07-27 13:31 ./meta/hooks/foo-hook
-rw-r--r-- root/root                 9 2021-07-27 13:31 ./meta/snap.yaml
EOF
`)
	defer mockUnsquashfs.Restore()
	s.testWalkMockedUnsquashfs(c)
}

func (s *SquashfsTestSuite) TestWalkMockedUnsquashfsOld(c *C) {
	// mock behavior of pre-4.5 squashfs-tools
	mockUnsquashfs := testutil.MockCommand(c, "unsquashfs", `
cat <<EOF
Parallel unsquashfs: Using 1 processor
5 inodes (1 blocks) to write

drwx------ root/root                55 2021-07-27 13:31 .
-rw-r--r-- root/root                 0 2021-07-27 13:31 ./data.bin
drwxr-xr-x root/root                27 2021-07-27 13:31 ./food
drwxr-xr-x root/root                27 2021-07-27 13:31 ./food/bard
drwxr-xr-x root/root                 3 2021-07-27 13:31 ./food/bard/bazd
drwxr-xr-x root/root                45 2021-07-27 13:31 ./meta
drwxr-xr-x root/root                58 2021-07-27 13:31 ./meta/hooks
-rwxr-xr-x root/root                 0 2021-07-27 13:31 ./meta/hooks/bar-hook
drwxr-xr-x root/root                26 2021-07-27 13:31 ./meta/hooks/dir
-rwxr-xr-x root/root                 0 2021-07-27 13:31 ./meta/hooks/dir/baz
-rwxr-xr-x root/root                 0 2021-07-27 13:31 ./meta/hooks/foo-hook
-rw-r--r-- root/root                 9 2021-07-27 13:31 ./meta/snap.yaml
EOF
`)
	defer mockUnsquashfs.Restore()
	s.testWalkMockedUnsquashfs(c)
}

// TestUnpackGlob tests the internal unpack
func (s *SquashfsTestSuite) TestUnpackGlob(c *C) {
	data := "some random data"
	sn := makeSnap(c, "", data)

	outputDir := c.MkDir()
	err := sn.Unpack("data*", outputDir)
	c.Assert(err, IsNil)

	// this is the file we expect
	c.Assert(filepath.Join(outputDir, "data.bin"), testutil.FileEquals, data)

	// ensure glob was honored
	c.Assert(osutil.FileExists(filepath.Join(outputDir, "meta/snap.yaml")), Equals, false)
}

func (s *SquashfsTestSuite) TestUnpackDetectsFailures(c *C) {
	mockUnsquashfs := testutil.MockCommand(c, "unsquashfs", `
cat >&2 <<EOF
Failed to write /tmp/1/modules/4.4.0-112-generic/modules.symbols, skipping

Write on output file failed because No space left on device

writer: failed to write data block 0

Failed to write /tmp/1/modules/4.4.0-112-generic/modules.symbols.bin, skipping

Write on output file failed because No space left on device

writer: failed to write data block 0

Failed to write /tmp/1/modules/4.4.0-112-generic/vdso/vdso32.so, skipping

Write on output file failed because No space left on device

writer: failed to write data block 0

Failed to write /tmp/1/modules/4.4.0-112-generic/vdso/vdso64.so, skipping

Write on output file failed because No space left on device

writer: failed to write data block 0

Failed to write /tmp/1/modules/4.4.0-112-generic/vdso/vdsox32.so, skipping

Write on output file failed because No space left on device

writer: failed to write data block 0

Failed to write /tmp/1/snap/manifest.yaml, skipping

Write on output file failed because No space left on device

writer: failed to write data block 0

Failed to write /tmp/1/snap/snapcraft.yaml, skipping
EOF
`)
	defer mockUnsquashfs.Restore()

	data := "mock kernel snap"
	sn := makeSnap(c, "", data)
	err := sn.Unpack("*", "some-output-dir")
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `cannot extract "*" to "some-output-dir": failed: "Failed to write /tmp/1/modules/4.4.0-112-generic/modules.symbols, skipping", "Write on output file failed because No space left on device", "writer: failed to write data block 0", "Failed to write /tmp/1/modules/4.4.0-112-generic/modules.symbols.bin, skipping", and 15 more`)
}

func (s *SquashfsTestSuite) TestUnpackDetectsFailuresViaExitCode(c *C) {
	mockUnsquashfs := testutil.MockCommand(c, "unsquashfs", `
cat <<EOF
Parallel unsquashfs: Using 16 processors
10522 inodes (11171 blocks) to write
EOF

cat >&2 <<EOF
Write on output file failed because No space left on device

FATAL ERROR: writer: failed to write file squashfs-root/etc/modprobe.d/some.conf
EOF
exit 1
`)
	defer mockUnsquashfs.Restore()

	data := "mock kernel snap"
	sn := makeSnap(c, "", data)
	err := sn.Unpack("*", "some-output-dir")
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `cannot extract "*" to "some-output-dir": 
-----
Write on output file failed because No space left on device

FATAL ERROR: writer: failed to write file squashfs-root/etc/modprobe.d/some.conf
-----`)
}

func (s *SquashfsTestSuite) TestBuildAll(c *C) {
	// please keep TestBuildUsesExcludes in sync with this one so it makes sense.
	buildDir := c.MkDir()
	err := os.MkdirAll(filepath.Join(buildDir, "/random/dir"), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(buildDir, "data.bin"), []byte("data"), 0644)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(buildDir, "random", "data.bin"), []byte("more data"), 0644)
	c.Assert(err, IsNil)

	sn := squashfs.New(filepath.Join(c.MkDir(), "foo.snap"))
	err = sn.Build(buildDir, &squashfs.BuildOpts{SnapType: "app"})
	c.Assert(err, IsNil)

	// pre-4.5 unsquashfs writes a funny header like:
	//     "Parallel unsquashfs: Using 1 processor"
	//     "1 inodes (1 blocks) to write"
	outputWithHeader, err := exec.Command("unsquashfs", "-n", "-l", sn.Path()).Output()
	c.Assert(err, IsNil)
	output := outputWithHeader
	if bytes.HasPrefix(outputWithHeader, []byte(`Parallel unsquashfs: `)) {
		split := bytes.Split(outputWithHeader, []byte("\n"))
		output = bytes.Join(split[3:], []byte("\n"))
	}
	c.Assert(string(output), Equals, `
squashfs-root
squashfs-root/data.bin
squashfs-root/random
squashfs-root/random/data.bin
squashfs-root/random/dir
`[1:]) // skip the first newline :-)
}

func (s *SquashfsTestSuite) TestBuildUsesExcludes(c *C) {
	// please keep TestBuild in sync with this one so it makes sense.
	buildDir := c.MkDir()
	err := os.MkdirAll(filepath.Join(buildDir, "/random/dir"), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(buildDir, "data.bin"), []byte("data"), 0644)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(buildDir, "random", "data.bin"), []byte("more data"), 0644)
	c.Assert(err, IsNil)

	excludesFilename := filepath.Join(buildDir, ".snapignore")
	err = os.WriteFile(excludesFilename, []byte(`
# ignore just one of the data.bin files we just added (the toplevel one)
data.bin
# also ignore ourselves
.snapignore
# oh and anything called "dir" anywhere
... dir
`), 0644)
	c.Assert(err, IsNil)

	sn := squashfs.New(filepath.Join(c.MkDir(), "foo.snap"))
	err = sn.Build(buildDir, &squashfs.BuildOpts{
		SnapType:     "app",
		ExcludeFiles: []string{excludesFilename},
	})
	c.Assert(err, IsNil)

	outputWithHeader, err := exec.Command("unsquashfs", "-n", "-l", sn.Path()).Output()
	c.Assert(err, IsNil)
	output := outputWithHeader
	if bytes.HasPrefix(outputWithHeader, []byte(`Parallel unsquashfs: `)) {
		split := bytes.Split(outputWithHeader, []byte("\n"))
		output = bytes.Join(split[3:], []byte("\n"))
	}
	// compare with TestBuild
	c.Assert(string(output), Equals, `
squashfs-root
squashfs-root/random
squashfs-root/random/data.bin
`[1:]) // skip the first newline :-)
}

func (s *SquashfsTestSuite) TestBuildSupportsMultipleExcludesWithOnlyOneWildcardsFlag(c *C) {
	defer squashfs.MockCommandFromSystemSnap(func(cmd string, args ...string) (*exec.Cmd, error) {
		c.Check(cmd, Equals, "/usr/bin/mksquashfs")
		return nil, errors.New("bzzt")
	})()
	mksq := testutil.MockCommand(c, "mksquashfs", `/usr/bin/mksquashfs "$@"`)
	defer mksq.Restore()

	fakeSourcedir := c.MkDir()
	for _, n := range []string{"exclude1", "exclude2", "exclude3"} {
		err := os.WriteFile(filepath.Join(fakeSourcedir, n), nil, 0644)
		c.Assert(err, IsNil)
	}
	snapPath := filepath.Join(c.MkDir(), "foo.snap")
	sn := squashfs.New(snapPath)
	err := sn.Build(fakeSourcedir, &squashfs.BuildOpts{
		SnapType:     "core",
		ExcludeFiles: []string{"exclude1", "exclude2", "exclude3"},
	})
	c.Assert(err, IsNil)
	calls := mksq.Calls()
	c.Assert(calls, HasLen, 1)
	c.Check(calls[0], DeepEquals, []string{
		// the usual:
		"mksquashfs", ".", snapPath, "-noappend", "-comp", "xz", "-no-fragments", "-no-progress",
		// the interesting bits:
		"-wildcards", "-ef", "exclude1", "-ef", "exclude2", "-ef", "exclude3",
	})
}

func (s *SquashfsTestSuite) TestBuildUsesMksquashfsFromCoreIfAvailable(c *C) {
	usedFromCore := false
	defer squashfs.MockCommandFromSystemSnap(func(cmd string, args ...string) (*exec.Cmd, error) {
		usedFromCore = true
		c.Check(cmd, Equals, "/usr/bin/mksquashfs")
		fakeCmd := exec.Cmd{Path: "/usr/bin/mksquashfs", Args: []string{"/usr/bin/mksquashfs"}}
		return &fakeCmd, nil
	})()
	mksq := testutil.MockCommand(c, "mksquashfs", "exit 1")
	defer mksq.Restore()

	buildDir := c.MkDir()

	sn := squashfs.New(filepath.Join(c.MkDir(), "foo.snap"))
	err := sn.Build(buildDir, nil)
	c.Assert(err, IsNil)
	c.Check(usedFromCore, Equals, true)
	c.Check(mksq.Calls(), HasLen, 0)
}

func (s *SquashfsTestSuite) TestBuildUsesMksquashfsFromClassicIfCoreUnavailable(c *C) {
	triedFromCore := false
	defer squashfs.MockCommandFromSystemSnap(func(cmd string, args ...string) (*exec.Cmd, error) {
		triedFromCore = true
		c.Check(cmd, Equals, "/usr/bin/mksquashfs")
		return nil, errors.New("bzzt")
	})()
	mksq := testutil.MockCommand(c, "mksquashfs", `/usr/bin/mksquashfs "$@"`)
	defer mksq.Restore()

	buildDir := c.MkDir()

	sn := squashfs.New(filepath.Join(c.MkDir(), "foo.snap"))
	err := sn.Build(buildDir, nil)
	c.Assert(err, IsNil)
	c.Check(triedFromCore, Equals, true)
	c.Check(mksq.Calls(), HasLen, 1)
}

func (s *SquashfsTestSuite) TestBuildFailsIfNoMksquashfs(c *C) {
	triedFromCore := false
	defer squashfs.MockCommandFromSystemSnap(func(cmd string, args ...string) (*exec.Cmd, error) {
		triedFromCore = true
		c.Check(cmd, Equals, "/usr/bin/mksquashfs")
		return nil, errors.New("bzzt")
	})()
	mksq := testutil.MockCommand(c, "mksquashfs", "exit 1")
	defer mksq.Restore()

	buildDir := c.MkDir()

	sn := squashfs.New(filepath.Join(c.MkDir(), "foo.snap"))
	err := sn.Build(buildDir, nil)
	c.Assert(err, ErrorMatches, "mksquashfs call failed:.*")
	c.Check(triedFromCore, Equals, true)
	c.Check(mksq.Calls(), HasLen, 1)
}

func (s *SquashfsTestSuite) TestBuildVariesArgsByType(c *C) {
	defer squashfs.MockCommandFromSystemSnap(func(cmd string, args ...string) (*exec.Cmd, error) {
		return nil, errors.New("bzzt")
	})()
	mksq := testutil.MockCommand(c, "mksquashfs", `/usr/bin/mksquashfs "$@"`)
	defer mksq.Restore()

	buildDir := c.MkDir()
	filename := filepath.Join(c.MkDir(), "foo.snap")
	snap := squashfs.New(filename)

	permissiveTypeArgs := []string{".", filename, "-noappend", "-comp", "xz", "-no-fragments", "-no-progress"}
	restrictedTypeArgs := append(permissiveTypeArgs, "-all-root", "-no-xattrs")
	tests := []struct {
		snapType string
		args     []string
	}{
		{"", restrictedTypeArgs},
		{"app", restrictedTypeArgs},
		{"gadget", restrictedTypeArgs},
		{"kernel", restrictedTypeArgs},
		{"snapd", restrictedTypeArgs},
		{"base", permissiveTypeArgs},
		{"os", permissiveTypeArgs},
		{"core", permissiveTypeArgs},
	}

	for _, t := range tests {
		mksq.ForgetCalls()
		comm := Commentf("type: %s", t.snapType)

		c.Check(snap.Build(buildDir, &squashfs.BuildOpts{SnapType: t.snapType}), IsNil, comm)
		c.Assert(mksq.Calls(), HasLen, 1, comm)
		c.Assert(mksq.Calls()[0], HasLen, len(t.args)+1)
		c.Check(mksq.Calls()[0][0], Equals, "mksquashfs", comm)
		c.Check(mksq.Calls()[0][1:], DeepEquals, t.args, comm)
	}
}

func (s *SquashfsTestSuite) TestBuildReportsFailures(c *C) {
	mockUnsquashfs := testutil.MockCommand(c, "mksquashfs", `
echo Yeah, nah. >&2
exit 1
`)
	defer mockUnsquashfs.Restore()

	data := "mock kernel snap"
	dir := makeSnapContents(c, "", data)
	sn := squashfs.New("foo.snap")
	c.Check(sn.Build(dir, &squashfs.BuildOpts{SnapType: "kernel"}), ErrorMatches, `mksquashfs call failed: Yeah, nah.`)
}

func (s *SquashfsTestSuite) TestUnsquashfsStderrWriter(c *C) {
	for _, t := range []struct {
		inp         []string
		expectedErr string
	}{
		{
			inp:         []string{"failed to write something\n"},
			expectedErr: `failed: "failed to write something"`,
		},
		{
			inp:         []string{"fai", "led to write", " something\nunrelated\n"},
			expectedErr: `failed: "failed to write something"`,
		},
		{
			inp:         []string{"failed to write\nfailed to read\n"},
			expectedErr: `failed: "failed to write", and "failed to read"`,
		},
		{
			inp:         []string{"failed 1\nfailed 2\n3 failed\n"},
			expectedErr: `failed: "failed 1", "failed 2", and "3 failed"`,
		},
		{
			inp:         []string{"failed 1\nfailed 2\n3 Failed\n4 Failed\n"},
			expectedErr: `failed: "failed 1", "failed 2", "3 Failed", and "4 Failed"`,
		},
		{
			inp:         []string{"failed 1\nfailed 2\n3 Failed\n4 Failed\nfailed #5\n"},
			expectedErr: `failed: "failed 1", "failed 2", "3 Failed", "4 Failed", and 1 more`,
		},
	} {
		usw := squashfs.NewUnsquashfsStderrWriter()
		for _, l := range t.inp {
			usw.Write([]byte(l))
		}
		if t.expectedErr != "" {
			c.Check(usw.Err(), ErrorMatches, t.expectedErr, Commentf("inp: %q failed", t.inp))
		} else {
			c.Check(usw.Err(), IsNil)
		}
	}
}

func (s *SquashfsTestSuite) TestBuildDate(c *C) {
	// This env is used in reproducible builds and will force
	// squashfs to use a specific date. We need to unset it
	// for this specific test.
	if oldEnv := os.Getenv("SOURCE_DATE_EPOCH"); oldEnv != "" {
		os.Unsetenv("SOURCE_DATE_EPOCH")
		defer func() { os.Setenv("SOURCE_DATE_EPOCH", oldEnv) }()
	}

	// make a directory
	d := c.MkDir()
	// set its time waaay back
	now := time.Now()
	then := now.Add(-10000 * time.Hour)
	c.Assert(os.Chtimes(d, then, then), IsNil)
	// make a snap using this directory
	filename := filepath.Join(c.MkDir(), "foo.snap")
	sn := squashfs.New(filename)
	c.Assert(sn.Build(d, nil), IsNil)
	// and see it's BuildDate is _now_, not _then_.
	c.Check(squashfs.BuildDate(filename), Equals, sn.BuildDate())
	c.Check(math.Abs(now.Sub(sn.BuildDate()).Seconds()) <= 61, Equals, true, Commentf("Unexpected build date %s", sn.BuildDate()))
}

func (s *SquashfsTestSuite) TestBuildChecksReadDifferentFiles(c *C) {
	if os.Geteuid() == 0 {
		c.Skip("cannot be tested when running as root")
	}
	// make a directory
	d := c.MkDir()

	err := os.MkdirAll(filepath.Join(d, "ro-dir"), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(d, "ro-dir", "in-ro-dir"), []byte("123"), 0664)
	c.Assert(err, IsNil)
	err = os.Chmod(filepath.Join(d, "ro-dir"), 0000)
	c.Assert(err, IsNil)
	// so that tear down does not complain
	defer os.Chmod(filepath.Join(d, "ro-dir"), 0755)

	err = os.WriteFile(filepath.Join(d, "ro-file"), []byte("123"), 0000)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(d, "ro-empty-file"), nil, 0000)
	c.Assert(err, IsNil)

	err = syscall.Mkfifo(filepath.Join(d, "fifo"), 0000)
	c.Assert(err, IsNil)

	filename := filepath.Join(c.MkDir(), "foo.snap")
	sn := squashfs.New(filename)
	err = sn.Build(d, nil)
	c.Assert(err, ErrorMatches, `(?s)cannot access the following locations in the snap source directory:
- ro-(file|dir) \(owner [0-9]+:[0-9]+ mode 000\)
- ro-(file|dir) \(owner [0-9]+:[0-9]+ mode 000\)
`)

}

func (s *SquashfsTestSuite) TestBuildChecksReadErrorLimit(c *C) {
	if os.Geteuid() == 0 {
		c.Skip("cannot be tested when running as root")
	}
	// make a directory
	d := c.MkDir()

	// make more than maxErrPaths entries
	for i := 0; i < squashfs.MaxErrPaths; i++ {
		p := filepath.Join(d, fmt.Sprintf("0%d", i))
		err := os.WriteFile(p, []byte("123"), 0000)
		c.Assert(err, IsNil)
		err = os.Chmod(p, 0000)
		c.Assert(err, IsNil)
	}
	filename := filepath.Join(c.MkDir(), "foo.snap")
	sn := squashfs.New(filename)
	err := sn.Build(d, nil)
	c.Assert(err, ErrorMatches, `(?s)cannot access the following locations in the snap source directory:
(- [0-9]+ \(owner [0-9]+:[0-9]+ mode 000.*\).){10}- too many errors, listing first 10 entries
`)
}

func (s *SquashfsTestSuite) TestBuildBadSource(c *C) {
	filename := filepath.Join(c.MkDir(), "foo.snap")
	sn := squashfs.New(filename)
	err := sn.Build("does-not-exist", nil)
	c.Assert(err, ErrorMatches, ".*does-not-exist/: no such file or directory")
}

func (s *SquashfsTestSuite) TestBuildWithCompressionHappy(c *C) {
	buildDir := c.MkDir()
	err := os.MkdirAll(filepath.Join(buildDir, "/random/dir"), 0755)
	c.Assert(err, IsNil)

	defaultComp := "xz"
	for _, comp := range []string{"", "xz", "gzip", "lzo", "zstd"} {
		sn := squashfs.New(filepath.Join(c.MkDir(), "foo.snap"))
		err = sn.Build(buildDir, &squashfs.BuildOpts{
			Compression: comp,
		})
		c.Assert(err, IsNil)

		// check compression
		outputWithHeader, err := exec.Command("unsquashfs", "-n", "-s", sn.Path()).CombinedOutput()
		c.Assert(err, IsNil)
		// ensure default is xz
		if comp == "" {
			comp = defaultComp
		}
		c.Assert(string(outputWithHeader), Matches, fmt.Sprintf(`(?ms).*Compression %s$`, comp))
	}
}

func (s *SquashfsTestSuite) TestBuildWithCompressionUnhappy(c *C) {
	buildDir := c.MkDir()
	err := os.MkdirAll(filepath.Join(buildDir, "/random/dir"), 0755)
	c.Assert(err, IsNil)

	sn := squashfs.New(filepath.Join(c.MkDir(), "foo.snap"))
	err = sn.Build(buildDir, &squashfs.BuildOpts{
		Compression: "silly",
	})
	c.Assert(err, ErrorMatches, "(?m)^mksquashfs call failed: ")
}

func (s *SquashfsTestSuite) TestBuildBelowMinimumSize(c *C) {
	// this snap is empty. without truncating it to be larger, it should be smaller than
	// the minimum snap size
	sn := squashfs.New(filepath.Join(c.MkDir(), "truncate_me.snap"))
	sn.Build(c.MkDir(), nil)

	size, err := sn.Size()
	c.Assert(err, IsNil)

	switch size {
	case squashfs.MinimumSnapSize:
		// all good
	case 65536:
		// some distros carry out of tree patches for squashfs-tools and
		// pad to 64k by default
	default:
		c.Fatalf("unexpected squashfs size %v", size)
	}
}

func (s *SquashfsTestSuite) TestBuildAboveMinimumSize(c *C) {
	// fill a snap with random data that will not compress well. it should be forced
	// to be bigger than the minimum threshold
	randomData := randutil.RandomString(int(squashfs.MinimumSnapSize * 2))
	sn := makeSnapInDir(c, c.MkDir(), "name: do_not_truncate_me", randomData)

	size, err := sn.Size()
	c.Assert(err, IsNil)

	c.Assert(int(size), testutil.IntGreaterThan, int(squashfs.MinimumSnapSize), Commentf("random snap data: %s", randomData))
}
