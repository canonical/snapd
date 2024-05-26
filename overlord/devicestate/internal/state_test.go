// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package internal_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate/internal"
	"github.com/snapcore/snapd/overlord/state"
)

func TestInternal(t *testing.T) { TestingT(t) }

type internalSuite struct {
	state *state.State
}

var _ = Suite(&internalSuite{})

func (s *internalSuite) SetUpTest(c *C) {
	s.state = state.New(nil)
}

func (s *internalSuite) TestSetDevice(c *C) {
	s.state.Lock()
	device := mylog.Check2(internal.Device(s.state))
	s.state.Unlock()
	c.Check(err, IsNil)
	c.Check(device, DeepEquals, &auth.DeviceState{})

	s.state.Lock()
	mylog.Check(internal.SetDevice(s.state, &auth.DeviceState{Brand: "some-brand"}))
	c.Check(err, IsNil)
	device = mylog.Check2(internal.Device(s.state))
	s.state.Unlock()
	c.Check(err, IsNil)
	c.Check(device, DeepEquals, &auth.DeviceState{Brand: "some-brand"})
}
