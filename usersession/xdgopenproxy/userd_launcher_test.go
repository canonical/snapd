// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"os"
	"path/filepath"
	"syscall"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/xdgopenproxy"
)

type userdSuite struct {
	testutil.DBusTest

	userd *fakeUserd

	openError *dbus.Error
	calls     []string
}

var _ = Suite(&userdSuite{})

func (s *userdSuite) SetUpSuite(c *C) {
	s.DBusTest.SetUpSuite(c)

	s.userd = &fakeUserd{s}
	mylog.Check(s.SessionBus.Export(s.userd, xdgopenproxy.UserdLauncherObjectPath, xdgopenproxy.UserdLauncherIface))


	_ = mylog.Check2(s.SessionBus.RequestName(xdgopenproxy.UserdLauncherBusName, dbus.NameFlagAllowReplacement|dbus.NameFlagReplaceExisting))

}

func (s *userdSuite) TearDownSuite(c *C) {
	if s.SessionBus != nil {
		_ := mylog.Check2(s.SessionBus.ReleaseName(xdgopenproxy.UserdLauncherBusName))
		c.Check(err, IsNil)
	}

	s.DBusTest.TearDownSuite(c)
}

func (s *userdSuite) SetUpTest(c *C) {
	s.DBusTest.SetUpTest(c)

	s.openError = nil
	s.calls = nil
}

func (s *userdSuite) TestOpenFile(c *C) {
	launcher := &xdgopenproxy.UserdLauncher{}

	path := filepath.Join(c.MkDir(), "test.txt")
	c.Assert(os.WriteFile(path, []byte("hello world"), 0644), IsNil)
	mylog.Check(launcher.OpenFile(s.SessionBus, path))
	c.Check(err, IsNil)
	c.Check(s.calls, DeepEquals, []string{
		"OpenFile",
	})
}

func (s *userdSuite) TestOpenFileError(c *C) {
	s.openError = dbus.MakeFailedError(fmt.Errorf("failure"))

	launcher := &xdgopenproxy.UserdLauncher{}

	path := filepath.Join(c.MkDir(), "test.txt")
	c.Assert(os.WriteFile(path, []byte("hello world"), 0644), IsNil)
	mylog.Check(launcher.OpenFile(s.SessionBus, path))
	c.Check(err, FitsTypeOf, dbus.Error{})
	c.Check(err, ErrorMatches, "failure")
	c.Check(s.calls, DeepEquals, []string{
		"OpenFile",
	})
}

func (s *userdSuite) TestOpenDir(c *C) {
	launcher := &xdgopenproxy.UserdLauncher{}

	dir := c.MkDir()
	mylog.Check(launcher.OpenFile(s.SessionBus, dir))
	c.Check(err, IsNil)
	c.Check(s.calls, DeepEquals, []string{
		"OpenFile",
	})
}

func (s *userdSuite) TestOpenMissingFile(c *C) {
	launcher := &xdgopenproxy.UserdLauncher{}

	path := filepath.Join(c.MkDir(), "no-such-file.txt")
	mylog.Check(launcher.OpenFile(s.SessionBus, path))
	c.Check(err, ErrorMatches, "no such file or directory")
	c.Check(s.calls, HasLen, 0)
}

func (s *userdSuite) TestOpenUnreadableFile(c *C) {
	launcher := &xdgopenproxy.UserdLauncher{}

	path := filepath.Join(c.MkDir(), "test.txt")
	c.Assert(os.WriteFile(path, []byte("hello world"), 0644), IsNil)
	c.Assert(os.Chmod(path, 0), IsNil)
	mylog.Check(launcher.OpenFile(s.SessionBus, path))
	c.Check(err, ErrorMatches, "permission denied")
	c.Check(s.calls, HasLen, 0)
}

func (s *userdSuite) TestOpenURI(c *C) {
	launcher := &xdgopenproxy.UserdLauncher{}
	mylog.Check(launcher.OpenURI(s.SessionBus, "http://example.com"))
	c.Check(err, IsNil)
	c.Check(s.calls, DeepEquals, []string{
		"OpenURI http://example.com",
	})
}

func (s *userdSuite) TestOpenURIError(c *C) {
	s.openError = dbus.MakeFailedError(fmt.Errorf("failure"))

	launcher := &xdgopenproxy.UserdLauncher{}
	mylog.Check(launcher.OpenURI(s.SessionBus, "http://example.com"))
	c.Check(err, FitsTypeOf, dbus.Error{})
	c.Check(err, ErrorMatches, "failure")
	c.Check(s.calls, DeepEquals, []string{
		"OpenURI http://example.com",
	})
}

type fakeUserd struct {
	*userdSuite
}

func (p *fakeUserd) OpenFile(parentWindow string, clientFD dbus.UnixFD) *dbus.Error {
	p.calls = append(p.calls, "OpenFile")

	fd := int(clientFD)
	defer syscall.Close(fd)

	return p.openError
}

func (p *fakeUserd) OpenURL(uri string) *dbus.Error {
	p.calls = append(p.calls, "OpenURI "+uri)
	return p.openError
}
