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
	. "gopkg.in/check.v1"

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

	c.Check(seclog.Peer{UID: ^uint32(0)}.String(), Equals, "<unknown>:<unknown>:<unknown>")
}
