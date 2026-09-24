// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"bytes"
	"errors"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/seclog"
	"github.com/snapcore/snapd/seclog/seclogtest"
	"github.com/snapcore/snapd/testutil"
)

type seclogSuite struct{}

var _ = Suite(&seclogSuite{})

func (s *seclogSuite) TestSeclogPeerNil(c *C) {
	var ucred *daemon.Ucrednet
	peer := ucred.SeclogPeer()

	c.Check(peer, DeepEquals, seclog.Peer{
		UID: seclog.PeerNobody,
		PID: seclog.PeerNoProcess,
	})
	c.Check(peer.String(), Equals, "<unknown>:<unknown>:<unknown>")
}

func (s *seclogSuite) TestSeclogPeer(c *C) {
	ucred := daemon.NewUcrednet("snap.firefox.firefox", "/usr/bin/snap", 1000, "/run/snapd.socket")
	ucred.PIDForPolkit = 4242

	peer := ucred.SeclogPeer()
	c.Check(peer, DeepEquals, seclog.Peer{
		Socket:       "/run/snapd.socket",
		UID:          1000,
		PID:          4242,
		Exe:          "/usr/bin/snap",
		InstanceName: "firefox",
		Runnable:     "app=firefox",
	})
}

func (s *seclogSuite) TestSeclogPeerComponentHook(c *C) {
	ucred := daemon.NewUcrednet("snap.mysnap+widget.hook.install", "/usr/bin/snap", 1000, "/run/snapd.socket")
	ucred.PIDForPolkit = 4242

	peer := ucred.SeclogPeer()
	c.Check(peer, DeepEquals, seclog.Peer{
		Socket:       "/run/snapd.socket",
		UID:          1000,
		PID:          4242,
		Exe:          "/usr/bin/snap",
		InstanceName: "mysnap",
		Runnable:     "comp-hook=widget:install",
	})
}

func (s *seclogSuite) TestSeclogPeerComponentHookInstance(c *C) {
	ucred := daemon.NewUcrednet("snap.mysnap_foo+widget.hook.install", "/usr/bin/snap", 1000, "/run/snapd.socket")
	ucred.PIDForPolkit = 4242

	peer := ucred.SeclogPeer()
	c.Check(peer, DeepEquals, seclog.Peer{
		Socket:       "/run/snapd.socket",
		UID:          1000,
		PID:          4242,
		Exe:          "/usr/bin/snap",
		InstanceName: "mysnap_foo",
		Runnable:     "comp-hook=widget:install",
	})
}

func (s *seclogSuite) TestSeclogPeerMissingTag(c *C) {
	ucred := daemon.NewUcrednet("", "/usr/bin/snap", 0, "/run/snapd.socket")
	ucred.PIDForPolkit = 10

	peer := ucred.SeclogPeer()

	c.Check(peer, DeepEquals, seclog.Peer{
		Socket: "/run/snapd.socket",
		UID:    0,
		PID:    10,
		Exe:    "/usr/bin/snap",
	})
}

func (s *seclogSuite) TestSeclogPeerExeError(c *C) {
	ucred := daemon.NewUcrednet("snap.firefox.firefox", "", 1000, "/run/snapd.socket")
	ucred.PIDForPolkit = 4242
	ucred.SetUntrustedProcessExeNameErr(errors.New("cannot read executable"))

	peer := ucred.SeclogPeer()

	c.Check(peer, DeepEquals, seclog.Peer{
		Socket:       "/run/snapd.socket",
		UID:          1000,
		PID:          4242,
		InstanceName: "firefox",
		Runnable:     "app=firefox",
	})
}

func (s *seclogSuite) TestSeclogSnapdUserFromAuth(c *C) {
	c.Check(daemon.SeclogSnapdUserFromAuth(nil), DeepEquals, seclog.SnapdUser{})

	exp := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	user := daemon.SeclogSnapdUserFromAuth(&auth.UserState{
		ID:         7,
		Username:   "admin",
		Email:      "admin@example.com",
		Expiration: exp,
	})
	c.Check(user, DeepEquals, seclog.SnapdUser{
		ID:             7,
		StoreUserName:  "admin",
		StoreUserEmail: "admin@example.com",
		Expiration:     exp,
	})
}

func (s *seclogSuite) TestSeclogEndpointFromRequest(c *C) {
	c.Check(daemon.SeclogEndpointFromRequest("/v2/snaps", "POST", "install"), DeepEquals, seclog.Endpoint{
		Method: "POST",
		Path:   "/v2/snaps",
		Action: "install",
	})
	c.Check(daemon.SeclogEndpointFromRequest("/v2/snaps", "GET", ""), DeepEquals, seclog.Endpoint{
		Method: "GET",
		Path:   "/v2/snaps",
	})
}

func (s *seclogSuite) setupSecLog() (*bytes.Buffer, func()) {
	buf := &bytes.Buffer{}
	seclog.Setup(seclogtest.MockSecurityLogger(buf))
	return buf, func() {
		seclog.Setup(seclog.NewNopLogger())
	}
}

func (s *seclogSuite) TestAuthzRecorderEmitGranted(c *C) {
	buf, restore := s.setupSecLog()
	defer restore()

	user := seclog.SnapdUser{ID: 7, StoreUserEmail: "admin@example.com", StoreUserName: "admin"}
	peer := seclog.Peer{Socket: "/run/snapd.socket", UID: 0, PID: 42}
	endpoint := seclog.Endpoint{Method: "POST", Path: "/v2/snaps", Action: "install"}

	rec := daemon.NewAuthzRecorder().
		WithUser(user).
		WithPeer(peer).
		WithEndpoint(endpoint)
	rec.RecordGranted(seclog.GrantRootAuth, "desktop-launch", true)
	rec.Emit()

	c.Check(buf.String(), testutil.Contains, "authz_admin")
	c.Check(buf.String(), testutil.Contains, "admin@example.com")
	c.Check(buf.String(), testutil.Contains, "POST:/v2/snaps:install")
	c.Check(buf.String(), testutil.Contains, `[reason_granted="root-auth desktop-launch plug"]`)
	c.Check(buf.String(), Not(testutil.Contains), "authz_fail")
}

func (s *seclogSuite) TestAuthzRecorderEmitSlotSide(c *C) {
	buf, restore := s.setupSecLog()
	defer restore()

	rec := daemon.NewAuthzRecorder()
	rec.RecordGranted(seclog.GrantRootAuth, "fwupd", false)
	rec.Emit()

	c.Check(buf.String(), testutil.Contains, `[reason_granted="root-auth fwupd slot"]`)
}

func (s *seclogSuite) TestAuthzRecorderReplaceOutcome(c *C) {
	buf, restore := s.setupSecLog()
	defer restore()

	rec := daemon.NewAuthzRecorder().
		WithUser(seclog.SnapdUser{ID: 7, StoreUserEmail: "admin@example.com"}).
		WithPeer(seclog.Peer{Socket: "/run/snapd.socket", UID: 1000, PID: 42}).
		WithEndpoint(seclog.Endpoint{Method: "POST", Path: "/v2/snaps", Action: "install"})

	rec.RecordGranted(seclog.GrantRootAuth, "desktop-launch", true)
	rec.RecordDenied(seclog.DenialUserAuth)
	rec.Emit()

	c.Check(buf.String(), testutil.Contains, "authz_fail")
	c.Check(buf.String(), testutil.Contains, `[reason_denied="user-auth-denied"]`)
	c.Check(buf.String(), Not(testutil.Contains), "authz_admin")

	buf.Reset()
	rec.RecordGranted(seclog.GrantUserAuth, "", false)
	rec.Emit()

	c.Check(buf.String(), testutil.Contains, "authz_admin")
	c.Check(buf.String(), testutil.Contains, `[reason_granted="user-auth"]`)
	c.Check(buf.String(), Not(testutil.Contains), "authz_fail")
}

func (s *seclogSuite) TestAuthzRecorderEmitWithoutOutcome(c *C) {
	buf, restore := s.setupSecLog()
	defer restore()

	daemon.NewAuthzRecorder().
		WithUser(seclog.SnapdUser{ID: 1}).
		WithPeer(seclog.Peer{UID: 0}).
		WithEndpoint(seclog.Endpoint{Method: "GET", Path: "/v2/snaps"}).
		Emit()

	c.Check(buf.String(), Equals, "")
}

func (s *seclogSuite) TestNopAuthzRecorder(c *C) {
	buf, restore := s.setupSecLog()
	defer restore()

	rec := daemon.NewNopAuthzRecorder().
		WithUser(seclog.SnapdUser{ID: 1, StoreUserEmail: "admin@example.com"}).
		WithPeer(seclog.Peer{Socket: "/run/snapd.socket", UID: 0, PID: 42}).
		WithEndpoint(seclog.Endpoint{Method: "POST", Path: "/v2/snaps", Action: "install"})
	rec.RecordGranted(seclog.GrantRootAuth, "desktop-launch", true)
	rec.RecordDenied(seclog.DenialRootAuth)
	rec.Emit()

	c.Check(buf.String(), Equals, "")
}
