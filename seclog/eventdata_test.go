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

package seclog_test

import (
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/seclog"
)

func (s *SecLogSuite) TestReasonString(c *C) {
	// Code and message set.
	c.Check(seclog.Reason{
		Code: 401, Kind: "invalid-credentials", Message: "bad password",
	}.String(), Equals, "401:bad password")

	// All fields empty — all "<unknown>".
	c.Check(seclog.Reason{}.String(), Equals, "<unknown>:<unknown>")

	// Only code set.
	c.Check(seclog.Reason{Code: 500, Kind: "internal"}.String(), Equals, "500:<unknown>")

	// Only message set.
	c.Check(seclog.Reason{Message: "something broke"}.String(), Equals, "<unknown>:something broke")
}

func (s *SecLogSuite) TestSnapdUserString(c *C) {
	// All fields set.
	c.Check(seclog.SnapdUser{
		ID: 42, StoreUserEmail: "a@b.com", StoreUserName: "jdoe",
	}.String(), Equals, "42:a@b.com:jdoe")

	// All fields zero/empty — all "<unknown>".
	c.Check(seclog.SnapdUser{}.String(), Equals, "<unknown>:<unknown>:<unknown>")

	// Only ID set.
	c.Check(seclog.SnapdUser{ID: 7}.String(), Equals, "7:<unknown>:<unknown>")

	// Only email set.
	c.Check(seclog.SnapdUser{StoreUserEmail: "x@y.z"}.String(), Equals, "<unknown>:x@y.z:<unknown>")

	// Only username set.
	c.Check(seclog.SnapdUser{StoreUserName: "root"}.String(), Equals, "<unknown>:<unknown>:root")
}

func (s *SecLogSuite) TestEndpointString(c *C) {
	c.Check(seclog.Endpoint{
		Method: "POST", Path: "/v2/snaps", Action: "install",
	}.String(), Equals, "POST:/v2/snaps:install")

	c.Check(seclog.Endpoint{Method: "DELETE", Path: "/v2/snaps/core"}.String(),
		Equals, "DELETE:/v2/snaps/core:<none>")

	c.Check(seclog.Endpoint{}.String(), Equals, "<unknown>:<unknown>:<none>")

	c.Check(seclog.Endpoint{Method: "GET"}.String(), Equals, "GET:<unknown>:<none>")
}

func (s *SecLogSuite) TestPeerString(c *C) {
	c.Check(seclog.Peer{
		Socket: "/run/snapd.socket", UID: 0, PID: 4242,
	}.String(), Equals, "/run/snapd.socket:0:4242")

	// Zero UID is root; only the nobody sentinel is unknown.
	c.Check(seclog.Peer{}.String(), Equals, "<unknown>:0:<unknown>")

	c.Check(seclog.Peer{Socket: "/run/snapd.socket"}.String(), Equals, "/run/snapd.socket:0:<unknown>")

	c.Check(seclog.Peer{UID: seclog.PeerNobody}.String(), Equals, "<unknown>:<unknown>:<unknown>")
}

func (s *SecLogSuite) TestGrantReasonWithInterface(c *C) {
	c.Check(seclog.GrantRootAuth.WithInterface("desktop-launch", true),
		Equals, seclog.GrantReason("root-auth desktop-launch plug"))
	c.Check(seclog.GrantUserAuth.WithInterface("snap-themes-control", false),
		Equals, seclog.GrantReason("user-auth snap-themes-control slot"))
	c.Check(seclog.GrantPolkitAuth.WithInterface("snap-fde-control", true),
		Equals, seclog.GrantReason("polkit-auth snap-fde-control plug"))

	// Empty iface means no interface contributed; the base reason is unchanged.
	c.Check(seclog.GrantRootAuth.WithInterface("", true), Equals, seclog.GrantRootAuth)
	c.Check(seclog.GrantRootAuth.WithInterface("", false), Equals, seclog.GrantRootAuth)
}

func (s *SecLogSuite) TestSystemUserAddOptionsFromStoreEmail(c *C) {
	opts := &osutil.AddUserOptions{
		Gecos:               "karl@example.com,Karl Popper",
		Sudoer:              true,
		ExtraUsers:          true,
		ForcePasswordChange: true,
	}

	got := seclog.SystemUserAddOptionsFrom(opts, nil)
	c.Check(got, DeepEquals, seclog.SystemUserAddOptions{
		RealUserName:        "Karl Popper",
		Sudoer:              true,
		ExtraUsers:          true,
		ForcePasswordChange: true,
		Known:               false,
		Assertion:           nil,
	})
}

func (s *SecLogSuite) TestSystemUserAddOptionsFromGecosWithoutName(c *C) {
	// No comma: RealUserName is left empty.
	c.Check(seclog.SystemUserAddOptionsFrom(&osutil.AddUserOptions{
		Gecos: "only-email@example.com",
	}, nil).RealUserName, Equals, "")

	c.Check(seclog.SystemUserAddOptionsFrom(&osutil.AddUserOptions{}, nil).RealUserName, Equals, "")
}

func (s *SecLogSuite) TestSystemUserAddOptionsFromAssertion(c *C) {
	su := assertstest.FakeAssertion(map[string]any{
		"type":         "system-user",
		"authority-id": "my-brand",
		"brand-id":     "my-brand",
		"email":        "foo@bar.com",
		"username":     "example-user",
		"name":         "Example User",
		"since":        time.Now().Format(time.RFC3339),
		"until":        time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		"revision":     "3",
	}).(*asserts.SystemUser)

	got := seclog.SystemUserAddOptionsFrom(&osutil.AddUserOptions{
		Gecos:  "foo@bar.com,Example User",
		Sudoer: true,
	}, su)
	c.Check(got, DeepEquals, seclog.SystemUserAddOptions{
		RealUserName: "Example User",
		Sudoer:       true,
		Known:        true,
		Assertion: &seclog.AssertionRef{
			Type:       "system-user",
			PrimaryKey: []string{"my-brand", "foo@bar.com"},
			Revision:   3,
		},
	})
}

func (s *SecLogSuite) TestAssertionRefFrom(c *C) {
	c.Check(seclog.AssertionRefFrom(nil), IsNil)

	su := assertstest.FakeAssertion(map[string]any{
		"type":         "system-user",
		"authority-id": "my-brand",
		"brand-id":     "my-brand",
		"email":        "foo@bar.com",
		"username":     "example-user",
		"since":        time.Now().Format(time.RFC3339),
		"until":        time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	}).(*asserts.SystemUser)

	c.Check(seclog.AssertionRefFrom(su), DeepEquals, &seclog.AssertionRef{
		Type:       "system-user",
		PrimaryKey: []string{"my-brand", "foo@bar.com"},
		Revision:   0,
	})
}
