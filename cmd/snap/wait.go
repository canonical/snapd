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

package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/progress"
)

var (
	maxGoneTime = 5 * time.Second
	pollTime    = 100 * time.Millisecond
)

type waitMixin struct {
	clientMixin
	NoWait    bool `long:"no-wait"`
	skipAbort bool

	// Wait also for tasks in the "wait" state.
	waitForTasksInWaitStatus bool
}

var waitDescs = mixinDescs{
	// TRANSLATORS: This should not start with a lowercase letter.
	"no-wait": i18n.G("Do not wait for the operation to finish but just print the change id."),
}

var noWait = errors.New("no wait for op")

func (wmx waitMixin) wait(id string) (*client.Change, error) {
	if wmx.NoWait {
		fmt.Fprintf(Stdout, "%s\n", id)
		return nil, noWait
	}
	cli := wmx.client
	// Intercept sigint
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt)
	go func() {
		sig := <-c
		// sig is nil if c was closed
		if sig == nil || wmx.skipAbort {
			return
		}
		_, err := wmx.client.Abort(id)
		if err != nil {
			fmt.Fprintf(Stderr, err.Error()+"\n")
		}
	}()

	pb := progress.MakeProgressBar(Stdout)
	defer func() {
		pb.Finished()
		// next two not strictly needed for CLI, but without
		// them the tests will leak goroutines.
		signal.Stop(c)
		close(c)
	}()

	tMax := time.Time{}

	var lastID string
	lastLog := map[string]string{}
	for {
		var rebootingErr error
		chg, err := cli.Change(id)
		if err != nil {
			// a client.Error means we were able to communicate with
			// the server (got an answer)
			if e, ok := err.(*client.Error); ok {
				return nil, e
			}

			// A non-client error here means the server most likely went away.
			// First thing we should check is whether this is a part of a system restart,
			// as in that case we want to to report this to user instead of looping here until
			// the restart does happen. (Or in the case of spread tests, blocks forever).
			if e, ok := cli.Maintenance().(*client.Error); ok && e.Kind == client.ErrorKindSystemRestart {
				return nil, e
			}

			// Otherwise it's most likely a daemon restart, assume it might come up again.
			// XXX: it actually can be a bunch of other things; fix client to expose it better
			now := time.Now()
			if tMax.IsZero() {
				tMax = now.Add(maxGoneTime)
			}
			if now.After(tMax) {
				return nil, err
			}
			pb.Spin(i18n.G("Waiting for server to restart"))
			time.Sleep(pollTime)
			continue
		}
		if maintErr, ok := cli.Maintenance().(*client.Error); ok && maintErr.Kind == client.ErrorKindSystemRestart {
			rebootingErr = maintErr
		}
		if !tMax.IsZero() {
			pb.Finished()
			tMax = time.Time{}
		}

		maybeShowLog := func(t *client.Task) {
			nowLog := lastLogStr(t.Log)
			if lastLog[t.ID] != nowLog {
				pb.Notify(nowLog)
				lastLog[t.ID] = nowLog
			}
		}

		// Tasks in "wait" state communicate the wait reason
		// via the log mechanism. So make sure the log is
		// visible even if the normal progress reporting
		// has tasks in "Doing" state (like "check-refresh")
		// that would suppress displaying the log. This will
		// ensure on a classic+modes system the user sees
		// the messages: "Task set to wait until a manual system restart allows to continue"
		for _, t := range chg.Tasks {
			if t.Status == "Wait" {
				maybeShowLog(t)
			}
		}

		// progress reporting
		for _, t := range chg.Tasks {
			switch {
			case t.Status != "Doing" && t.Status != "Wait":
				continue
			case t.Progress.Total == 1:
				pb.Spin(t.Summary)
				maybeShowLog(t)
			case t.ID == lastID:
				pb.Set(float64(t.Progress.Done))
			default:
				pb.Start(t.Summary, float64(t.Progress.Total))
				lastID = t.ID
			}
			break
		}

		if !wmx.waitForTasksInWaitStatus && chg.Status == "Wait" {
			return chg, nil
		}

		if chg.Ready {
			if chg.Status == "Done" {
				return chg, nil
			}

			if chg.Err != "" {
				return chg, errors.New(chg.Err)
			}

			return nil, fmt.Errorf(i18n.G("change finished in status %q with no error message"), chg.Status)
		}

		if rebootingErr != nil {
			return nil, rebootingErr
		}

		// note this very purposely is not a ticker; we want
		// to sleep 100ms between calls, not call once every
		// 100ms.
		time.Sleep(pollTime)
	}
}

func lastLogStr(logs []string) string {
	if len(logs) == 0 {
		return ""
	}
	return logs[len(logs)-1]
}
