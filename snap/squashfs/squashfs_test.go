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

package squashfs

import (
	"errors"
	"io/ioutil"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap/snapdir"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type SquashfsTestSuite struct {
	oldStdout, oldStderr, outf *os.File
}

var _ = Suite(&SquashfsTestSuite{})

func makeSnap(c *C, manifest, data string) *Snap {
	cur, _ := os.Getwd()
	return makeSnapInDir(c, cur, manifest, data)
}

func makeSnapContents(c *C, manifest, data string) string {
	tmp := c.MkDir()
	err := os.MkdirAll(filepath.Join(tmp, "meta", "hooks", "dir"), 0755)
	c.Assert(err, IsNil)

	// our regular snap.yaml
	err = ioutil.WriteFile(filepath.Join(tmp, "meta", "snap.yaml"), []byte(manifest), 0644)
	c.Assert(err, IsNil)

	// some hooks
	err = ioutil.WriteFile(filepath.Join(tmp, "meta", "hooks", "foo-hook"), nil, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(tmp, "meta", "hooks", "bar-hook"), nil, 0755)
	c.Assert(err, IsNil)
	// And a file in another directory in there, just for testing (not a valid
	// hook)
	err = ioutil.WriteFile(filepath.Join(tmp, "meta", "hooks", "dir", "baz"), nil, 0755)
	c.Assert(err, IsNil)

	// some empty directories
	err = os.MkdirAll(filepath.Join(tmp, "food", "bard", "bazd"), 0755)
	c.Assert(err, IsNil)

	// some data
	err = ioutil.WriteFile(filepath.Join(tmp, "data.bin"), []byte(data), 0644)
	c.Assert(err, IsNil)

	return tmp
}

func makeSnapInDir(c *C, dir, manifest, data string) *Snap {
	tmp := makeSnapContents(c, manifest, data)
	// build it
	snap := New(filepath.Join(dir, "foo.snap"))
	err := snap.Build(tmp)
	c.Assert(err, IsNil)

	return snap
}

func (s *SquashfsTestSuite) SetUpTest(c *C) {
	d := c.MkDir()
	dirs.SetRootDir(d)
	err := os.Chdir(d)
	c.Assert(err, IsNil)

	s.outf, err = ioutil.TempFile(c.MkDir(), "")
	c.Assert(err, IsNil)
	s.oldStdout, s.oldStderr = os.Stdout, os.Stderr
	os.Stdout, os.Stderr = s.outf, s.outf
}

func (s *SquashfsTestSuite) TearDownTest(c *C) {
	os.Stdout, os.Stderr = s.oldStdout, s.oldStderr

	// this ensures things were quiet
	_, err := s.outf.Seek(0, 0)
	c.Assert(err, IsNil)
	outbuf, err := ioutil.ReadAll(s.outf)
	c.Assert(err, IsNil)
	c.Check(string(outbuf), Equals, "")
}

func (s *SquashfsTestSuite) TestInstallSimpleNoCp(c *C) {
	// mock cp but still cp
	cmd := testutil.MockCommand(c, "cp", `#!/bin/sh
exec /bin/cp "$@"
`)
	defer cmd.Restore()
	// mock link but still link
	linked := 0
	r := mockLink(func(a, b string) error {
		linked++
		return os.Link(a, b)
	})
	defer r()

	snap := makeSnap(c, "name: test", "")
	targetPath := filepath.Join(c.MkDir(), "target.snap")
	mountDir := c.MkDir()
	err := snap.Install(targetPath, mountDir)
	c.Assert(err, IsNil)
	c.Check(osutil.FileExists(targetPath), Equals, true)
	c.Check(linked, Equals, 1)
	c.Check(cmd.Calls(), HasLen, 0)
}

func mockLink(newLink func(string, string) error) (restore func()) {
	oldLink := osLink
	osLink = newLink
	return func() {
		osLink = oldLink
	}
}

func noLink() func() {
	return mockLink(func(string, string) error { return errors.New("no.") })
}

func (s *SquashfsTestSuite) TestInstallNotCopyTwice(c *C) {
	// first, disable os.Link
	defer noLink()()

	// then, mock cp but still cp
	cmd := testutil.MockCommand(c, "cp", `#!/bin/sh
exec /bin/cp "$@"
`)
	defer cmd.Restore()

	snap := makeSnap(c, "name: test2", "")
	targetPath := filepath.Join(c.MkDir(), "target.snap")
	mountDir := c.MkDir()
	err := snap.Install(targetPath, mountDir)
	c.Assert(err, IsNil)
	c.Check(cmd.Calls(), HasLen, 1)

	err = snap.Install(targetPath, mountDir)
	c.Assert(err, IsNil)
	c.Check(cmd.Calls(), HasLen, 1) // and not 2 \o/
}

func (s *SquashfsTestSuite) TestInstallSeedNoLink(c *C) {
	defer noLink()()

	c.Assert(os.MkdirAll(dirs.SnapSeedDir, 0755), IsNil)
	snap := makeSnapInDir(c, dirs.SnapSeedDir, "name: test2", "")
	targetPath := filepath.Join(c.MkDir(), "target.snap")
	_, err := os.Lstat(targetPath)
	c.Check(os.IsNotExist(err), Equals, true)

	err = snap.Install(targetPath, c.MkDir())
	c.Assert(err, IsNil)
	c.Check(osutil.IsSymlink(targetPath), Equals, true) // \o/
}

func (s *SquashfsTestSuite) TestPath(c *C) {
	p := "/path/to/foo.snap"
	snap := New("/path/to/foo.snap")
	c.Assert(snap.Path(), Equals, p)
}

func (s *SquashfsTestSuite) TestReadFile(c *C) {
	snap := makeSnap(c, "name: foo", "")

	content, err := snap.ReadFile("meta/snap.yaml")
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "name: foo")
}

func (s *SquashfsTestSuite) TestListDir(c *C) {
	snap := makeSnap(c, "name: foo", "")

	fileNames, err := snap.ListDir("meta/hooks")
	c.Assert(err, IsNil)
	c.Assert(len(fileNames), Equals, 3)
	c.Check(fileNames[0], Equals, "bar-hook")
	c.Check(fileNames[1], Equals, "dir")
	c.Check(fileNames[2], Equals, "foo-hook")
}

func alike(a, b os.FileInfo, c *C, comment CommentInterface) {
	c.Check(a, NotNil, comment)
	c.Check(b, NotNil, comment)
	if a == nil || b == nil {
		return
	}

	// the .Name() of the root will be different on non-squashfs things
	_, asq := a.(*stat)
	_, bsq := b.(*stat)
	if !((asq && a.Name() == "/") || (bsq && b.Name() == "/")) {
		c.Check(a.Name(), Equals, b.Name(), comment)
	}

	c.Check(a.Mode(), Equals, b.Mode(), comment)
	if a.Mode().IsRegular() {
		c.Check(a.Size(), Equals, b.Size(), comment)
	}
	am := a.ModTime().UTC().Truncate(time.Minute)
	bm := b.ModTime().UTC().Truncate(time.Minute)
	c.Check(am.Equal(bm), Equals, true, Commentf("%s != %s (%s)", am, bm, comment))
}

func (s *SquashfsTestSuite) TestWalk(c *C) {
	sub := "."
	snap := makeSnap(c, "name: foo", "")
	sqw := map[string]os.FileInfo{}
	snap.Walk(sub, func(path string, info os.FileInfo, err error) error {
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
	c.Assert(snap.Unpack("*", base), IsNil)

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
		alike(sqw[k], fpw[k], c, Commentf(k))
		alike(sdw[k], fpw[k], c, Commentf(k))
	}

	for k := range sqw {
		alike(fpw[k], sqw[k], c, Commentf(k))
		alike(sdw[k], sqw[k], c, Commentf(k))
	}

	for k := range sdw {
		alike(fpw[k], sdw[k], c, Commentf(k))
		alike(sqw[k], sdw[k], c, Commentf(k))
	}

}

// TestUnpackGlob tests the internal unpack
func (s *SquashfsTestSuite) TestUnpackGlob(c *C) {
	data := "some random data"
	snap := makeSnap(c, "", data)

	outputDir := c.MkDir()
	err := snap.Unpack("data*", outputDir)
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
	snap := makeSnap(c, "", data)
	err := snap.Unpack("*", "some-output-dir")
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `cannot extract "*" to "some-output-dir": failed: "Failed to write /tmp/1/modules/4.4.0-112-generic/modules.symbols, skipping", "Write on output file failed because No space left on device", "writer: failed to write data block 0", "Failed to write /tmp/1/modules/4.4.0-112-generic/modules.symbols.bin, skipping", and 15 more`)
}

func (s *SquashfsTestSuite) TestBuild(c *C) {
	buildDir := c.MkDir()
	err := os.MkdirAll(filepath.Join(buildDir, "/random/dir"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(buildDir, "data.bin"), []byte("data"), 0644)
	c.Assert(err, IsNil)

	snap := New(filepath.Join(c.MkDir(), "foo.snap"))
	err = snap.Build(buildDir)
	c.Assert(err, IsNil)

	// unsquashfs writes a funny header like:
	//     "Parallel unsquashfs: Using 1 processor"
	//     "1 inodes (1 blocks) to write"
	outputWithHeader, err := exec.Command("unsquashfs", "-n", "-l", snap.path).Output()
	c.Assert(err, IsNil)
	split := strings.Split(string(outputWithHeader), "\n")
	output := strings.Join(split[3:], "\n")
	c.Assert(string(output), Equals, `squashfs-root
squashfs-root/data.bin
squashfs-root/random
squashfs-root/random/dir
`)
}

func (s *SquashfsTestSuite) TestBuildReportsFailures(c *C) {
	mockUnsquashfs := testutil.MockCommand(c, "mksquashfs", `
echo Yeah, nah. >&2
exit 1
`)
	defer mockUnsquashfs.Restore()

	data := "mock kernel snap"
	dir := makeSnapContents(c, "", data)
	snap := New("foo.snap")
	c.Check(snap.Build(dir), ErrorMatches, `mksquashfs call failed: Yeah, nah.`)
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
		usw := newUnsquashfsStderrWriter()
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
	// make a directory
	d := c.MkDir()
	// set its time waaay back
	now := time.Now()
	then := now.Add(-10000 * time.Hour)
	c.Assert(os.Chtimes(d, then, then), IsNil)
	// make a snap using this directory
	filename := filepath.Join(c.MkDir(), "foo.snap")
	snap := New(filename)
	c.Assert(snap.Build(d), IsNil)
	// and see it's BuildDate is _now_, not _then_.
	c.Check(BuildDate(filename), Equals, snap.BuildDate())
	c.Check(math.Abs(now.Sub(snap.BuildDate()).Seconds()) <= 61, Equals, true, Commentf("Unexpected build date %s", snap.BuildDate()))
}
