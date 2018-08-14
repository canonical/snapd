// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package backend_test

import (
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/squashfs"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type mountunitSuite struct {
	umount *testutil.MockCmd

	systemctlRestorer func()
}

var _ = Suite(&mountunitSuite{})

func (s *mountunitSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc", "systemd", "system", "multi-user.target.wants"), 0755)
	c.Assert(err, IsNil)

	s.systemctlRestorer = systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	})
	s.umount = testutil.MockCommand(c, "umount", "")
}

func (s *mountunitSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	s.umount.Restore()
	s.systemctlRestorer()
}

func (s *mountunitSuite) TestAddMountUnit(c *C) {
	restore := squashfs.MockUseFuse(false)
	defer restore()

	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(13),
		},
		Version:       "1.1",
		Architectures: []string{"all"},
	}
	err := backend.AddMountUnit(info, progress.Null)
	c.Assert(err, IsNil)

	// ensure correct mount unit
	un := fmt.Sprintf("%s.mount", systemd.EscapeUnitNamePath(filepath.Join(dirs.StripRootDir(dirs.SnapMountDir), "foo", "13")))
	c.Assert(filepath.Join(dirs.SnapServicesDir, un), testutil.FileEquals, fmt.Sprintf(`
[Unit]
Description=Mount unit for foo, revision 13
Before=snapd.service

[Mount]
What=/var/lib/snapd/snaps/foo_13.snap
Where=%s/foo/13
Type=squashfs
Options=nodev,ro,x-gdu.hide

[Install]
WantedBy=multi-user.target
`[1:], dirs.StripRootDir(dirs.SnapMountDir)))
}

func (s *mountunitSuite) TestRemoveMountUnit(c *C) {
	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(13),
		},
		Version:       "1.1",
		Architectures: []string{"all"},
	}

	err := backend.AddMountUnit(info, progress.Null)
	c.Assert(err, IsNil)

	// ensure we have the files
	un := fmt.Sprintf("%s.mount", systemd.EscapeUnitNamePath(filepath.Join(dirs.StripRootDir(dirs.SnapMountDir), "foo", "13")))
	p := filepath.Join(dirs.SnapServicesDir, un)
	c.Assert(osutil.FileExists(p), Equals, true)

	// now call remove and ensure they are gone
	err = backend.RemoveMountUnit(info.MountDir(), progress.Null)
	c.Assert(err, IsNil)
	p = filepath.Join(dirs.SnapServicesDir, un)
	c.Assert(osutil.FileExists(p), Equals, false)
}
