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
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/godbus/dbus"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/desktop/portal"
	"github.com/snapcore/snapd/osutil"
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
	calls              []string
}

var _ = Suite(&documentPortalSuite{})

func (s *documentPortalSuite) SetUpSuite(c *C) {
	s.DBusTest.SetUpSuite(c)

	s.portal = &fakeDocumentPortal{s}
	err := s.SessionBus.Export(s.portal, portal.DocumentPortalObjectPath, portal.DocumentPortalIface)
	c.Assert(err, IsNil)

	_, err = s.SessionBus.RequestName(portal.DocumentPortalBusName, dbus.NameFlagAllowReplacement|dbus.NameFlagReplaceExisting)
	c.Assert(err, IsNil)

	s.BaseTest.AddCleanup(portal.MockOsutilIsMounted(func(path string) (bool, error) {
		return false, nil
	}))

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
	s.getMountPointError = nil
	s.mountPointResponse = ""
	s.calls = nil
}

func (s *documentPortalSuite) TestActivateWithUserError(c *C) {
	userError := errors.New("some user error")
	restore := portal.MockUserCurrent(func() (*user.User, error) {
		return nil, userError
	})
	defer restore()

	document := &portal.Document{}
	err := document.Activate()
	c.Check(err, ErrorMatches, ".*some user error")
}

func (s *documentPortalSuite) TestActivateWhenMounted(c *C) {
	var queriedPath string
	restore := portal.MockOsutilIsMounted(func(path string) (bool, error) {
		queriedPath = path
		return true, nil
	})
	defer restore()

	document := &portal.Document{}
	err := document.Activate()
	c.Check(err, IsNil)
	c.Check(queriedPath, Equals, filepath.Join(s.userRuntimePath, "doc"))
}

func (s *documentPortalSuite) TestActivateWithoutDBus(c *C) {
	os.Setenv("DBUS_SESSION_BUS_ADDRESS", "")

	document := &portal.Document{}
	err := document.Activate()
	c.Check(err, IsNil)
}

func (s *documentPortalSuite) TestActivateWithUnavailableSentinel(c *C) {
	sentinelFile := filepath.Join(s.userRuntimePath, ".portals-unavailable")
	f, err := os.OpenFile(sentinelFile, os.O_RDWR|os.O_CREATE, 0666)
	c.Assert(err, IsNil)
	f.Close()

	document := &portal.Document{}
	err = document.Activate()
	c.Check(err, IsNil)
}

func (s *documentPortalSuite) TestActivateResponseError(c *C) {
	s.getMountPointError = dbus.MakeFailedError(errors.New("something went wrong"))

	document := &portal.Document{}
	err := document.Activate()
	c.Check(err, FitsTypeOf, dbus.Error{})
	c.Check(err, ErrorMatches, `something went wrong`)
	c.Check(s.calls, DeepEquals, []string{
		"GetMountPoint",
	})
}

func (s *documentPortalSuite) TestActivateNotAvailable(c *C) {
	s.getMountPointError = &dbus.Error{
		Name: "org.freedesktop.DBus.Error.ServiceUnknown",
		Body: []interface{}{"not running"},
	}

	document := &portal.Document{}
	err := document.Activate()
	c.Check(err, IsNil)
	c.Check(s.calls, DeepEquals, []string{
		"GetMountPoint",
	})

	// Check that the sentinel file has been created
	sentinelFile := filepath.Join(s.userRuntimePath, ".portals-unavailable")
	c.Check(osutil.FileExists(sentinelFile), Equals, true)
}

func (s *documentPortalSuite) TestActivateWithWrongPath(c *C) {
	s.mountPointResponse = "/some/other/path"
	document := &portal.Document{}
	err := document.Activate()
	expectedError := fmt.Sprintf("Expected portal at .*, got %q", s.mountPointResponse)
	c.Check(err, ErrorMatches, expectedError)
	c.Check(s.calls, DeepEquals, []string{
		"GetMountPoint",
	})
}

func (s *documentPortalSuite) TestActivateAvailable(c *C) {
	s.mountPointResponse = filepath.Join(s.userRuntimePath, "doc")

	document := &portal.Document{}
	err := document.Activate()
	c.Check(err, IsNil)
	c.Check(s.calls, DeepEquals, []string{
		"GetMountPoint",
	})

	// Check that the sentinel file has not been created
	sentinelFile := filepath.Join(s.userRuntimePath, ".portals-unavailable")
	c.Check(osutil.FileExists(sentinelFile), Equals, false)
}

type fakeDocumentPortal struct {
	*documentPortalSuite
}

func (p *fakeDocumentPortal) GetMountPoint() ([]byte, *dbus.Error) {
	p.calls = append(p.calls, "GetMountPoint")

	return []byte(p.mountPointResponse), p.getMountPointError
}
