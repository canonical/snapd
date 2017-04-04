// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

type serviceSuite struct{}

var _ = Suite(&serviceSuite{})

func (s *serviceSuite) TestString(c *C) {
	service1 := systemd.Service{ExecStart: "/bin/true"}
	c.Assert(service1.String(), Equals, "[Service]\nExecStart=/bin/true\n\n[Install]\nWantedBy=multi-user.target\n")
	service2 := systemd.Service{Type: "oneshot"}
	c.Assert(service2.String(), Equals, "[Service]\nType=oneshot\n\n[Install]\nWantedBy=multi-user.target\n")
	service3 := systemd.Service{RemainAfterExit: true}
	c.Assert(service3.String(), Equals, "[Service]\nRemainAfterExit=yes\n\n[Install]\nWantedBy=multi-user.target\n")
	service4 := systemd.Service{RemainAfterExit: false}
	c.Assert(service4.String(), Equals, "[Service]\n\n[Install]\nWantedBy=multi-user.target\n")
	service5 := systemd.Service{ExecStop: "/bin/true"}
	c.Assert(service5.String(), Equals, "[Service]\nExecStop=/bin/true\n\n[Install]\nWantedBy=multi-user.target\n")
	service6 := systemd.Service{Description: "ohai"}
	c.Assert(service6.String(), Equals, "[Unit]\nDescription=ohai\n\n[Service]\n\n[Install]\nWantedBy=multi-user.target\n")
}
