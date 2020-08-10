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
	"bytes"
	"net/http/httptest"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/polkit"
	"github.com/snapcore/snapd/testutil"
)

type accessSuite struct{}

var _ = Suite(&accessSuite{})

func (s *accessSuite) TestOpenAccess(c *C) {
	var ac accessChecker = openAccess{}

	// openAccess denies access from snapd-snap.socket
	ucred := &ucrednet{uid: 42, pid: 100, socket: dirs.SnapSocket}
	c.Check(ac.checkAccess(nil, ucred, nil), Equals, accessForbidden)

	// Access allowed from other sockets
	ucred.socket = dirs.SnapdSocket
	c.Check(ac.checkAccess(nil, ucred, nil), Equals, accessOK)

	// Access forbidden without peer credentials.  This will need
	// to be revisited if the API is ever exposed over TCP.
	c.Check(ac.checkAccess(nil, nil, nil), Equals, accessForbidden)
}

func (s *accessSuite) TestAuthenticatedAccess(c *C) {
	var ac accessChecker = authenticatedAccess{}

	req := httptest.NewRequest("GET", "/", nil)
	user := &auth.UserState{}

	// authenticatedAccess denies access from snapd-snap.socket
	ucred := &ucrednet{uid: 0, pid: 100, socket: dirs.SnapSocket}
	c.Check(ac.checkAccess(req, ucred, nil), Equals, accessForbidden)
	c.Check(ac.checkAccess(req, ucred, user), Equals, accessForbidden)

	// With macaroon auth, a normal user is granted access
	ucred = &ucrednet{uid: 42, pid: 100}
	c.Check(ac.checkAccess(req, ucred, user), Equals, accessOK)

	// Macaroon access is also possible without peer credentials
	c.Check(ac.checkAccess(req, nil, user), Equals, accessOK)

	// Without macaroon auth, normal users are unauthorized
	c.Check(ac.checkAccess(req, ucred, nil), Equals, accessUnauthorized)

	// The root user is granted access without a macaroon
	ucred = &ucrednet{uid: 0, pid: 100}
	c.Check(ac.checkAccess(req, ucred, nil), Equals, accessOK)
}

func (s *accessSuite) TestAuthenticatedAccessPolkit(c *C) {
	defer func() {
		polkitCheckAuthorization = polkit.CheckAuthorization
		logger.SetLogger(logger.NullLogger)
	}()

	var ac accessChecker = authenticatedAccess{Polkit: "action-id"}

	var logbuf bytes.Buffer
	log, err := logger.New(&logbuf, logger.DefaultFlags)
	c.Assert(err, IsNil)
	logger.SetLogger(log)

	req := httptest.NewRequest("GET", "/", nil)
	user := &auth.UserState{}
	ucred := &ucrednet{uid: 0, pid: 100}

	// polkit is not checked if any of:
	//   * ucred is missing
	//   * macaroon auth is provided
	//   * user is root
	polkitCheckAuthorization = func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error) {
		c.Fail()
		return true, nil
	}
	c.Check(ac.checkAccess(req, nil, nil), Equals, accessUnauthorized)
	c.Check(ac.checkAccess(req, nil, user), Equals, accessOK)
	c.Check(ac.checkAccess(req, ucred, nil), Equals, accessOK)

	// Access granted if polkit authorizes the request
	ucred = &ucrednet{uid: 42, pid: 1000}
	polkitCheckAuthorization = func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error) {
		c.Check(pid, Equals, int32(1000))
		c.Check(uid, Equals, uint32(42))
		c.Check(actionId, Equals, "action-id")
		c.Check(details, IsNil)
		c.Check(flags, Equals, polkit.CheckFlags(0))
		return true, nil
	}
	c.Check(ac.checkAccess(req, ucred, nil), Equals, accessOK)
	c.Check(logbuf.String(), Equals, "")

	// Unauthorized if polkit denies the request
	polkitCheckAuthorization = func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error) {
		return false, nil
	}
	c.Check(ac.checkAccess(req, ucred, nil), Equals, accessUnauthorized)
	c.Check(logbuf.String(), Equals, "")

	// Cancelled if the user dismisses the auth check
	polkitCheckAuthorization = func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error) {
		return false, polkit.ErrDismissed
	}
	c.Check(ac.checkAccess(req, ucred, nil), Equals, accessCancelled)
	c.Check(logbuf.String(), Equals, "")

	// The X-Allow-Interaction header can be set to tell polkitd
	// that interaction with the user is allowed.
	req.Header.Set(client.AllowInteractionHeader, "true")
	polkitCheckAuthorization = func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error) {
		c.Check(flags, Equals, polkit.CheckFlags(polkit.CheckAllowInteraction))
		return true, nil
	}
	c.Check(ac.checkAccess(req, ucred, nil), Equals, accessOK)
	c.Check(logbuf.String(), Equals, "")

	// Bad values in the request header are logged
	req.Header.Set(client.AllowInteractionHeader, "garbage")
	polkitCheckAuthorization = func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error) {
		c.Check(flags, Equals, polkit.CheckFlags(0))
		return true, nil
	}
	c.Check(ac.checkAccess(req, ucred, nil), Equals, accessOK)
	c.Check(logbuf.String(), testutil.Contains, "error parsing X-Allow-Interaction header:")
}

func (s *accessSuite) TestRootAccess(c *C) {
	var ac accessChecker = rootAccess{}

	user := &auth.UserState{}

	// rootAccess denies access from snapd-snap.socket
	ucred := &ucrednet{uid: 0, pid: 100, socket: dirs.SnapSocket}
	c.Check(ac.checkAccess(nil, ucred, nil), Equals, accessForbidden)
	c.Check(ac.checkAccess(nil, ucred, user), Equals, accessForbidden)

	// Non-root users are forbidden, even with macaroon auth
	ucred = &ucrednet{uid: 42, pid: 100}
	c.Check(ac.checkAccess(nil, ucred, nil), Equals, accessForbidden)
	c.Check(ac.checkAccess(nil, ucred, user), Equals, accessForbidden)

	// Root is granted access
	ucred = &ucrednet{uid: 0, pid: 100}
	c.Check(ac.checkAccess(nil, ucred, nil), Equals, accessOK)
}

func (s *accessSuite) TestSnapAccess(c *C) {
	var ac accessChecker = snapAccess{}

	// snapAccess allows access from snapd-snap.socket
	ucred := &ucrednet{uid: 42, pid: 100, socket: dirs.SnapSocket}
	c.Check(ac.checkAccess(nil, ucred, nil), Equals, accessOK)

	// access is forbidden on the main socket or without peer creds
	ucred.socket = dirs.SnapdSocket
	c.Check(ac.checkAccess(nil, ucred, nil), Equals, accessForbidden)
	c.Check(ac.checkAccess(nil, nil, nil), Equals, accessForbidden)
}
