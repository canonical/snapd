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

package main_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/godbus/dbus"
	snaprun "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/dbusutil/dbustest"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	usersessionclient "github.com/snapcore/snapd/usersession/client"
	. "gopkg.in/check.v1"
)

type fakeInhibitionFlow struct {
	start  func(ctx context.Context) error
	finish func(ctx context.Context) error
}

func (flow *fakeInhibitionFlow) StartInhibitionNotification(ctx context.Context) error {
	if flow.start == nil {
		return fmt.Errorf("StartInhibitionNotification is not implemented")
	}
	return flow.start(ctx)
}

func (flow *fakeInhibitionFlow) FinishInhibitionNotification(ctx context.Context) error {
	if flow.finish == nil {
		return fmt.Errorf("FinishInhibitionNotification is not implemented")
	}
	return flow.finish(ctx)
}

func (s *RunSuite) TestWaitWhileInhibitedRunThrough(c *C) {
	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{Revision: snap.R(11)})

	inhibitInfo := runinhibit.InhibitInfo{Previous: snap.R(11)}
	c.Assert(runinhibit.LockWithHint("snapname", runinhibit.HintInhibitedForRefresh, inhibitInfo), IsNil)

	var waitWhileInhibitedCalled int
	restore := snaprun.MockWaitWhileInhibited(func(ctx context.Context, snapName string, notInhibited func(ctx context.Context) error, inhibited func(ctx context.Context, hint runinhibit.Hint, inhibitInfo *runinhibit.InhibitInfo) (cont bool, err error), interval time.Duration) (flock *osutil.FileLock, retErr error) {
		waitWhileInhibitedCalled++

		c.Check(snapName, Equals, "snapname")
		c.Check(ctx, NotNil)
		for i := 0; i < 3; i++ {
			cont, err := inhibited(ctx, runinhibit.HintInhibitedForRefresh, &inhibitInfo)
			c.Assert(err, IsNil)
			// non-service apps should keep waiting
			c.Check(cont, Equals, false)
		}
		err := notInhibited(ctx)
		c.Assert(err, IsNil)

		flock, err = openHintFileLock(snapName)
		c.Assert(err, IsNil)
		err = flock.ReadLock()
		c.Assert(err, IsNil)
		return flock, nil
	})
	defer restore()

	var startCalled, finishCalled int
	inhibitionFlow := fakeInhibitionFlow{
		start: func(ctx context.Context) error {
			startCalled++
			return nil
		},
		finish: func(ctx context.Context) error {
			finishCalled++
			return nil
		},
	}
	restore = snaprun.MockInhibitionFlow(&inhibitionFlow)
	defer restore()

	info, app, hintLock, err := snaprun.WaitWhileInhibited(context.TODO(), "snapname", "app")
	defer hintLock.Unlock()
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "snapname")
	c.Check(app.Name, Equals, "app")

	c.Check(startCalled, Equals, 1)
	c.Check(finishCalled, Equals, 1)
	c.Check(waitWhileInhibitedCalled, Equals, 1)
	checkHintFileLocked(c, "snapname")
}

func (s *RunSuite) TestWaitWhileInhibitedErrorOnStartNotification(c *C) {
	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{Revision: snap.R(11)})

	inhibitInfo := runinhibit.InhibitInfo{Previous: snap.R(11)}
	c.Assert(runinhibit.LockWithHint("snapname", runinhibit.HintInhibitedForRefresh, inhibitInfo), IsNil)

	var startCalled, finishCalled int
	inhibitionFlow := fakeInhibitionFlow{
		start: func(ctx context.Context) error {
			startCalled++
			return fmt.Errorf("boom")
		},
		finish: func(ctx context.Context) error {
			finishCalled++
			return nil
		},
	}
	restore := snaprun.MockInhibitionFlow(&inhibitionFlow)
	defer restore()

	info, app, hintLock, err := snaprun.WaitWhileInhibited(context.TODO(), "snapname", "app")
	c.Assert(err, ErrorMatches, "boom")
	c.Check(info, IsNil)
	c.Check(app, IsNil)
	c.Check(hintLock, IsNil)

	c.Check(startCalled, Equals, 1)
	c.Check(finishCalled, Equals, 0)
	// lock must be released
	checkHintFileNotLocked(c, "snapname")
}

func (s *RunSuite) TestWaitWhileInhibitedErrorOnFinishNotification(c *C) {
	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{Revision: snap.R(11)})

	inhibitInfo := runinhibit.InhibitInfo{Previous: snap.R(11)}
	c.Assert(runinhibit.LockWithHint("snapname", runinhibit.HintInhibitedForRefresh, inhibitInfo), IsNil)

	var waitWhileInhibitedCalled int
	restore := snaprun.MockWaitWhileInhibited(func(ctx context.Context, snapName string, notInhibited func(ctx context.Context) error, inhibited func(ctx context.Context, hint runinhibit.Hint, inhibitInfo *runinhibit.InhibitInfo) (cont bool, err error), interval time.Duration) (flock *osutil.FileLock, retErr error) {
		waitWhileInhibitedCalled++

		c.Check(snapName, Equals, "snapname")
		c.Check(ctx, NotNil)
		for i := 0; i < 3; i++ {
			cont, err := inhibited(ctx, runinhibit.HintInhibitedForRefresh, &inhibitInfo)
			c.Assert(err, IsNil)
			// non-service apps should keep waiting
			c.Check(cont, Equals, false)
		}
		err := notInhibited(ctx)
		c.Assert(err, IsNil)

		flock, err = openHintFileLock(snapName)
		c.Assert(err, IsNil)
		err = flock.ReadLock()
		c.Assert(err, IsNil)
		return flock, nil
	})
	defer restore()

	var startCalled, finishCalled int
	inhibitionFlow := fakeInhibitionFlow{
		start: func(ctx context.Context) error {
			startCalled++
			return nil
		},
		finish: func(ctx context.Context) error {
			finishCalled++
			return fmt.Errorf("boom")
		},
	}
	restore = snaprun.MockInhibitionFlow(&inhibitionFlow)
	defer restore()

	info, app, hintLock, err := snaprun.WaitWhileInhibited(context.TODO(), "snapname", "app")
	c.Assert(err, ErrorMatches, "boom")
	c.Check(info, IsNil)
	c.Check(app, IsNil)
	c.Check(hintLock, IsNil)

	c.Check(startCalled, Equals, 1)
	c.Check(finishCalled, Equals, 1)
	c.Check(waitWhileInhibitedCalled, Equals, 1)
	// lock must be released
	checkHintFileNotLocked(c, "snapname")
}

func (s *RunSuite) TestWaitWhileInhibitedContextCancellationOnError(c *C) {
	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{Revision: snap.R(11)})

	inhibitInfo := runinhibit.InhibitInfo{Previous: snap.R(11)}
	c.Assert(runinhibit.LockWithHint("snapname", runinhibit.HintInhibitedForRefresh, inhibitInfo), IsNil)

	originalCtx, cancel := context.WithCancel(context.Background())
	inhibitionFlow := fakeInhibitionFlow{
		start: func(ctx context.Context) error {
			// check context is propagated properly
			c.Assert(ctx, Equals, originalCtx)
			c.Check(ctx.Err(), IsNil)
			// cancel context to trigger cancellation error
			cancel()
			return nil
		},
		finish: func(ctx context.Context) error {
			return fmt.Errorf("this should never be reached")
		},
	}
	restore := snaprun.MockInhibitionFlow(&inhibitionFlow)
	defer restore()

	_, _, _, err := snaprun.WaitWhileInhibited(originalCtx, "snapname", "app")
	c.Assert(err, ErrorMatches, "context canceled")
	c.Assert(errors.Is(err, context.Canceled), Equals, true)
	c.Assert(errors.Is(originalCtx.Err(), context.Canceled), Equals, true)
}

func (s *RunSuite) TestWaitWhileInhibitedGateRefreshNoNotification(c *C) {
	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{Revision: snap.R(11)})

	inhibitInfo := runinhibit.InhibitInfo{Previous: snap.R(11)}
	c.Assert(runinhibit.LockWithHint("snapname", runinhibit.HintInhibitedGateRefresh, inhibitInfo), IsNil)

	var called int
	restore := snaprun.MockWaitWhileInhibited(func(ctx context.Context, snapName string, notInhibited func(ctx context.Context) error, inhibited func(ctx context.Context, hint runinhibit.Hint, inhibitInfo *runinhibit.InhibitInfo) (cont bool, err error), interval time.Duration) (flock *osutil.FileLock, retErr error) {
		called++

		c.Check(snapName, Equals, "snapname")
		c.Check(ctx, NotNil)
		for i := 0; i < 3; i++ {
			cont, err := inhibited(ctx, runinhibit.HintInhibitedGateRefresh, &inhibitInfo)
			c.Assert(err, IsNil)
			// non-service apps should keep waiting
			c.Check(cont, Equals, false)
		}
		err := notInhibited(ctx)
		c.Assert(err, IsNil)

		flock, err = openHintFileLock(snapName)
		c.Assert(err, IsNil)
		err = flock.ReadLock()
		c.Assert(err, IsNil)
		return flock, nil
	})
	defer restore()

	inhibitionFlow := fakeInhibitionFlow{
		start: func(ctx context.Context) error {
			return fmt.Errorf("this should never be reached")
		},
		finish: func(ctx context.Context) error {
			return fmt.Errorf("this should never be reached")
		},
	}
	restore = snaprun.MockInhibitionFlow(&inhibitionFlow)
	defer restore()

	info, app, hintLock, err := snaprun.WaitWhileInhibited(context.TODO(), "snapname", "app")
	defer hintLock.Unlock()
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "snapname")
	c.Check(app.Name, Equals, "app")

	c.Check(called, Equals, 1)
	checkHintFileLocked(c, "snapname")
}

func (s *RunSuite) TestWaitWhileInhibitedNotInhibitedNoNotification(c *C) {
	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{Revision: snap.R(11)})

	inhibitionFlow := fakeInhibitionFlow{
		start: func(ctx context.Context) error {
			return fmt.Errorf("this should never be reached")
		},
		finish: func(ctx context.Context) error {
			return fmt.Errorf("this should never be reached")
		},
	}
	restore := snaprun.MockInhibitionFlow(&inhibitionFlow)
	defer restore()

	info, app, hintLock, err := snaprun.WaitWhileInhibited(context.TODO(), "snapname", "app")
	c.Assert(err, IsNil)
	c.Assert(hintLock, IsNil)
	c.Check(info.InstanceName(), Equals, "snapname")
	c.Check(app.Name, Equals, "app")

	c.Check(runinhibit.HintFile("snapname"), testutil.FileAbsent)
}

func (s *RunSuite) TestWaitWhileInhibitedNotInhibitHintFileOngoingRefresh(c *C) {
	inhibitionFlow := fakeInhibitionFlow{
		start: func(ctx context.Context) error {
			return fmt.Errorf("this should never be reached")
		},
		finish: func(ctx context.Context) error {
			return fmt.Errorf("this should never be reached")
		},
	}
	restore := snaprun.MockInhibitionFlow(&inhibitionFlow)
	defer restore()

	_, _, hintLock, err := snaprun.WaitWhileInhibited(context.TODO(), "snapname", "app")
	c.Assert(err, testutil.ErrorIs, snaprun.ErrSnapRefreshConflict)
	c.Assert(hintLock, IsNil)
}

func makeDBusMethodNotAvailableMessage(c *C, msg *dbus.Message) *dbus.Message {
	return &dbus.Message{
		Type: dbus.TypeError,
		Headers: map[dbus.HeaderField]dbus.Variant{
			dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
			dbus.FieldSender:      dbus.MakeVariant(":1"), // This does not matter.
			// dbus.FieldDestination is provided automatically by DBus test helper.
			dbus.FieldErrorName: dbus.MakeVariant("org.freedesktop.DBus.Error.UnknownMethod"),
		},
	}
}

func makeDBusMethodAvailableMessage(c *C, msg *dbus.Message) *dbus.Message {
	c.Assert(msg.Type, Equals, dbus.TypeMethodCall)
	c.Check(msg.Flags, Equals, dbus.Flags(0))

	c.Check(msg.Headers, DeepEquals, map[dbus.HeaderField]dbus.Variant{
		dbus.FieldDestination: dbus.MakeVariant("io.snapcraft.SnapDesktopIntegration"),
		dbus.FieldPath:        dbus.MakeVariant(dbus.ObjectPath("/io/snapcraft/SnapDesktopIntegration")),
		dbus.FieldInterface:   dbus.MakeVariant("io.snapcraft.SnapDesktopIntegration"),
		dbus.FieldMember:      dbus.MakeVariant("ApplicationIsBeingRefreshed"),
		dbus.FieldSignature:   dbus.MakeVariant(dbus.SignatureOf("", "", make(map[string]dbus.Variant))),
	})
	c.Check(msg.Body[0], Equals, "some-snap")
	param2 := fmt.Sprintf("%s", msg.Body[1])
	c.Check(strings.HasSuffix(param2, "/var/lib/snapd/inhibit/some-snap.lock"), Equals, true)
	return &dbus.Message{
		Type: dbus.TypeMethodReply,
		Headers: map[dbus.HeaderField]dbus.Variant{
			dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
			dbus.FieldSender:      dbus.MakeVariant(":1"), // This does not matter.
		},
	}
}

func (s *RunSuite) TestTextInhibitionFlow(c *C) {
	textFlow := snaprun.NewInhibitionFlow("snapname")

	c.Assert(textFlow.StartInhibitionNotification(context.TODO()), IsNil)
	c.Check(s.Stdout(), Equals, "snap package \"snapname\" is being refreshed, please wait\n")

	// Finish is a no-op
	c.Assert(textFlow.FinishInhibitionNotification(context.TODO()), IsNil)
	c.Check(s.Stdout(), Equals, "snap package \"snapname\" is being refreshed, please wait\n")

}

func (s *RunSuite) TestDesktopIntegrationInhibitionFlow(c *C) {
	// mock snapd-desktop-integration dbus available
	var dbusCalled int
	conn, _, err := dbustest.InjectableConnection(func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		dbusCalled++
		return []*dbus.Message{makeDBusMethodAvailableMessage(c, msg)}, nil
	})
	c.Assert(err, IsNil)

	restore := dbusutil.MockOnlySessionBusAvailable(conn)
	defer restore()

	// check that the normal graphical notification flow is not called
	restorePendingRefreshNotification := snaprun.MockPendingRefreshNotification(func(ctx context.Context, refreshInfo *usersessionclient.PendingSnapRefreshInfo) error {
		return fmt.Errorf("this should never be reached")
	})
	defer restorePendingRefreshNotification()
	restoreFinishRefreshNotification := snaprun.MockFinishRefreshNotification(func(ctx context.Context, refreshInfo *usersessionclient.FinishedSnapRefreshInfo) error {
		return fmt.Errorf("this should never be reached")
	})
	defer restoreFinishRefreshNotification()

	restoreIsGraphicalSession := snaprun.MockIsGraphicalSession(true)
	defer restoreIsGraphicalSession()

	graphicalFlow := snaprun.NewInhibitionFlow("some-snap")

	c.Assert(graphicalFlow.StartInhibitionNotification(context.TODO()), IsNil)
	c.Check(dbusCalled, Equals, 1)
	// check that text flow is not called
	c.Check(s.Stdout(), Equals, "")

	c.Assert(graphicalFlow.FinishInhibitionNotification(context.TODO()), IsNil)
	// Finish is a no-op, so dbus is only called once
	c.Check(dbusCalled, Equals, 1)
	// check that text flow is not called
	c.Check(s.Stdout(), Equals, "")
}

func (s *RunSuite) TestGraphicalSessionInhibitionFlow(c *C) {
	_, r := logger.MockLogger()
	defer r()

	// mock snapd-desktop-integration dbus available
	var dbusCalled int
	conn, _, err := dbustest.InjectableConnection(func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		dbusCalled++
		return []*dbus.Message{makeDBusMethodNotAvailableMessage(c, msg)}, nil
	})
	c.Assert(err, IsNil)

	restore := dbusutil.MockOnlySessionBusAvailable(conn)
	defer restore()

	// check that the normal graphical notification flow is called
	var pendingRefreshNotificationCalled int
	restorePendingRefreshNotification := snaprun.MockPendingRefreshNotification(func(ctx context.Context, refreshInfo *usersessionclient.PendingSnapRefreshInfo) error {
		pendingRefreshNotificationCalled++
		return nil
	})
	defer restorePendingRefreshNotification()
	var finishRefreshNotificationCalled int
	restoreFinishRefreshNotification := snaprun.MockFinishRefreshNotification(func(ctx context.Context, refreshInfo *usersessionclient.FinishedSnapRefreshInfo) error {
		finishRefreshNotificationCalled++
		return nil
	})
	defer restoreFinishRefreshNotification()

	restoreIsGraphicalSession := snaprun.MockIsGraphicalSession(true)
	defer restoreIsGraphicalSession()

	graphicalFlow := snaprun.NewInhibitionFlow("some-snap")

	c.Assert(graphicalFlow.StartInhibitionNotification(context.TODO()), IsNil)
	c.Check(pendingRefreshNotificationCalled, Equals, 1)
	c.Check(finishRefreshNotificationCalled, Equals, 0)
	// snapd-desktop-integration dbus is checked for availability
	c.Check(dbusCalled, Equals, 1)
	// check that text flow is not called
	c.Check(s.Stdout(), Equals, "")

	c.Assert(graphicalFlow.FinishInhibitionNotification(context.TODO()), IsNil)
	c.Check(pendingRefreshNotificationCalled, Equals, 1)
	c.Check(finishRefreshNotificationCalled, Equals, 1)
	// snapd-desktop-integration dbus is not checked on finish
	c.Check(dbusCalled, Equals, 1)
	// check that text flow is not called
	c.Check(s.Stdout(), Equals, "")
}

func (s *RunSuite) TestDesktopIntegrationNoDBus(c *C) {
	_, r := logger.MockLogger()
	defer r()

	noDBus := func() (*dbus.Conn, error) { return nil, fmt.Errorf("dbus not available") }
	restore := dbusutil.MockConnections(noDBus, noDBus)
	defer restore()

	sent := snaprun.TryNotifyRefreshViaSnapDesktopIntegrationFlow(context.TODO(), "Test")
	c.Assert(sent, Equals, false)
}
