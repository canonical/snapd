// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package agentnotify_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/agentnotify"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func TestAgentNotify(t *testing.T) { TestingT(t) }

type agentNotifySuite struct {
	st *state.State
}

var _ = Suite(&agentNotifySuite{})

func (s *agentNotifySuite) SetUpTest(c *C) {
	s.st = state.New(nil)

}

func (s *agentNotifySuite) TestNotifyAgentOnLinkChange(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	var i int
	r := agentnotify.MockSendClientFinishRefreshNotification(func(snapsup *snapstate.SnapSetup) {
		c.Check(snapsup.InstanceName(), Equals, "some-snap")
		i++
	})
	defer r()

	for _, tc := range []struct {
		active                 bool
		isContinuedAutoRefresh bool
		expectedCallCount      int
	}{
		{false, false, 0},
		{false, true, 0},
		{true, false, 0},
		{true, true, 1},
	} {
		i = 0
		snapstate.Set(s.st, "some-snap", &snapstate.SnapState{
			Active: tc.active,
			Sequence: []*snap.SideInfo{{
				RealName: "some-snap", Revision: snap.R(1)},
			},
			Current: snap.R(1),
		})
		snapsup := &snapstate.SnapSetup{
			Flags: snapstate.Flags{IsContinuedAutoRefresh: tc.isContinuedAutoRefresh},
			SideInfo: &snap.SideInfo{
				RealName: "some-snap",
			},
		}
		err := agentnotify.NotifyAgentOnLinkageChange(s.st, snapsup)
		c.Assert(err, IsNil)
		c.Check(i, Equals, tc.expectedCallCount)
	}
}
