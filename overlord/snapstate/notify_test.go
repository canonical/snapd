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

package snapstate_test

import (
	"context"
	"time"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	userclient "github.com/snapcore/snapd/usersession/client"
	. "gopkg.in/check.v1"
)

func createTmpTask(s *handlersSuite, summary string, name string, snapId string, revision int, change *state.Change) *state.Task {
	task := s.state.NewTask("link-snap", summary)
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: name,
			Revision: snap.R(revision),
			SnapID:   snapId,
		},
		Channel: "beta",
		UserID:  2,
	}
	task.Set("snap-setup", snapsup)
	task.SetStatus(state.DoStatus)
	change.AddTask(task)
	return task
}

func (s *handlersSuite) TestNotifyBeingRefreshed(c *C) {

	s.mockNBR() // restore notifyBeginRefresh
	defer func() {
		s.mockNBR = snapstate.MockNotifyBeginRefresh()
	}()
	workChannel := make(chan bool)
	var operation int
	var instanceName string
	var extraText string
	var changeId string
	var taskIds []string

	restore1 := snapstate.MockAsyncBeginDeferredRefreshNotification(func(context context.Context, client *userclient.Client, refreshInfo *userclient.BeginDeferredRefreshNotificationInfo) {
		operation = 1
		instanceName = refreshInfo.InstanceName
		extraText = ""
		changeId = refreshInfo.ChangeId
		taskIds = refreshInfo.TaskIDs
		workChannel <- true
	})
	defer restore1()

	operation = 0
	instanceName = ""
	extraText = ""

	s.state.Lock()
	change := s.state.NewChange("auto-refresh", "...")
	s.state.Unlock()
	go snapstate.NotifyBeginRefresh(s.state, "task3", snap.R(3), "task3", "")

	select {
	case <-workChannel:
		c.Fatal("Received an event")
	case <-time.After(1 * time.Second):
	}
	c.Assert(operation, Equals, 0)
	c.Assert(instanceName, Equals, "")
	c.Assert(extraText, Equals, "")
	s.state.Lock()
	t1 := createTmpTask(s, "summary1", "task3", "snapId", 3, change)
	t2 := createTmpTask(s, "summary2", "task3", "snapId", 3, change)
	t3 := createTmpTask(s, "summary3", "task3", "snapId", 3, change)
	s.state.Unlock()
	_ = <-workChannel
	c.Assert(operation, Equals, 1)
	c.Assert(instanceName, Equals, "task3")
	c.Assert(extraText, Equals, "")
	c.Assert(changeId, Equals, "1")
	c.Assert(len(taskIds), Equals, 3)

	s.state.Lock()
	t1.SetStatus(state.DoneStatus) // Done
	t2.SetStatus(state.DoneStatus) // Done
	t3.SetStatus(state.DoneStatus) // Done
	s.state.Unlock()

}
