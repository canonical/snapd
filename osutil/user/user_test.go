// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package user_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/osutil/user"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type suite struct {
	restore func()
}

var _ = check.Suite(&suite{})

func (s *suite) SetUpTest(c *check.C) {
	rootDir := c.MkDir()
	dirs.SetRootDir(rootDir)
	// create some users
	passdata := fmt.Sprintf(`
user1::%d:%d::/home/user1:/bin/sh
user2::4294967294:4294967294::/home/user2:/bin/sh
`[1:], sys.Getuid(), sys.Getgid())

	for _, dir := range user.Passwds() {
		c.Assert(os.MkdirAll(filepath.Dir(filepath.Join(rootDir, dir)), 0755), check.IsNil)
	}
	c.Assert(ioutil.WriteFile(filepath.Join(rootDir, "/var/lib/extrausers/passwd"), []byte(passdata), 0644), check.IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(rootDir, "etc/passwd"), []byte("sshd:x:124:65534::/var/run/sshd:/usr/sbin/nologin\n"), 0644), check.IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(rootDir, "etc", "shells"), []byte("/bin/sh\n"), 0644), check.IsNil)
	s.restore = user.Mock()
}

func (s suite) TearDownTest(c *check.C) {
	s.restore()
	dirs.SetRootDir("")
}

func (suite) TestCurrent(c *check.C) {
	me, err := user.Current()
	c.Assert(err, check.IsNil)
	c.Check(me.Name(), check.DeepEquals, "user1")
}

func (suite) TestFromUID(c *check.C) {
	u1, err := user.FromUID(0xfffffffe)
	c.Assert(err, check.IsNil)
	c.Check(u1.UID(), check.Equals, sys.UserID(0xfffffffe))
	c.Check(u1.Name(), check.Equals, "user2")
	c.Check(u1.Home(), check.Equals, "/home/user2")
}

func (suite) TestFromName(c *check.C) {
	u1, err := user.FromName("user2")
	c.Assert(err, check.IsNil)
	c.Check(u1.UID(), check.Equals, sys.UserID(0xfffffffe))
	c.Check(u1.Name(), check.Equals, "user2")
	c.Check(u1.Home(), check.Equals, "/home/user2")
}

func (suite) TestFromNameOtherSource(c *check.C) {
	u1, err := user.FromName("sshd")
	c.Assert(err, check.IsNil)
	c.Check(u1.UID(), check.Equals, sys.UserID(124))
	c.Check(u1.Name(), check.Equals, "sshd")
	c.Check(u1.Home(), check.Equals, "/var/run/sshd")
}

func (suite) TestIterFull(c *check.C) {
	var us []string
	var it user.Iter
	defer it.Finish()
	for it.Scan() {
		us = append(us, it.User().Name())
	}
	// XXX this assumes an order, which isn't necessarily true for arbitrary backends
	c.Check(us, check.DeepEquals, []string{"user1", "user2", "sshd"})
}

func (suite) TestIterNoSystem(c *check.C) {
	var us []string
	var it user.Iter
	defer it.Finish()
	for it.Scan(user.IsNonSystem) {
		us = append(us, it.User().Name())
	}
	// XXX this assumes an order, which isn't necessarily true for arbitrary backends
	c.Check(us, check.DeepEquals, []string{"user1", "user2"})
}

func (suite) TestIterArbitrary(c *check.C) {
	var us []string
	var it user.Iter
	defer it.Finish()
	for it.Scan(func(u *user.User) bool {
		return !strings.HasSuffix(u.Name(), "2")
	}) {
		us = append(us, it.User().Name())
	}
	// XXX this assumes an order, which isn't necessarily true for arbitrary backends
	c.Check(us, check.DeepEquals, []string{"user1", "sshd"})
}

func (suite) TestFirst(c *check.C) {
	u, err := user.First(func(*user.User) bool { return false })
	c.Assert(u, check.IsNil)
	c.Check(err, check.Equals, user.NotFound)
}

func (suite) TestMinUIDHappy(c *check.C) {
	fname := filepath.Join(dirs.GlobalRootDir, "/etc/login.defs")
	c.Assert(ioutil.WriteFile(fname, []byte(`
BLA bla
# UID_MIN 999
UID_MIN potatoes
UID_MIN 7 is a lie
UID_MIN 64# comments must be on their own line
UID_MIN 42
UID_MIN 12345
`), 0644), check.IsNil)
	// it picks up the first spec-compliant value
	c.Check(user.MinUID(), check.Equals, sys.UserID(42))
}

func (suite) TestMinUIDEmpty(c *check.C) {
	fname := filepath.Join(dirs.GlobalRootDir, "/etc/login.defs")
	c.Assert(ioutil.WriteFile(fname, nil, 0644), check.IsNil)
	c.Check(user.MinUID(), check.Equals, sys.UserID(1000))
}

func (suite) TestMinUIDMissing(c *check.C) {
	fname := filepath.Join(dirs.GlobalRootDir, "/etc/login.defs")
	c.Assert(osutil.FileExists(fname), check.Equals, false)
	c.Check(user.MinUID(), check.Equals, sys.UserID(1000))
}

func (suite) TestMinUIDMissingLine(c *check.C) {
	fname := filepath.Join(dirs.GlobalRootDir, "/etc/login.defs")
	c.Assert(ioutil.WriteFile(fname, []byte("HELLO THERE\n"), 0644), check.IsNil)
	c.Check(user.MinUID(), check.Equals, sys.UserID(1000))
}
