// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"bytes"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/sandbox/cgroup"
)

type systemSuite struct{}

var _ = Suite(&systemSuite{})

func (s *systemSuite) TestLockCgroup(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")

	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	var frozen []string
	var thawed []string
	happyFreeze := func(snapName string) error {
		frozen = append(frozen, snapName)
		return nil
	}
	happyThaw := func(snapName string) error {
		thawed = append(thawed, snapName)
		return nil
	}
	cgroup.MockFreezing(happyFreeze, happyThaw)

	upCtx := update.NewSystemProfileUpdateContext("foo", false)
	unlock, err := upCtx.Lock()
	c.Assert(err, IsNil)
	c.Check(unlock, NotNil)

	c.Check(frozen, DeepEquals, []string{"foo"})
	c.Check(thawed, HasLen, 0)

	unlock()
	c.Check(frozen, DeepEquals, []string{"foo"})
	c.Check(thawed, DeepEquals, []string{"foo"})
}

func (s *systemSuite) TestAssumptions(c *C) {
	// Non-instances can access /tmp, /var/snap and /snap/$SNAP_NAME
	upCtx := update.NewSystemProfileUpdateContext("foo", false)
	as := upCtx.Assumptions()
	c.Check(as.UnrestrictedPaths(), DeepEquals, []string{"/tmp", "/var/snap", "/snap/foo", "/dev/shm", "/run/systemd", "/var/lib/snapd/hostfs/tmp"})
	c.Check(as.ModeForPath("/stuff"), Equals, os.FileMode(0755))
	c.Check(as.ModeForPath("/tmp"), Equals, os.FileMode(0755))
	c.Check(as.ModeForPath("/var/lib/snapd/hostfs/tmp"), Equals, os.FileMode(0755))
	c.Check(as.ModeForPath("/var/lib/snapd/hostfs/tmp/snap-private-tmp/snap.x11-server"), Equals, os.FileMode(0700))
	c.Check(as.ModeForPath("/var/lib/snapd/hostfs/tmp/snap-private-tmp/snap.x11-server/tmp"), Equals, os.FileMode(0777)|os.ModeSticky)
	c.Check(as.ModeForPath("/var/lib/snapd/hostfs/tmp/snap-private-tmp/snap.x11-server/foo"), Equals, os.FileMode(0755))
	c.Check(as.ModeForPath("/var/lib/snapd/hostfs/tmp/snap-private-tmp/snap.x11-server/tmp/.X11-unix"), Equals, os.FileMode(0777)|os.ModeSticky)
	c.Check(as.ModeForPath("/dev/shm/snap.some-snap"), Equals, os.FileMode(0777)|os.ModeSticky)

	// Instances can, in addition, access /snap/$SNAP_INSTANCE_NAME
	upCtx = update.NewSystemProfileUpdateContext("foo_instance", false)
	as = upCtx.Assumptions()
	c.Check(as.UnrestrictedPaths(), DeepEquals, []string{"/tmp", "/var/snap", "/snap/foo_instance", "/dev/shm", "/run/systemd", "/snap/foo", "/var/lib/snapd/hostfs/tmp"})
}

func (s *systemSuite) TestLoadDesiredProfile(c *C) {
	// Mock directories.
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")

	upCtx := update.NewSystemProfileUpdateContext("foo", false)
	text := "/snap/foo/42/dir /snap/bar/13/dir none bind,rw 0 0\n"

	// Write a desired system mount profile for snap "foo".
	path := update.DesiredSystemProfilePath(upCtx.InstanceName())
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(os.WriteFile(path, []byte(text), 0644), IsNil)

	// Ask the system profile update helper to read the desired profile.
	profile, err := upCtx.LoadDesiredProfile()
	c.Assert(err, IsNil)
	builder := &bytes.Buffer{}
	profile.WriteTo(builder)

	c.Check(builder.String(), Equals, text)
}

func (s *systemSuite) TestLoadCurrentProfile(c *C) {
	// Mock directories.
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")

	upCtx := update.NewSystemProfileUpdateContext("foo", false)
	text := "/snap/foo/42/dir /snap/bar/13/dir none bind,rw 0 0\n"

	// Write a current system mount profile for snap "foo".
	path := update.CurrentSystemProfilePath(upCtx.InstanceName())
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(os.WriteFile(path, []byte(text), 0644), IsNil)

	// Ask the system profile update helper to read the current profile.
	profile, err := upCtx.LoadCurrentProfile()
	c.Assert(err, IsNil)
	builder := &bytes.Buffer{}
	profile.WriteTo(builder)

	// The profile is returned unchanged.
	c.Check(builder.String(), Equals, text)
}

func (s *systemSuite) TestSaveCurrentProfile(c *C) {
	// Mock directories and create directory for runtime mount profiles.
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")
	c.Assert(os.MkdirAll(dirs.SnapRunNsDir, 0755), IsNil)

	upCtx := update.NewSystemProfileUpdateContext("foo", false)
	text := "/snap/foo/42/dir /snap/bar/13/dir none bind,rw 0 0\n"

	// Prepare a mount profile to be saved.
	profile, err := osutil.LoadMountProfileText(text)
	c.Assert(err, IsNil)

	// Ask the system profile update to write the current profile.
	var profilePath string
	var savedProfile string
	restore := update.MockSaveMountProfile(func(p *osutil.MountProfile, fname string, uid sys.UserID, gid sys.GroupID) (err error) {
		profilePath = fname
		savedProfile, err = osutil.SaveMountProfileText(p)
		return err
	})
	defer restore()
	c.Assert(upCtx.SaveCurrentProfile(profile), IsNil)
	c.Check(profilePath, Equals, update.CurrentSystemProfilePath(upCtx.InstanceName()))
	c.Check(savedProfile, Equals, text)
}

func (s *systemSuite) TestDesiredSystemProfilePath(c *C) {
	c.Check(update.DesiredSystemProfilePath("foo"), Equals, "/var/lib/snapd/mount/snap.foo.fstab")
}

func (s *systemSuite) TestCurrentSystemProfilePath(c *C) {
	c.Check(update.CurrentSystemProfilePath("foo"), Equals, "/run/snapd/ns/snap.foo.fstab")
}
