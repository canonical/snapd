// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package dirs_test

import (
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/dirs/dirstest"
	"github.com/snapcore/snapd/release"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&DirsTestSuite{})

type DirsTestSuite struct{}

func (s *DirsTestSuite) TestSnapHomeDirs(c *C) {
	defer dirs.SetRootDir("")
	hidden := dirs.SnapDirOptions{HiddenSnapDataDir: true}
	// Expected ouptut should be ROOTDIR/home
	c.Check(dirs.SnapHomeDirs(), DeepEquals, []string{filepath.Join(dirs.GlobalRootDir, "/home")})
	c.Check(dirs.DataHomeGlobs(nil), DeepEquals, []string{filepath.Join(dirs.GlobalRootDir, "/home", "*", "snap")})
	c.Check(dirs.DataHomeGlobs(&hidden), DeepEquals, []string{filepath.Join(dirs.GlobalRootDir, "/home", "*", ".snap", "data")})

	// Expected output should remove all trailing '/' and add /home if it is not present.
	getSnapHomeFromSet := dirs.SetSnapHomeDirs("/home/homeDir1,/home/homeDirs/homeDir1///,/home/homeDir2/,/home/homeTest/users/")
	snapHomeDirs := []string{"/home/homeDir1", "/home/homeDirs/homeDir1", "/home/homeDir2", "/home/homeTest/users", "/home"}
	dataGlob := []string{"/home/homeDir1/*/snap", "/home/homeDirs/homeDir1/*/snap", "/home/homeDir2/*/snap", "/home/homeTest/users/*/snap",
		"/home/*/snap"}
	hiddenDataGlob := []string{"/home/homeDir1/*/.snap/data", "/home/homeDirs/homeDir1/*/.snap/data", "/home/homeDir2/*/.snap/data",
		"/home/homeTest/users/*/.snap/data", "/home/*/.snap/data"}
	getSnapHomeDirs := dirs.SnapHomeDirs()
	c.Check(getSnapHomeDirs, DeepEquals, snapHomeDirs)
	c.Check(dirs.DataHomeGlobs(nil), DeepEquals, dataGlob)
	c.Check(dirs.DataHomeGlobs(&hidden), DeepEquals, hiddenDataGlob)
	// Useful for detecting if any output changes were made to dirs.SnapHomeDirs() and not reflected in dirs.SetSnapHomeDirs()
	c.Check(getSnapHomeFromSet, DeepEquals, snapHomeDirs)

	// Expected output, should remove all trailing '/' and not add /home since it is already present.
	getSnapHomeFromSet = dirs.SetSnapHomeDirs("/home/homeDir1,/home/homeDirs/homeDir1///,/home/,/home/homeDir2/,/home/homeTest/users/")
	snapHomeDirs = []string{"/home/homeDir1", "/home/homeDirs/homeDir1", "/home", "/home/homeDir2", "/home/homeTest/users"}
	dataGlob = []string{"/home/homeDir1/*/snap", "/home/homeDirs/homeDir1/*/snap", "/home/*/snap", "/home/homeDir2/*/snap",
		"/home/homeTest/users/*/snap"}
	hiddenDataGlob = []string{"/home/homeDir1/*/.snap/data", "/home/homeDirs/homeDir1/*/.snap/data", "/home/*/.snap/data",
		"/home/homeDir2/*/.snap/data", "/home/homeTest/users/*/.snap/data"}
	getSnapHomeDirs = dirs.SnapHomeDirs()
	c.Check(getSnapHomeDirs, DeepEquals, snapHomeDirs)
	c.Check(getSnapHomeFromSet, DeepEquals, snapHomeDirs)
	c.Check(dirs.DataHomeGlobs(nil), DeepEquals, dataGlob)
	c.Check(dirs.DataHomeGlobs(&hidden), DeepEquals, hiddenDataGlob)

	// Expected output should only return /ROOTDIR/home as a home directory
	dirs.SetRootDir("/new/root")
	getSnapHomeFromSet = dirs.SetSnapHomeDirs("")
	snapHomeDirs = []string{"/new/root/home"}
	dataGlob = []string{"/new/root/home/*/snap"}
	hiddenDataGlob = []string{"/new/root/home/*/.snap/data"}
	getSnapHomeDirs = dirs.SnapHomeDirs()
	c.Check(getSnapHomeDirs, DeepEquals, snapHomeDirs)
	c.Check(getSnapHomeFromSet, DeepEquals, snapHomeDirs)
	c.Check(dirs.DataHomeGlobs(nil), DeepEquals, dataGlob)
	c.Check(dirs.DataHomeGlobs(&hidden), DeepEquals, hiddenDataGlob)

	// Expected output should append /ROOTDIR to each entry except if already present
	getSnapHomeFromSet = dirs.SetSnapHomeDirs("/home/homeDir1,/new/root/home/homeDirs/homeDir1///,/home/homeDir2/,/new/root/home/homeTest/users/")
	snapHomeDirs = []string{"/new/root/home/homeDir1", "/new/root/home/homeDirs/homeDir1", "/new/root/home/homeDir2", "/new/root/home/homeTest/users", "/new/root/home"}
	dataGlob = []string{"/new/root/home/homeDir1/*/snap", "/new/root/home/homeDirs/homeDir1/*/snap", "/new/root/home/homeDir2/*/snap",
		"/new/root/home/homeTest/users/*/snap", "/new/root/home/*/snap"}
	hiddenDataGlob = []string{"/new/root/home/homeDir1/*/.snap/data", "/new/root/home/homeDirs/homeDir1/*/.snap/data", "/new/root/home/homeDir2/*/.snap/data",
		"/new/root/home/homeTest/users/*/.snap/data", "/new/root/home/*/.snap/data"}
	getSnapHomeDirs = dirs.SnapHomeDirs()
	c.Check(getSnapHomeDirs, DeepEquals, snapHomeDirs)
	c.Check(getSnapHomeFromSet, DeepEquals, snapHomeDirs)
	c.Check(dirs.DataHomeGlobs(nil), DeepEquals, dataGlob)
	c.Check(dirs.DataHomeGlobs(&hidden), DeepEquals, hiddenDataGlob)

	// setting a Root directory should reset the home directory
	dirs.SetRootDir("/new")
	c.Check(dirs.SnapHomeDirs(), DeepEquals, []string{"/new/home"})
	c.Check(dirs.DataHomeGlobs(nil), DeepEquals, []string{"/new/home/*/snap"})
	c.Check(dirs.DataHomeGlobs(&hidden), DeepEquals, []string{"/new/home/*/.snap/data"})
	dirs.SetRootDir("/")
	c.Check(dirs.SnapHomeDirs(), DeepEquals, []string{"/home"})
	c.Check(dirs.DataHomeGlobs(nil), DeepEquals, []string{"/home/*/snap"})
	c.Check(dirs.DataHomeGlobs(&hidden), DeepEquals, []string{"/home/*/.snap/data"})

}

func (s *DirsTestSuite) TestStripRootDir(c *C) {
	dirs.SetRootDir("/")
	// strip does nothing if the default (empty) root directory is used
	c.Check(dirs.StripRootDir("/foo/bar"), Equals, "/foo/bar")
	// strip only works on absolute paths
	c.Check(func() { dirs.StripRootDir("relative") }, Panics, `supplied path is not absolute "relative"`)
	// with an alternate root
	dirs.SetRootDir("/alt/")
	defer dirs.SetRootDir("")
	// strip behaves as expected, returning absolute paths without the prefix
	c.Check(dirs.StripRootDir("/alt/foo/bar"), Equals, "/foo/bar")
	// strip only works on paths that begin with the global root directory
	c.Check(func() { dirs.StripRootDir("/other/foo/bar") }, Panics, `supplied path is not related to global root "/other/foo/bar"`)
}

func (s *DirsTestSuite) TestClassicConfinementSupport(c *C) {
	defer dirs.SetRootDir("")
	// distribution with /snap
	d := c.MkDir()
	dirstest.MustMockCanonicalSnapMountDir(d)
	dirs.SetRootDir(d)
	c.Check(dirs.SupportsClassicConfinement(), Equals, true)

	// distribution with /var/lib/snapd/snap
	d = c.MkDir()
	dirstest.MustMockAltSnapMountDir(d)
	dirs.SetRootDir(d)
	c.Check(dirs.SupportsClassicConfinement(), Equals, false)
}

func (s *DirsTestSuite) TestClassicConfinementSymlinkWorkaround(c *C) {
	defer dirs.SetRootDir("")

	altRoot := c.MkDir()
	dirstest.MustMockAltSnapMountDir(altRoot)
	dirs.SetRootDir(altRoot)
	c.Check(dirs.SupportsClassicConfinement(), Equals, false)

	dirstest.MustMockClassicConfinementAltDirSupport(altRoot)

	c.Check(dirs.SupportsClassicConfinement(), Equals, true)
}

func (s *DirsTestSuite) TestClassicConfinementFullPathSymlinkAltDistro(c *C) {
	defer dirs.SetRootDir("")

	altRoot := c.MkDir()
	c.Assert(os.Symlink(filepath.Join(altRoot, "/var/lib/snapd/snap"), filepath.Join(altRoot, "/snap")), IsNil)

	dirs.SetRootDir(altRoot)
	// just symlink, no actual alt mount directory
	c.Check(dirs.SupportsClassicConfinement(), Equals, false)
	d := filepath.Join(altRoot, "/var/lib/snapd/snap")
	os.MkdirAll(d, 0o755)
	c.Check(dirs.SupportsClassicConfinement(), Equals, true)
}

func (s *DirsTestSuite) TestMountDirKnownDistro(c *C) {
	// the test changes RootDir, restore correct one when retuning
	defer dirs.SetRootDir("/")

	for _, t := range []struct {
		ID           string
		IDLike       []string
		canonicalDir bool
	}{
		{"ubuntucoreinitramfs", nil, true},
	} {
		c.Logf("case %+v", t)
		func() {
			defer release.MockReleaseInfo(&release.OS{ID: t.ID, IDLike: t.IDLike})()
			// make a new root directory each time to isolate the test from
			// local filesystem state and any previous test runs
			dirs.SetRootDir(c.MkDir())
			if t.canonicalDir {
				c.Check(dirs.SnapMountDir, Equals, filepath.Join(dirs.GlobalRootDir, "/snap"))
			} else {
				c.Check(dirs.SnapMountDir, Equals, filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/snap"))
			}
		}()
	}
}

func (s *DirsTestSuite) TestMountDirProbeHappy(c *C) {
	defer dirs.SetRootDir("/")

	defer release.MockReleaseInfo(&release.OS{ID: "my-distro", IDLike: []string{"no-other-distro"}})()

	d := c.MkDir()
	dirs.SetRootDir(d)
	// no /snap or /var/lib/snapd/snap delivered through packaging
	c.Check(dirs.SnapMountDirDetectionOutcome(), IsNil)
	c.Check(dirs.SnapMountDir, Equals, filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/snap"))

	// pretend we have a directory
	c.Assert(os.Mkdir(filepath.Join(d, "snap"), 0o755), IsNil)
	dirs.SetRootDir(d)
	c.Check(dirs.SnapMountDirDetectionOutcome(), IsNil)
	c.Check(dirs.SnapMountDir, Equals, filepath.Join(dirs.GlobalRootDir, "/snap"))

	c.Assert(os.Remove(filepath.Join(d, "/snap")), IsNil)
	// pretend we have a relative symlink
	c.Assert(os.Symlink("var/lib/snapd/snap", filepath.Join(d, "/snap")), IsNil)
	dirs.SetRootDir(d)
	c.Check(dirs.SnapMountDirDetectionOutcome(), IsNil)
	c.Check(dirs.SnapMountDir, Equals, filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/snap"))

	c.Assert(os.Remove(filepath.Join(d, "/snap")), IsNil)
	// pretend we have an absolute symlink
	c.Assert(os.Symlink("/var/lib/snapd/snap", filepath.Join(d, "/snap")), IsNil)
	dirs.SetRootDir(d)
	c.Check(dirs.SnapMountDirDetectionOutcome(), IsNil)
	c.Check(dirs.SnapMountDir, Equals, filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/snap"))

	// also accepts an absolute path under explicit root dir commonly set during tests
	c.Assert(os.Remove(filepath.Join(d, "/snap")), IsNil)
	// pretend we have an absolute symlink
	c.Assert(os.Symlink(filepath.Join(d, "/var/lib/snapd/snap"), filepath.Join(d, "/snap")), IsNil)
	dirs.SetRootDir(d)
	c.Check(dirs.SnapMountDirDetectionOutcome(), IsNil)
	c.Check(dirs.SnapMountDir, Equals, filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/snap"))
}

func (s *DirsTestSuite) TestMountDirProbeErrBadSymlink(c *C) {
	defer dirs.SetRootDir("/")

	defer release.MockReleaseInfo(&release.OS{ID: "my-distro", IDLike: []string{"no-other-distro"}})()

	d := c.MkDir()
	// pretend we have a relative symlink
	c.Assert(os.Symlink("foo/bar", filepath.Join(d, "/snap")), IsNil)
	dirs.SetRootDir(d)
	c.Check(dirs.SnapMountDir, Equals, "mount-dir-is-unset")
	c.Check(dirs.SnapMountDirDetectionOutcome(), ErrorMatches, "cannot resolve snap mount directory: /.*/snap must be a symbolic link to /var/lib/snapd/snap")
}

func (s *DirsTestSuite) TestMountDirProbeErrBadStat(c *C) {
	if os.Geteuid() == 0 {
		c.Skip("test cannot be run as root")
	}

	defer dirs.SetRootDir("/")

	defer release.MockReleaseInfo(&release.OS{ID: "my-distro", IDLike: []string{"no-other-distro"}})()

	d := c.MkDir()
	// pretend we have a relative symlink
	c.Assert(os.Symlink("foo/bar", filepath.Join(d, "/snap")), IsNil)
	c.Assert(os.Chmod(d, 0o000), IsNil)
	defer os.Chmod(d, 0o755)

	dirs.SetRootDir(d)
	c.Check(dirs.SnapMountDir, Equals, "mount-dir-is-unset")
	c.Check(dirs.SnapMountDirDetectionOutcome(), ErrorMatches, "cannot resolve snap mount directory: cannot stat /.*/snap: lstat /.*/snap: permission denied")
}

func (s *DirsTestSuite) TestInsideBaseSnap(c *C) {
	d := c.MkDir()

	snapYaml := filepath.Join(d, "snap.yaml")
	restore := dirs.MockMetaSnapPath(snapYaml)
	defer restore()

	inside, err := dirs.IsInsideBaseSnap()
	c.Assert(err, IsNil)
	c.Assert(inside, Equals, false)

	err = os.WriteFile(snapYaml, []byte{}, 0o755)
	c.Assert(err, IsNil)

	inside, err = dirs.IsInsideBaseSnap()
	c.Assert(err, IsNil)
	c.Assert(inside, Equals, true)
}

func (s *DirsTestSuite) TestCompleteShPath(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// old-style in-core complete.sh
	c.Check(dirs.CompleteShPath(""), Equals, filepath.Join(dirs.SnapMountDir, "core/current/usr/lib/snapd/complete.sh"))
	// new-style in-host complete.sh
	c.Check(dirs.CompleteShPath("x"), Equals, filepath.Join(dirs.DistroLibExecDir, "complete.sh"))
	// new-style in-snapd complete.sh
	c.Check(os.MkdirAll(filepath.Join(dirs.SnapMountDir, "snapd/current/usr/lib/snapd"), 0o755), IsNil)
	c.Check(dirs.CompleteShPath("x"), Equals, filepath.Join(dirs.SnapMountDir, "snapd/current/usr/lib/snapd/complete.sh"))
}

func (s *DirsTestSuite) TestIsCompleteShSymlink(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	tests := map[string]string{
		filepath.Join(dirs.GlobalRootDir, "no-base"):        filepath.Join(dirs.SnapMountDir, "core/current/usr/lib/snapd/complete.sh"),
		filepath.Join(dirs.GlobalRootDir, "no-snapd"):       filepath.Join(dirs.DistroLibExecDir, "complete.sh"),
		filepath.Join(dirs.GlobalRootDir, "base-and-snapd"): filepath.Join(dirs.SnapMountDir, "snapd/current/usr/lib/snapd/complete.sh"),
	}
	for target, d := range tests {
		c.Check(os.Symlink(d, target), IsNil)
		c.Check(dirs.IsCompleteShSymlink(target), Equals, true)
	}
	c.Check(dirs.IsCompleteShSymlink("/etc/passwd"), Equals, false)
	c.Check(dirs.IsCompleteShSymlink("/does-not-exist"), Equals, false)
}

func (s *DirsTestSuite) TestUnder(c *C) {
	dirs.SetRootDir("/nowhere")
	defer dirs.SetRootDir("")

	rootdir := "/other-root"

	c.Check(dirs.SnapBlobDirUnder(rootdir), Equals, "/other-root/var/lib/snapd/snaps")
	c.Check(dirs.SnapSeedDirUnder(rootdir), Equals, "/other-root/var/lib/snapd/seed")
}

func (s *DirsTestSuite) TestAddRootDirCallback(c *C) {
	dirs.SetRootDir("/")

	someVar := filepath.Join(dirs.GlobalRootDir, "my", "path")
	// also test that derived vars work to be updated this way as well
	someDerivedVar := filepath.Join(dirs.SnapDataDir, "other", "mnt")

	// register a callback
	dirs.AddRootDirCallback(func(rootdir string) {
		someVar = filepath.Join(rootdir, "my", "path")
		// the var derived from rootdir was also updated before the callback is
		// run for simplicity
		someDerivedVar = filepath.Join(dirs.SnapDataDir, "other", "mnt")
	})

	// change root dir
	dirs.SetRootDir("/hello")

	// ensure our local vars were updated
	c.Assert(someVar, Equals, filepath.Join("/hello", "my", "path"))
	c.Assert(someDerivedVar, Equals, filepath.Join("/hello", "var", "snap", "other", "mnt"))
}

func (s *DirsTestSuite) TestLibexecDirDetect(c *C) {
	defer dirs.SetRootDir("/")

	d := c.MkDir()
	// no libexec directory, we default to /usr/lib/snapd
	dirs.SetRootDir(d)
	c.Check(dirs.StripRootDir(dirs.DistroLibExecDir), Equals, "/usr/lib/snapd")

	d = c.MkDir()
	// with /usr/lib/snapd present
	dirstest.MustMockDefaultLibExecDir(d)
	dirs.SetRootDir(d)
	c.Check(dirs.StripRootDir(dirs.DistroLibExecDir), Equals, "/usr/lib/snapd")
	// which has priority even if /usr/libexec/snapd was to exist
	dirstest.MustMockAltLibExecDir(d)
	dirs.SetRootDir(d)
	c.Check(dirs.StripRootDir(dirs.DistroLibExecDir), Equals, "/usr/lib/snapd")

	d = c.MkDir()
	dirstest.MustMockAltLibExecDir(d)
	dirs.SetRootDir(d)
	c.Check(dirs.StripRootDir(dirs.DistroLibExecDir), Equals, "/usr/libexec/snapd")
}

func (s *DirsTestSuite) TestWritableMountPath(c *C) {
	release.MockOnClassic(true)
	dirs.SetRootDir("/")

	c.Check(dirs.WritableMountPath, Equals, "/")
	c.Check(dirs.WritableUbuntuCoreSystemDataDir, Equals, "/system-data")
}

func (s *DirsTestSuite) TestWritableMountPathCore(c *C) {
	release.MockOnClassic(false)
	dirs.SetRootDir("/")

	c.Check(dirs.WritableMountPath, Equals, "/writable")
	c.Check(dirs.WritableUbuntuCoreSystemDataDir, Equals, "/writable/system-data")
}
