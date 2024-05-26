// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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

package snapdtool_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snapdtool"
)

var truePath = osutil.LookPathDefault("true", "/bin/true")

type cmdutilSuite struct{}

var _ = Suite(&cmdutilSuite{})

func (s *cmdutilSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *cmdutilSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *cmdutilSuite) makeMockLdSoConf(c *C, root string) {
	ldSoConf := filepath.Join(root, "/etc/ld.so.conf")
	ldSoConfD := ldSoConf + ".d"
	mylog.Check(os.MkdirAll(filepath.Dir(ldSoConf), 0755))

	mylog.Check(os.MkdirAll(ldSoConfD, 0755))

	mylog.Check(os.WriteFile(ldSoConf, []byte("include /etc/ld.so.conf.d/*.conf"), 0644))


	ldSoConf1 := filepath.Join(ldSoConfD, "x86_64-linux-gnu.conf")
	mylog.Check(os.WriteFile(ldSoConf1, []byte(`
# Multiarch support
/lib/x86_64-linux-gnu
/usr/lib/x86_64-linux-gnu`), 0644))

}

func (s *cmdutilSuite) TestCommandFromSystemSnap(c *C) {
	for _, snap := range []string{"core", "snapd"} {

		root := filepath.Join(dirs.SnapMountDir, snap, "current")
		s.makeMockLdSoConf(c, root)

		os.MkdirAll(filepath.Join(root, "/usr/bin"), 0755)
		osutil.CopyFile(truePath, filepath.Join(root, "/usr/bin/xdelta3"), 0)
		cmd := mylog.Check2(snapdtool.CommandFromSystemSnap("/usr/bin/xdelta3", "--some-xdelta-arg"))


		out := mylog.Check2(exec.Command("/bin/sh", "-c", fmt.Sprintf("readelf -l %s |grep interpreter:|cut -f2 -d:|cut -f1 -d]", truePath)).Output())

		interp := strings.TrimSpace(string(out))

		c.Check(cmd.Args, DeepEquals, []string{
			filepath.Join(root, interp),
			"--library-path",
			fmt.Sprintf("%s/lib/x86_64-linux-gnu:%s/usr/lib/x86_64-linux-gnu", root, root),
			filepath.Join(root, "/usr/bin/xdelta3"),
			"--some-xdelta-arg",
		})
	}
}

func (s *cmdutilSuite) TestCommandFromCoreSymlinkCycle(c *C) {
	root := filepath.Join(dirs.SnapMountDir, "/core/current")
	s.makeMockLdSoConf(c, root)

	os.MkdirAll(filepath.Join(root, "/usr/bin"), 0755)
	osutil.CopyFile(truePath, filepath.Join(root, "/usr/bin/xdelta3"), 0)

	out := mylog.Check2(exec.Command("/bin/sh", "-c", "readelf -l /bin/true |grep interpreter:|cut -f2 -d:|cut -f1 -d]").Output())

	interp := strings.TrimSpace(string(out))

	coreInterp := filepath.Join(root, interp)
	c.Assert(os.MkdirAll(filepath.Dir(coreInterp), 0755), IsNil)
	c.Assert(os.Symlink(filepath.Base(coreInterp), coreInterp), IsNil)

	_ = mylog.Check2(snapdtool.CommandFromSystemSnap("/usr/bin/xdelta3", "--some-xdelta-arg"))
	c.Assert(err, ErrorMatches, "cannot run command from core: symlink cycle found")
}
