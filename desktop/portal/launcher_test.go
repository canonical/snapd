// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020-2021 Canonical Ltd
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

package portal_test

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/godbus/dbus"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/desktop/portal"
	"github.com/snapcore/snapd/testutil"
)

type portalSuite struct {
	testutil.DBusTest

	portal  *fakePortal
	request *fakePortalRequest

	openError    *dbus.Error
	sendResponse bool
	openResponse uint32
	calls        []string
}

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&portalSuite{})

const portalRequestPath = "/org/freedesktop/portal/desktop/request/1"

func (s *portalSuite) SetUpSuite(c *C) {
	s.DBusTest.SetUpSuite(c)

	s.portal = &fakePortal{s}
	err := s.SessionBus.Export(s.portal, portal.DesktopPortalObjectPath, portal.DesktopPortalOpenURIIface)
	c.Assert(err, IsNil)
	s.request = &fakePortalRequest{s}
	err = s.SessionBus.Export(s.request, portalRequestPath, portal.DesktopPortalRequestIface)
	c.Assert(err, IsNil)

	_, err = s.SessionBus.RequestName(portal.DesktopPortalBusName, dbus.NameFlagAllowReplacement|dbus.NameFlagReplaceExisting)
	c.Assert(err, IsNil)
}

func (s *portalSuite) TearDownSuite(c *C) {
	if s.SessionBus != nil {
		_, err := s.SessionBus.ReleaseName(portal.DesktopPortalBusName)
		c.Check(err, IsNil)
	}

	s.DBusTest.TearDownSuite(c)
}

func (s *portalSuite) SetUpTest(c *C) {
	s.DBusTest.SetUpTest(c)

	s.openError = nil
	s.openResponse = 0
	s.sendResponse = true
	s.calls = nil
}

func (s *portalSuite) TestOpenFile(c *C) {
	path := filepath.Join(c.MkDir(), "test.txt")
	c.Assert(os.WriteFile(path, []byte("hello world"), 0644), IsNil)

	err := portal.OpenFile(s.SessionBus, path)
	c.Check(err, IsNil)
	c.Check(s.calls, DeepEquals, []string{
		"OpenFile",
	})
}

func (s *portalSuite) TestOpenFileCallError(c *C) {
	s.openError = dbus.MakeFailedError(fmt.Errorf("failure"))

	path := filepath.Join(c.MkDir(), "test.txt")
	c.Assert(os.WriteFile(path, []byte("hello world"), 0644), IsNil)

	err := portal.OpenFile(s.SessionBus, path)
	c.Check(err, FitsTypeOf, dbus.Error{})
	c.Check(err, ErrorMatches, "failure")
	c.Check(s.calls, DeepEquals, []string{
		"OpenFile",
	})
}

func (s *portalSuite) TestOpenFileResponseError(c *C) {
	s.openResponse = 2

	path := filepath.Join(c.MkDir(), "test.txt")
	c.Assert(os.WriteFile(path, []byte("hello world"), 0644), IsNil)

	err := portal.OpenFile(s.SessionBus, path)
	c.Check(err, FitsTypeOf, (*portal.ResponseError)(nil))
	c.Check(err, ErrorMatches, `request declined by the user \(code 2\)`)
	c.Check(s.calls, DeepEquals, []string{
		"OpenFile",
	})
}

func (s *portalSuite) TestOpenFileTimeout(c *C) {
	s.sendResponse = false
	restore := portal.MockPortalTimeout(5 * time.Millisecond)
	defer restore()

	file := filepath.Join(c.MkDir(), "test.txt")
	c.Assert(os.WriteFile(file, []byte("hello world"), 0644), IsNil)

	err := portal.OpenFile(s.SessionBus, file)
	c.Check(err, FitsTypeOf, (*portal.ResponseError)(nil))
	c.Check(err, ErrorMatches, "timeout waiting for user response")
	c.Check(s.calls, DeepEquals, []string{
		"OpenFile",
		"Request.Close",
	})
}

func (s *portalSuite) TestOpenDir(c *C) {
	dir := c.MkDir()
	err := portal.OpenFile(s.SessionBus, dir)
	c.Check(err, IsNil)
	c.Check(s.calls, DeepEquals, []string{
		"OpenFile",
	})
}

func (s *portalSuite) TestOpenMissingFile(c *C) {
	path := filepath.Join(c.MkDir(), "no-such-file.txt")
	err := portal.OpenFile(s.SessionBus, path)
	c.Check(err, ErrorMatches, "no such file or directory")
	c.Check(s.calls, HasLen, 0)
}

func (s *portalSuite) TestOpenUnreadableFile(c *C) {
	path := filepath.Join(c.MkDir(), "test.txt")
	c.Assert(os.WriteFile(path, []byte("hello world"), 0644), IsNil)
	c.Assert(os.Chmod(path, 0), IsNil)

	err := portal.OpenFile(s.SessionBus, path)
	c.Check(err, ErrorMatches, "permission denied")
	c.Check(s.calls, HasLen, 0)
}

func (s *portalSuite) TestOpenURI(c *C) {
	err := portal.OpenURI(s.SessionBus, "http://example.com")
	c.Check(err, IsNil)
	c.Check(s.calls, DeepEquals, []string{
		"OpenURI http://example.com",
	})
}

func (s *portalSuite) TestOpenURICallError(c *C) {
	s.openError = dbus.MakeFailedError(fmt.Errorf("failure"))

	err := portal.OpenURI(s.SessionBus, "http://example.com")
	c.Check(err, FitsTypeOf, dbus.Error{})
	c.Check(err, ErrorMatches, "failure")
	c.Check(s.calls, DeepEquals, []string{
		"OpenURI http://example.com",
	})
}

func (s *portalSuite) TestOpenURIResponseError(c *C) {
	s.openResponse = 2

	err := portal.OpenURI(s.SessionBus, "http://example.com")
	c.Check(err, FitsTypeOf, (*portal.ResponseError)(nil))
	c.Check(err, ErrorMatches, `request declined by the user \(code 2\)`)
	c.Check(s.calls, DeepEquals, []string{
		"OpenURI http://example.com",
	})
}

func (s *portalSuite) TestOpenURITimeout(c *C) {
	s.sendResponse = false
	restore := portal.MockPortalTimeout(5 * time.Millisecond)
	defer restore()

	err := portal.OpenURI(s.SessionBus, "http://example.com")
	c.Check(err, FitsTypeOf, (*portal.ResponseError)(nil))
	c.Check(err, ErrorMatches, "timeout waiting for user response")
	c.Check(s.calls, DeepEquals, []string{
		"OpenURI http://example.com",
		"Request.Close",
	})
}

type fakePortal struct {
	*portalSuite
}

func (p *fakePortal) OpenFile(parent string, clientFD dbus.UnixFD, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	p.calls = append(p.calls, "OpenFile")

	fd := int(clientFD)
	defer syscall.Close(fd)

	if p.sendResponse {
		var results map[string]dbus.Variant
		p.SessionBus.Emit(portalRequestPath, portal.DesktopPortalRequestIface+".Response", p.openResponse, results)
	}
	return portalRequestPath, p.openError
}

func (p *fakePortal) OpenURI(parent, uri string, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	p.calls = append(p.calls, "OpenURI "+uri)
	if p.sendResponse {
		var results map[string]dbus.Variant
		p.SessionBus.Emit(portalRequestPath, portal.DesktopPortalRequestIface+".Response", p.openResponse, results)
	}
	return portalRequestPath, p.openError
}

type fakePortalRequest struct {
	*portalSuite
}

func (r *fakePortalRequest) Close() *dbus.Error {
	r.calls = append(r.calls, "Request.Close")
	return nil
}
