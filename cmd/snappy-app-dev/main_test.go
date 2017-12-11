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

package main_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	main "github.com/snapcore/snapd/cmd/snappy-app-dev"
	"github.com/snapcore/snapd/logger"
)

func Test(t *testing.T) { TestingT(t) }

type snappyAppDevSuite struct {
	restoreLogger func()
}

var _ = Suite(&snappyAppDevSuite{})

func (s *snappyAppDevSuite) TestActionAndNameValid(c *C) {
	for _, t := range []struct {
		inp string
		tag string
		nam string
	}{
		{"add", "snap_foo_bar", "/sys/fs/cgroup/devices/snap.foo.bar/devices.allow"},
		{"change", "snap_foo_bar", "/sys/fs/cgroup/devices/snap.foo.bar/devices.allow"},
		{"remove", "snap_foo_bar", "/sys/fs/cgroup/devices/snap.foo.bar/devices.deny"},
	} {
		fn, err := main.GetDeviceCgroupFn(t.inp, t.tag)
		c.Assert(err, IsNil)
		c.Check(fn, DeepEquals, t.nam)
	}
}

func (s *snappyAppDevSuite) TestActionOrNameInvalid(c *C) {
	for _, t := range []struct {
		inp string
		tag string
		msg string
	}{
		{"not-a-command", "snap_foo_bar", `unsupported action "not-a-command"`},
		{"add", "sn@p_foo_bar", `appname should be snap_NAME_COMMAND`},
		{"change", "snap_foo", `appname should be snap_NAME_COMMAND`},
		{"remove", "snap_f%o_bar", `invalid snap name: "f%o"`},
		{"remove", "snap_foo_b@r", `cannot have "b@r" as app name - use letters, digits, and dash as separator`},
	} {
		fn, err := main.GetDeviceCgroupFn(t.inp, t.tag)
		c.Assert(err, NotNil)
		c.Check(err, ErrorMatches, t.msg, Commentf("%q %q errors in unexpected ways, got %q, expected %q", t.inp, t.tag, err, t.msg))
		c.Check(fn, DeepEquals, "")
	}
}

func (s *snappyAppDevSuite) TestDevPathAndMajorMinorValid(c *C) {
	for _, t := range []struct {
		dev string
		mod string
		exp string
	}{
		{"/devices/virtual/mem/kmsg", "1:11", "c 1:11 rwm"},
		{"/devices/pci0000:00/0000:00:07.0/virtio2/block/vda", "253:0", "b 253:0 rwm"},
	} {
		acl, err := main.GetAcl(t.dev, t.mod)
		c.Assert(err, IsNil)
		c.Check(acl, DeepEquals, t.exp)
	}
}

func (s *snappyAppDevSuite) TestDevPathOrMajorMinorInvalid(c *C) {
	for _, t := range []struct {
		dev string
		mod string
		msg string
	}{
		{"kmsg", "1:11", "DEVPATH should start with /"},
		{"/devices/virtual/mem/../foo/kmsg", "1:11", `invalid DEVPATH "/devices/virtual/mem/../foo/kmsg"`},
		{"/devices/virtual/mem/kmsg", "1", "should be MAJOR:MINOR"},
		{"/devices/virtual/mem/kmsg", ":1", "MAJOR and MINOR should be uint32"},
		{"/devices/virtual/mem/kmsg", "1:", "MAJOR and MINOR should be uint32"},
		{"/devices/virtual/mem/kmsg", "bad:11", "MAJOR and MINOR should be uint32"},
		{"/devices/virtual/mem/kmsg", "1:bad", "MAJOR and MINOR should be uint32"},
		{"/devices/virtual/mem/kmsg", "1:-1", "MAJOR and MINOR should be uint32"},
		{"/devices/virtual/mem/kmsg", "1:1\\01", "MAJOR and MINOR should be uint32"},
	} {
		acl, err := main.GetAcl(t.dev, t.mod)
		c.Assert(err, NotNil)
		c.Check(err, ErrorMatches, t.msg, Commentf("%q %q errors in unexpected ways, got %q, expected %q", t.dev, t.mod, err, t.msg))
		c.Check(acl, DeepEquals, "")
	}
}

func (s *snappyAppDevSuite) TestRunNoCgroup(c *C) {
	restore := main.MockDeviceCgroupDir(c)
	defer restore()
	restoreLogger := main.MockInitLogger(logger.SimpleSetup)
	defer restoreLogger()

	name := "snap.foo.bar"
	tag := strings.Replace(name, ".", "_", -1)
	path := filepath.Join(main.DeviceCgroupDir(), name)
	cmd := []string{"sad", "add", tag, "/devices/virtual/mem/kmsg", "1:11"}

	// When the device cgroup directory for the snap doesn't exist
	c.Assert(func() { main.DoRun(cmd) }, PanicMatches, `.*/devices.allow: no such file or directory\n`)
	_, err := os.Stat(path)
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (s *snappyAppDevSuite) TestRun(c *C) {
	restore := main.MockDeviceCgroupDir(c)
	defer restore()

	name := "snap.foo.bar"
	tag := strings.Replace(name, ".", "_", -1)
	path := filepath.Join(main.DeviceCgroupDir(), name)
	allow := filepath.Join(path, "devices.allow")
	cmd := []string{"sad", "add", tag, "/devices/virtual/mem/kmsg", "1:11"}

	// When the cgroup exists we write to it
	c.Assert(os.MkdirAll(path, 0755), IsNil)
	c.Assert(main.DoRun(cmd), IsNil)
	_, err := os.Stat(allow)
	c.Assert(err, IsNil)
	data, err := ioutil.ReadFile(allow)
	c.Assert(err, IsNil)
	c.Assert(string(data), DeepEquals, "c 1:11 rwm\n")
}

func (s *snappyAppDevSuite) TestRunBadArgs(c *C) {
	restore := main.MockInitLogger(logger.SimpleSetup)
	defer restore()

	for _, t := range []struct {
		cmd []string
		msg string
	}{
		{[]string{"add", "snap_foo_bar", "/devices/virtual/mem/kmsg", "1:11"}, `add ACTION APPNAME DEVPATH MAJOR:MINOR\n`},
		{[]string{"sad", "bad", "snap_foo_bar", "/devices/virtual/mem/kmsg", "1:11"}, `unsupported action "bad"\n`},
		{[]string{"sad", "add", "snap_foo_bar", "/devices/virtual/mem/kmsg", "1"}, `should be MAJOR:MINOR\n`},
	} {
		c.Assert(func() { main.DoRun(t.cmd) }, PanicMatches, t.msg)
	}
}

func (s *snappyAppDevSuite) TestInitLoggerFail(c *C) {
	restore := main.MockInitLogger(main.InitLoggerFail)
	defer restore()

	cmd := []string{"sad", "add", "snap_foo_bar", "/devices/virtual/mem/kmsg", "1:11"}
	err := main.DoRun(cmd)
	c.Assert(err, NotNil)
	expected := "mock failure"
	c.Check(err, ErrorMatches, expected, Commentf("%q errors in unexpected ways, got %q, expected %q", cmd, err, expected))
}
