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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/snapcore/snapd/client"
	snaprun "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	"gopkg.in/check.v1"
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

	info, app, hintLock, err := snaprun.WaitWhileInhibited(context.TODO(), snaprun.Client(), "snapname", "app")
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

	info, app, hintLock, err := snaprun.WaitWhileInhibited(context.TODO(), snaprun.Client(), "snapname", "app")
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

	info, app, hintLock, err := snaprun.WaitWhileInhibited(context.TODO(), snaprun.Client(), "snapname", "app")
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

	_, _, _, err := snaprun.WaitWhileInhibited(originalCtx, snaprun.Client(), "snapname", "app")
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

	info, app, hintLock, err := snaprun.WaitWhileInhibited(context.TODO(), snaprun.Client(), "snapname", "app")
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

	info, app, hintLock, err := snaprun.WaitWhileInhibited(context.TODO(), snaprun.Client(), "snapname", "app")
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

	_, _, hintLock, err := snaprun.WaitWhileInhibited(context.TODO(), snaprun.Client(), "snapname", "app")
	c.Assert(err, testutil.ErrorIs, snaprun.ErrSnapRefreshConflict)
	c.Assert(hintLock, IsNil)
}

func (s *RunSuite) TestInhibitionFlow(c *C) {
	restore := snaprun.MockIsStdoutTTY(true)
	defer restore()

	var noticeCreated int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/connections":
			c.Assert(r.Method, check.Equals, "GET")
			c.Check(r.URL.Query(), check.DeepEquals, url.Values{"interface": []string{"snap-refresh-observe"}})
			body, err := io.ReadAll(r.Body)
			c.Assert(err, check.IsNil)
			c.Check(body, check.DeepEquals, []byte{})
			EncodeResponseBody(c, w, map[string]any{
				"type": "sync",
				"result": client.Connections{
					// mock snap exists with connected marker interface
					Established: []client.Connection{{Interface: "snap-refresh-observe"}},
				},
			})
		case "/v2/notices":
			noticeCreated++
			c.Assert(r.Method, check.Equals, "POST")
			body, err := io.ReadAll(r.Body)
			c.Assert(err, check.IsNil)
			var noticeRequest map[string]string
			c.Assert(json.Unmarshal(body, &noticeRequest), check.IsNil)
			c.Check(noticeRequest["action"], check.Equals, "add")
			c.Check(noticeRequest["type"], check.Equals, "snap-run-inhibit")
			c.Check(noticeRequest["key"], check.Equals, "some-snap")
			EncodeResponseBody(c, w, map[string]any{
				"type":   "sync",
				"result": map[string]string{"id": "1"},
			})
		default:
			c.Error("this should never be reached")
		}
	})

	graphicalFlow := snaprun.NewInhibitionFlow(snaprun.Client(), "some-snap")

	c.Assert(graphicalFlow.StartInhibitionNotification(context.TODO()), IsNil)
	// A snap-run-inhibit notice is always created
	c.Check(noticeCreated, check.Equals, 1)
	c.Check(s.Stderr(), Equals, "")

	c.Assert(graphicalFlow.FinishInhibitionNotification(context.TODO()), IsNil)
	// Finish is no-op, no new notices
	c.Check(noticeCreated, check.Equals, 1)
	c.Check(s.Stderr(), Equals, "")
}

func (s *RunSuite) testInhibitionFlowTextFallback(c *C, connectionsAPIErr bool) {
	restore := snaprun.MockIsStdoutTTY(true)
	defer restore()

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/connections":
			if connectionsAPIErr {
				w.WriteHeader(500)
				EncodeResponseBody(c, w, map[string]any{"type": "error"})
			} else {
				EncodeResponseBody(c, w, map[string]any{"type": "sync", "result": nil})

			}
		case "/v2/notices":
			EncodeResponseBody(c, w, map[string]any{"type": "sync", "result": map[string]string{"id": "1"}})
		default:
			c.Error("this should never be reached")
		}
	})

	graphicalFlow := snaprun.NewInhibitionFlow(snaprun.Client(), "some-snap")

	c.Assert(graphicalFlow.StartInhibitionNotification(context.TODO()), IsNil)
	c.Check(s.Stderr(), Equals, "snap package \"some-snap\" is being refreshed, please wait\n")

	c.Assert(graphicalFlow.FinishInhibitionNotification(context.TODO()), IsNil)
	// Finish is a noop
	c.Check(s.Stderr(), Equals, "snap package \"some-snap\" is being refreshed, please wait\n")
}

func (s *RunSuite) TestInhibitionFlowTextFallbackNoMarkerInterface(c *C) {
	const connectionsAPIErr = false
	s.testInhibitionFlowTextFallback(c, connectionsAPIErr)
}

func (s *RunSuite) TestInhibitionFlowTextFallbackConnectionsAPIError(c *C) {
	const connectionsAPIErr = true
	s.testInhibitionFlowTextFallback(c, connectionsAPIErr)
}

func (s *RunSuite) TestInhibitionFlowNoTTY(c *C) {
	restore := snaprun.MockIsStdoutTTY(false)
	defer restore()

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/connections":
			// No marker interface connected
			EncodeResponseBody(c, w, map[string]any{"type": "sync", "result": nil})
		case "/v2/notices":
			EncodeResponseBody(c, w, map[string]any{"type": "sync", "result": map[string]string{"id": "1"}})
		default:
			c.Error("this should never be reached")
		}
	})

	graphicalFlow := snaprun.NewInhibitionFlow(snaprun.Client(), "some-snap")

	c.Assert(graphicalFlow.StartInhibitionNotification(context.TODO()), IsNil)
	// No TTY, no text notification
	c.Check(s.Stderr(), Equals, "")

	c.Assert(graphicalFlow.FinishInhibitionNotification(context.TODO()), IsNil)
	// No TTY, no text notification
	c.Check(s.Stderr(), Equals, "")
}

func (s *RunSuite) TestInhibitionFlowError(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/notices":
			c.Assert(r.Method, check.Equals, "POST")
			w.WriteHeader(500)
			EncodeResponseBody(c, w, map[string]any{"type": "error"})
		default:
			c.Error("this should never be reached")
		}
	})

	graphicalFlow := snaprun.NewInhibitionFlow(snaprun.Client(), "some-snap")
	c.Assert(graphicalFlow.StartInhibitionNotification(context.TODO()), ErrorMatches, `server error: "Internal Server Error"`)
}
