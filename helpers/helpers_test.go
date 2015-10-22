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

package helpers

import (
	"compress/gzip"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type HTestSuite struct{}

var _ = Suite(&HTestSuite{})

func (ts *HTestSuite) TestUnpack(c *C) {

	// setup tmpdir
	tmpdir := c.MkDir()
	tmpfile := filepath.Join(tmpdir, "foo.tar.gz")

	// ok, slightly silly
	path := "/etc/fstab"

	// create test dir, symlink, and also test file
	someDir := c.MkDir()
	c.Assert(os.Symlink(path, filepath.Join(someDir, "fstab")), IsNil)

	cmd := exec.Command("tar", "cvzf", tmpfile, path, someDir)
	output, err := cmd.CombinedOutput()
	c.Assert(err, IsNil)
	c.Check(string(output), Matches, `(?ms).*^/etc/fstab`,
		Commentf("Can not find expected output from tar"))

	// unpack
	unpackdir := filepath.Join(tmpdir, "t")
	f, err := os.Open(tmpfile)
	c.Assert(err, IsNil)

	f2, err := gzip.NewReader(f)
	c.Assert(err, IsNil)

	err = UnpackTar(f2, unpackdir, nil)
	c.Assert(err, IsNil)

	// we have the expected file
	_, err = os.Open(filepath.Join(tmpdir, "t/etc/fstab"))
	c.Assert(err, IsNil)

	// and the expected dir is there and has the right mode
	unpackedSomeDir := filepath.Join(tmpdir, "t", someDir)
	c.Assert(IsDirectory(unpackedSomeDir), Equals, true)
	st1, err := os.Stat(unpackedSomeDir)
	c.Assert(err, IsNil)
	st2, err := os.Stat(someDir)
	c.Assert(err, IsNil)
	c.Assert(st1.Mode(), Equals, st2.Mode())

	// and the symlink is there too
	fn, err := os.Readlink(filepath.Join(unpackedSomeDir, "fstab"))
	c.Check(err, IsNil)
	c.Check(fn, Equals, "/etc/fstab")
}

func (ts *HTestSuite) TestUbuntuArchitecture(c *C) {
	goarch = "arm"
	c.Check(UbuntuArchitecture(), Equals, "armhf")

	goarch = "amd64"
	c.Check(UbuntuArchitecture(), Equals, "amd64")

	goarch = "386"
	c.Check(UbuntuArchitecture(), Equals, "i386")
}

func (ts *HTestSuite) TestSupportedArchitectures(c *C) {
	goarch = "arm"
	c.Check(IsSupportedArchitecture([]string{"all"}), Equals, true)
	c.Check(IsSupportedArchitecture([]string{"amd64", "armhf", "powerpc"}), Equals, true)
	c.Check(IsSupportedArchitecture([]string{"armhf"}), Equals, true)
	c.Check(IsSupportedArchitecture([]string{"amd64", "powerpc"}), Equals, false)

	goarch = "amd64"
	c.Check(IsSupportedArchitecture([]string{"amd64", "armhf", "powerpc"}), Equals, true)
	c.Check(IsSupportedArchitecture([]string{"powerpc"}), Equals, false)
}

func (ts *HTestSuite) TestChdir(c *C) {
	tmpdir := c.MkDir()

	cwd, err := os.Getwd()
	c.Assert(err, IsNil)
	c.Assert(cwd, Not(Equals), tmpdir)
	ChDir(tmpdir, func() error {
		cwd, err := os.Getwd()
		c.Assert(err, IsNil)
		c.Assert(cwd, Equals, tmpdir)
		return err
	})
}

func (ts *HTestSuite) TestChdirErrorNoDir(c *C) {
	err := ChDir("random-dir-that-does-not-exist", func() error {
		return nil
	})
	c.Assert(err, ErrorMatches, "chdir .*: no such file or directory")
}

func (ts *HTestSuite) TestChdirErrorFromFunc(c *C) {
	err := ChDir("/", func() error {
		return fmt.Errorf("meep")
	})
	c.Assert(err, ErrorMatches, "meep")
}

func (ts *HTestSuite) TestExitCode(c *C) {
	cmd := exec.Command("true")
	err := cmd.Run()
	c.Assert(err, IsNil)

	cmd = exec.Command("false")
	err = cmd.Run()
	c.Assert(err, NotNil)
	e, err := ExitCode(err)
	c.Assert(err, IsNil)
	c.Assert(e, Equals, 1)

	cmd = exec.Command("sh", "-c", "exit 7")
	err = cmd.Run()
	e, err = ExitCode(err)
	c.Assert(e, Equals, 7)

	// ensure that non exec.ExitError values give a error
	_, err = os.Stat("/random/file/that/is/not/there")
	c.Assert(err, NotNil)
	_, err = ExitCode(err)
	c.Assert(err, NotNil)
}

func (ts *HTestSuite) TestMakeMapFromEnvList(c *C) {
	envList := []string{
		"PATH=/usr/bin:/bin",
		"DBUS_SESSION_BUS_ADDRESS=unix:abstract=something1234",
	}
	envMap := MakeMapFromEnvList(envList)
	c.Assert(envMap, DeepEquals, map[string]string{
		"PATH": "/usr/bin:/bin",
		"DBUS_SESSION_BUS_ADDRESS": "unix:abstract=something1234",
	})
}

func (ts *HTestSuite) TestMakeMapFromEnvListInvalidInput(c *C) {
	envList := []string{
		"nonsesne",
	}
	envMap := MakeMapFromEnvList(envList)
	c.Assert(envMap, DeepEquals, map[string]string(nil))
}

func (ts *HTestSuite) TestSha512sum(c *C) {
	tempdir := c.MkDir()

	p := filepath.Join(tempdir, "foo")
	err := ioutil.WriteFile(p, []byte("x"), 0644)
	c.Assert(err, IsNil)
	hashsum, err := Sha512sum(p)
	c.Assert(err, IsNil)
	c.Assert(hashsum, Equals, "a4abd4448c49562d828115d13a1fccea927f52b4d5459297f8b43e42da89238bc13626e43dcb38ddb082488927ec904fb42057443983e88585179d50551afe62")
}

func (ts *HTestSuite) TestFileDoesNotExist(c *C) {
	c.Assert(FileExists("/i-do-not-exist"), Equals, false)
}

func (ts *HTestSuite) TestFileExistsSimple(c *C) {
	fname := filepath.Join(c.MkDir(), "foo")
	err := ioutil.WriteFile(fname, []byte(fname), 0644)
	c.Assert(err, IsNil)

	c.Assert(FileExists(fname), Equals, true)
}

func (ts *HTestSuite) TestFileExistsExistsOddPermissions(c *C) {
	fname := filepath.Join(c.MkDir(), "foo")
	err := ioutil.WriteFile(fname, []byte(fname), 0100)
	c.Assert(err, IsNil)

	c.Assert(FileExists(fname), Equals, true)
}

func (ts *HTestSuite) TestIsDirectoryDoesNotExist(c *C) {
	c.Assert(IsDirectory("/i-do-not-exist"), Equals, false)
}

func (ts *HTestSuite) TestIsDirectorySimple(c *C) {
	dname := filepath.Join(c.MkDir(), "bar")
	err := os.Mkdir(dname, 0700)
	c.Assert(err, IsNil)

	c.Assert(IsDirectory(dname), Equals, true)
}

func (ts *HTestSuite) TestMakeRandomString(c *C) {
	// for our tests
	rand.Seed(1)

	s1 := MakeRandomString(10)
	c.Assert(s1, Equals, "gmwJgSapGA")

	s2 := MakeRandomString(5)
	c.Assert(s2, Equals, "tLMod")
}

func (ts *HTestSuite) TestAtomicWriteFile(c *C) {
	tmpdir := c.MkDir()

	p := filepath.Join(tmpdir, "foo")
	err := AtomicWriteFile(p, []byte("canary"), 0644)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(p)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "canary")

	// no files left behind!
	d, err := ioutil.ReadDir(tmpdir)
	c.Assert(err, IsNil)
	c.Assert(len(d), Equals, 1)
}

func (ts *HTestSuite) TestAtomicWriteFilePermissions(c *C) {
	tmpdir := c.MkDir()

	p := filepath.Join(tmpdir, "foo")
	err := AtomicWriteFile(p, []byte(""), 0600)
	c.Assert(err, IsNil)

	st, err := os.Stat(p)
	c.Assert(err, IsNil)
	c.Assert(st.Mode()&os.ModePerm, Equals, os.FileMode(0600))
}

func (ts *HTestSuite) TestAtomicWriteFileOverwrite(c *C) {
	tmpdir := c.MkDir()
	p := filepath.Join(tmpdir, "foo")
	c.Assert(ioutil.WriteFile(p, []byte("hello"), 0644), IsNil)
	c.Assert(AtomicWriteFile(p, []byte("hi"), 0600), IsNil)

	content, err := ioutil.ReadFile(p)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "hi")
}

func (ts *HTestSuite) TestAtomicWriteFileAbsoluteSymlinks(c *C) {
	tmpdir := c.MkDir()
	rodir := filepath.Join(tmpdir, "ro")
	p := filepath.Join(rodir, "foo")
	s := filepath.Join(tmpdir, "foo")
	c.Assert(os.MkdirAll(rodir, 0755), IsNil)
	c.Assert(os.Symlink(s, p), IsNil)
	c.Assert(os.Chmod(rodir, 0500), IsNil)
	defer os.Chmod(rodir, 0700)

	err := AtomicWriteFile(p, []byte("hi"), 0600)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(p)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "hi")
}

func (ts *HTestSuite) TestAtomicWriteFileOverwriteAbsoluteSymlink(c *C) {
	tmpdir := c.MkDir()
	rodir := filepath.Join(tmpdir, "ro")
	p := filepath.Join(rodir, "foo")
	s := filepath.Join(tmpdir, "foo")
	c.Assert(os.MkdirAll(rodir, 0755), IsNil)
	c.Assert(os.Symlink(s, p), IsNil)
	c.Assert(os.Chmod(rodir, 0500), IsNil)
	defer os.Chmod(rodir, 0700)

	c.Assert(ioutil.WriteFile(s, []byte("hello"), 0644), IsNil)
	c.Assert(AtomicWriteFile(p, []byte("hi"), 0600), IsNil)

	content, err := ioutil.ReadFile(p)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "hi")
}

func (ts *HTestSuite) TestAtomicWriteFileRelativeSymlinks(c *C) {
	tmpdir := c.MkDir()
	rodir := filepath.Join(tmpdir, "ro")
	p := filepath.Join(rodir, "foo")
	c.Assert(os.MkdirAll(rodir, 0755), IsNil)
	c.Assert(os.Symlink("../foo", p), IsNil)
	c.Assert(os.Chmod(rodir, 0500), IsNil)
	defer os.Chmod(rodir, 0700)

	err := AtomicWriteFile(p, []byte("hi"), 0600)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(p)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "hi")
}

func (ts *HTestSuite) TestAtomicWriteFileOverwriteRelativeSymlink(c *C) {
	tmpdir := c.MkDir()
	rodir := filepath.Join(tmpdir, "ro")
	p := filepath.Join(rodir, "foo")
	s := filepath.Join(tmpdir, "foo")
	c.Assert(os.MkdirAll(rodir, 0755), IsNil)
	c.Assert(os.Symlink("../foo", p), IsNil)
	c.Assert(os.Chmod(rodir, 0500), IsNil)
	defer os.Chmod(rodir, 0700)

	c.Assert(ioutil.WriteFile(s, []byte("hello"), 0644), IsNil)
	c.Assert(AtomicWriteFile(p, []byte("hi"), 0600), IsNil)

	content, err := ioutil.ReadFile(p)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "hi")
}

func (ts *HTestSuite) TestAtomicWriteFileNoOverwriteTmpExisting(c *C) {
	tmpdir := c.MkDir()
	realMakeRandomString := MakeRandomString
	defer func() { MakeRandomString = realMakeRandomString }()
	MakeRandomString = func(n int) string {
		// chosen by fair dice roll.
		// guranteed to be random.
		return "4"
	}

	p := filepath.Join(tmpdir, "foo")
	err := ioutil.WriteFile(p+".4", []byte(""), 0644)
	c.Assert(err, IsNil)

	err = AtomicWriteFile(p, []byte(""), 0600)
	c.Assert(err, ErrorMatches, "open .*: file exists")
}

func (ts *HTestSuite) TestCurrentHomeDirHOMEenv(c *C) {
	tmpdir := c.MkDir()

	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)

	os.Setenv("HOME", tmpdir)
	home, err := CurrentHomeDir()
	c.Assert(err, IsNil)
	c.Assert(home, Equals, tmpdir)
}

func (ts *HTestSuite) TestCurrentHomeDirNoHomeEnv(c *C) {
	oldHome := os.Getenv("HOME")
	if oldHome == "/sbuild-nonexistent" {
		c.Skip("running in schroot this test won't work")
	}
	defer os.Setenv("HOME", oldHome)

	os.Setenv("HOME", "")
	home, err := CurrentHomeDir()
	c.Assert(err, IsNil)
	c.Assert(home, Equals, oldHome)
}

func skipOnMissingDevKmsg(c *C) {
	_, err := os.Stat("/dev/kmsg")
	if err != nil {
		c.Skip("Can not stat /dev/kmsg")
	}
}

func (ts *HTestSuite) TestMajorMinorSimple(c *C) {
	skipOnMissingDevKmsg(c)

	stat, _ := os.Stat("/dev/kmsg")
	major, minor, err := MajorMinor(stat)
	c.Assert(err, IsNil)
	c.Assert(major, Equals, uint32(1))
	c.Assert(minor, Equals, uint32(11))
}

func (ts *HTestSuite) TestMajorMinorNoDevice(c *C) {
	stat, err := os.Stat(c.MkDir())
	c.Assert(err, IsNil)

	_, _, err = MajorMinor(stat)
	c.Assert(err, NotNil)
}

func (ts *HTestSuite) TestMakedev(c *C) {
	// $ python -c 'import os;print(os.makedev(1,11))'
	// 267
	c.Assert(Makedev(1, 11), Equals, uint32(267))
}

func (ts *HTestSuite) TestUnpacksMknod(c *C) {
	skipOnMissingDevKmsg(c)

	// mknod mock
	mknodWasCalled := false
	mknod = func(path string, mode uint32, dev int) error {
		mknodWasCalled = true
		return nil
	}

	// setup tmpdir
	tmpdir := c.MkDir()
	tmpfile := filepath.Join(tmpdir, "device.tar")

	cmd := exec.Command("tar", "cf", tmpfile, "/dev/kmsg")
	err := cmd.Run()
	c.Assert(err, IsNil)

	f, err := os.Open(tmpfile)
	c.Assert(err, IsNil)

	err = UnpackTar(f, c.MkDir(), nil)
	c.Assert(err, IsNil)
	c.Assert(mknodWasCalled, Equals, true)
}

func (ts *HTestSuite) TestGetattr(c *C) {
	T := struct {
		S string
		I int
	}{
		S: "foo",
		I: 42,
	}
	// works on values
	c.Assert(Getattr(T, "S").(string), Equals, "foo")
	c.Assert(Getattr(T, "I").(int), Equals, 42)
	// works for pointers too
	c.Assert(Getattr(&T, "S").(string), Equals, "foo")
	c.Assert(Getattr(&T, "I").(int), Equals, 42)
}

func makeTestFiles(c *C, srcDir, destDir string) {
	// a new file
	err := ioutil.WriteFile(filepath.Join(srcDir, "new"), []byte(nil), 0644)
	c.Assert(err, IsNil)

	// a existing file that needs update
	err = ioutil.WriteFile(filepath.Join(destDir, "existing-update"), []byte("old-content"), 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(srcDir, "existing-update"), []byte("some-new-content"), 0644)
	c.Assert(err, IsNil)

	// existing file that needs no update
	err = ioutil.WriteFile(filepath.Join(srcDir, "existing-unchanged"), []byte(nil), 0644)
	c.Assert(err, IsNil)
	err = exec.Command("cp", "-a", filepath.Join(srcDir, "existing-unchanged"), filepath.Join(destDir, "existing-unchanged")).Run()
	c.Assert(err, IsNil)

	// a file that needs removal
	err = ioutil.WriteFile(filepath.Join(destDir, "to-be-deleted"), []byte(nil), 0644)
	c.Assert(err, IsNil)
}

func compareDirs(c *C, srcDir, destDir string) {
	d1, err := exec.Command("ls", "-al", srcDir).CombinedOutput()
	c.Assert(err, IsNil)
	d2, err := exec.Command("ls", "-al", destDir).CombinedOutput()
	c.Assert(err, IsNil)
	c.Assert(string(d1), Equals, string(d2))
	// ensure content got updated
	c1, err := exec.Command("sh", "-c", fmt.Sprintf("find %s -type f |xargs cat", srcDir)).CombinedOutput()
	c.Assert(err, IsNil)
	c2, err := exec.Command("sh", "-c", fmt.Sprintf("find %s -type f |xargs cat", destDir)).CombinedOutput()
	c.Assert(err, IsNil)
	c.Assert(string(c1), Equals, string(c2))
}

func (ts *HTestSuite) TestSyncDirs(c *C) {

	for _, l := range [][2]string{
		[2]string{"src-short", "dst-loooooooooooong"},
		[2]string{"src-loooooooooooong", "dst-short"},
		[2]string{"src-eq", "dst-eq"},
	} {

		// ensure we have src, dest dirs with different length
		srcDir := filepath.Join(c.MkDir(), l[0])
		err := os.MkdirAll(srcDir, 0755)
		c.Assert(err, IsNil)
		destDir := filepath.Join(c.MkDir(), l[1])
		err = os.MkdirAll(destDir, 0755)
		c.Assert(err, IsNil)

		// add a src subdir
		subdir := filepath.Join(srcDir, "subdir")
		err = os.Mkdir(subdir, 0755)
		c.Assert(err, IsNil)
		makeTestFiles(c, subdir, destDir)

		// add a dst subdir that needs to get deleted
		subdir2 := filepath.Join(destDir, "to-be-deleted-subdir")
		err = os.Mkdir(subdir2, 0755)
		subdir3 := filepath.Join(subdir2, "to-be-deleted-sub-subdir")
		err = os.Mkdir(subdir3, 0755)

		// and a toplevel
		makeTestFiles(c, srcDir, destDir)

		// do it
		err = RSyncWithDelete(srcDir, destDir)
		c.Assert(err, IsNil)

		// ensure meta-data is identical
		compareDirs(c, srcDir, destDir)
		compareDirs(c, filepath.Join(srcDir, "subdir"), filepath.Join(destDir, "subdir"))
	}
}

func (ts *HTestSuite) TestSyncDirFails(c *C) {
	srcDir := c.MkDir()
	err := os.MkdirAll(srcDir, 0755)
	c.Assert(err, IsNil)

	destDir := c.MkDir()
	err = os.MkdirAll(destDir, 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(destDir, "meep"), []byte(nil), 0644)
	c.Assert(err, IsNil)

	// ensure remove fails
	err = os.Chmod(destDir, 0100)
	c.Assert(err, IsNil)
	// make tempdir cleanup work again
	defer os.Chmod(destDir, 0755)

	// do it
	err = RSyncWithDelete(srcDir, destDir)
	c.Check(err, NotNil)
	c.Check(err, ErrorMatches, ".*permission denied.*")
}

func (ts *HTestSuite) TestCopyIfDifferent(c *C) {
	srcDir := c.MkDir()
	dstDir := c.MkDir()

	// new file
	src := filepath.Join(srcDir, "bop")
	dst := filepath.Join(dstDir, "bob")
	err := ioutil.WriteFile(src, []byte(nil), 0644)
	c.Assert(err, IsNil)

	err = CopyIfDifferent(src, dst)
	c.Assert(err, IsNil)
	c.Check(FilesAreEqual(dst, src), Equals, true)

	// updated file
	src = filepath.Join(srcDir, "bip")
	dst = filepath.Join(dstDir, "bib")
	err = ioutil.WriteFile(src, []byte("123"), 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(dst, []byte(nil), 0644)
	c.Assert(err, IsNil)

	err = CopyIfDifferent(src, dst)
	c.Assert(err, IsNil)
	c.Check(FilesAreEqual(dst, src), Equals, true)
}

func (ts *HTestSuite) TestCopyIfDifferentErrorsOnNoSrc(c *C) {
	srcDir := c.MkDir()
	dstDir := c.MkDir()

	src := filepath.Join(srcDir, "mop")
	dst := filepath.Join(dstDir, "mop")

	err := CopyIfDifferent(src, dst)
	c.Assert(err, NotNil)
}

func (ts *HTestSuite) TestUnpackPermissions(c *C) {
	tarArchive := filepath.Join(c.MkDir(), "foo.tar")

	canaryName := "foo"
	canaryPerms := os.FileMode(0644)
	tmpdir := c.MkDir()
	err := ioutil.WriteFile(filepath.Join(tmpdir, canaryName), []byte(nil), canaryPerms)
	c.Assert(err, IsNil)

	ChDir(tmpdir, func() error {
		cmd := exec.Command("tar", "cvf", tarArchive, ".")
		_, err = cmd.CombinedOutput()
		c.Assert(err, IsNil)
		return err
	})

	// set crazy umask
	oldUmask := syscall.Umask(0077)
	defer syscall.Umask(oldUmask)

	// unpack
	unpackdir := c.MkDir()
	f, err := os.Open(tarArchive)
	c.Assert(err, IsNil)
	defer f.Close()
	UnpackTar(f, unpackdir, nil)

	st, err := os.Stat(filepath.Join(unpackdir, canaryName))
	c.Assert(err, IsNil)
	c.Assert(st.Mode()&os.ModePerm, Equals, canaryPerms)
}
