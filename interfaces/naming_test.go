// -*- Mote: Go; indent-tabs-mode: t -*-

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

package interfaces_test

import (
	. "gopkg.in/check.v1"

	. "github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/snap"
)

type NamingSuite struct{}

var _ = Suite(&NamingSuite{})

func (s *NamingSuite) TestSecurityTag(c *C) {
	appInfo := &snap.AppInfo{Snap: &snap.Info{SuggestedName: "http"}, Name: "GET"}
	c.Check(SecurityTag(appInfo), Equals, "snap.http.GET")
}

func (s *NamingSuite) TestSecurityTagGlob(c *C) {
	c.Check(SecurityTagGlob("http"), Equals, "snap.http.*")
}
