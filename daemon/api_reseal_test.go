// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"fmt"
	"net/http"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/state"
)

var _ = Suite(&recoveryKeysSuite{})

type resealSuite struct {
	apiBaseSuite
}

func (s *resealSuite) SetUpTest(c *C) {
	s.apiBaseSuite.SetUpTest(c)

	s.expectRootAccess()
}

func (s *resealSuite) testResealHappy(c *C, reboot bool) {
	d := s.daemon(c)

	devicestateResealCalls := 0
	defer daemon.MockDevicestateReseal(func(st *state.State, doReboot bool) *state.Change {
		devicestateResealCalls++
		c.Check(doReboot, Equals, reboot)
		return st.NewChange("test", "test")
	})()

	d.Overlord().Loop()
	defer d.Overlord().Stop()

	buf := bytes.NewBufferString(fmt.Sprintf(`{"reboot":%v}`, reboot))
	req, err := http.NewRequest("POST", "/v2/system-reseal", buf)
	c.Assert(err, IsNil)
	rsp := s.asyncReq(c, req, nil)
	c.Check(rsp.Status, Equals, 202)
	c.Check(devicestateResealCalls, Equals, 1)
}

func (s *resealSuite) TestResealRebootHappy(c *C) {
	s.testResealHappy(c, true)
}

func (s *resealSuite) TestResealNoRebootHappy(c *C) {
	s.testResealHappy(c, false)
}
