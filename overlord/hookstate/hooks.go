/*
 * Copyright (C) 2017 Canonical Ltd
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
	"sort"
	"time"

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
	experimentalRefreshAppAwareness, err := features.Flag(tr, features.RefreshAppAwareness)
	if err != nil && !config.IsNoOption(err) {
		return err
	}
	if !experimentalRefreshAppAwareness {
		return nil
	}

	h.refreshAppAwareness = true

	snapName := h.context.InstanceName()

	// obtain snap lock before manipulating runinhibit lock.
	lock, err := snaplock.OpenLock(snapName)
	if err != nil {
		return err
	}
	if err := lock.Lock(); err != nil {
		return err
	}
	defer lock.Unlock()

	if err := runinhibit.LockWithHint(snapName, runinhibit.HintInhibitedGateRefresh); err != nil {
		return err
	}

	return nil
}

func (h *gateAutoRefreshHookHandler) Done() (err error) {
	ctx := h.context
	st := ctx.State()
	ctx.Lock()
	defer ctx.Unlock()

	snapName := h.context.InstanceName()

	var action snapstate.GateAutoRefreshAction
	a := ctx.Cached("action")

	// obtain snap lock before manipulating runinhibit lock.
	var lock *osutil.FileLock
	if h.refreshAppAwareness {
		lock, err = snaplock.OpenLock(snapName)
		if err != nil {
			return err
		}
		if err := lock.Lock(); err != nil {
			return err
		}
		defer lock.Unlock()
	}

	// default behavior if action is not set
	if a == nil {
		// action is not set if the gate-auto-refresh hook exits 0 without
		// invoking --hold/--proceed; this means proceed (except for respecting
		// refresh inhibit).
		if h.refreshAppAwareness {
			if err := runinhibit.Unlock(snapName); err != nil {
				return fmt.Errorf("cannot unlock inhibit lock for snap %s: %v", snapName, err)
			}
		}
		return snapstate.ProceedWithRefresh(st, snapName)
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
			if err := runinhibit.Unlock(snapName); err != nil {
				return fmt.Errorf("cannot unlock inhibit lock of snap %s: %v", snapName, err)
			}
		}
	case snapstate.GateAutoRefreshProceed:
		// for action=proceed the ctlcmd doesn't call ProceedWithRefresh
		// immediately, do it here.
		if err := snapstate.ProceedWithRefresh(st, snapName); err != nil {
			return err
		}
		if h.refreshAppAwareness {
			// we have HintInhibitedGateRefresh lock already when running the hook,
			// change it to HintInhibitedForRefresh.
			if err := runinhibit.LockWithHint(snapName, runinhibit.HintInhibitedForRefresh); err != nil {
				return fmt.Errorf("cannot set inhibit lock for snap %s: %v", snapName, err)
			}
		}
	default:
		return fmt.Errorf("internal error: unexpected action %v", action)
	}

	return nil
}

// Error handles gate-auto-refresh hook failure; it assumes hold.
func (h *gateAutoRefreshHookHandler) Error(hookErr error) (err error) {
	ctx := h.context
	st := h.context.State()
	ctx.Lock()
	defer ctx.Unlock()

	snapName := h.context.InstanceName()

	var lock *osutil.FileLock

	// the refresh is going to be held, release runinhibit lock.
	if h.refreshAppAwareness {
		// obtain snap lock before manipulating runinhibit lock.
		lock, err = snaplock.OpenLock(snapName)
		if err != nil {
			return err
		}
		if err := lock.Lock(); err != nil {
			return err
		}
		defer lock.Unlock()

		if err := runinhibit.Unlock(snapName); err != nil {
			return fmt.Errorf("cannot release inhibit lock of snap %s: %v", snapName, err)
		}
	}

	if a := ctx.Cached("action"); a != nil {
		action, ok := a.(snapstate.GateAutoRefreshAction)
		if !ok {
			return fmt.Errorf("internal error: unexpected action type %T", a)
		}
		// nothing to do if the hook already requested hold.
		if action == snapstate.GateAutoRefreshHold {
			return nil
		}
	}

	// the hook didn't request --hold, or it was --proceed. since the hook
	// errored out, assume hold.

	var affecting []string
	if err := ctx.Get("affecting-snaps", &affecting); err != nil {
		return fmt.Errorf("internal error: cannot get affecting-snaps")
	}

	// no duration specified, use maximum allowed for this gating snap.
	var holdDuration time.Duration
	if err := snapstate.HoldRefresh(st, snapName, holdDuration, affecting...); err != nil {
		// note, previous hook error (hookErr) is going to be logged by hookmgr
		// after this Error() handler, so only log the new error.
		h.context.Errorf("error: %v (while handling previous hook error)", err)
		if _, ok := err.(*snapstate.HoldError); ok {
			return nil
		}
		// anything other than HoldError becomes an error of the handler.
		return err
	}

	// TODO: consider assigning a special health state for the snap.

	return nil
}

func NewGateAutoRefreshHookHandler(context *Context) *gateAutoRefreshHookHandler {
	return &gateAutoRefreshHookHandler{
		context: context,
	}
}

func SetupGateAutoRefreshHook(st *state.State, snapName string, base, restart bool, affectingSnaps map[string]bool) *state.Task {
	hookSup := &HookSetup{
		Snap:     snapName,
		Hook:     "gate-auto-refresh",
		Optional: true,
	}
	affecting := make([]string, 0, len(affectingSnaps))
	for sn := range affectingSnaps {
		affecting = append(affecting, sn)
	}
	sort.Strings(affecting)
	summary := fmt.Sprintf(i18n.G("Run hook %s of snap %q"), hookSup.Hook, hookSup.Snap)
	hookCtx := map[string]interface{}{
		"base":            base,
		"restart":         restart,
		"affecting-snaps": affecting,
	}
	task := HookTask(st, summary, hookSup, hookCtx)
	return task
}

type snapHookHandler struct {
}

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
