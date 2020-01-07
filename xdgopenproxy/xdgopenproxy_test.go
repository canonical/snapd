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
	"time"

	"github.com/godbus/dbus"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/xdgopenproxy"
)

func Test(t *testing.T) { TestingT(t) }

type xdgSnapcraftLauncherOpenSuite struct{}

var _ = Suite(&xdgSnapcraftLauncherOpenSuite{})

type busObj struct{ name, path string }

// type fakeBus func(name string, objectPath dbus.ObjectPath) dbus.BusObject
type fakeBus struct {
	c       *C
	mocks   map[busObj]fakeBusObject
	signals chan chan<- *dbus.Signal
}

func (b fakeBus) Object(name string, objectPath dbus.ObjectPath) dbus.BusObject {
	fbo, ok := b.mocks[busObj{name, string(objectPath)}]
	b.c.Assert(ok, Equals, true, Commentf("unexpected bus object: %v %v", name, objectPath))
	return fbo.atPath(string(objectPath))
}

func (b fakeBus) Close() error { return nil }

func (b fakeBus) Signal(ch chan<- *dbus.Signal)                      { b.signals <- ch }
func (b fakeBus) RemoveSignal(ch chan<- *dbus.Signal)                {}
func (b fakeBus) AddMatchSignal(option ...dbus.MatchOption) error    { return nil }
func (b fakeBus) RemoveMatchSignal(option ...dbus.MatchOption) error { return nil }

func noSuchObjectFake() fakeBusObject {
	return newFakeBusObject(func(method string, args ...interface{}) ([]interface{}, error) {
		return nil, &dbus.ErrMsgNoObject
	})
}

func setupFakeBus(c *C, objs map[busObj]fakeBusObject) (fake *fakeBus, restore func()) {
	f := fakeBus{
		c:       c,
		mocks:   objs,
		signals: make(chan chan<- *dbus.Signal, 1),
	}
	restore = xdgopenproxy.MockSessionBus(func() (xdgopenproxy.Bus, error) {
		return &f, nil
	})
	return &f, restore

}

func setupBusWithSnapcraftLauncher(c *C, obj fakeBusObject) (fake *fakeBus, restore func()) {
	return setupFakeBus(c, map[busObj]fakeBusObject{
		{"io.snapcraft.Launcher", "/io/snapcraft/Launcher"}:                   obj,
		{"org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop"}: noSuchObjectFake(),
	})
}

func (s *xdgSnapcraftLauncherOpenSuite) TestOpenURL(c *C) {
	launcher := newFakeBusObject(func(method string, args ...interface{}) ([]interface{}, error) {
		c.Check(method, Equals, "io.snapcraft.Launcher.OpenURL")
		c.Check(args, DeepEquals, []interface{}{"http://example.org"})
		return nil, nil
	})
	_, restore := setupBusWithSnapcraftLauncher(c, launcher)
	defer restore()

	c.Check(xdgopenproxy.Run("http://example.org"), IsNil)
}

func (s *xdgSnapcraftLauncherOpenSuite) TestOpenFile(c *C) {
	path := filepath.Join(c.MkDir(), "test.txt")
	c.Assert(ioutil.WriteFile(path, []byte("Hello world"), 0644), IsNil)

	called := false
	launcher := newFakeBusObject(func(method string, args ...interface{}) ([]interface{}, error) {
		c.Check(method, Equals, "io.snapcraft.Launcher.OpenFile")
		c.Assert(args, HasLen, 2)
		c.Check(args[0], Equals, "")
		c.Check(fdMatchesFile(int(args[1].(dbus.UnixFD)), path), IsNil)
		called = true
		return nil, nil
	})
	_, restore := setupBusWithSnapcraftLauncher(c, launcher)
	defer restore()

	c.Check(xdgopenproxy.Run(path), IsNil)
	c.Check(called, Equals, true)
}

func (s *xdgSnapcraftLauncherOpenSuite) TestOpenFileURL(c *C) {
	path := filepath.Join(c.MkDir(), "test.txt")
	c.Assert(ioutil.WriteFile(path, []byte("Hello world"), 0644), IsNil)

	called := false
	launcher := newFakeBusObject(func(method string, args ...interface{}) ([]interface{}, error) {
		c.Check(method, Equals, "io.snapcraft.Launcher.OpenFile")
		c.Assert(args, HasLen, 2)
		c.Check(args[0], Equals, "")
		c.Check(fdMatchesFile(int(args[1].(dbus.UnixFD)), path), IsNil)
		called = true
		return nil, nil
	})

	_, restore := setupBusWithSnapcraftLauncher(c, launcher)
	defer restore()

	u := url.URL{Scheme: "file", Path: path}
	c.Check(xdgopenproxy.Run(u.String()), IsNil)
	c.Check(called, Equals, true)
}

func (s *xdgSnapcraftLauncherOpenSuite) TestOpenDir(c *C) {
	dir := c.MkDir()

	called := false
	launcher := newFakeBusObject(func(method string, args ...interface{}) ([]interface{}, error) {
		c.Check(method, Equals, "io.snapcraft.Launcher.OpenFile")
		c.Assert(args, HasLen, 2)
		c.Check(args[0], Equals, "")
		c.Check(fdMatchesFile(int(args[1].(dbus.UnixFD)), dir), IsNil)
		called = true
		return nil, nil
	})
	_, restore := setupBusWithSnapcraftLauncher(c, launcher)
	defer restore()

	c.Check(xdgopenproxy.Run(dir), IsNil)
	c.Check(called, Equals, true)
}

func (s *xdgSnapcraftLauncherOpenSuite) TestOpenMissingFile(c *C) {
	path := filepath.Join(c.MkDir(), "no-such-file.txt")

	launcher := newFakeBusObject(func(method string, args ...interface{}) ([]interface{}, error) {
		c.Error("unexpected D-Bus call")
		return nil, nil
	})
	_, restore := setupBusWithSnapcraftLauncher(c, launcher)
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

	launcher := newFakeBusObject(func(method string, args ...interface{}) ([]interface{}, error) {
		c.Error("unexpected D-Bus call")
		return nil, nil
	})
	_, restore := setupBusWithSnapcraftLauncher(c, launcher)
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

func setupBusWithFreedesktopPortal(c *C, obj, req fakeBusObject) (fake *fakeBus, restore func()) {
	return setupFakeBus(c, map[busObj]fakeBusObject{
		{"io.snapcraft.Launcher", "/io/snapcraft/Launcher"}:                   noSuchObjectFake(),
		{"org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop"}: obj,
		// predefined request object
		{"org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop/request/1"}: req,
	})
}

func (s *xdgFreedesktopPortalOpenSuite) TestOpenURL(c *C) {
	launcherCalled := false
	// signals that launcher OpenURI call is done and the signal can be sent
	launcherCallDone := make(chan bool, 1)
	launcher := newFakeBusObject(func(method string, args ...interface{}) ([]interface{}, error) {
		defer func() { launcherCallDone <- true }()

		c.Check(method, Equals, "org.freedesktop.portal.OpenURI.OpenURI")
		c.Assert(args, HasLen, 3)
		c.Check(args[1], Equals, "http://example.org")

		launcherCalled = true
		// use predefined request object path
		return []interface{}{dbus.ObjectPath("/org/freedesktop/portal/desktop/request/1")}, nil
	})
	request := newFakeBusObject(func(method string, args ...interface{}) ([]interface{}, error) {
		c.Fatalf("unexpected call")
		return nil, nil
	})
	fb, restore := setupBusWithFreedesktopPortal(c, launcher, request)
	defer restore()

	go func() {
		<-launcherCallDone
		// launcher was called, mock a signal from the request object
		sigChannel := <-fb.signals
		sigChannel <- &dbus.Signal{
			Path: dbus.ObjectPath("/org/freedesktop/portal/desktop/request/1"),
			Body: []interface{}{0, map[string]interface{}{}},
		}
	}()

	c.Check(xdgopenproxy.Run("http://example.org"), IsNil)
	c.Check(launcherCalled, Equals, true)
}

func (s *xdgFreedesktopPortalOpenSuite) TestOpenFile(c *C) {
	path := filepath.Join(c.MkDir(), "test.txt")
	c.Assert(ioutil.WriteFile(path, []byte("Hello world"), 0644), IsNil)

	launcherCalled := false
	// signals that launcher OpenURI call is done and the signal can be sent
	launcherCallDone := make(chan bool, 1)

	launcher := newFakeBusObject(func(method string, args ...interface{}) ([]interface{}, error) {
		defer func() { launcherCallDone <- true }()

		c.Check(method, Equals, "org.freedesktop.portal.OpenURI.OpenFile")
		c.Assert(args, HasLen, 3)
		c.Check(args[0], Equals, "")
		c.Check(fdMatchesFile(int(args[1].(dbus.UnixFD)), path), IsNil)

		launcherCalled = true
		// use predefined request object path
		return []interface{}{dbus.ObjectPath("/org/freedesktop/portal/desktop/request/1")}, nil
	})
	request := newFakeBusObject(func(method string, args ...interface{}) ([]interface{}, error) {
		c.Fatalf("unexpected call")
		return nil, nil
	})

	fb, restore := setupBusWithFreedesktopPortal(c, launcher, request)
	defer restore()

	go func() {
		<-launcherCallDone
		// launcher was called, mock a signal from the request object
		sigChannel := <-fb.signals
		sigChannel <- &dbus.Signal{
			Path: dbus.ObjectPath("/org/freedesktop/portal/desktop/request/1"),
			Body: []interface{}{0, map[string]interface{}{}},
		}
	}()

	err := xdgopenproxy.Run(path)
	c.Check(err, IsNil)
	c.Check(launcherCalled, Equals, true)
}

func (s *xdgFreedesktopPortalOpenSuite) TestOpenTimeout(c *C) {
	restore := xdgopenproxy.MockPortalTimeout(5 * time.Millisecond)
	defer restore()

	called := false
	launcher := newFakeBusObject(func(method string, args ...interface{}) ([]interface{}, error) {
		c.Check(method, Equals, "org.freedesktop.portal.OpenURI.OpenURI")
		c.Assert(args, HasLen, 3)
		c.Check(args[1], Equals, "http://example.org")
		called = true
		return []interface{}{dbus.ObjectPath("/org/freedesktop/portal/desktop/request/1")}, nil
	})
	closeCalled := false
	request := newFakeBusObject(func(method string, args ...interface{}) ([]interface{}, error) {
		c.Assert(method, Equals, "org.freedesktop.portal.Request.Close")
		closeCalled = true
		return nil, nil
	})

	_, restore = setupBusWithFreedesktopPortal(c, launcher, request)
	defer restore()

	c.Check(xdgopenproxy.Run("http://example.org"), ErrorMatches, "timeout waiting for user response")
	c.Check(called, Equals, true)
	c.Check(closeCalled, Equals, true)
}

// fakeBusObject is a dbus.BusObject implementation that forwards
// Call invocations
type fakeBusObject struct {
	path dbus.ObjectPath
	call func(method string, args ...interface{}) ([]interface{}, error)
}

func newFakeBusObject(call func(method string, args ...interface{}) ([]interface{}, error)) fakeBusObject {
	return fakeBusObject{call: call}
}

func (f *fakeBusObject) atPath(path string) *fakeBusObject {
	return &fakeBusObject{
		call: f.call,
		path: dbus.ObjectPath(path),
	}
}

func (f *fakeBusObject) Call(method string, flags dbus.Flags, args ...interface{}) *dbus.Call {
	body, err := f.call(method, args...)
	return &dbus.Call{Err: err, Body: body}
}

func (f *fakeBusObject) CallWithContext(ctx context.Context, method string, flags dbus.Flags, args ...interface{}) *dbus.Call {
	body, err := f.call(method, args...)
	return &dbus.Call{Err: err, Body: body}
}

func (f *fakeBusObject) Go(method string, flags dbus.Flags, ch chan *dbus.Call, args ...interface{}) *dbus.Call {
	return nil
}

func (f *fakeBusObject) GoWithContext(ctx context.Context, method string, flags dbus.Flags, ch chan *dbus.Call, args ...interface{}) *dbus.Call {
	return nil
}

func (f *fakeBusObject) AddMatchSignal(iface, member string, options ...dbus.MatchOption) *dbus.Call {
	return nil
}

func (f *fakeBusObject) RemoveMatchSignal(iface, member string, options ...dbus.MatchOption) *dbus.Call {
	return nil
}

func (f *fakeBusObject) GetProperty(prop string) (dbus.Variant, error) {
	return dbus.Variant{}, nil
}

func (f *fakeBusObject) SetProperty(p string, v interface{}) error {
	return nil
}

func (f *fakeBusObject) Destination() string {
	return ""
}

func (f *fakeBusObject) Path() dbus.ObjectPath {
	return f.path
}
