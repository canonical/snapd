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
	"context"
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

type xdgSnapcraftLauncherOpenSuite struct{}

var _ = Suite(&xdgSnapcraftLauncherOpenSuite{})

type busObj struct{ name, path string }

type fakeBus func(name string, objectPath dbus.ObjectPath) dbus.BusObject

func (b fakeBus) Object(name string, objectPath dbus.ObjectPath) dbus.BusObject {
	return b(name, objectPath)
}

func (b fakeBus) Close() error { return nil }

func unsupportedFakeBusObject() fakeBusObject {
	return fakeBusObject(func(method string, args ...interface{}) error {
		return fmt.Errorf("not supported")
	})
}

func setupFakeBus(c *C, objs map[busObj]fakeBusObject) (restore func()) {
	restore = xdgopenproxy.MockSessionBus(func() (xdgopenproxy.Bus, error) {
		return fakeBus(func(name string, objectPath dbus.ObjectPath) dbus.BusObject {
			fbo, ok := objs[busObj{name, string(objectPath)}]
			c.Assert(ok, Equals, true, Commentf("unexpected bus object: %v %v", name, objectPath))
			return fbo
		}), nil
	})
	return restore

}

func setupBusWithSnapcraftLauncher(c *C, obj fakeBusObject) (restore func()) {
	return setupFakeBus(c, map[busObj]fakeBusObject{
		{"io.snapcraft.Launcher", "/io/snapcraft/Launcher"}:                   obj,
		{"org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop"}: unsupportedFakeBusObject(),
	})
}

func (s *xdgSnapcraftLauncherOpenSuite) TestOpenURL(c *C) {
	launcher := fakeBusObject(func(method string, args ...interface{}) error {
		c.Check(method, Equals, "io.snapcraft.Launcher.OpenURL")
		c.Check(args, DeepEquals, []interface{}{"http://example.org"})
		return nil
	})
	restore := setupBusWithSnapcraftLauncher(c, launcher)
	defer restore()

	c.Check(xdgopenproxy.Run("http://example.org"), IsNil)
}

func (s *xdgSnapcraftLauncherOpenSuite) TestOpenFile(c *C) {
	path := filepath.Join(c.MkDir(), "test.txt")
	c.Assert(ioutil.WriteFile(path, []byte("Hello world"), 0644), IsNil)

	called := false
	launcher := fakeBusObject(func(method string, args ...interface{}) error {
		c.Check(method, Equals, "io.snapcraft.Launcher.OpenFile")
		c.Assert(args, HasLen, 2)
		c.Check(args[0], Equals, "")
		c.Check(fdMatchesFile(int(args[1].(dbus.UnixFD)), path), IsNil)
		called = true
		return nil
	})
	restore := setupBusWithSnapcraftLauncher(c, launcher)
	defer restore()

	c.Check(xdgopenproxy.Run(path), IsNil)
	c.Check(called, Equals, true)
}

func (s *xdgSnapcraftLauncherOpenSuite) TestOpenFileURL(c *C) {
	path := filepath.Join(c.MkDir(), "test.txt")
	c.Assert(ioutil.WriteFile(path, []byte("Hello world"), 0644), IsNil)

	called := false
	launcher := fakeBusObject(func(method string, args ...interface{}) error {
		c.Check(method, Equals, "io.snapcraft.Launcher.OpenFile")
		c.Assert(args, HasLen, 2)
		c.Check(args[0], Equals, "")
		c.Check(fdMatchesFile(int(args[1].(dbus.UnixFD)), path), IsNil)
		called = true
		return nil
	})

	restore := setupBusWithSnapcraftLauncher(c, launcher)
	defer restore()

	u := url.URL{Scheme: "file", Path: path}
	c.Check(xdgopenproxy.Run(u.String()), IsNil)
	c.Check(called, Equals, true)
}

func (s *xdgSnapcraftLauncherOpenSuite) TestOpenDir(c *C) {
	dir := c.MkDir()

	called := false
	launcher := fakeBusObject(func(method string, args ...interface{}) error {
		c.Check(method, Equals, "io.snapcraft.Launcher.OpenFile")
		c.Assert(args, HasLen, 2)
		c.Check(args[0], Equals, "")
		c.Check(fdMatchesFile(int(args[1].(dbus.UnixFD)), dir), IsNil)
		called = true
		return nil
	})
	restore := setupBusWithSnapcraftLauncher(c, launcher)
	defer restore()

	c.Check(xdgopenproxy.Run(dir), IsNil)
	c.Check(called, Equals, true)
}

func (s *xdgSnapcraftLauncherOpenSuite) TestOpenMissingFile(c *C) {
	path := filepath.Join(c.MkDir(), "no-such-file.txt")

	launcher := fakeBusObject(func(method string, args ...interface{}) error {
		c.Error("unexpected D-Bus call")
		return nil
	})
	restore := setupBusWithSnapcraftLauncher(c, launcher)
	defer restore()

	c.Check(xdgopenproxy.Run(path), ErrorMatches, "no such file or directory")
}

func (s *xdgSnapcraftLauncherOpenSuite) TestOpenUnreadableFile(c *C) {
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
	restore := setupBusWithSnapcraftLauncher(c, launcher)
	defer restore()

	c.Check(xdgopenproxy.Run(path), ErrorMatches, "permission denied")
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

type xdgFreedesktopPortalOpenSuite struct{}

var _ = Suite(&xdgFreedesktopPortalOpenSuite{})

func setupBusWithFreedesktopPortal(c *C, obj fakeBusObject) (restore func()) {
	return setupFakeBus(c, map[busObj]fakeBusObject{
		{"io.snapcraft.Launcher", "/io/snapcraft/Launcher"}:                   unsupportedFakeBusObject(),
		{"org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop"}: obj,
	})
}

func (s *xdgFreedesktopPortalOpenSuite) TestOpenURL(c *C) {
	called := false
	launcher := fakeBusObject(func(method string, args ...interface{}) error {
		if method == "org.freedesktop.DBus.Peer.Ping" {
			return nil
		}
		c.Check(method, Equals, "org.freedesktop.portal.OpenURI.OpenURI")
		c.Assert(args, HasLen, 3)
		c.Check(args[1], Equals, "http://example.org")
		called = true
		return nil
	})
	restore := setupBusWithFreedesktopPortal(c, launcher)
	defer restore()

	c.Check(xdgopenproxy.Run("http://example.org"), IsNil)
	c.Check(called, Equals, true)
}

func (s *xdgFreedesktopPortalOpenSuite) TestOpenFile(c *C) {
	path := filepath.Join(c.MkDir(), "test.txt")
	c.Assert(ioutil.WriteFile(path, []byte("Hello world"), 0644), IsNil)

	called := false
	launcher := fakeBusObject(func(method string, args ...interface{}) error {
		if method == "org.freedesktop.DBus.Peer.Ping" {
			return nil
		}
		c.Check(method, Equals, "org.freedesktop.portal.OpenURI.OpenFile")
		c.Assert(args, HasLen, 3)
		c.Check(args[0], Equals, "")
		c.Check(fdMatchesFile(int(args[1].(dbus.UnixFD)), path), IsNil)
		called = true
		return nil
	})
	restore := setupBusWithFreedesktopPortal(c, launcher)
	defer restore()

	c.Check(xdgopenproxy.Run(path), IsNil)
	c.Check(called, Equals, true)
}

// fakeBusObject is a dbus.BusObject implementation that forwards
// Call invocations
type fakeBusObject func(method string, args ...interface{}) error

func (f fakeBusObject) Call(method string, flags dbus.Flags, args ...interface{}) *dbus.Call {
	err := f(method, args...)
	return &dbus.Call{Err: err}
}

func (f fakeBusObject) CallWithContext(ctx context.Context, method string, flags dbus.Flags, args ...interface{}) *dbus.Call {
	err := f(method, args...)
	return &dbus.Call{Err: err}
}

func (f fakeBusObject) Go(method string, flags dbus.Flags, ch chan *dbus.Call, args ...interface{}) *dbus.Call {
	return nil
}

func (f fakeBusObject) GoWithContext(ctx context.Context, method string, flags dbus.Flags, ch chan *dbus.Call, args ...interface{}) *dbus.Call {
	return nil
}

func (f fakeBusObject) AddMatchSignal(iface, member string, options ...dbus.MatchOption) *dbus.Call {
	return nil
}

func (f fakeBusObject) RemoveMatchSignal(iface, member string, options ...dbus.MatchOption) *dbus.Call {
	return nil
}

func (f fakeBusObject) GetProperty(prop string) (dbus.Variant, error) {
	return dbus.Variant{}, nil
}

func (f fakeBusObject) SetProperty(p string, v interface{}) error {
	return nil
}

func (f fakeBusObject) Destination() string {
	return ""
}

func (f fakeBusObject) Path() dbus.ObjectPath {
	return ""
}
