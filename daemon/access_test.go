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

package daemon_test

import (
	"net/http"
	"net/http/httptest"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/polkit"
	"github.com/snapcore/snapd/testutil"
)

type accessSuite struct{}

var _ = Suite(&accessSuite{})

func (s *accessSuite) TestOpenAccess(c *C) {
	var ac daemon.AccessChecker = daemon.OpenAccess{}

	// openAccess denies access from snapd-snap.socket
	ucred := &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapSocket}
	c.Check(ac.CheckAccess(nil, ucred, nil), Equals, daemon.AccessForbidden)

	// Access allowed from other sockets
	ucred.Socket = dirs.SnapdSocket
	c.Check(ac.CheckAccess(nil, ucred, nil), Equals, daemon.AccessOK)

	// Access forbidden without peer credentials.  This will need
	// to be revisited if the API is ever exposed over TCP.
	c.Check(ac.CheckAccess(nil, nil, nil), Equals, daemon.AccessForbidden)
}

func (s *accessSuite) TestAuthenticatedAccess(c *C) {
	restore := daemon.MockCheckPolkitAction(func(r *http.Request, ucred *daemon.Ucrednet, action string) daemon.AccessResult {
		// Polkit is not consulted if no action is specified
		c.Fail()
		return daemon.AccessForbidden
	})
	defer restore()

	var ac daemon.AccessChecker = daemon.AuthenticatedAccess{}

	req := httptest.NewRequest("GET", "/", nil)
	user := &auth.UserState{}

	// authenticatedAccess denies access from snapd-snap.socket
	ucred := &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapSocket}
	c.Check(ac.CheckAccess(req, ucred, nil), Equals, daemon.AccessForbidden)
	c.Check(ac.CheckAccess(req, ucred, user), Equals, daemon.AccessForbidden)

	// With macaroon auth, a normal user is granted access
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapdSocket}
	c.Check(ac.CheckAccess(req, ucred, user), Equals, daemon.AccessOK)

	// Macaroon access requires peer credentials
	c.Check(ac.CheckAccess(req, nil, user), Equals, daemon.AccessForbidden)

	// Without macaroon auth, normal users are unauthorized
	c.Check(ac.CheckAccess(req, ucred, nil), Equals, daemon.AccessUnauthorized)

	// The root user is granted access without a macaroon
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapdSocket}
	c.Check(ac.CheckAccess(req, ucred, nil), Equals, daemon.AccessOK)
}

func (s *accessSuite) TestAuthenticatedAccessPolkit(c *C) {
	var ac daemon.AccessChecker = daemon.AuthenticatedAccess{Polkit: "action-id"}

	req := httptest.NewRequest("GET", "/", nil)
	user := &auth.UserState{}
	ucred := &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapdSocket}

	// polkit is not checked if any of:
	//   * ucred is missing
	//   * macaroon auth is provided
	//   * user is root
	restore := daemon.MockCheckPolkitAction(func(r *http.Request, ucred *daemon.Ucrednet, action string) daemon.AccessResult {
		c.Fail()
		return daemon.AccessForbidden
	})
	defer restore()
	c.Check(ac.CheckAccess(req, nil, nil), Equals, daemon.AccessForbidden)
	c.Check(ac.CheckAccess(req, nil, user), Equals, daemon.AccessForbidden)
	c.Check(ac.CheckAccess(req, ucred, nil), Equals, daemon.AccessOK)

	// polkit is checked for regular users without macaroon auth
	restore = daemon.MockCheckPolkitAction(func(r *http.Request, u *daemon.Ucrednet, action string) daemon.AccessResult {
		c.Check(r, Equals, req)
		c.Check(u, Equals, ucred)
		c.Check(action, Equals, "action-id")
		return daemon.AccessOK
	})
	defer restore()
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapdSocket}
	c.Check(ac.CheckAccess(req, ucred, nil), Equals, daemon.AccessOK)
}

func (s *accessSuite) TestCheckPolkitActionImpl(c *C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

	req := httptest.NewRequest("GET", "/", nil)
	ucred := &daemon.Ucrednet{Uid: 42, Pid: 1000, Socket: dirs.SnapdSocket}

	// Access granted if polkit authorizes the request
	restore = daemon.MockPolkitCheckAuthorization(func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error) {
		c.Check(pid, Equals, int32(1000))
		c.Check(uid, Equals, uint32(42))
		c.Check(actionId, Equals, "action-id")
		c.Check(details, IsNil)
		c.Check(flags, Equals, polkit.CheckFlags(0))
		return true, nil
	})
	defer restore()
	c.Check(daemon.CheckPolkitActionImpl(req, ucred, "action-id"), Equals, daemon.AccessOK)
	c.Check(logbuf.String(), Equals, "")

	// Unauthorized if polkit denies the request
	restore = daemon.MockPolkitCheckAuthorization(func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error) {
		return false, nil
	})
	defer restore()
	c.Check(daemon.CheckPolkitActionImpl(req, ucred, "action-id"), Equals, daemon.AccessUnauthorized)
	c.Check(logbuf.String(), Equals, "")

	// Cancelled if the user dismisses the auth check
	restore = daemon.MockPolkitCheckAuthorization(func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error) {
		return false, polkit.ErrDismissed
	})
	defer restore()
	c.Check(daemon.CheckPolkitActionImpl(req, ucred, "action-id"), Equals, daemon.AccessCancelled)
	c.Check(logbuf.String(), Equals, "")

	// The X-Allow-Interaction header can be set to tell polkitd
	// that interaction with the user is allowed.
	req.Header.Set(client.AllowInteractionHeader, "true")
	restore = daemon.MockPolkitCheckAuthorization(func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error) {
		c.Check(flags, Equals, polkit.CheckFlags(polkit.CheckAllowInteraction))
		return true, nil
	})
	defer restore()
	c.Check(daemon.CheckPolkitActionImpl(req, ucred, "action-id"), Equals, daemon.AccessOK)
	c.Check(logbuf.String(), Equals, "")

	// Bad values in the request header are logged
	req.Header.Set(client.AllowInteractionHeader, "garbage")
	restore = daemon.MockPolkitCheckAuthorization(func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error) {
		c.Check(flags, Equals, polkit.CheckFlags(0))
		return true, nil
	})
	defer restore()
	c.Check(daemon.CheckPolkitActionImpl(req, ucred, "action-id"), Equals, daemon.AccessOK)
	c.Check(logbuf.String(), testutil.Contains, "error parsing X-Allow-Interaction header:")
}

func (s *accessSuite) TestRootAccess(c *C) {
	var ac daemon.AccessChecker = daemon.RootAccess{}

	user := &auth.UserState{}

	// rootAccess denies access without ucred
	c.Check(ac.CheckAccess(nil, nil, nil), Equals, daemon.AccessForbidden)
	c.Check(ac.CheckAccess(nil, nil, user), Equals, daemon.AccessForbidden)

	// rootAccess denies access from snapd-snap.socket
	ucred := &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapSocket}
	c.Check(ac.CheckAccess(nil, ucred, nil), Equals, daemon.AccessForbidden)
	c.Check(ac.CheckAccess(nil, ucred, user), Equals, daemon.AccessForbidden)

	// Non-root users are forbidden, even with macaroon auth
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapdSocket}
	c.Check(ac.CheckAccess(nil, ucred, nil), Equals, daemon.AccessForbidden)
	c.Check(ac.CheckAccess(nil, ucred, user), Equals, daemon.AccessForbidden)

	// Root is granted access
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapdSocket}
	c.Check(ac.CheckAccess(nil, ucred, nil), Equals, daemon.AccessOK)
}

func (s *accessSuite) TestSnapAccess(c *C) {
	var ac daemon.AccessChecker = daemon.SnapAccess{}

	// snapAccess allows access from snapd-snap.socket
	ucred := &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapSocket}
	c.Check(ac.CheckAccess(nil, ucred, nil), Equals, daemon.AccessOK)

	// access is forbidden on the main socket or without peer creds
	ucred.Socket = dirs.SnapdSocket
	c.Check(ac.CheckAccess(nil, ucred, nil), Equals, daemon.AccessForbidden)
	c.Check(ac.CheckAccess(nil, nil, nil), Equals, daemon.AccessForbidden)
}
