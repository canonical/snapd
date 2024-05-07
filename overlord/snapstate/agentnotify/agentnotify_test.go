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

	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/agentnotify"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	userclient "github.com/snapcore/snapd/usersession/client"
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

	var callCount int
	r := agentnotify.MockMaybeSendClientFinishRefreshNotification(func(st *state.State, snapsup *snapstate.SnapSetup) {
		c.Check(snapsup.InstanceName(), Equals, "some-snap")
		callCount++
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
		callCount = 0
		snapstate.Set(s.st, "some-snap", &snapstate.SnapState{
			Active: tc.active,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
				RealName: "some-snap", Revision: snap.R(1)},
			}),
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
		c.Check(callCount, Equals, tc.expectedCallCount)
	}
}

func (s *agentNotifySuite) TestMaybeAsyncFinishedRefreshNotification(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	var connCheckCalled int
	restore := agentnotify.MockHasActiveConnection(func(st *state.State, iface string) (bool, error) {
		connCheckCalled++
		c.Check(iface, Equals, "snap-refresh-observe")
		// no snap has the marker interface connected
		return false, nil
	})
	defer restore()

	sendInfo := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "pkg"},
	}
	expectedInfo := userclient.FinishedSnapRefreshInfo{
		InstanceName: "pkg",
	}
	notificationCalled := 0
	restore = agentnotify.MockAsyncFinishRefreshNotification(func(info *userclient.FinishedSnapRefreshInfo) {
		notificationCalled++
		c.Check(info.InstanceName, Equals, expectedInfo.InstanceName)
	})
	defer restore()

	tr := config.NewTransaction(s.st)
	tr.Set("core", "experimental.refresh-app-awareness-ux", true)
	tr.Commit()

	agentnotify.MaybeSendClientFinishedRefreshNotification(s.st, sendInfo)
	// no notification as refresh-appawareness-ux is enabled
	// i.e. notices + warnings fallback is used instead
	c.Check(connCheckCalled, Equals, 0)
	c.Check(notificationCalled, Equals, 0)

	tr.Set("core", "experimental.refresh-app-awareness-ux", false)
	tr.Commit()

	agentnotify.MaybeSendClientFinishedRefreshNotification(s.st, sendInfo)
	// notification sent as refresh-appawareness-ux is now disabled
	c.Check(connCheckCalled, Equals, 1)
	c.Check(notificationCalled, Equals, 1)
}

func (s *agentNotifySuite) TestMaybeAsyncFinishedRefreshNotificationSkips(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	var connCheckCalled int
	restore := agentnotify.MockHasActiveConnection(func(st *state.State, iface string) (bool, error) {
		connCheckCalled++
		c.Check(iface, Equals, "snap-refresh-observe")
		// marker interface found
		return true, nil
	})
	defer restore()

	restore = agentnotify.MockAsyncFinishRefreshNotification(func(info *userclient.FinishedSnapRefreshInfo) {
		c.Fatal("shouldn't trigger Finished refresh notification because marker interface is connected")
	})
	defer restore()

	agentnotify.MaybeSendClientFinishedRefreshNotification(s.st, &snapstate.SnapSetup{})
}
