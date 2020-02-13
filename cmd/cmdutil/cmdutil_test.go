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

package cmdutil_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/cmdutil"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

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

	err := os.MkdirAll(filepath.Dir(ldSoConf), 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(ldSoConfD, 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(ldSoConf, []byte("include /etc/ld.so.conf.d/*.conf"), 0644)
	c.Assert(err, IsNil)

	ldSoConf1 := filepath.Join(ldSoConfD, "x86_64-linux-gnu.conf")

	err = ioutil.WriteFile(ldSoConf1, []byte(`
# Multiarch support
/lib/x86_64-linux-gnu
/usr/lib/x86_64-linux-gnu`), 0644)
	c.Assert(err, IsNil)
}

func (s *cmdutilSuite) TestCommandFromSystemSnap(c *C) {
	for _, snap := range []string{"core", "snapd"} {

		root := filepath.Join(dirs.SnapMountDir, snap, "current")
		s.makeMockLdSoConf(c, root)

		os.MkdirAll(filepath.Join(root, "/usr/bin"), 0755)
		osutil.CopyFile(truePath, filepath.Join(root, "/usr/bin/xdelta3"), 0)
		cmd, err := cmdutil.CommandFromSystemSnap("/usr/bin/xdelta3", "--some-xdelta-arg")
		c.Assert(err, IsNil)

		out, err := exec.Command("/bin/sh", "-c", fmt.Sprintf("readelf -l %s |grep interpreter:|cut -f2 -d:|cut -f1 -d]", truePath)).Output()
		c.Assert(err, IsNil)
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

	out, err := exec.Command("/bin/sh", "-c", "readelf -l /bin/true |grep interpreter:|cut -f2 -d:|cut -f1 -d]").Output()
	c.Assert(err, IsNil)
	interp := strings.TrimSpace(string(out))

	coreInterp := filepath.Join(root, interp)
	c.Assert(os.MkdirAll(filepath.Dir(coreInterp), 0755), IsNil)
	c.Assert(os.Symlink(filepath.Base(coreInterp), coreInterp), IsNil)

	_, err = cmdutil.CommandFromSystemSnap("/usr/bin/xdelta3", "--some-xdelta-arg")
	c.Assert(err, ErrorMatches, "cannot run command from core: symlink cycle found")
}
