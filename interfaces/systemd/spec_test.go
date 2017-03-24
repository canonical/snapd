// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package systemd_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/systemd"
)

type specSuite struct{}

var _ = Suite(&specSuite{})

func (s *specSuite) TestSmoke(c *C) {
	spec := systemd.Specification{}
	c.Assert(spec.Services(), IsNil)
	svc1 := systemd.Service{ExecStart: "one"}
	err := spec.AddService("svc1.service", svc1)
	c.Assert(err, IsNil)
	svc2 := systemd.Service{ExecStart: "two"}
	err = spec.AddService("svc2.service", svc2)
	c.Assert(err, IsNil)
	c.Assert(spec.Services(), DeepEquals, map[string]systemd.Service{
		"svc1.service": svc1,
		"svc2.service": svc2,
	})
}

func (s *specSuite) TestClashing(c *C) {
	svc1 := systemd.Service{ExecStart: "one"}
	svc2 := systemd.Service{ExecStart: "two"}
	spec := systemd.Specification{}
	err := spec.AddService("foo.service", svc1)
	c.Assert(err, IsNil)
	err = spec.AddService("foo.service", svc2)
	c.Assert(err, ErrorMatches, "interface requires conflicting system needs")
}
