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

package snap_test

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
)

type processInfoSuite struct{}

var _ = Suite(&processInfoSuite{})

func (s *processInfoSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *processInfoSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *processInfoSuite) TestAppArmorLabelForPidImpl(c *C) {
	// When no /proc/$pid/attr/current exists, assume unconfined
	label, err := snap.AppArmorLabelForPidImpl(42)
	c.Check(label, Equals, "unconfined")
	c.Check(err, IsNil)

	procFile := filepath.Join(dirs.GlobalRootDir, "/proc/42/attr/current")
	c.Assert(os.MkdirAll(filepath.Dir(procFile), 0755), IsNil)
	for _, t := range []struct {
		contents []byte
		label    string
	}{
		{[]byte("unconfined\n"), "unconfined"},
		{[]byte("/usr/sbin/cupsd (enforce)\n"), "/usr/sbin/cupsd"},
		{[]byte("snap.foo.app (complain)\n"), "snap.foo.app"},
	} {
		c.Assert(ioutil.WriteFile(procFile, t.contents, 0644), IsNil)
		label, err := snap.AppArmorLabelForPidImpl(42)
		c.Check(err, IsNil)
		c.Check(label, Equals, t.label)
	}
}

func (s *processInfoSuite) TestDecodeAppArmorLabel(c *C) {
	label := snap.AppSecurityTag("snap_name", "my-app")
	info, err := snap.DecodeAppArmorLabel(label)
	c.Assert(err, IsNil)
	c.Check(info.InstanceName, Equals, "snap_name")
	c.Check(info.AppName, Equals, "my-app")
	c.Check(info.HookName, Equals, "")

	label = snap.HookSecurityTag("snap_name", "my-hook")
	info, err = snap.DecodeAppArmorLabel(label)
	c.Assert(err, IsNil)
	c.Check(info.InstanceName, Equals, "snap_name")
	c.Check(info.AppName, Equals, "")
	c.Check(info.HookName, Equals, "my-hook")

	_, err = snap.DecodeAppArmorLabel("unconfined")
	c.Assert(err, ErrorMatches, `security label "unconfined" does not belong to a snap`)

	_, err = snap.DecodeAppArmorLabel("/usr/bin/ntpd")
	c.Assert(err, ErrorMatches, `security label "/usr/bin/ntpd" does not belong to a snap`)
}

func (s *processInfoSuite) TestDecodeAppArmorLabelUnrecognisedSnapLabel(c *C) {
	_, err := snap.DecodeAppArmorLabel("snap.weird")
	c.Assert(err, ErrorMatches, `unknown snap related security label "snap.weird"`)
}

func (s *processInfoSuite) TestNameFromPidHappy(c *C) {
	restore := snap.MockCgroupSnapNameFromPid(func(pid int) (string, error) {
		c.Assert(pid, Equals, 333)
		return "hello-world", nil
	})
	defer restore()
	restore = snap.MockAppArmorLabelForPid(func(pid int) (string, error) {
		c.Assert(pid, Equals, 333)
		return "snap.hello-world.app", nil
	})
	defer restore()
	info, err := snap.NameFromPid(333)
	c.Assert(err, IsNil)
	c.Check(info.InstanceName, Equals, "hello-world")
	c.Check(info.AppName, Equals, "app")
	c.Check(info.HookName, Equals, "")
}

func (s *processInfoSuite) TestNameFromPidNoAppArmor(c *C) {
	restore := snap.MockCgroupSnapNameFromPid(func(pid int) (string, error) {
		c.Assert(pid, Equals, 333)
		return "hello-world", nil
	})
	defer restore()
	restore = snap.MockAppArmorLabelForPid(func(pid int) (string, error) {
		c.Assert(pid, Equals, 333)
		return "", errors.New("no label")
	})
	defer restore()
	info, err := snap.NameFromPid(333)
	c.Assert(err, IsNil)
	c.Check(info.InstanceName, Equals, "hello-world")
	c.Check(info.AppName, Equals, "")
	c.Check(info.HookName, Equals, "")
}

func (s *processInfoSuite) TestNameFromPidUnhappy(c *C) {
	restore := snap.MockCgroupSnapNameFromPid(func(pid int) (string, error) {
		c.Assert(pid, Equals, 333)
		return "", errors.New("nada")
	})
	defer restore()
	restore = snap.MockAppArmorLabelForPid(func(pid int) (string, error) {
		c.Error("unexpected appArmorLabelForPid call")
		return "", errors.New("no label")
	})
	defer restore()
	info, err := snap.NameFromPid(333)
	c.Assert(err, ErrorMatches, "nada")
	c.Check(info, DeepEquals, snap.ProcessInfo{})
}
