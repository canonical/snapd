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

package daemon

import (
	"net/http/httptest"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/polkit"
)

type accessSuite struct{}

var _ = Suite(&accessSuite{})

func (s *accessSuite) TestAllowSnapSocket(c *C) {
	var ac accessChecker = allowSnapSocket{}

	// checker allows requests from snapd-snap.socket
	ucred := &ucrednet{uid: 42, pid: 100, socket: dirs.SnapSocket}
	c.Check(ac.canAccess(nil, ucred, nil), Equals, accessOK)

	// no decision made about other sockets
	ucred.socket = dirs.SnapdSocket
	c.Check(ac.canAccess(nil, ucred, nil), Equals, accessUnknown)
}

func (s *accessSuite) TestDenySnapSocket(c *C) {
	var ac accessChecker = denySnapSocket{}

	// checker denies requests from snapd-snap.socket
	ucred := &ucrednet{uid: 42, pid: 100, socket: dirs.SnapSocket}
	c.Check(ac.canAccess(nil, ucred, nil), Equals, accessUnauthorized)

	// no decision made about other sockets
	ucred.socket = dirs.SnapdSocket
	c.Check(ac.canAccess(nil, ucred, nil), Equals, accessUnknown)
}

func (s *accessSuite) TestAllowByGuest(c *C) {
	var ac accessChecker = allowGetByGuest{}

	req := httptest.NewRequest("GET", "/", nil)
	ucred := &ucrednet{uid: 42, pid: 100}
	// checker allows GET requests with or without ucred info
	c.Check(ac.canAccess(req, nil, nil), Equals, accessOK)
	c.Check(ac.canAccess(req, ucred, nil), Equals, accessOK)

	// no decision made about other HTTP methods
	req = httptest.NewRequest("POST", "/", nil)
	c.Check(ac.canAccess(req, nil, nil), Equals, accessUnknown)
	c.Check(ac.canAccess(req, ucred, nil), Equals, accessUnknown)
}

func (s *accessSuite) TestAllowByUser(c *C) {
	var ac accessChecker = allowGetByUser{}

	req := httptest.NewRequest("GET", "/", nil)
	ucred := &ucrednet{uid: 42, pid: 100}
	// checker allows GET requests from requests with uid credentials
	c.Check(ac.canAccess(req, ucred, nil), Equals, accessOK)

	// no decision made if ucred is missing
	c.Check(ac.canAccess(req, nil, nil), Equals, accessUnknown)

	// or for other HTTP methods
	req = httptest.NewRequest("POST", "/", nil)
	c.Check(ac.canAccess(req, ucred, nil), Equals, accessUnknown)
}

func (s *accessSuite) TestAllowSnapUser(c *C) {
	var ac accessChecker = allowSnapUser{}

	// checker allow requests that provide macaroon user auth
	user := &auth.UserState{}
	c.Check(ac.canAccess(nil, nil, user), Equals, accessOK)

	// no decision made for unauthenticated requests
	c.Check(ac.canAccess(nil, nil, nil), Equals, accessUnknown)
}

func (s *accessSuite) TestAllowRoot(c *C) {
	var ac accessChecker = allowRoot{}

	// checker allows requests from uid=0
	ucred := &ucrednet{uid: 0, pid: 1000}
	c.Check(ac.canAccess(nil, ucred, nil), Equals, accessOK)

	// no decision made for other user IDs or no ucred
	ucred.uid = 42
	c.Check(ac.canAccess(nil, ucred, nil), Equals, accessUnknown)
	c.Check(ac.canAccess(nil, nil, nil), Equals, accessUnknown)
}

func (s *accessSuite) TestPolkitCheck(c *C) {
	defer func() {
		polkitCheckAuthorization = polkit.CheckAuthorization
	}()

	var ac accessChecker = polkitCheck{"action-id"}

	// checker grants access if polkitd says it is okay
	req := httptest.NewRequest("GET", "/", nil)
	ucred := &ucrednet{uid: 42, pid: 1000}
	polkitCheckAuthorization = func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error) {
		c.Check(pid, Equals, int32(1000))
		c.Check(uid, Equals, uint32(42))
		c.Check(actionId, Equals, "action-id")
		c.Check(details, IsNil)
		c.Check(flags, Equals, polkit.CheckFlags(0))
		return true, nil
	}
	c.Check(ac.canAccess(req, ucred, nil), Equals, accessOK)

	// no decision made if polkitd does not authorise the request
	polkitCheckAuthorization = func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error) {
		return false, nil
	}
	c.Check(ac.canAccess(req, ucred, nil), Equals, accessUnknown)

	// mark request as cancelled if the user dismisses the auth check
	polkitCheckAuthorization = func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error) {
		return false, polkit.ErrDismissed
	}
	c.Check(ac.canAccess(req, ucred, nil), Equals, accessCancelled)

	// The X-Allow-Interaction header can be set to tell polkitd
	// that interaction with the user is allowed.
	req.Header.Set(client.AllowInteractionHeader, "true")
	polkitCheckAuthorization = func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error) {
		c.Check(flags, Equals, polkit.CheckFlags(polkit.CheckAllowInteraction))
		return true, nil
	}

	// if ucred is missing, polkitd is not consulted and no decision made
	polkitCheckAuthorization = func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error) {
		c.Fail()
		return true, nil
	}
	c.Check(ac.canAccess(req, nil, nil), Equals, accessUnknown)
}
