// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package notifications

import (
	"net/http"

	. "gopkg.in/check.v1"
)

type SubscriberSuite struct{}

var _ = Suite(&SubscriberSuite{})

func (s *SubscriberSuite) TestNewSubscriber(c *C) {
	tests := []struct {
		path     string
		types    []string
		resource string
	}{
		{"/events", []string(nil), ""},
		{"/events?types=logging", []string{"logging"}, ""},
		{"/events?types=logging,operations", []string{"logging", "operations"}, ""},
		{"/events?resource=123", []string(nil), "123"},
		{"/events?types=logging&resource=123", []string{"logging"}, "123"},
	}

	for _, tt := range tests {
		conn := &fakeConn{}
		req, err := http.NewRequest("GET", tt.path, nil)
		c.Assert(err, IsNil)

		sub := NewSubscriber(conn, req)
		c.Assert(sub.uuid, Matches, `^[A-Za-z0-9]{16}$`)
		c.Assert(sub.conn, DeepEquals, conn)
		c.Assert(sub.types, DeepEquals, tt.types)
		c.Assert(sub.resource, DeepEquals, tt.resource)
	}
}
