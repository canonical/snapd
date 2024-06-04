// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"sync"

	"github.com/godbus/dbus"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/desktop/portal"
	"github.com/snapcore/snapd/testutil"
)

const (
	fakeUserId = "1234"
)

type documentPortalSuite struct {
	testutil.BaseTest
	testutil.DBusTest

	portal *fakeDocumentPortal

	userRuntimePath string

	getMountPointError *dbus.Error
	mountPointResponse string

	m     sync.Mutex
	calls []string
}

var _ = Suite(&documentPortalSuite{})

func (s *documentPortalSuite) SetUpSuite(c *C) {
	s.DBusTest.SetUpSuite(c)

	s.portal = &fakeDocumentPortal{s}
	err := s.SessionBus.Export(s.portal, portal.DocumentPortalObjectPath, portal.DocumentPortalIface)
	c.Assert(err, IsNil)

	_, err = s.SessionBus.RequestName(portal.DocumentPortalBusName, dbus.NameFlagAllowReplacement|dbus.NameFlagReplaceExisting)
	c.Assert(err, IsNil)

	s.BaseTest.AddCleanup(portal.MockUserCurrent(func() (*user.User, error) {
		return &user.User{Uid: fakeUserId}, nil
	}))

	portal.MockXdgRuntimeDir("/tmp/snap-doc-portal-test")
	s.BaseTest.AddCleanup(func() {
		os.RemoveAll("/tmp/snap-doc-portal-test")
	})
	s.userRuntimePath = filepath.Join("/tmp/snap-doc-portal-test", fakeUserId)
}

func (s *documentPortalSuite) TearDownSuite(c *C) {
	if s.SessionBus != nil {
		_, err := s.SessionBus.ReleaseName(portal.DocumentPortalBusName)
		c.Check(err, IsNil)
	}

	s.DBusTest.TearDownSuite(c)
}

func (s *documentPortalSuite) SetUpTest(c *C) {
	s.DBusTest.SetUpTest(c)

	os.RemoveAll(s.userRuntimePath)
	err := os.MkdirAll(s.userRuntimePath, 0777)
	c.Assert(err, IsNil)
	s.withLocked(func() {
		s.getMountPointError = nil
		s.mountPointResponse = ""
		s.calls = nil
	})
}

func (s *documentPortalSuite) withLocked(f func()) {
	s.m.Lock()
	defer s.m.Unlock()

	f()
}

func (s *documentPortalSuite) TestGetDefaultMountPointWithUserError(c *C) {
	userError := errors.New("some user error")
	restore := portal.MockUserCurrent(func() (*user.User, error) {
		return nil, userError
	})
	defer restore()

	document := &portal.Document{}
	mountPoint, err := document.GetDefaultMountPoint()
	c.Check(err, ErrorMatches, ".*some user error")
	c.Check(mountPoint, Equals, "")
}

func (s *documentPortalSuite) TestGetDefaultMountPointHappy(c *C) {
	document := &portal.Document{}
	mountPoint, err := document.GetDefaultMountPoint()
	c.Check(err, IsNil)
	expectedMountPoint := filepath.Join(s.userRuntimePath, "doc")
	c.Check(mountPoint, Equals, expectedMountPoint)
}

func (s *documentPortalSuite) TestGetMountPointResponseError(c *C) {
	s.withLocked(func() {
		s.getMountPointError = dbus.MakeFailedError(errors.New("something went wrong"))
	})

	document := &portal.Document{}
	mountPoint, err := document.GetMountPoint()
	c.Check(err, FitsTypeOf, dbus.Error{})
	c.Check(err, ErrorMatches, `something went wrong`)
	c.Check(mountPoint, Equals, "")
	s.withLocked(func() {
		c.Check(s.calls, DeepEquals, []string{
			"GetMountPoint",
		})
	})
}

func (s *documentPortalSuite) TestGetMountPointHappy(c *C) {
	s.withLocked(func() {
		s.mountPointResponse = filepath.Join(s.userRuntimePath, "doc")
	})

	document := &portal.Document{}
	mountPoint, err := document.GetMountPoint()
	c.Check(err, IsNil)
	c.Check(mountPoint, Equals, s.mountPointResponse)
	s.withLocked(func() {
		c.Check(s.calls, DeepEquals, []string{
			"GetMountPoint",
		})
	})
}

type fakeDocumentPortal struct {
	*documentPortalSuite
}

func (p *fakeDocumentPortal) GetMountPoint() ([]byte, *dbus.Error) {
	p.m.Lock()
	defer p.m.Unlock()
	p.calls = append(p.calls, "GetMountPoint")

	return []byte(p.mountPointResponse), p.getMountPointError
}
