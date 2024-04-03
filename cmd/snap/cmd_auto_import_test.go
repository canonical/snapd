// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2023 Canonical Ltd
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

package main_test

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

func (s *SnapSuite) TestAutoImportAssertsHappy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	fakeAssertData := []byte("my-assertion")

	n := 0
	total := 2
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/assertions")
			postData, err := io.ReadAll(r.Body)
			c.Assert(err, IsNil)
			c.Check(postData, DeepEquals, fakeAssertData)
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done"}}`)
			n++
		case 1:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/users")
			postData, err := io.ReadAll(r.Body)
			c.Assert(err, IsNil)
			c.Check(string(postData), Equals, `{"action":"create","automatic":true}`)

			fmt.Fprintln(w, `{"type": "sync", "result": [{"username": "foo"}]}`)
			n++
		default:
			c.Fatalf("unexpected request: %v (expected %d got %d)", r, total, n)
		}

	})

	testDir := c.MkDir()
	fakeAssertsFn := filepath.Join(testDir, "auto-import.assert")
	err := os.WriteFile(fakeAssertsFn, fakeAssertData, 0644)
	c.Assert(err, IsNil)

	mockMountInfoFmt := fmt.Sprintf(`24 0 8:18 / %s rw,relatime shared:1 - ext4 /dev/sdb2 rw,errors=remount-ro,data=ordered
`, testDir)
	restore = osutil.MockMountInfo(mockMountInfoFmt)
	defer restore()

	logbuf, restore := logger.MockLogger()
	defer restore()

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"auto-import"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, `created user "foo"`+"\n")
	// matches because we may get a:
	//   "WARNING: cannot create syslog logger\n"
	// in the output
	c.Check(logbuf.String(), Matches, fmt.Sprintf("(?ms).*imported %s\n", fakeAssertsFn))
	c.Check(n, Equals, total)
}

func (s *SnapSuite) TestAutoImportAssertsNotImportedFromLoop(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	fakeAssertData := []byte("bad-assertion")

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		// assertion is ignored, nothing is posted to this endpoint
		panic("not reached")
	})

	testDir := c.MkDir()
	fakeAssertsFn := filepath.Join(testDir, "auto-import.assert")
	err := os.WriteFile(fakeAssertsFn, fakeAssertData, 0644)
	c.Assert(err, IsNil)

	mockMountInfoFmtWithLoop := `24 0 8:18 / %s rw,relatime shared:1 - squashfs /dev/loop1 rw,errors=remount-ro,data=ordered`
	content := fmt.Sprintf(mockMountInfoFmtWithLoop, filepath.Dir(fakeAssertsFn))
	restore = osutil.MockMountInfo(content)
	defer restore()

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"auto-import"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestAutoImportAssertsHappyNotOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	fakeAssertData := []byte("my-assertion")
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Errorf("auto-import on classic is disabled, but something tried to do a %q with %s", r.Method, r.URL.Path)
	})

	fakeAssertsFn := filepath.Join(c.MkDir(), "auto-import.assert")
	err := os.WriteFile(fakeAssertsFn, fakeAssertData, 0644)
	c.Assert(err, IsNil)

	mockMountInfoFmt := `
24 0 8:18 / %s rw,relatime shared:1 - ext4 /dev/sdb2 rw,errors=remount-ro,data=ordered`
	restore = osutil.MockMountInfo(mockMountInfoFmt)
	defer restore()

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"auto-import"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "auto-import is disabled on classic\n")
}

func (s *SnapSuite) TestAutoImportIntoSpool(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	logbuf, restore := logger.MockLogger()
	defer restore()

	fakeAssertData := []byte("good-assertion")

	// ensure we can not connect
	snap.ClientConfig.BaseURL = "can-not-connect-to-this-url"

	fakeAssertsFn := filepath.Join(c.MkDir(), "auto-import.assert")
	err := os.WriteFile(fakeAssertsFn, fakeAssertData, 0644)
	c.Assert(err, IsNil)

	mockMountInfoFmt := fmt.Sprintf(`24 0 8:18 / %s rw,relatime shared:1 - squashfs /dev/sc1 rw,errors=remount-ro,data=ordered`, filepath.Dir(fakeAssertsFn))
	restore = osutil.MockMountInfo(mockMountInfoFmt)
	defer restore()

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"auto-import"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, "")
	// matches because we may get a:
	//   "WARNING: cannot create syslog logger\n"
	// in the output
	c.Check(logbuf.String(), Matches, "(?ms).*queuing for later.*\n")

	files, err := os.ReadDir(dirs.SnapAssertsSpoolDir)
	c.Assert(err, IsNil)
	c.Check(files, HasLen, 1)
	c.Check(files[0].Name(), Equals, "iOkaeet50rajLvL-0Qsf2ELrTdn3XIXRIBlDewcK02zwRi3_TJlUOTl9AaiDXmDn.assert")
}

func (s *SnapSuite) TestAutoImportFromSpoolHappy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = osutil.MockMountInfo(``)
	defer restore()

	fakeAssertData := []byte("my-assertion")

	n := 0
	total := 2
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/assertions")
			postData, err := io.ReadAll(r.Body)
			c.Assert(err, IsNil)
			c.Check(postData, DeepEquals, fakeAssertData)
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done"}}`)
			n++
		case 1:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/users")
			postData, err := io.ReadAll(r.Body)
			c.Assert(err, IsNil)
			c.Check(string(postData), Equals, `{"action":"create","automatic":true}`)

			fmt.Fprintln(w, `{"type": "sync", "result": [{"username": "foo"}]}`)
			n++
		default:
			c.Fatalf("unexpected request: %v (expected %d got %d)", r, total, n)
		}

	})

	fakeAssertsFn := filepath.Join(dirs.SnapAssertsSpoolDir, "1234343")
	err := os.MkdirAll(filepath.Dir(fakeAssertsFn), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(fakeAssertsFn, fakeAssertData, 0644)
	c.Assert(err, IsNil)

	logbuf, restore := logger.MockLogger()
	defer restore()

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"auto-import"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, `created user "foo"`+"\n")
	// matches because we may get a:
	//   "WARNING: cannot create syslog logger\n"
	// in the output
	c.Check(logbuf.String(), Matches, fmt.Sprintf("(?ms).*imported %s\n", fakeAssertsFn))
	c.Check(n, Equals, total)

	c.Check(osutil.FileExists(fakeAssertsFn), Equals, false)
}

func (s *SnapSuite) TestAutoImportIntoSpoolUnhappyTooBig(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	_, restoreLogger := logger.MockLogger()
	defer restoreLogger()

	// fake data is bigger than the default assertion limit
	fakeAssertData := make([]byte, 641*1024)

	// ensure we can not connect
	snap.ClientConfig.BaseURL = "can-not-connect-to-this-url"

	fakeAssertsFn := filepath.Join(c.MkDir(), "auto-import.assert")
	err := os.WriteFile(fakeAssertsFn, fakeAssertData, 0644)
	c.Assert(err, IsNil)

	mockMountInfoFmt := fmt.Sprintf(`24 0 8:18 / %s rw,relatime shared:1 - squashfs /dev/sc1 rw,errors=remount-ro,data=ordered`, filepath.Dir(fakeAssertsFn))
	restore = osutil.MockMountInfo(mockMountInfoFmt)
	defer restore()

	_, err = snap.Parser(snap.Client()).ParseArgs([]string{"auto-import"})
	c.Assert(err, ErrorMatches, "cannot queue .*, file size too big: 656384")
}

func (s *SnapSuite) testAutoImportUnhappyInInstallMode(c *C, mode string) {
	restoreRelease := release.MockOnClassic(false)
	defer restoreRelease()

	restoreMountInfo := osutil.MockMountInfo(``)
	defer restoreMountInfo()

	_, restoreLogger := logger.MockLogger()
	defer restoreLogger()

	modeenvContent := fmt.Sprintf(`mode=%s
recovery_system=20200202
`, mode)
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapModeenvFile), 0755), IsNil)
	c.Assert(os.WriteFile(dirs.SnapModeenvFile, []byte(modeenvContent), 0644), IsNil)

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"auto-import"})
	c.Assert(err, IsNil)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "auto-import is disabled in install modes\n")
}

func (s *SnapSuite) TestAutoImportUnhappyInInstallMode(c *C) {
	s.testAutoImportUnhappyInInstallMode(c, "install")
}

func (s *SnapSuite) TestAutoImportUnhappyInFactoryResetMode(c *C) {
	s.testAutoImportUnhappyInInstallMode(c, "factory-reset")
}

func (s *SnapSuite) TestAutoImportUnhappyInInstallInInitrdMode(c *C) {
	restoreRelease := release.MockOnClassic(false)
	defer restoreRelease()

	restoreMountInfo := osutil.MockMountInfo(``)
	defer restoreMountInfo()

	_, restoreLogger := logger.MockLogger()
	defer restoreLogger()

	modeenvContent := `mode=run
recovery_system=20200202
`
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapModeenvFile), 0755), IsNil)
	c.Assert(os.WriteFile(dirs.SnapModeenvFile, []byte(modeenvContent), 0644), IsNil)

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"auto-import"})
	c.Assert(err, IsNil)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

var mountStatic = []string{"mount", "-t", "ext4,vfat", "-o", "ro", "--make-private"}

func (s *SnapSuite) TestAutoImportFromRemovable(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = osutil.MockMountInfo(``)
	defer restore()

	_, restoreLogger := logger.MockLogger()
	defer restoreLogger()

	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)

	var umounts []string
	restore = snap.MockSyscallUmount(func(p string, _ int) error {
		umounts = append(umounts, p)
		return nil
	})
	defer restore()

	var tmpdirIdx int
	restore = snap.MockIoutilTempDir(func(where string, p string) (string, error) {
		c.Check(where, Equals, "")
		tmpdirIdx++
		return filepath.Join(rootdir, fmt.Sprintf("/tmp/%s%d", p, tmpdirIdx)), nil
	})
	defer restore()

	mountCmd := testutil.MockCommand(c, "mount", "")
	defer mountCmd.Restore()

	snaptest.PopulateDir(rootdir, [][]string{
		// removable without partitions
		{"sys/block/sdremovable/removable", "1\n"},
		// fixed disk
		{"sys/block/sdfixed/removable", "0\n"},
		// removable with partitions
		{"sys/block/sdpart/removable", "1\n"},
		{"sys/block/sdpart/sdpart1/partition", "1\n"},
		{"sys/block/sdpart/sdpart2/partition", "0\n"},
		{"sys/block/sdpart/sdpart3/partition", "1\n"},
		// removable but subdevices are not partitions?
		{"sys/block/sdother/removable", "1\n"},
		{"sys/block/sdother/sdother1/partition", "0\n"},
	})

	// do not mock mountinfo contents, we just want to observe whether we
	// try to mount and umount the right stuff

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"auto-import"})
	c.Assert(err, IsNil)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
	c.Check(mountCmd.Calls(), DeepEquals, [][]string{
		append(mountStatic, "/dev/sdpart1", filepath.Join(rootdir, "/tmp/snapd-auto-import-mount-1")),
		append(mountStatic, "/dev/sdpart3", filepath.Join(rootdir, "/tmp/snapd-auto-import-mount-2")),
		append(mountStatic, "/dev/sdremovable", filepath.Join(rootdir, "/tmp/snapd-auto-import-mount-3")),
	})
	c.Check(umounts, DeepEquals, []string{
		filepath.Join(rootdir, "/tmp/snapd-auto-import-mount-3"),
		filepath.Join(rootdir, "/tmp/snapd-auto-import-mount-2"),
		filepath.Join(rootdir, "/tmp/snapd-auto-import-mount-1"),
	})
}

func (s *SnapSuite) TestAutoImportNoRemovable(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)

	var umounts []string
	restore = snap.MockSyscallUmount(func(p string, _ int) error {
		return fmt.Errorf("unexpected call")
	})
	defer restore()

	restore = osutil.MockMountInfo(``)
	defer restore()

	mountCmd := testutil.MockCommand(c, "mount", "exit 1")
	defer mountCmd.Restore()

	snaptest.PopulateDir(rootdir, [][]string{
		// fixed disk
		{"sys/block/sdfixed/removable", "0\n"},
		// removable but subdevices are not partitions?
		{"sys/block/sdother/removable", "1\n"},
		{"sys/block/sdother/sdother1/partition", "0\n"},
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"auto-import"})
	c.Assert(err, IsNil)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
	c.Check(mountCmd.Calls(), HasLen, 0)
	c.Check(umounts, HasLen, 0)
}

func (s *SnapSuite) TestAutoImportFromMount(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = osutil.MockMountInfo(``)
	defer restore()

	_, restoreLogger := logger.MockLogger()
	defer restoreLogger()

	mountCmd := testutil.MockCommand(c, "mount", "")

	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)

	var umounts []string
	restore = snap.MockSyscallUmount(func(p string, _ int) error {
		c.Assert(umounts, HasLen, 0)
		umounts = append(umounts, p)
		return nil
	})
	defer restore()

	var tmpdircalls int
	restore = snap.MockIoutilTempDir(func(where string, p string) (string, error) {
		c.Check(where, Equals, "")
		c.Assert(tmpdircalls, Equals, 0)
		tmpdircalls++
		return filepath.Join(rootdir, fmt.Sprintf("/tmp/%s1", p)), nil
	})
	defer restore()

	// do not mock mountinfo contents, we just want to observe whether we
	// try to mount and umount the right stuff

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"auto-import", "--mount", "/dev/foobar"})
	c.Assert(err, IsNil)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
	c.Check(mountCmd.Calls(), DeepEquals, [][]string{
		append(mountStatic, "/dev/foobar", filepath.Join(rootdir, "/tmp/snapd-auto-import-mount-1")),
	})
	c.Check(umounts, DeepEquals, []string{
		filepath.Join(rootdir, "/tmp/snapd-auto-import-mount-1"),
	})
}

func (s *SnapSuite) TestAutoImportUC20CandidatesIgnoresSystemPartitions(c *C) {

	mountDirs := []string{
		"/writable/system-data/var/lib/snapd/seed",
		"/var/lib/snapd/seed",
		"/run/mnt/ubuntu-boot",
		"/run/mnt/ubuntu-seed",
		"/run/mnt/ubuntu-data",
		"/mnt/real-device",
	}

	rootDir := c.MkDir()
	dirs.SetRootDir(rootDir)
	defer func() { dirs.SetRootDir("") }()

	args := make([]interface{}, 0, len(mountDirs)+1)
	args = append(args, dirs.GlobalRootDir)
	// pretend there are auto-import.asserts on all of them
	for _, dir := range mountDirs {
		args = append(args, dir)
		file := filepath.Join(rootDir, dir, "auto-import.assert")
		c.Assert(os.MkdirAll(filepath.Dir(file), 0755), IsNil)
		c.Assert(os.WriteFile(file, nil, 0644), IsNil)
	}

	mockMountInfoFmtWithLoop := `24 0 8:18 / %[1]s%[2]s rw,relatime foo - ext3 /dev/meep2 rw,errors=remount-ro,data=ordered
24 0 8:18 / %[1]s%[3]s rw,relatime - ext3 /dev/meep2 rw,errors=remount-ro,data=ordered
24 0 8:18 / %[1]s%[4]s rw,relatime opt:1 - ext4 /dev/meep3 rw,errors=remount-ro,data=ordered
24 0 8:18 / %[1]s%[5]s rw,relatime opt:1 opt:2 - ext2 /dev/meep4 rw,errors=remount-ro,data=ordered
24 0 8:18 / %[1]s%[6]s rw,relatime opt:1 opt:2 - ext2 /dev/meep5 rw,errors=remount-ro,data=ordered
24 0 8:18 / %[1]s%[7]s rw,relatime opt:1 opt:2 - ext2 /dev/meep78 rw,errors=remount-ro,data=ordered`

	content := fmt.Sprintf(mockMountInfoFmtWithLoop, args...)
	restore := osutil.MockMountInfo(content)
	defer restore()

	l, err := snap.AutoImportCandidates()
	c.Check(err, IsNil)

	// only device should be the /mnt/real-device one
	c.Check(l, DeepEquals, []string{filepath.Join(rootDir, "/mnt/real-device", "auto-import.assert")})
}

func (s *SnapSuite) TestAutoImportAssertsManagedEmptyReply(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	_, restore = logger.MockLogger()
	defer restore()

	fakeAssertData := []byte("my-assertion")

	n := 0
	total := 2
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/assertions")
			postData, err := io.ReadAll(r.Body)
			c.Assert(err, IsNil)
			c.Check(postData, DeepEquals, fakeAssertData)
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done"}}`)
			n++
		case 1:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/users")
			postData, err := io.ReadAll(r.Body)
			c.Assert(err, IsNil)
			c.Check(string(postData), Equals, `{"action":"create","automatic":true}`)

			fmt.Fprintln(w, `{"type": "sync", "result": []}`)
			n++
		default:
			c.Fatalf("unexpected request: %v (expected %d got %d)", r, total, n)
		}
	})

	fakeAssertsFn := filepath.Join(c.MkDir(), "auto-import.assert")
	err := os.WriteFile(fakeAssertsFn, fakeAssertData, 0644)
	c.Assert(err, IsNil)

	mockMountInfoFmt := `24 0 8:18 / %s rw,relatime shared:1 - ext4 /dev/sdb2 rw,errors=remount-ro,data=ordered`
	content := fmt.Sprintf(mockMountInfoFmt, filepath.Dir(fakeAssertsFn))
	restore = osutil.MockMountInfo(content)
	defer restore()

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"auto-import"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, ``)
	c.Check(n, Equals, total)
}
