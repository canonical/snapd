// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package selinux_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/sandbox/selinux"
	"github.com/snapcore/snapd/testutil"
)

type labelSuite struct {
	path string
}

var _ = check.Suite(&labelSuite{})

func (l *labelSuite) SetUpTest(c *check.C) {
	l.path = filepath.Join(c.MkDir(), "foo")
	os.WriteFile(l.path, []byte("foo"), 0644)
}

func (l *labelSuite) TestVerifyHappyOk(c *check.C) {
	cmd := testutil.MockCommand(c, "matchpathcon", "")
	defer cmd.Restore()

	ok := mylog.Check2(selinux.VerifyPathContext(l.path))
	c.Assert(err, check.IsNil)
	c.Assert(ok, check.Equals, true)
	c.Assert(cmd.Calls(), check.DeepEquals, [][]string{
		{"matchpathcon", "-V", l.path},
	})
}

func (l *labelSuite) TestVerifyFailGibberish(c *check.C) {
	cmd := testutil.MockCommand(c, "matchpathcon", "echo gibberish; exit 1 ")
	defer cmd.Restore()

	ok := mylog.Check2(selinux.VerifyPathContext(l.path))
	c.Assert(err, check.ErrorMatches, "exit status 1")
	c.Assert(ok, check.Equals, false)
}

func (l *labelSuite) TestVerifyFailStatus(c *check.C) {
	cmd := testutil.MockCommand(c, "matchpathcon", "echo gibberish; exit 5 ")
	defer cmd.Restore()

	ok := mylog.Check2(selinux.VerifyPathContext(l.path))
	c.Assert(err, check.ErrorMatches, "exit status 5")
	c.Assert(ok, check.Equals, false)
}

func (l *labelSuite) TestVerifyFailNoPath(c *check.C) {
	cmd := testutil.MockCommand(c, "matchpathcon", ``)
	defer cmd.Restore()

	ok := mylog.Check2(selinux.VerifyPathContext("does-not-exist"))
	c.Assert(err, check.ErrorMatches, ".* does-not-exist: no such file or directory")
	c.Assert(ok, check.Equals, false)
}

func (l *labelSuite) TestVerifyFailNoTool(c *check.C) {
	if _ := mylog.Check2(exec.LookPath("matchpathcon")); err == nil {
		c.Skip("matchpathcon found in $PATH")
	}
	ok := mylog.Check2(selinux.VerifyPathContext(l.path))
	c.Assert(err, check.ErrorMatches, `exec: "matchpathcon": executable file not found in \$PATH`)
	c.Assert(ok, check.Equals, false)
}

func (l *labelSuite) TestVerifyHappyMismatch(c *check.C) {
	cmd := testutil.MockCommand(c, "matchpathcon", fmt.Sprintf(`
echo %s has context unconfined_u:object_r:user_home_t:s0, should be unconfined_u:object_r:snappy_home_t:s0
exit 1`, l.path))
	defer cmd.Restore()

	ok := mylog.Check2(selinux.VerifyPathContext(l.path))
	c.Assert(err, check.IsNil)
	c.Assert(ok, check.Equals, false)
}

func (l *labelSuite) TestRestoreHappy(c *check.C) {
	cmd := testutil.MockCommand(c, "restorecon", "")
	defer cmd.Restore()
	mylog.Check(selinux.RestoreContext(l.path, selinux.RestoreMode{}))
	c.Assert(err, check.IsNil)
	c.Assert(cmd.Calls(), check.DeepEquals, [][]string{
		{"restorecon", l.path},
	})

	cmd.ForgetCalls()
	mylog.Check(selinux.RestoreContext(l.path, selinux.RestoreMode{Recursive: true}))
	c.Assert(err, check.IsNil)
	c.Assert(cmd.Calls(), check.DeepEquals, [][]string{
		{"restorecon", "-R", l.path},
	})
}

func (l *labelSuite) TestRestoreFailNoTool(c *check.C) {
	if _ := mylog.Check2(exec.LookPath("matchpathcon")); err == nil {
		c.Skip("matchpathcon found in $PATH")
	}
	mylog.Check(selinux.RestoreContext(l.path, selinux.RestoreMode{}))
	c.Assert(err, check.ErrorMatches, `exec: "restorecon": executable file not found in \$PATH`)
}

func (l *labelSuite) TestRestoreFail(c *check.C) {
	cmd := testutil.MockCommand(c, "restorecon", "exit 1")
	defer cmd.Restore()
	mylog.Check(selinux.RestoreContext(l.path, selinux.RestoreMode{}))
	c.Assert(err, check.ErrorMatches, "exit status 1")
	mylog.Check(selinux.RestoreContext("does-not-exist", selinux.RestoreMode{}))
	c.Assert(err, check.ErrorMatches, ".* does-not-exist: no such file or directory")
}
