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

package xdgopenproxy_test

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/godbus/dbus"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/xdgopenproxy"
)

func Test(t *testing.T) { TestingT(t) }

type xdgOpenSuite struct{}

var _ = Suite(&xdgOpenSuite{})

func (s *xdgOpenSuite) TestOpenURL(c *C) {
	launcher := fakeBusObject(func(method string, args ...interface{}) error {
		c.Check(method, Equals, "io.snapcraft.Launcher.OpenURL")
		c.Check(args, DeepEquals, []interface{}{"http://example.org"})
		return nil
	})
	c.Check(xdgopenproxy.Launch(launcher, "http://example.org"), IsNil)
}

func (s *xdgOpenSuite) TestOpenFile(c *C) {
	path := filepath.Join(c.MkDir(), "test.txt")
	c.Assert(ioutil.WriteFile(path, []byte("Hello world"), 0644), IsNil)

	launcher := fakeBusObject(func(method string, args ...interface{}) error {
		c.Check(method, Equals, "io.snapcraft.Launcher.OpenFile")
		c.Assert(args, HasLen, 2)
		c.Check(args[0], Equals, "")
		c.Check(fdMatchesFile(int(args[1].(dbus.UnixFD)), path), IsNil)
		return nil
	})
	c.Check(xdgopenproxy.Launch(launcher, path), IsNil)
}

func (s *xdgOpenSuite) TestOpenFileURL(c *C) {
	path := filepath.Join(c.MkDir(), "test.txt")
	c.Assert(ioutil.WriteFile(path, []byte("Hello world"), 0644), IsNil)

	launcher := fakeBusObject(func(method string, args ...interface{}) error {
		c.Check(method, Equals, "io.snapcraft.Launcher.OpenFile")
		c.Assert(args, HasLen, 2)
		c.Check(args[0], Equals, "")
		c.Check(fdMatchesFile(int(args[1].(dbus.UnixFD)), path), IsNil)
		return nil
	})

	u := url.URL{Scheme: "file", Path: path}
	c.Check(xdgopenproxy.Launch(launcher, u.String()), IsNil)
}

func (s *xdgOpenSuite) TestOpenDir(c *C) {
	dir := c.MkDir()

	launcher := fakeBusObject(func(method string, args ...interface{}) error {
		c.Check(method, Equals, "io.snapcraft.Launcher.OpenFile")
		c.Assert(args, HasLen, 2)
		c.Check(args[0], Equals, "")
		c.Check(fdMatchesFile(int(args[1].(dbus.UnixFD)), dir), IsNil)
		return nil
	})
	c.Check(xdgopenproxy.Launch(launcher, dir), IsNil)
}

func (s *xdgOpenSuite) TestOpenMissingFile(c *C) {
	path := filepath.Join(c.MkDir(), "no-such-file.txt")

	launcher := fakeBusObject(func(method string, args ...interface{}) error {
		c.Error("unexpected D-Bus call")
		return nil
	})
	c.Check(xdgopenproxy.Launch(launcher, path), ErrorMatches, "no such file or directory")
}

func (s *xdgOpenSuite) TestOpenUnreadableFile(c *C) {
	if os.Getuid() == 0 {
		c.Skip("test will not work for root")
	}

	path := filepath.Join(c.MkDir(), "test.txt")
	c.Assert(ioutil.WriteFile(path, []byte("Hello world"), 0644), IsNil)
	c.Assert(os.Chmod(path, 0), IsNil)

	launcher := fakeBusObject(func(method string, args ...interface{}) error {
		c.Error("unexpected D-Bus call")
		return nil
	})
	c.Check(xdgopenproxy.Launch(launcher, path), ErrorMatches, "permission denied")
}

func fdMatchesFile(fd int, filename string) error {
	var fdStat, fileStat syscall.Stat_t
	if err := syscall.Fstat(fd, &fdStat); err != nil {
		return err
	}
	if err := syscall.Stat(filename, &fileStat); err != nil {
		return err
	}
	if fdStat.Dev != fileStat.Dev || fdStat.Ino != fileStat.Ino {
		return fmt.Errorf("File descriptor and fd do not match")
	}
	return nil
}

// fakeBusObject is a dbus.BusObject implementation that forwards
// Call invocations
type fakeBusObject func(method string, args ...interface{}) error

func (f fakeBusObject) Call(method string, flags dbus.Flags, args ...interface{}) *dbus.Call {
	err := f(method, args...)
	return &dbus.Call{Err: err}
}

func (f fakeBusObject) Go(method string, flags dbus.Flags, ch chan *dbus.Call, args ...interface{}) *dbus.Call {
	return nil
}

func (f fakeBusObject) GetProperty(prop string) (dbus.Variant, error) {
	return dbus.Variant{}, nil
}

func (f fakeBusObject) Destination() string {
	return ""
}

func (f fakeBusObject) Path() dbus.ObjectPath {
	return ""
}
