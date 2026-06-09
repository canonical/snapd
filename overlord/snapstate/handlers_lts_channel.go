// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package snapstate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/ltschannel"
)

// ltsChannelInjectionKey is the check-lts-channel task data key for
// ltsChannelInjectionSetup. Written after the first injection so retries wait
// for that work instead of calling updateManyFiltered again.
const ltsChannelInjectionKey = "lts-channel-injection-setup"

// ltsChannelInjectionSetup records switch+refresh tasks added to the change by
// doCheckLTSChannel. The handler retries until every TaskID is Done.
type ltsChannelInjectionSetup struct {
	TaskIDs []string `json:"task-ids,omitempty"`
}

var ltsChannelUpdateMany = updateManyFiltered

var ltsChannelRetryTimeout = time.Second / 2

// appendCheckLTSChannelAtEndOfSnapdRefresh appends a check-lts-channel task at
// the end of snapd's refresh spine, after post-link work (including
// check-health). The check runs under the new snapd from link-snap's daemon
// restart and may extend the change with switch+refresh on the LTS channel.
// Currently used only on snapd refresh (not first install); see DESIGN.md open
// questions on whether first install should also get this task.
//
// Must be called before lanes are joined and before cross-snap ordering is
// arranged so the new task is part of both and becomes finalSnapdTask.
func appendCheckLTSChannelAtEndOfSnapdRefresh(st *state.State, sts *snapInstallTaskSet) error {
	if sts.snapsup.Type != snap.TypeSnapd {
		return fmt.Errorf("internal error: cannot add LTS channel check to %q task set: not the snapd snap", sts.snapsup.InstanceName())
	}

	setupTask := sts.ts.MaybeEdge(SnapSetupEdge)
	if setupTask == nil {
		return errors.New("internal error: cannot add LTS channel check: no snap-setup task in snapd's update task set")
	}

	if len(sts.afterLinkSnapAndPostReboot) == 0 {
		return errors.New("internal error: cannot add LTS channel check: no post link-snap tasks in snapd's update task set")
	}

	postLinkTail := sts.afterLinkSnapAndPostReboot[len(sts.afterLinkSnapAndPostReboot)-1]

	check := st.NewTask("check-lts-channel", i18n.G("Check snapd channel and switch to its LTS channel if required"))
	check.Set("snap-setup-task", setupTask.ID())
	check.WaitFor(postLinkTail)

	sts.ts.AddTask(check)
	sts.afterLinkSnapAndPostReboot = append(sts.afterLinkSnapAndPostReboot, check)

	return nil
}

// doCheckLTSChannel is the check-lts-channel task handler. It runs at the end
// of snapd refresh changes, after post-link work under the new snapd. If LTS
// channel policy applies and snapd's tracking channel is wrong, it adds
// switch+refresh tasks to the same change and waits for them before completing.
//
// Older snapd without this handler treats the task as unknown: the overlord
// optional handler logs and no-ops it (task still reaches Done). LTS switching
// then relies on ensureSnapdTrackTransition or a later refresh cycle.
func (m *SnapManager) doCheckLTSChannel(t *state.Task, tomb *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	if restart.PendingForChangeTasks(st, t.Change(), nil) {
		return restart.TaskWaitForRestart(t)
	}

	deviceCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}
	model := deviceCtx.Model()
	if model == nil {
		return nil
	}

	_, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	if !snapst.IsInstalled() {
		return fmt.Errorf("internal error: cannot check LTS channel: snapd not installed")
	}
	sideInfo := snapst.CurrentSideInfo()
	if sideInfo == nil || sideInfo.SnapID == "" {
		// unasserted snapd: track lockdown not implemented yet
		return nil
	}

	required, err := ltschannel.SnapdLTSChannel(model, snapst.TrackingChannel)
	if err != nil {
		return err
	}
	if required == snapst.TrackingChannel {
		t.Logf("snapd tracking channel %q is already the LTS channel", snapst.TrackingChannel)
		return nil
	}

	var setup ltsChannelInjectionSetup
	err = t.Get(ltsChannelInjectionKey, &setup)
	if errors.Is(err, state.ErrNoState) {
		return m.injectLTSChannelSwitchRefresh(t, tomb, required)
	}
	if err != nil {
		return err
	}

	return waitForLTSChannelInjection(t, setup)
}

func (m *SnapManager) injectLTSChannelSwitchRefresh(t *state.Task, tomb *tomb.Tomb, channel string) error {
	chg := t.Change()

	ctx := context.Background()
	if tomb != nil {
		ctx = tomb.Context(nil)
	}

	flags := &Flags{NoReRefresh: true}

	st := t.State()
	st.Unlock()
	_, updateTss, err := ltsChannelUpdateMany(ctx, st, []string{"snapd"}, []*RevisionOptions{{Channel: channel}}, 0, nil, flags, chg.ID())
	st.Lock()
	if err != nil {
		return err
	}

	if len(updateTss.Refresh) == 0 {
		return fmt.Errorf("internal error: cannot switch snapd to LTS channel %q: no refresh tasks produced", channel)
	}

	taskIDs := addTaskSetsToChange(chg, t, updateTss.Refresh)

	t.Set(ltsChannelInjectionKey, ltsChannelInjectionSetup{TaskIDs: taskIDs})
	t.Logf("switching snapd tracking channel to %q", channel)
	st.EnsureBefore(0)

	return &state.Retry{After: ltsChannelRetryTimeout, Reason: "waiting for LTS channel switch refresh"}
}

func waitForLTSChannelInjection(t *state.Task, setup ltsChannelInjectionSetup) error {
	st := t.State()
	for _, id := range setup.TaskIDs {
		task := st.Task(id)
		if task == nil {
			return fmt.Errorf("internal error: LTS channel injection tasks pruned")
		}
		switch task.Status() {
		case state.DoneStatus:
			continue
		case state.ErrorStatus:
			// Failure scope (whole change vs switch leg only) is undecided; see
			// DESIGN.md open questions. Today this fails check-lts-channel.
			return fmt.Errorf("cannot switch snapd to LTS channel: task %q failed", task.Kind())
		default:
			return &state.Retry{After: ltsChannelRetryTimeout, Reason: "waiting for LTS channel switch refresh"}
		}
	}
	return nil
}
