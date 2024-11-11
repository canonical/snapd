// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021-2024 Canonical Ltd
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
	"fmt"
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

type accessSuite struct {
	apiBaseSuite
}

var _ = Suite(&accessSuite{})

var (
	errForbidden    = daemon.Forbidden("access denied")
	errUnauthorized = daemon.Unauthorized("access denied")
)

func (s *accessSuite) TestOpenAccess(c *C) {
	var ac daemon.AccessChecker = daemon.OpenAccess{}

	// openAccess denies access from snapd-snap.socket
	ucred := &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapSocket}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil), DeepEquals, errForbidden)

	// Access allowed from snapd.socket
	ucred.Socket = dirs.SnapdSocket
	c.Check(ac.CheckAccess(nil, nil, ucred, nil), IsNil)

	// Access forbidden without peer credentials.  This will need
	// to be revisited if the API is ever exposed over TCP.
	c.Check(ac.CheckAccess(nil, nil, nil, nil), DeepEquals, errForbidden)
}

func (s *accessSuite) TestAuthenticatedAccess(c *C) {
	restore := daemon.MockCheckPolkitAction(func(r *http.Request, ucred *daemon.Ucrednet, action string) *daemon.APIError {
		// Polkit is not consulted if no action is specified
		c.Fail()
		return daemon.Forbidden("access denied")
	})
	defer restore()

	var ac daemon.AccessChecker = daemon.AuthenticatedAccess{}

	req := httptest.NewRequest("GET", "/", nil)
	user := &auth.UserState{}

	// authenticatedAccess denies access from snapd-snap.socket
	ucred := &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapSocket}
	c.Check(ac.CheckAccess(nil, req, ucred, nil), DeepEquals, errForbidden)
	c.Check(ac.CheckAccess(nil, req, ucred, user), DeepEquals, errForbidden)

	// the same for unknown sockets
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: "unexpected.socket"}
	c.Check(ac.CheckAccess(nil, req, ucred, nil), DeepEquals, errForbidden)

	// With macaroon auth, a normal user is granted access
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapdSocket}
	c.Check(ac.CheckAccess(nil, req, ucred, user), IsNil)

	// Macaroon access requires peer credentials
	c.Check(ac.CheckAccess(nil, req, nil, user), DeepEquals, errForbidden)

	// Without macaroon auth, normal users are unauthorized
	c.Check(ac.CheckAccess(nil, req, ucred, nil), DeepEquals, errUnauthorized)

	// The root user is granted access without a macaroon
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapdSocket}
	c.Check(ac.CheckAccess(nil, req, ucred, nil), IsNil)
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
	restore := daemon.MockCheckPolkitAction(func(r *http.Request, ucred *daemon.Ucrednet, action string) *daemon.APIError {
		c.Fail()
		return daemon.Forbidden("access denied")
	})
	defer restore()
	c.Check(ac.CheckAccess(nil, req, nil, nil), DeepEquals, errForbidden)
	c.Check(ac.CheckAccess(nil, req, nil, user), DeepEquals, errForbidden)
	c.Check(ac.CheckAccess(nil, req, ucred, nil), IsNil)

	// polkit is checked for regular users without macaroon auth
	restore = daemon.MockCheckPolkitAction(func(r *http.Request, u *daemon.Ucrednet, action string) *daemon.APIError {
		c.Check(r, Equals, req)
		c.Check(u, Equals, ucred)
		c.Check(action, Equals, "action-id")
		return nil
	})
	defer restore()
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapdSocket}
	c.Check(ac.CheckAccess(nil, req, ucred, nil), IsNil)
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
	c.Check(daemon.CheckPolkitActionImpl(req, ucred, "action-id"), IsNil)
	c.Check(logbuf.String(), Equals, "")

	// Unauthorized if polkit denies the request
	restore = daemon.MockPolkitCheckAuthorization(func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error) {
		return false, nil
	})
	defer restore()
	c.Check(daemon.CheckPolkitActionImpl(req, ucred, "action-id"), DeepEquals, errUnauthorized)
	c.Check(logbuf.String(), Equals, "")

	// Cancelled if the user dismisses the auth check
	restore = daemon.MockPolkitCheckAuthorization(func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error) {
		return false, polkit.ErrDismissed
	})
	defer restore()
	rspe := daemon.CheckPolkitActionImpl(req, ucred, "action-id")
	c.Check(rspe, DeepEquals, daemon.AuthCancelled("cancelled"))
	c.Check(logbuf.String(), Equals, "")

	// The X-Allow-Interaction header can be set to tell polkitd
	// that interaction with the user is allowed.
	req.Header.Set(client.AllowInteractionHeader, "true")
	restore = daemon.MockPolkitCheckAuthorization(func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error) {
		c.Check(flags, Equals, polkit.CheckFlags(polkit.CheckAllowInteraction))
		return true, nil
	})
	defer restore()
	c.Check(daemon.CheckPolkitActionImpl(req, ucred, "action-id"), IsNil)
	c.Check(logbuf.String(), Equals, "")

	// Bad values in the request header are logged
	req.Header.Set(client.AllowInteractionHeader, "garbage")
	restore = daemon.MockPolkitCheckAuthorization(func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error) {
		c.Check(flags, Equals, polkit.CheckFlags(0))
		return true, nil
	})
	defer restore()
	c.Check(daemon.CheckPolkitActionImpl(req, ucred, "action-id"), IsNil)
	c.Check(logbuf.String(), testutil.Contains, "error parsing X-Allow-Interaction header:")
}

func (s *accessSuite) TestRootAccess(c *C) {
	var ac daemon.AccessChecker = daemon.RootAccess{}

	user := &auth.UserState{}

	// rootAccess denies access without ucred
	c.Check(ac.CheckAccess(nil, nil, nil, nil), DeepEquals, errForbidden)
	c.Check(ac.CheckAccess(nil, nil, nil, user), DeepEquals, errForbidden)

	// rootAccess denies access from snapd-snap.socket
	ucred := &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapSocket}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil), DeepEquals, errForbidden)
	c.Check(ac.CheckAccess(nil, nil, ucred, user), DeepEquals, errForbidden)

	// Non-root users are forbidden, even with macaroon auth
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapdSocket}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil), DeepEquals, errForbidden)
	c.Check(ac.CheckAccess(nil, nil, ucred, user), DeepEquals, errForbidden)

	// Root is granted access
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapdSocket}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil), IsNil)
}

func (s *accessSuite) TestSnapAccess(c *C) {
	var ac daemon.AccessChecker = daemon.SnapAccess{}

	// snapAccess allows access from snapd-snap.socket
	ucred := &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapSocket}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil), IsNil)

	// access is forbidden on the main socket or without peer creds
	ucred.Socket = dirs.SnapdSocket
	c.Check(ac.CheckAccess(nil, nil, ucred, nil), DeepEquals, errForbidden)
	c.Check(ac.CheckAccess(nil, nil, nil, nil), DeepEquals, errForbidden)
}

func (s *accessSuite) TestRequireInterfaceApiAccessImpl(c *C) {
	d := s.daemon(c)
	s.mockSnap(c, `
name: core
type: os
version: 1
slots:
  snap-themes-control:
  snap-refresh-control:
`)
	s.mockSnap(c, `
name: some-snap
version: 1
plugs:
  snap-themes-control:
  snap-refresh-control:
`)

	restore := daemon.MockCgroupSnapNameFromPid(func(pid int) (string, error) {
		if pid == 42 {
			return "some-snap", nil
		}
		return "", fmt.Errorf("not a snap")
	})
	defer restore()

	var ac daemon.AccessChecker = daemon.InterfaceOpenAccess{Interfaces: []string{"snap-themes-control", "snap-refresh-control"}}

	// Access with no ucred data is forbidden
	c.Check(ac.CheckAccess(d, nil, nil, nil), DeepEquals, errForbidden)

	// Access from snapd.socket is allowed
	ucred := &daemon.Ucrednet{Uid: 1000, Pid: 1001, Socket: dirs.SnapdSocket}
	req := http.Request{RemoteAddr: ucred.String()}
	c.Check(ac.CheckAccess(d, nil, ucred, nil), IsNil)
	c.Check(req.RemoteAddr, Equals, ucred.String())

	// Access from unknown sockets is forbidden
	ucred = &daemon.Ucrednet{Uid: 1000, Pid: 1001, Socket: "unknown.socket"}
	c.Check(ac.CheckAccess(d, nil, ucred, nil), DeepEquals, errForbidden)

	// Access from pids that cannot be mapped to a snap on
	// snapd-snap.socket are rejected
	ucred = &daemon.Ucrednet{Uid: 1000, Pid: 1001, Socket: dirs.SnapSocket}
	c.Check(ac.CheckAccess(d, nil, ucred, nil), DeepEquals, daemon.Forbidden("could not determine snap name for pid: not a snap"))

	// Access from snapd-snap.socket is rejected by default
	ucred = &daemon.Ucrednet{Uid: 1000, Pid: 42, Socket: dirs.SnapSocket}
	c.Check(ac.CheckAccess(d, nil, ucred, nil), DeepEquals, errForbidden)

	// Now connect the marker interface
	st := d.Overlord().State()
	st.Lock()
	st.Set("conns", map[string]interface{}{
		"some-snap:snap-themes-control core:snap-themes-control": map[string]interface{}{
			"interface": "snap-themes-control",
		},
	})
	st.Unlock()

	// Access is allowed now that the snap has the plug connected
	req = http.Request{RemoteAddr: ucred.String()}
	c.Check(ac.CheckAccess(s.d, &req, ucred, nil), IsNil)
	// Interface is attached to RemoteAddr
	c.Check(req.RemoteAddr, Equals, fmt.Sprintf("%siface=snap-themes-control;", ucred))

	// Now connect both interfaces
	st.Lock()
	st.Set("conns", map[string]interface{}{
		"some-snap:snap-themes-control core:snap-themes-control": map[string]interface{}{
			"interface": "snap-themes-control",
		},
		"some-snap:snap-refresh-control core:snap-refresh-control": map[string]interface{}{
			"interface": "snap-refresh-control",
		},
	})
	st.Unlock()
	req = http.Request{RemoteAddr: ucred.String()}
	c.Check(ac.CheckAccess(s.d, &req, ucred, nil), IsNil)
	// Check that both interfaces are attached to RemoteAddr.
	// Since conns is a map, order is not guaranteed.
	c.Check(req.RemoteAddr, Matches, fmt.Sprintf("^%siface=(snap-themes-control&snap-refresh-control|snap-refresh-control&snap-themes-control);$", ucred))

	// A left over "undesired" connection does not grant access
	st.Lock()
	st.Set("conns", map[string]interface{}{
		"some-snap:snap-themes-control core:snap-themes-control": map[string]interface{}{
			"interface": "snap-themes-control",
			"undesired": true,
		},
	})
	st.Unlock()
	req = http.Request{RemoteAddr: ucred.String()}
	c.Check(ac.CheckAccess(d, nil, ucred, nil), DeepEquals, errForbidden)
	c.Check(req.RemoteAddr, Equals, ucred.String())
}

func (s *accessSuite) TestInterfaceOpenAccess(c *C) {
	var ac daemon.AccessChecker = daemon.InterfaceOpenAccess{Interfaces: []string{"snap-themes-control", "snap-interfaces-requests-control"}}

	s.daemon(c)
	// interfaceOpenAccess allows access if requireInterfaceApiAccess() succeeds
	ucred := &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapSocket}
	restore := daemon.MockRequireInterfaceApiAccess(func(d *daemon.Daemon, r *http.Request, u *daemon.Ucrednet, interfaceNames []string) *daemon.APIError {
		c.Check(d, Equals, s.d)
		c.Check(u, Equals, ucred)
		return nil
	})
	defer restore()
	c.Check(ac.CheckAccess(s.d, nil, ucred, nil), IsNil)

	// Access is forbidden if requireInterfaceApiAccess() fails
	restore = daemon.MockRequireInterfaceApiAccess(func(d *daemon.Daemon, r *http.Request, u *daemon.Ucrednet, interfaceNames []string) *daemon.APIError {
		return errForbidden
	})
	defer restore()
	c.Check(ac.CheckAccess(s.d, nil, ucred, nil), DeepEquals, errForbidden)
}

func (s *accessSuite) TestInterfaceAuthenticatedAccess(c *C) {
	restore := daemon.MockCheckPolkitAction(func(r *http.Request, ucred *daemon.Ucrednet, action string) *daemon.APIError {
		// Polkit is not consulted if no action is specified
		c.Fail()
		return errForbidden
	})
	defer restore()

	var ac daemon.AccessChecker = daemon.InterfaceAuthenticatedAccess{}

	req := httptest.NewRequest("GET", "/", nil)
	user := &auth.UserState{}
	s.daemon(c)

	// interfaceAuthenticatedAccess denies access if requireInterfaceApiAccess fails
	ucred := &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapSocket}
	restore = daemon.MockRequireInterfaceApiAccess(func(d *daemon.Daemon, r *http.Request, u *daemon.Ucrednet, interfaceNames []string) *daemon.APIError {
		c.Check(d, Equals, s.d)
		c.Check(u, Equals, ucred)
		return errForbidden
	})
	defer restore()
	c.Check(ac.CheckAccess(s.d, req, ucred, nil), DeepEquals, errForbidden)
	c.Check(ac.CheckAccess(s.d, req, ucred, user), DeepEquals, errForbidden)

	// If requireInterfaceApiAccess succeeds, root is granted access
	restore = daemon.MockRequireInterfaceApiAccess(func(d *daemon.Daemon, r *http.Request, u *daemon.Ucrednet, interfaceNames []string) *daemon.APIError {
		return nil
	})
	defer restore()
	c.Check(ac.CheckAccess(s.d, req, ucred, nil), IsNil)

	// Macaroon auth will grant a normal user access too
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapSocket}
	c.Check(ac.CheckAccess(s.d, req, ucred, user), IsNil)

	// Without macaroon auth, normal users are unauthorized
	c.Check(ac.CheckAccess(s.d, req, ucred, nil), DeepEquals, errUnauthorized)
}

func (s *accessSuite) TestInterfaceAuthenticatedAccessPolkit(c *C) {
	var ac daemon.AccessChecker = daemon.InterfaceAuthenticatedAccess{Polkit: "action-id"}

	req := httptest.NewRequest("GET", "/", nil)
	user := &auth.UserState{}
	ucred := &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapSocket}

	s.daemon(c)
	restore := daemon.MockRequireInterfaceApiAccess(func(d *daemon.Daemon, r *http.Request, u *daemon.Ucrednet, interfaceNames []string) *daemon.APIError {
		c.Check(d, Equals, s.d)
		c.Check(u, Equals, ucred)
		return nil
	})
	defer restore()

	// polkit is not checked if any of:
	//   * user is root
	//   * regular users with macaroon auth
	restore = daemon.MockCheckPolkitAction(func(r *http.Request, ucred *daemon.Ucrednet, action string) *daemon.APIError {
		c.Fail()
		return errForbidden
	})
	defer restore()
	c.Check(ac.CheckAccess(s.d, req, ucred, nil), IsNil)
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapdSocket}
	c.Check(ac.CheckAccess(s.d, req, ucred, user), IsNil)

	// polkit is checked for regular users without macaroon auth
	restore = daemon.MockCheckPolkitAction(func(r *http.Request, u *daemon.Ucrednet, action string) *daemon.APIError {
		c.Check(r, Equals, req)
		c.Check(u, Equals, ucred)
		c.Check(action, Equals, "action-id")
		return nil
	})
	defer restore()
	c.Check(ac.CheckAccess(s.d, req, ucred, nil), IsNil)
}
