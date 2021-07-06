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
	"testing"

	"github.com/godbus/dbus"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/usersession/xdgopenproxy"
)

func Test(t *testing.T) { TestingT(t) }

type xdgOpenSuite struct{}

type launcherModifiers = xdgopenproxy.LauncherModifiers

var _ = Suite(&xdgOpenSuite{})

func (s *xdgOpenSuite) TestOpenURL(c *C) {
	launcher := &fakeLauncher{}
	var options launcherModifiers
	c.Check(xdgopenproxy.LaunchWithOne(nil, launcher, "http://example.org", options), IsNil)
	c.Check(launcher.calls, DeepEquals, []string{
		"OpenURI http://example.org",
	})
}

func (s *xdgOpenSuite) TestOpenFile(c *C) {
	launcher := &fakeLauncher{}
	var options launcherModifiers
	c.Check(xdgopenproxy.LaunchWithOne(nil, launcher, "/path/test.txt", options), IsNil)
	c.Check(launcher.calls, DeepEquals, []string{
		"OpenFile /path/test.txt",
	})
}

func (s *xdgOpenSuite) TestOpenFileURL(c *C) {
	launcher := &fakeLauncher{}
	var options launcherModifiers
	c.Check(xdgopenproxy.LaunchWithOne(nil, launcher, "file:///path/test.txt", options), IsNil)
	c.Check(launcher.calls, DeepEquals, []string{
		"OpenFile /path/test.txt",
	})
}

func (s *xdgOpenSuite) TestStopOnFirstSuccess(c *C) {
	l1 := &fakeLauncher{err: fmt.Errorf("failure one")}
	l2 := &fakeLauncher{err: nil}
	l3 := &fakeLauncher{err: nil}
	launchers := []xdgopenproxy.DesktopLauncher{l1, l2, l3}
	var options launcherModifiers

	err := xdgopenproxy.Launch(nil, launchers, "http://example.org", options)
	c.Check(err, IsNil)
	c.Check(l1.calls, DeepEquals, []string{
		"OpenURI http://example.org",
	})
	c.Check(l2.calls, DeepEquals, []string{
		"OpenURI http://example.org",
	})
	c.Check(l3.calls, HasLen, 0)
}

func (s *xdgOpenSuite) TestStopOnResponseError(c *C) {
	l1 := &fakeLauncher{err: fmt.Errorf("failure one")}
	l2 := &fakeLauncher{err: xdgopenproxy.MakeResponseError("hello")}
	l3 := &fakeLauncher{err: nil}
	launchers := []xdgopenproxy.DesktopLauncher{l1, l2, l3}
	var options launcherModifiers

	err := xdgopenproxy.Launch(nil, launchers, "http://example.org", options)
	c.Check(err, Equals, l2.err)
	c.Check(l3.calls, HasLen, 0)
}

type fakeLauncher struct {
	err   error
	calls []string
}

func (l *fakeLauncher) OpenFile(bus *dbus.Conn, path string, options launcherModifiers) error {
	l.calls = append(l.calls, "OpenFile "+path)
	return l.err
}

func (l *fakeLauncher) OpenURI(bus *dbus.Conn, uri string, options launcherModifiers) error {
	l.calls = append(l.calls, "OpenURI "+uri)
	return l.err
}
