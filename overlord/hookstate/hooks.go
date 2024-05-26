/*
 * Copyright (C) 2017-2022 Canonical Ltd
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

package hookstate

import (
	"fmt"
	"regexp"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/cmd/snaplock"
	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

func init() {
	snapstate.SetupInstallHook = SetupInstallHook
	snapstate.SetupPreRefreshHook = SetupPreRefreshHook
	snapstate.SetupPostRefreshHook = SetupPostRefreshHook
	snapstate.SetupRemoveHook = SetupRemoveHook
	snapstate.SetupGateAutoRefreshHook = SetupGateAutoRefreshHook
}

func SetupInstallHook(st *state.State, snapName string) *state.Task {
	hooksup := &HookSetup{
		Snap:     snapName,
		Hook:     "install",
		Optional: true,
	}

	summary := fmt.Sprintf(i18n.G("Run install hook of %q snap if present"), hooksup.Snap)
	task := HookTask(st, summary, hooksup, nil)

	return task
}

func SetupPostRefreshHook(st *state.State, snapName string) *state.Task {
	hooksup := &HookSetup{
		Snap:     snapName,
		Hook:     "post-refresh",
		Optional: true,
	}

	summary := fmt.Sprintf(i18n.G("Run post-refresh hook of %q snap if present"), hooksup.Snap)
	return HookTask(st, summary, hooksup, nil)
}

func SetupPreRefreshHook(st *state.State, snapName string) *state.Task {
	hooksup := &HookSetup{
		Snap:     snapName,
		Hook:     "pre-refresh",
		Optional: true,
	}

	summary := fmt.Sprintf(i18n.G("Run pre-refresh hook of %q snap if present"), hooksup.Snap)
	task := HookTask(st, summary, hooksup, nil)

	return task
}

type gateAutoRefreshHookHandler struct {
	context             *Context
	refreshAppAwareness bool
}

func (h *gateAutoRefreshHookHandler) Before() error {
	st := h.context.State()
	st.Lock()
	defer st.Unlock()

	tr := config.NewTransaction(st)
	experimentalRefreshAppAwareness := mylog.Check2(features.Flag(tr, features.RefreshAppAwareness))
	if err != nil && !config.IsNoOption(err) {
		return err
	}
	if !experimentalRefreshAppAwareness {
		return nil
	}

	h.refreshAppAwareness = true

	snapName := h.context.InstanceName()
	snapInfo := mylog.Check2(snapstate.CurrentInfo(st, snapName))

	snapRev := snapInfo.SnapRevision()

	// obtain snap lock before manipulating runinhibit lock.
	lock := mylog.Check2(snaplock.OpenLock(snapName))
	mylog.Check(lock.Lock())

	defer lock.Unlock()

	inhibitInfo := runinhibit.InhibitInfo{Previous: snapRev}
	mylog.Check(runinhibit.LockWithHint(snapName, runinhibit.HintInhibitedGateRefresh, inhibitInfo))

	return nil
}

func (h *gateAutoRefreshHookHandler) Done() (err error) {
	ctx := h.context
	st := ctx.State()
	ctx.Lock()
	defer ctx.Unlock()

	snapName := ctx.InstanceName()

	var action snapstate.GateAutoRefreshAction
	a := ctx.Cached("action")

	// obtain snap lock before manipulating runinhibit lock.
	var lock *osutil.FileLock
	if h.refreshAppAwareness {
		lock = mylog.Check2(snaplock.OpenLock(snapName))
		mylog.Check(lock.Lock())

		defer lock.Unlock()
	}

	// default behavior if action is not set
	if a == nil {
		// action is not set if the gate-auto-refresh hook exits 0 without
		// invoking --hold/--proceed; this means proceed (except for respecting
		// refresh inhibit).
		if h.refreshAppAwareness {
			mylog.Check(runinhibit.Unlock(snapName))
		}
		return snapstate.ProceedWithRefresh(st, snapName, nil)
	} else {
		var ok bool
		action, ok = a.(snapstate.GateAutoRefreshAction)
		if !ok {
			return fmt.Errorf("internal error: unexpected action type %T", a)
		}
	}

	// action is set if snapctl refresh --hold/--proceed was called from the hook.
	switch action {
	case snapstate.GateAutoRefreshHold:
		// for action=hold the ctlcmd calls HoldRefresh; only unlock runinhibit.
		if h.refreshAppAwareness {
			mylog.Check(runinhibit.Unlock(snapName))
		}
	case snapstate.GateAutoRefreshProceed:
		mylog.Check(
			// for action=proceed the ctlcmd doesn't call ProceedWithRefresh
			// immediately, do it here.
			snapstate.ProceedWithRefresh(st, snapName, nil))

		if h.refreshAppAwareness {
			// we have HintInhibitedGateRefresh lock already when running the hook, change
			// it to HintInhibitedForRefresh.
			// Also let's reuse inhibit info that was saved in Before().
			_, inhibitInfo := mylog.Check3(runinhibit.IsLocked(snapName))
			mylog.Check(runinhibit.LockWithHint(snapName, runinhibit.HintInhibitedForRefresh, inhibitInfo))

		}
	default:
		return fmt.Errorf("internal error: unexpected action %v", action)
	}

	return nil
}

// Error handles gate-auto-refresh hook failure; it assumes hold.
func (h *gateAutoRefreshHookHandler) Error(hookErr error) (ignoreHookErr bool, err error) {
	ctx := h.context
	st := h.context.State()
	ctx.Lock()
	defer ctx.Unlock()

	snapName := h.context.InstanceName()

	var lock *osutil.FileLock

	// the refresh is going to be held, release runinhibit lock.
	if h.refreshAppAwareness {
		// obtain snap lock before manipulating runinhibit lock.
		lock = mylog.Check2(snaplock.OpenLock(snapName))
		mylog.Check(lock.Lock())

		defer lock.Unlock()
		mylog.Check(runinhibit.Unlock(snapName))

	}

	if a := ctx.Cached("action"); a != nil {
		action, ok := a.(snapstate.GateAutoRefreshAction)
		if !ok {
			return false, fmt.Errorf("internal error: unexpected action type %T", a)
		}
		// nothing to do if the hook already requested hold.
		if action == snapstate.GateAutoRefreshHold {
			ctx.Errorf("ignoring hook error: %v", hookErr)
			// tell hook manager to ignore hook error.
			return true, nil
		}
	}

	// the hook didn't request --hold, or it was --proceed. since the hook
	// errored out, assume hold.

	affecting := mylog.Check2(snapstate.AffectingSnapsForAffectedByRefreshCandidates(st, snapName))

	// becomes error of the handler

	// no duration specified, use maximum allowed for this gating snap.
	var holdDuration time.Duration
	mylog.Check2(snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, snapName, holdDuration, affecting...))
	// log the original hook error as we either ignore it or error out from
	// this handler, in both cases hookErr won't be logged by hook manager.

	// TODO: consider delaying for another hour.

	// anything other than HoldError becomes an error of the handler.

	// TODO: consider assigning a special health state for the snap.

	ctx.Errorf("ignoring hook error: %v", hookErr)
	// tell hook manager to ignore hook error.
	return true, nil
}

func NewGateAutoRefreshHookHandler(context *Context) *gateAutoRefreshHookHandler {
	return &gateAutoRefreshHookHandler{
		context: context,
	}
}

func SetupGateAutoRefreshHook(st *state.State, snapName string) *state.Task {
	hookSup := &HookSetup{
		Snap:     snapName,
		Hook:     "gate-auto-refresh",
		Optional: true,
	}
	summary := fmt.Sprintf(i18n.G("Run hook %s of snap %q"), hookSup.Hook, hookSup.Snap)
	task := HookTask(st, summary, hookSup, nil)
	return task
}

type snapHookHandler struct{}

func (h *snapHookHandler) Before() error {
	return nil
}

func (h *snapHookHandler) Done() error {
	return nil
}

func (h *snapHookHandler) Error(err error) (bool, error) {
	return false, nil
}

func SetupRemoveHook(st *state.State, snapName string) *state.Task {
	hooksup := &HookSetup{
		Snap:        snapName,
		Hook:        "remove",
		Optional:    true,
		IgnoreError: true,
	}

	summary := fmt.Sprintf(i18n.G("Run remove hook of %q snap if present"), hooksup.Snap)
	task := HookTask(st, summary, hooksup, nil)

	return task
}

func setupHooks(hookMgr *HookManager) {
	handlerGenerator := func(context *Context) Handler {
		return &snapHookHandler{}
	}
	gateAutoRefreshHandlerGenerator := func(context *Context) Handler {
		return NewGateAutoRefreshHookHandler(context)
	}

	hookMgr.Register(regexp.MustCompile("^install$"), handlerGenerator)
	hookMgr.Register(regexp.MustCompile("^post-refresh$"), handlerGenerator)
	hookMgr.Register(regexp.MustCompile("^pre-refresh$"), handlerGenerator)
	hookMgr.Register(regexp.MustCompile("^remove$"), handlerGenerator)
	hookMgr.Register(regexp.MustCompile("^gate-auto-refresh$"), gateAutoRefreshHandlerGenerator)
}
