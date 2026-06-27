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
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/polkit"
	"github.com/snapcore/snapd/seclog"
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

func (s *accessSuite) TestAccessOptionsValidation(c *C) {
	opts := daemon.AccessOptions{}
	c.Check(daemon.CheckAccess(nil, nil, nil, nil, opts, daemon.NewNopAuthzRecorder()), ErrorMatches, `unexpected access level "" \(api 500\)`)

	opts = daemon.AccessOptions{AccessLevel: "some-level"}
	c.Check(daemon.CheckAccess(nil, nil, nil, nil, opts, daemon.NewNopAuthzRecorder()), ErrorMatches, `unexpected access level "some-level" \(api 500\)`)

	opts = daemon.AccessOptions{AccessLevel: "root"}
	c.Check(daemon.CheckAccess(nil, nil, nil, nil, opts, daemon.NewNopAuthzRecorder()), ErrorMatches, `no sockets specified \(api 500\)`)

	opts = daemon.AccessOptions{AccessLevel: "root", Sockets: []string{"some-socket"}}
	c.Check(daemon.CheckAccess(nil, nil, nil, nil, opts, daemon.NewNopAuthzRecorder()), ErrorMatches, `unexpected socket "some-socket" \(api 500\)`)
}

func (s *accessSuite) TestOpenAccess(c *C) {
	var ac daemon.AccessChecker = daemon.OpenAccess{}

	// openAccess denies access from snapd-snap.socket
	ucred := &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapSocket}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)

	// Access allowed from snapd.socket
	ucred.Socket = dirs.SnapdSocket
	c.Check(ac.CheckAccess(nil, nil, ucred, nil, daemon.NewNopAuthzRecorder()), IsNil)

	// Access forbidden without peer credentials.  This will need
	// to be revisited if the API is ever exposed over TCP.
	c.Check(ac.CheckAccess(nil, nil, nil, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)
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
	c.Check(ac.CheckAccess(nil, req, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)
	c.Check(ac.CheckAccess(nil, req, ucred, user, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)

	// the same for unknown sockets
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: "unexpected.socket"}
	c.Check(ac.CheckAccess(nil, req, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)

	// With macaroon auth, a normal user is granted access
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapdSocket}
	c.Check(ac.CheckAccess(nil, req, ucred, user, daemon.NewNopAuthzRecorder()), IsNil)

	// Macaroon access requires peer credentials
	c.Check(ac.CheckAccess(nil, req, nil, user, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)

	// Without macaroon auth, normal users are unauthorized
	c.Check(ac.CheckAccess(nil, req, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errUnauthorized)

	// The root user is granted access without a macaroon
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapdSocket}
	c.Check(ac.CheckAccess(nil, req, ucred, nil, daemon.NewNopAuthzRecorder()), IsNil)
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
	c.Check(ac.CheckAccess(nil, req, nil, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)
	c.Check(ac.CheckAccess(nil, req, nil, user, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)
	c.Check(ac.CheckAccess(nil, req, ucred, nil, daemon.NewNopAuthzRecorder()), IsNil)

	// polkit is checked for regular users without macaroon auth
	restore = daemon.MockCheckPolkitAction(func(r *http.Request, u *daemon.Ucrednet, action string) *daemon.APIError {
		c.Check(r, Equals, req)
		c.Check(u, Equals, ucred)
		c.Check(action, Equals, "action-id")
		return nil
	})
	defer restore()
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapdSocket}
	c.Check(ac.CheckAccess(nil, req, ucred, nil, daemon.NewNopAuthzRecorder()), IsNil)
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
	c.Check(ac.CheckAccess(nil, nil, nil, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)
	c.Check(ac.CheckAccess(nil, nil, nil, user, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)

	// rootAccess denies access from snapd-snap.socket
	ucred := &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapSocket}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)
	c.Check(ac.CheckAccess(nil, nil, ucred, user, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)

	// Non-root users are forbidden, even with macaroon auth
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapdSocket}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)
	c.Check(ac.CheckAccess(nil, nil, ucred, user, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)

	// Root is granted access
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapdSocket}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil, daemon.NewNopAuthzRecorder()), IsNil)
}

func (s *accessSuite) TestSnapAccess(c *C) {
	var ac daemon.AccessChecker = daemon.SnapAccess{}

	// snapAccess allows access from snapd-snap.socket
	ucred := &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapSocket}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil, daemon.NewNopAuthzRecorder()), IsNil)

	// access is forbidden on the main socket or without peer creds
	ucred.Socket = dirs.SnapdSocket
	c.Check(ac.CheckAccess(nil, nil, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)
	c.Check(ac.CheckAccess(nil, nil, nil, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)
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
	c.Check(ac.CheckAccess(d, nil, nil, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)

	// Access from snapd.socket is allowed
	ucred := &daemon.Ucrednet{Uid: 1000, Pid: 1001, Socket: dirs.SnapdSocket}
	req := http.Request{RemoteAddr: ucred.String()}
	c.Check(ac.CheckAccess(d, nil, ucred, nil, daemon.NewNopAuthzRecorder()), IsNil)
	c.Check(req.RemoteAddr, Equals, ucred.String())

	// Access from unknown sockets is forbidden
	ucred = &daemon.Ucrednet{Uid: 1000, Pid: 1001, Socket: "unknown.socket"}
	c.Check(ac.CheckAccess(d, nil, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)

	// Access from pids that cannot be mapped to a snap on
	// snapd-snap.socket are rejected
	ucred = &daemon.Ucrednet{Uid: 1000, Pid: 1001, Socket: dirs.SnapSocket}
	c.Check(ac.CheckAccess(d, nil, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, daemon.Forbidden("could not determine snap name for pid: not a snap"))

	// Access from snapd-snap.socket is rejected by default
	ucred = &daemon.Ucrednet{Uid: 1000, Pid: 42, Socket: dirs.SnapSocket}
	c.Check(ac.CheckAccess(d, nil, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)

	// Now connect the marker interface
	st := d.Overlord().State()
	st.Lock()
	st.Set("conns", map[string]any{
		"some-snap:snap-themes-control core:snap-themes-control": map[string]any{
			"interface": "snap-themes-control",
		},
	})
	st.Unlock()

	// Access is allowed now that the snap has the plug connected
	req = http.Request{RemoteAddr: ucred.String()}
	c.Check(ac.CheckAccess(s.d, &req, ucred, nil, daemon.NewNopAuthzRecorder()), IsNil)
	// Interface is attached to RemoteAddr
	c.Check(req.RemoteAddr, Equals, fmt.Sprintf("%siface=snap-themes-control;", ucred))

	// Now connect both interfaces
	st.Lock()
	st.Set("conns", map[string]any{
		"some-snap:snap-themes-control core:snap-themes-control": map[string]any{
			"interface": "snap-themes-control",
		},
		"some-snap:snap-refresh-control core:snap-refresh-control": map[string]any{
			"interface": "snap-refresh-control",
		},
	})
	st.Unlock()
	req = http.Request{RemoteAddr: ucred.String()}
	c.Check(ac.CheckAccess(s.d, &req, ucred, nil, daemon.NewNopAuthzRecorder()), IsNil)
	// Check that both interfaces are attached to RemoteAddr.
	// Since conns is a map, order is not guaranteed.
	c.Check(req.RemoteAddr, Matches, fmt.Sprintf("^%siface=(snap-themes-control&snap-refresh-control|snap-refresh-control&snap-themes-control);$", ucred))

	// A left over "undesired" connection does not grant access
	st.Lock()
	st.Set("conns", map[string]any{
		"some-snap:snap-themes-control core:snap-themes-control": map[string]any{
			"interface": "snap-themes-control",
			"undesired": true,
		},
	})
	st.Unlock()
	req = http.Request{RemoteAddr: ucred.String()}
	c.Check(ac.CheckAccess(d, nil, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)
	c.Check(req.RemoteAddr, Equals, ucred.String())
}

func (s *accessSuite) TestInterfaceOpenAccess(c *C) {
	var ac daemon.AccessChecker = daemon.InterfaceOpenAccess{Interfaces: []string{"snap-themes-control", "snap-interfaces-requests-control"}}

	s.daemon(c)
	// interfaceOpenAccess allows access if requireInterfaceApiAccess() succeeds
	ucred := &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapSocket}
	restore := daemon.MockRequireInterfaceApiAccess(func(
		d *daemon.Daemon, r *http.Request, u *daemon.Ucrednet, reqs daemon.InterfaceAccessReqs, _ daemon.AuthzRecorder, _ daemon.AccessLevel,
	) (daemon.InterfaceAccessOutcome, *daemon.APIError) {
		c.Check(d, Equals, s.d)
		c.Check(u, Equals, ucred)
		c.Check(reqs, DeepEquals, daemon.InterfaceAccessReqs{
			Interfaces: []string{"snap-themes-control", "snap-interfaces-requests-control"},
			Plug:       true,
		})
		return daemon.InterfaceAccessOutcome{}, nil
	})
	defer restore()
	c.Check(ac.CheckAccess(s.d, nil, ucred, nil, daemon.NewNopAuthzRecorder()), IsNil)

	// Access is forbidden if requireInterfaceApiAccess() fails
	restore = daemon.MockRequireInterfaceApiAccess(func(
		d *daemon.Daemon, r *http.Request, u *daemon.Ucrednet, req daemon.InterfaceAccessReqs, _ daemon.AuthzRecorder, _ daemon.AccessLevel,
	) (daemon.InterfaceAccessOutcome, *daemon.APIError) {
		return daemon.InterfaceAccessOutcome{}, errForbidden
	})
	defer restore()
	c.Check(ac.CheckAccess(s.d, nil, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)
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
	restore = daemon.MockRequireInterfaceApiAccess(func(
		d *daemon.Daemon, r *http.Request, u *daemon.Ucrednet, reqs daemon.InterfaceAccessReqs, _ daemon.AuthzRecorder, _ daemon.AccessLevel,
	) (daemon.InterfaceAccessOutcome, *daemon.APIError) {
		c.Check(d, Equals, s.d)
		c.Check(u, Equals, ucred)
		c.Check(reqs, DeepEquals, daemon.InterfaceAccessReqs{
			Plug: true,
		})
		return daemon.InterfaceAccessOutcome{}, errForbidden
	})
	defer restore()
	c.Check(ac.CheckAccess(s.d, req, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)
	c.Check(ac.CheckAccess(s.d, req, ucred, user, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)

	// If requireInterfaceApiAccess succeeds, root is granted access
	restore = daemon.MockRequireInterfaceApiAccess(func(
		d *daemon.Daemon, r *http.Request, u *daemon.Ucrednet, reqs daemon.InterfaceAccessReqs, _ daemon.AuthzRecorder, _ daemon.AccessLevel,
	) (daemon.InterfaceAccessOutcome, *daemon.APIError) {
		return daemon.InterfaceAccessOutcome{}, nil
	})
	defer restore()
	c.Check(ac.CheckAccess(s.d, req, ucred, nil, daemon.NewNopAuthzRecorder()), IsNil)

	// Macaroon auth will grant a normal user access too
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapSocket}
	c.Check(ac.CheckAccess(s.d, req, ucred, user, daemon.NewNopAuthzRecorder()), IsNil)

	// Without macaroon auth, normal users are unauthorized
	c.Check(ac.CheckAccess(s.d, req, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errUnauthorized)
}

func (s *accessSuite) TestInterfaceAuthenticatedAccessPolkit(c *C) {
	var ac daemon.AccessChecker = daemon.InterfaceAuthenticatedAccess{Polkit: "action-id"}

	req := httptest.NewRequest("GET", "/", nil)
	user := &auth.UserState{}
	ucred := &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapSocket}

	s.daemon(c)
	restore := daemon.MockRequireInterfaceApiAccess(func(
		d *daemon.Daemon, r *http.Request, u *daemon.Ucrednet, reqs daemon.InterfaceAccessReqs, _ daemon.AuthzRecorder, _ daemon.AccessLevel,
	) (daemon.InterfaceAccessOutcome, *daemon.APIError) {
		c.Check(d, Equals, s.d)
		c.Check(u, Equals, ucred)
		c.Check(reqs, DeepEquals, daemon.InterfaceAccessReqs{
			Plug: true,
		})
		return daemon.InterfaceAccessOutcome{}, nil
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
	c.Check(ac.CheckAccess(s.d, req, ucred, nil, daemon.NewNopAuthzRecorder()), IsNil)
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapdSocket}
	c.Check(ac.CheckAccess(s.d, req, ucred, user, daemon.NewNopAuthzRecorder()), IsNil)

	// polkit is checked for regular users without macaroon auth
	restore = daemon.MockCheckPolkitAction(func(r *http.Request, u *daemon.Ucrednet, action string) *daemon.APIError {
		c.Check(r, Equals, req)
		c.Check(u, Equals, ucred)
		c.Check(action, Equals, "action-id")
		return nil
	})
	defer restore()
	c.Check(ac.CheckAccess(s.d, req, ucred, nil, daemon.NewNopAuthzRecorder()), IsNil)
}

func (s *accessSuite) TestInterfaceProviderRootAccessCallsWithCorrectArgs(c *C) {
	restore := daemon.MockCheckPolkitAction(func(r *http.Request, ucred *daemon.Ucrednet, action string) *daemon.APIError {
		// Polkit is not consulted if no action is specified
		c.Fail()
		return errForbidden
	})
	defer restore()

	var ac daemon.AccessChecker = daemon.InterfaceProviderRootAccess{
		Interfaces: []string{"fwupd"},
	}

	req := httptest.NewRequest("GET", "/", nil)
	s.daemon(c)

	ucred := &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapSocket}

	// mock and check whether correct arguments are passed
	called := 0
	restore = daemon.MockRequireInterfaceApiAccess(func(
		d *daemon.Daemon, r *http.Request, u *daemon.Ucrednet, reqs daemon.InterfaceAccessReqs, _ daemon.AuthzRecorder, _ daemon.AccessLevel,
	) (daemon.InterfaceAccessOutcome, *daemon.APIError) {
		c.Check(d, Equals, s.d)
		c.Check(u, Equals, ucred)
		c.Check(reqs, DeepEquals, daemon.InterfaceAccessReqs{
			Interfaces: []string{"fwupd"},
			Slot:       true,
		})
		called++
		return daemon.InterfaceAccessOutcome{}, errForbidden
	})
	defer restore()
	c.Check(ac.CheckAccess(s.d, req, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)
	c.Assert(called, Equals, 1)
}

func (s *accessSuite) TestInterfaceProviderRootAccessChecks(c *C) {
	d := s.daemon(c)
	s.mockSnap(c, `
name: fwupd-app
type: app
version: 1
slots:
  fwupd-provider:
    interface: fwupd
plugs:
  fwupd-consumer:
    interface: fwupd
  `)
	s.mockSnap(c, `
name: connected-fwupd-caller
version: 1
plugs:
  fwupd-consumer:
    interface: fwupd
`)
	s.mockSnap(c, `
name: disconnected-fwupd-caller
version: 1
plugs:
  fwupd-consumer:
    interface: fwupd
`)

	restore := daemon.MockCgroupSnapNameFromPid(func(pid int) (string, error) {
		switch pid {
		case 42:
			return "fwupd-app", nil
		case 1042:
			return "connected-fwupd-caller", nil
		case 10042:
			return "disconnected-fwupd-caller", nil
		default:
			return "", fmt.Errorf("not a snap")
		}
	})
	defer restore()

	restore = daemon.MockCheckPolkitAction(func(r *http.Request, ucred *daemon.Ucrednet, action string) *daemon.APIError {
		// Polkit is not consulted if no action is specified
		c.Fail()
		return errForbidden
	})
	defer restore()

	var ac daemon.AccessChecker = daemon.InterfaceProviderRootAccess{
		Interfaces: []string{"fwupd"},
	}

	user := &auth.UserState{}

	// fwupd-app, but unconnected and over snap socket
	ucred := &daemon.Ucrednet{Uid: 0, Pid: 42, Socket: dirs.SnapSocket}
	req := &http.Request{
		RemoteAddr: ucred.String(),
	}
	c.Check(ac.CheckAccess(s.d, req, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)

	// Now connect both interfaces
	st := d.Overlord().State()
	st.Lock()
	st.Set("conns", map[string]any{
		"fwupd-app:fwupd-consumer fwupd-app:fwupd-provider": map[string]any{
			"interface": "fwupd",
		},
		"connected-fwupd-caller:fwupd fwupd-app:fwupd-provider": map[string]any{
			"interface": "fwupd",
		},
	})
	st.Unlock()

	// fwupd-app, connected on the slot side
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 42, Socket: dirs.SnapSocket}
	req = &http.Request{
		RemoteAddr: ucred.String(),
	}
	c.Check(ac.CheckAccess(s.d, req, ucred, user, daemon.NewNopAuthzRecorder()), IsNil)

	// connected-fwupd-caller, but on the plug side
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 1042, Socket: dirs.SnapSocket}
	req = &http.Request{
		RemoteAddr: ucred.String(),
	}
	c.Check(ac.CheckAccess(s.d, req, ucred, user, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)

	// disconnected-fwupd-caller
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 10042, Socket: dirs.SnapSocket}
	req = &http.Request{
		RemoteAddr: ucred.String(),
	}
	c.Check(ac.CheckAccess(s.d, req, ucred, user, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)

	// normal user has no access even with a Macaroon auth
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 42, Socket: dirs.SnapSocket}
	req = &http.Request{
		RemoteAddr: ucred.String(),
	}
	c.Check(ac.CheckAccess(s.d, req, ucred, user, daemon.NewNopAuthzRecorder()), DeepEquals, errUnauthorized)

	// Without macaroon auth, normal users are unauthorized
	c.Check(ac.CheckAccess(s.d, req, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errUnauthorized)

	// on snapd socket, non-root is unauthorized
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 123, Socket: dirs.SnapdSocket}
	req = &http.Request{
		RemoteAddr: ucred.String(),
	}
	c.Check(ac.CheckAccess(s.d, req, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errUnauthorized)

	// but root is
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 123, Socket: dirs.SnapdSocket}
	req = &http.Request{
		RemoteAddr: ucred.String(),
	}
	c.Check(ac.CheckAccess(s.d, req, ucred, nil, daemon.NewNopAuthzRecorder()), IsNil)
}

func (s *accessSuite) TestInterfaceRootAccessCallsWithCorrectArgs(c *C) {
	restore := daemon.MockCheckPolkitAction(func(r *http.Request, ucred *daemon.Ucrednet, action string) *daemon.APIError {
		// Polkit is not consulted if no action is specified
		c.Fail()
		return errForbidden
	})
	defer restore()

	var ac daemon.AccessChecker = daemon.InterfaceRootAccess{
		Interfaces: []string{"fwupd"},
	}

	req := httptest.NewRequest("GET", "/", nil)
	s.daemon(c)

	ucred := &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapSocket}

	// mock and check whether correct arguments are passed
	called := 0
	restore = daemon.MockRequireInterfaceApiAccess(func(
		d *daemon.Daemon, r *http.Request, u *daemon.Ucrednet, reqs daemon.InterfaceAccessReqs, _ daemon.AuthzRecorder, _ daemon.AccessLevel,
	) (daemon.InterfaceAccessOutcome, *daemon.APIError) {
		c.Check(d, Equals, s.d)
		c.Check(u, Equals, ucred)
		c.Check(reqs, DeepEquals, daemon.InterfaceAccessReqs{
			Interfaces: []string{"fwupd"},
			Plug:       true,
		})
		called++
		return daemon.InterfaceAccessOutcome{}, errForbidden
	})
	defer restore()
	c.Check(ac.CheckAccess(s.d, req, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)
	c.Assert(called, Equals, 1)
}

func (s *accessSuite) TestInterfaceRootAccessChecks(c *C) {
	d := s.daemon(c)
	s.mockSnap(c, `
name: fwupd-app
type: app
version: 1
slots:
  fwupd-provider:
    interface: fwupd
  `)
	s.mockSnap(c, `
name: connected-fwupd-caller
version: 1
plugs:
  fwupd-consumer:
    interface: fwupd
`)

	restore := daemon.MockCgroupSnapNameFromPid(func(pid int) (string, error) {
		switch pid {
		case 42:
			return "fwupd-app", nil
		case 1042:
			return "connected-fwupd-caller", nil
		default:
			return "", fmt.Errorf("not a snap")
		}
	})
	defer restore()

	restore = daemon.MockCheckPolkitAction(func(r *http.Request, ucred *daemon.Ucrednet, action string) *daemon.APIError {
		// Polkit is not consulted if no action is specified
		c.Fail()
		return errForbidden
	})
	defer restore()

	var ac daemon.AccessChecker = daemon.InterfaceRootAccess{
		Interfaces: []string{"fwupd"},
	}

	user := &auth.UserState{}

	// connected-fwupd-caller, but unconnected and over snap socket
	ucred := &daemon.Ucrednet{Uid: 0, Pid: 1042, Socket: dirs.SnapSocket}
	req := &http.Request{
		RemoteAddr: ucred.String(),
	}
	c.Check(ac.CheckAccess(s.d, req, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)

	// Now connect connected-fwupd-caller (plug) to fwupd-app (slot)
	st := d.Overlord().State()
	st.Lock()
	st.Set("conns", map[string]any{
		"connected-fwupd-caller:fwupd fwupd-app:fwupd-provider": map[string]any{
			"interface": "fwupd",
		},
	})
	st.Unlock()

	// connected-fwupd-caller, connected on the plug side
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 1042, Socket: dirs.SnapSocket}
	req = &http.Request{
		RemoteAddr: ucred.String(),
	}
	c.Check(ac.CheckAccess(s.d, req, ucred, user, daemon.NewNopAuthzRecorder()), IsNil)

	// fwupd-app, connected on the slot side
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 42, Socket: dirs.SnapSocket}
	req = &http.Request{
		RemoteAddr: ucred.String(),
	}
	c.Check(ac.CheckAccess(s.d, req, ucred, user, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)

	// normal user has no access even with a Macaroon auth
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 1042, Socket: dirs.SnapSocket}
	req = &http.Request{
		RemoteAddr: ucred.String(),
	}
	c.Check(ac.CheckAccess(s.d, req, ucred, user, daemon.NewNopAuthzRecorder()), DeepEquals, errUnauthorized)

	// Without macaroon auth, normal users are unauthorized
	c.Check(ac.CheckAccess(s.d, req, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errUnauthorized)

	// on snapd socket, non-root is unauthorized
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 123, Socket: dirs.SnapdSocket}
	req = &http.Request{
		RemoteAddr: ucred.String(),
	}
	c.Check(ac.CheckAccess(s.d, req, ucred, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errUnauthorized)

	// but root is
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 123, Socket: dirs.SnapdSocket}
	req = &http.Request{
		RemoteAddr: ucred.String(),
	}
	c.Check(ac.CheckAccess(s.d, req, ucred, nil, daemon.NewNopAuthzRecorder()), IsNil)
}

func (s *accessSuite) TestInterfaceRootAccessPolkit(c *C) {
	d := s.daemon(c)
	s.mockSnap(c, `
name: fwupd-app
type: app
version: 1
slots:
  fwupd-provider:
    interface: fwupd
  `)
	s.mockSnap(c, `
name: connected-fwupd-caller
version: 1
plugs:
  fwupd-consumer:
    interface: fwupd
`)

	restore := daemon.MockCgroupSnapNameFromPid(func(pid int) (string, error) {
		switch pid {
		case 42:
			return "fwupd-app", nil
		case 1042:
			return "connected-fwupd-caller", nil
		default:
			return "", fmt.Errorf("not a snap")
		}
	})
	defer restore()

	st := d.Overlord().State()
	st.Lock()
	st.Set("conns", map[string]any{
		"connected-fwupd-caller:fwupd fwupd-app:fwupd-provider": map[string]any{
			"interface": "fwupd",
		},
	})
	st.Unlock()

	var ac daemon.AccessChecker = daemon.InterfaceRootAccess{
		Polkit:     "action-id",
		Interfaces: []string{"fwupd"},
	}

	req := httptest.NewRequest("GET", "/", nil)
	user := &auth.UserState{}

	// polkit is not checked if any of:
	//   * ucred is missing
	//   * user is root
	//   * snap request (as root) with relevant connected plug
	//   * snap request without relevant connected plug
	restore = daemon.MockCheckPolkitAction(func(r *http.Request, ucred *daemon.Ucrednet, action string) *daemon.APIError {
		c.Fail()
		return daemon.Forbidden("access denied")
	})
	defer restore()
	// ucred is missing
	c.Check(ac.CheckAccess(nil, req, nil, nil, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)
	// user is root (on snapd.socket)
	c.Check(ac.CheckAccess(nil, req, &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapdSocket}, nil, daemon.NewNopAuthzRecorder()), IsNil)
	// snap request (as root) with relevant connected plug (on snapd-snap.socket)
	c.Check(ac.CheckAccess(s.d, req, &daemon.Ucrednet{Uid: 0, Pid: 1042, Socket: dirs.SnapSocket}, nil, daemon.NewNopAuthzRecorder()), IsNil)
	// snap request without relevant connected plug (on snapd-snap.socket)
	c.Check(ac.CheckAccess(s.d, req, &daemon.Ucrednet{Uid: 0, Pid: 42, Socket: dirs.SnapSocket}, user, daemon.NewNopAuthzRecorder()), DeepEquals, errForbidden)

	// polkit is checked for snaps with connected plug
	called := 0
	restore = daemon.MockCheckPolkitAction(func(r *http.Request, u *daemon.Ucrednet, action string) *daemon.APIError {
		called++
		c.Check(r, Equals, req)
		c.Check(action, Equals, "action-id")
		return nil
	})
	defer restore()
	// regular user (on snapd.socket)
	c.Check(ac.CheckAccess(nil, req, &daemon.Ucrednet{Uid: 1001, Pid: 100, Socket: dirs.SnapdSocket}, nil, daemon.NewNopAuthzRecorder()), IsNil)
	// snap request (with macaroon) with relevant connected plug (on snapd-snap.socket)
	c.Check(ac.CheckAccess(s.d, req, &daemon.Ucrednet{Uid: 1001, Pid: 1042, Socket: dirs.SnapSocket}, user, daemon.NewNopAuthzRecorder()), IsNil)
	// snap request (without macaroon) with relevant connected plug (on snapd-snap.socket)
	c.Check(ac.CheckAccess(s.d, req, &daemon.Ucrednet{Uid: 1001, Pid: 1042, Socket: dirs.SnapSocket}, nil, daemon.NewNopAuthzRecorder()), IsNil)
	c.Check(called, Equals, 3)
}

func (s *accessSuite) TestRequireInterfaceApiAccessErrorChecks(c *C) {
	d := s.daemon(c)
	req := &http.Request{}
	rec := daemon.NewNopAuthzRecorder()
	level := daemon.AccessLevelAuthenticated

	// no side of the connection is specified
	_, err := daemon.RequireInterfaceApiAccessImpl(d, req, nil, daemon.InterfaceAccessReqs{}, rec, level)
	c.Check(err, DeepEquals, daemon.InternalError("required connection side is unspecified"))

	// check on both sides
	_, err = daemon.RequireInterfaceApiAccessImpl(d, req, nil, daemon.InterfaceAccessReqs{
		Plug:       true,
		Slot:       true,
		Interfaces: []string{"foo"},
	}, rec, level)
	c.Check(err, DeepEquals, daemon.InternalError("snap cannot be specified on both sides of the connection"))

	// no interfaces
	_, err = daemon.RequireInterfaceApiAccessImpl(d, req, nil, daemon.InterfaceAccessReqs{
		Plug: true,
	}, rec, level)
	c.Check(err, DeepEquals, daemon.InternalError("interfaces access check, but interfaces list is empty"))

	// this one actually reaches the credentials check
	_, err = daemon.RequireInterfaceApiAccessImpl(d, req, nil, daemon.InterfaceAccessReqs{
		Plug:       true,
		Interfaces: []string{"foo"},
	}, rec, level)
	c.Check(err, DeepEquals, errForbidden)
}

func reqWithAction(c *C, action string, isJSON, malformed bool) *http.Request {
	rawBody := fmt.Sprintf(`{"action": "%s", "some-field": "some-data"}`, action)
	if malformed {
		rawBody = "}this is not json{"
	}
	body := strings.NewReader(rawBody)
	req, err := http.NewRequest("POST", "/v2/system-volumes", body)
	c.Assert(err, IsNil)
	if isJSON {
		req.Header.Add("Content-Type", "application/json")
	}
	return req
}

func (s *accessSuite) TestByActionAccess(c *C) {
	byAction := map[string]daemon.AccessChecker{
		"action-1": daemon.RootAccess{},
		"action-2": daemon.AuthenticatedAccess{},
		"action-3": daemon.OpenAccess{},
	}

	var ac daemon.AccessChecker = daemon.ByActionAccess{
		ByAction: byAction,
		Default:  daemon.RootAccess{},
	}

	type testcase struct {
		ucred       daemon.Ucrednet
		expectedErr map[string]*daemon.APIError
		noAuth      bool
		notJSON     bool
		malformed   bool
	}

	tcs := []testcase{
		{
			ucred:  daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapSocket},
			noAuth: true,
			expectedErr: map[string]*daemon.APIError{
				"action-1": errForbidden,
				"action-2": errForbidden,
				"action-3": errForbidden,
				"default":  errForbidden,
			},
		},
		{
			ucred: daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapdSocket},
			expectedErr: map[string]*daemon.APIError{
				"action-1": errForbidden,
				"default":  errForbidden,
			},
		},
		{
			ucred:  daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapdSocket},
			noAuth: true,
			expectedErr: map[string]*daemon.APIError{
				"action-1": errForbidden,
				"action-2": errUnauthorized,
				"default":  errForbidden,
			},
		},
		{
			ucred:   daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapdSocket},
			notJSON: true,
			expectedErr: map[string]*daemon.APIError{
				"action-1": daemon.BadRequest(`unexpected content type: ""`),
				"action-2": daemon.BadRequest(`unexpected content type: ""`),
				"action-3": daemon.BadRequest(`unexpected content type: ""`),
				"default":  daemon.BadRequest(`unexpected content type: ""`),
			},
		},
		{
			ucred: daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapdSocket},
			// content type is JSON, but it's invalid
			malformed: true,
			expectedErr: map[string]*daemon.APIError{
				"action-1": daemon.BadRequest("invalid character '}' looking for beginning of value"),
				"action-2": daemon.BadRequest("invalid character '}' looking for beginning of value"),
				"action-3": daemon.BadRequest("invalid character '}' looking for beginning of value"),
				"default":  daemon.BadRequest("invalid character '}' looking for beginning of value"),
			},
		},
		{
			ucred:  daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapdSocket},
			noAuth: true,
		},
		{
			ucred:   daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapdSocket},
			notJSON: true,
			noAuth:  true,
			expectedErr: map[string]*daemon.APIError{
				"action-1": daemon.BadRequest(`unexpected content type: ""`),
				"action-2": daemon.BadRequest(`unexpected content type: ""`),
				"action-3": daemon.BadRequest(`unexpected content type: ""`),
				"default":  daemon.BadRequest(`unexpected content type: ""`),
			},
		},
		{
			ucred:  daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapdSocket},
			noAuth: true,
			// content type is JSON, but it's invalid
			malformed: true,
			expectedErr: map[string]*daemon.APIError{
				"action-1": daemon.BadRequest("invalid character '}' looking for beginning of value"),
				"action-2": daemon.BadRequest("invalid character '}' looking for beginning of value"),
				"action-3": daemon.BadRequest("invalid character '}' looking for beginning of value"),
				"default":  daemon.BadRequest("invalid character '}' looking for beginning of value"),
			},
		},
	}

	for idx, tc := range tcs {
		user := &auth.UserState{}
		if tc.noAuth {
			user = nil
		}

		for action := range byAction {
			cmt := Commentf("sub-test tcs[%d] failed for action %q", idx, action)
			err := ac.CheckAccess(nil, reqWithAction(c, action, !tc.notJSON, tc.malformed), &tc.ucred, user, daemon.NewNopAuthzRecorder())
			if expectedErr := tc.expectedErr[action]; err != nil {
				c.Check(err, DeepEquals, expectedErr, cmt)
			} else {
				c.Check(err, IsNil, cmt)
			}
		}

		cmt := Commentf("sub-test tcs[%d] failed for default action", idx)
		err := ac.CheckAccess(nil, reqWithAction(c, "default", !tc.notJSON, tc.malformed), &tc.ucred, user, daemon.NewNopAuthzRecorder())
		if expectedErr := tc.expectedErr["default"]; err != nil {
			c.Check(err, DeepEquals, expectedErr, cmt)
		} else {
			c.Check(err, IsNil, cmt)
		}
	}
}

func (s *accessSuite) TestByActionAccessDefaultMustBeRoot(c *C) {
	type testcase struct {
		ac           daemon.AccessChecker
		canBeDefault bool
	}

	tcs := []testcase{
		{ac: daemon.RootAccess{}, canBeDefault: true},
		{ac: daemon.InterfaceRootAccess{Interfaces: []string{"iface"}}, canBeDefault: true},
		{ac: daemon.InterfaceProviderRootAccess{Interfaces: []string{"iface"}}, canBeDefault: true},

		{ac: daemon.OpenAccess{}, canBeDefault: false},
		{ac: daemon.AuthenticatedAccess{}, canBeDefault: false},
		{ac: daemon.SnapAccess{}, canBeDefault: false},
		{ac: daemon.InterfaceOpenAccess{Interfaces: []string{"iface"}}, canBeDefault: false},
		{ac: daemon.InterfaceAuthenticatedAccess{Interfaces: []string{"iface"}}, canBeDefault: false},
		{ac: daemon.ByActionAccess{Default: daemon.RootAccess{}}, canBeDefault: false},
	}

	for _, tc := range tcs {
		body := strings.NewReader(`{"action": "unknown"}`)
		req, err := http.NewRequest("POST", "/v2/system-volumes", body)
		c.Assert(err, IsNil)
		req.Header.Add("Content-Type", "application/json")

		ac := daemon.ByActionAccess{Default: tc.ac}

		ucred := daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapdSocket}
		err = ac.CheckAccess(nil, req, &ucred, nil, daemon.NewNopAuthzRecorder())
		if tc.canBeDefault {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, DeepEquals, daemon.InternalError("internal error: default access checker must have root-level access: got %T", tc.ac))
		}
	}

}

func (s *accessSuite) TestByActionAccessLargeJSON(c *C) {
	body := strings.NewReader(fmt.Sprintf(`{"action": "%s"}`, strings.Repeat("a", 4*1024*1024)))
	req, err := http.NewRequest("POST", "/v2/system-volumes", body)
	c.Assert(err, IsNil)
	req.Header.Add("Content-Type", "application/json")

	ac := daemon.ByActionAccess{Default: daemon.RootAccess{}}

	ucred := daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapdSocket}
	err = ac.CheckAccess(nil, req, &ucred, nil, daemon.NewNopAuthzRecorder())
	c.Assert(err, DeepEquals, daemon.BadRequest("body size limit exceeded"))
}

func (s *accessSuite) TestByActionAccessDataAfterJOSN(c *C) {
	body := strings.NewReader(fmt.Sprintf(`{"action": "some-action"} data`))
	req, err := http.NewRequest("POST", "/v2/system-volumes", body)
	c.Assert(err, IsNil)
	req.Header.Add("Content-Type", "application/json")

	ac := daemon.ByActionAccess{Default: daemon.RootAccess{}}

	ucred := daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapdSocket}
	err = ac.CheckAccess(nil, req, &ucred, nil, daemon.NewNopAuthzRecorder())
	c.Assert(err, DeepEquals, daemon.BadRequest("unexpected data after request body"))
}

func (s *accessSuite) TestCheckAccessAuthzRecording(c *C) {
	req := httptest.NewRequest("GET", "/", nil)

	// Open access: denials are not audited.
	rec := daemon.NewAuthzTestRecorder()
	var ac daemon.AccessChecker = daemon.OpenAccess{}
	c.Check(ac.CheckAccess(nil, nil, nil, nil, rec), DeepEquals, errForbidden)
	c.Check(rec.DeniedReason, Equals, "")
	c.Check(rec.GrantedReason, Equals, "")

	// Authenticated access denied without credentials.
	rec = daemon.NewAuthzTestRecorder()
	ac = daemon.AuthenticatedAccess{}
	ucred := &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapdSocket}
	c.Check(ac.CheckAccess(nil, req, ucred, nil, rec), DeepEquals, errUnauthorized)
	c.Check(rec.DeniedReason, Equals, seclog.ReasonDeniedUserAuthDenied)

	// Authenticated access granted via macaroon.
	rec = daemon.NewAuthzTestRecorder()
	user := &auth.UserState{}
	c.Check(ac.CheckAccess(nil, req, ucred, user, rec), IsNil)
	c.Check(rec.GrantedReason, Equals, seclog.ReasonGrantedUserAuth)

	// Root access granted.
	rec = daemon.NewAuthzTestRecorder()
	ac = daemon.RootAccess{}
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapdSocket}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil, rec), IsNil)
	c.Check(rec.GrantedReason, Equals, seclog.ReasonGrantedRootAuth)

	// Polkit grant and deny.
	ac = daemon.AuthenticatedAccess{Polkit: "action-id"}
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapdSocket}
	restore := daemon.MockCheckPolkitAction(func(r *http.Request, u *daemon.Ucrednet, action string) *daemon.APIError {
		c.Check(action, Equals, "action-id")
		return nil
	})
	defer restore()
	rec = daemon.NewAuthzTestRecorder()
	c.Check(ac.CheckAccess(nil, req, ucred, nil, rec), IsNil)
	c.Check(rec.GrantedReason, Equals, seclog.ReasonGrantedPolkitAuth)

	restore = daemon.MockCheckPolkitAction(func(r *http.Request, u *daemon.Ucrednet, action string) *daemon.APIError {
		return errUnauthorized
	})
	defer restore()
	rec = daemon.NewAuthzTestRecorder()
	c.Check(ac.CheckAccess(nil, req, ucred, nil, rec), DeepEquals, errUnauthorized)
	c.Check(rec.DeniedReason, Equals, seclog.ReasonDeniedPolkitAuthDenied)

	// Wrong socket on an audited endpoint.
	rec = daemon.NewAuthzTestRecorder()
	ac = daemon.AuthenticatedAccess{}
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: dirs.SnapSocket}
	c.Check(ac.CheckAccess(nil, req, ucred, nil, rec), DeepEquals, errForbidden)
	c.Check(rec.DeniedReason, Equals, seclog.ReasonDeniedSocketNotPermitted)

	// Interface connection contributes to grant postfix.
	rec = daemon.NewAuthzTestRecorder()
	ac = daemon.InterfaceRootAccess{Interfaces: []string{"desktop-launch"}}
	restore = daemon.MockRequireInterfaceApiAccess(func(
		d *daemon.Daemon, r *http.Request, u *daemon.Ucrednet, reqs daemon.InterfaceAccessReqs, _ daemon.AuthzRecorder, _ daemon.AccessLevel,
	) (daemon.InterfaceAccessOutcome, *daemon.APIError) {
		return daemon.InterfaceAccessOutcome{MatchedIface: "desktop-launch", Plug: true}, nil
	})
	defer restore()
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: dirs.SnapSocket}
	c.Check(ac.CheckAccess(nil, req, ucred, nil, rec), IsNil)
	c.Check(rec.GrantedReason, Equals, seclog.ReasonGrantedRootAuth+" desktop-launch+plug")
}
