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

	"github.com/snapcore/snapd/seclog"
)

func (s *SecLogSuite) TestReasonString(c *C) {
	// Both fields set.
	c.Check(seclog.Reason{
		Code: seclog.ReasonInvalidCredentials, Message: "bad password",
	}.String(), Equals, "invalid-credentials:bad password")

	// Both fields empty — all "<unknown>".
	c.Check(seclog.Reason{}.String(), Equals, "<unknown>:<unknown>")

	// Only code set.
	c.Check(seclog.Reason{Code: seclog.ReasonInternal}.String(), Equals, "internal:<unknown>")

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

func (s *SecLogSuite) TestChangedFieldsNoChanges(c *C) {
	a := seclog.SnapdUserState{
		SnapdUser: seclog.SnapdUser{
			ID: 1, StoreUserName: "jdoe", StoreUserEmail: "j@d.com",
		},
		LocalMacaroon: "m1", StoreMacaroon: "sm1",
	}
	c.Check(a.ChangedFields(a), HasLen, 0)
}

func (s *SecLogSuite) TestChangedFieldsAllFields(c *C) {
	a := seclog.SnapdUserState{
		SnapdUser: seclog.SnapdUser{
			ID: 1, StoreUserName: "a", StoreUserEmail: "a@a.com",
			Expiration: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		LocalMacaroon:   "m1",
		LocalDischarges: []string{"d1"},
		StoreMacaroon:   "sm1",
		StoreDischarges: []string{"sd1"},
	}
	b := seclog.SnapdUserState{
		SnapdUser: seclog.SnapdUser{
			ID: 2, StoreUserName: "b", StoreUserEmail: "b@b.com",
			Expiration: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		},
		LocalMacaroon:   "m2",
		LocalDischarges: []string{"d2"},
		StoreMacaroon:   "sm2",
		StoreDischarges: []string{"sd2"},
	}
	c.Check(a.ChangedFields(b), DeepEquals, []string{
		"expiration",
		"local-discharges", "local-macaroon",
		"snapd-user-id",
		"store-discharges", "store-macaroon",
		"store-user-email", "store-user-name",
	})
}

func (s *SecLogSuite) TestChangedFieldsSliceOrderIndependent(c *C) {
	// Slice fields with the same elements in different order must not
	// produce a spurious entry in changed_fields.
	a := seclog.SnapdUserState{
		SnapdUser:       seclog.SnapdUser{ID: 1},
		LocalDischarges: []string{"a", "b", "c"},
		StoreDischarges: []string{"x", "y"},
	}
	b := seclog.SnapdUserState{
		SnapdUser:       seclog.SnapdUser{ID: 1},
		LocalDischarges: []string{"c", "a", "b"},
		StoreDischarges: []string{"y", "x"},
	}
	c.Check(a.ChangedFields(b), HasLen, 0)
}

func (s *SecLogSuite) TestChangedFieldsSingleField(c *C) {
	a := seclog.SnapdUserState{
		SnapdUser: seclog.SnapdUser{
			ID: 1, StoreUserEmail: "old@test.com",
		},
		StoreMacaroon: "sm1",
	}
	b := a
	b.StoreUserEmail = "new@test.com"
	c.Check(a.ChangedFields(b), DeepEquals, []string{"store-user-email"})
}

func (s *SecLogSuite) TestChangedFieldsExpirationLocationIndependent(c *C) {
	// Same instant expressed in UTC and a non-UTC fixed zone must not
	// produce a spurious expiration entry in changed_fields.
	instant := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	loc := time.FixedZone("UTC+10", 10*60*60)
	a := seclog.SnapdUserState{SnapdUser: seclog.SnapdUser{ID: 1, Expiration: instant}}
	b := seclog.SnapdUserState{SnapdUser: seclog.SnapdUser{ID: 1, Expiration: instant.In(loc)}}
	c.Check(a.ChangedFields(b), HasLen, 0)
}

func (s *SecLogSuite) TestChangedFieldsExpirationMonotonicIndependent(c *C) {
	// time.Now() carries a monotonic clock reading. A round-trip through
	// JSON strips it, leaving only the wall clock. Both must compare as
	// equal so that re-loading a user does not produce a spurious
	// expiration entry in changed_fields.
	now := time.Now().Add(time.Hour)
	a := seclog.SnapdUserState{SnapdUser: seclog.SnapdUser{ID: 1, Expiration: now}}
	b := seclog.SnapdUserState{SnapdUser: seclog.SnapdUser{ID: 1, Expiration: now.Round(0)}}
	c.Check(a.ChangedFields(b), HasLen, 0)
}
