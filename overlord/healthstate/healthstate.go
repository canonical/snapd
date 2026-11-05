// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package healthstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/strutil"
)

var checkTimeout = 30 * time.Second

func init() {
	if s, ok := os.LookupEnv("SNAPD_CHECK_HEALTH_HOOK_TIMEOUT"); ok {
		if to, err := time.ParseDuration(s); err == nil {
			checkTimeout = to
		} else {
			logger.Debugf("cannot override check-health timeout: %v", err)
		}
	}

	snapstate.CheckHealthHook = Hook
}

func Hook(st *state.State, snapName string, snapRev snap.Revision) *state.Task {
	summary := fmt.Sprintf("Run health check of %q snap", snapName)
	hooksup := &hookstate.HookSetup{
		Snap:     snapName,
		Revision: snapRev,
		Hook:     "check-health",
		Optional: true,
		Timeout:  checkTimeout,
	}

	return hookstate.HookTask(st, summary, hooksup, nil)
}

type HealthStatus int

const (
	UnknownStatus = HealthStatus(iota)
	OkayStatus
	WaitingStatus
	BlockedStatus
	ErrorStatus
)

var knownStatuses = []string{"unknown", "okay", "waiting", "blocked", "error"}

func StatusLookup(str string) (HealthStatus, error) {
	for i, k := range knownStatuses {
		if k == str {
			return HealthStatus(i), nil
		}
	}
	return -1, fmt.Errorf("invalid status %q, must be one of %s", str, strutil.Quoted(knownStatuses))
}

func (s HealthStatus) String() string {
	if s < 0 || s >= HealthStatus(len(knownStatuses)) {
		return fmt.Sprintf("invalid (%d)", s)
	}
	return knownStatuses[s]
}

type HealthState struct {
	Revision  snap.Revision `json:"revision"`
	Timestamp time.Time     `json:"timestamp"`
	Status    HealthStatus  `json:"status"`
	Message   string        `json:"message,omitempty"`
	Code      string        `json:"code,omitempty"`
}

func Init(hookManager *hookstate.HookManager) {
	hookManager.Register(regexp.MustCompile("^check-health$"), newHealthHandler)
}

func newHealthHandler(ctx *hookstate.Context) hookstate.Handler {
	return &healthHandler{context: ctx}
}

type healthHandler struct {
	context *hookstate.Context
}

// Before is called just before the hook runs -- nothing to do beyond setting a marker
func (h *healthHandler) Before() error {
	// we use the 'health' entry as a marker to not add OnDone to
	// the snapctl set-health execution
	h.context.Lock()
	h.context.Set("health", struct{}{})
	h.context.Unlock()
	return nil
}

func (h *healthHandler) Done() error {
	var health HealthState

	h.context.Lock()
	err := h.context.Get("health", &health)
	h.context.Unlock()

	if err != nil && !errors.Is(err, state.ErrNoState) {
		// note it can't actually be state.ErrNoState because Before sets it
		// (but if it were, health.Timestamp would still be zero)
		return err
	}
	if health.Timestamp.IsZero() {
		// health was actually the marker (or errors.Is(err, state.ErrNoState))
		health = HealthState{
			Revision:  h.context.SnapRevision(),
			Timestamp: time.Now(),
			Status:    UnknownStatus,
			Code:      "snapd-hook-no-health-set",
			Message:   "hook did not call set-health",
		}
	}

	return h.appendHealth(&health)
}

func (h *healthHandler) Error(err error) (bool, error) {
	return false, h.appendHealth(&HealthState{
		Revision:  h.context.SnapRevision(),
		Timestamp: time.Now(),
		Status:    UnknownStatus,
		Code:      "snapd-hook-failed",
		Message:   "hook failed",
	})
}

func (h *healthHandler) appendHealth(health *HealthState) error {
	st := h.context.State()
	st.Lock()
	defer st.Unlock()

	return appendHealth(h.context, health)
}

func appendHealth(ctx *hookstate.Context, health *HealthState) error {
	st := ctx.State()

	var hs map[string]*HealthState
	if err := st.Get("health", &hs); err != nil {
		if !errors.Is(err, state.ErrNoState) {
			return err
		}
		hs = map[string]*HealthState{}
	}
	instanceName := ctx.InstanceName().String()
	if health.Revision.Unset() {
		// hooks of snaps installed from a file, and snapctl invoked
		// outside of a hook, do not know the revision; record the
		// current one so that the health can be told apart from that
		// of other revisions
		var snapst snapstate.SnapState
		if err := snapstate.Get(st, instanceName, &snapst); err != nil && !errors.Is(err, state.ErrNoState) {
			return err
		}
		health.Revision = snapst.Current
	}
	hs[instanceName] = health
	st.Set("health", hs)

	return nil
}

// SetFromHookContext extracts the health of a snap from a hook
// context, and saves it in snapd's state.
// Must be called with the context lock held.
func SetFromHookContext(ctx *hookstate.Context) error {
	var health HealthState
	err := ctx.Get("health", &health)

	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return nil
		}
		return err
	}
	return appendHealth(ctx, &health)
}

// isStale returns true if the health refers to a revision other than the
// current revision of the snap, for example because the check-health hook
// ran for a revision whose installation or refresh was later undone. It is
// also stale if it was recorded before the current revision was installed
// or refreshed to: a local revision number is reused by the next local
// install after an undone one, so the revision alone does not identify it.
func isStale(health *HealthState, snapst *snapstate.SnapState) bool {
	if health.Revision.Unset() {
		// health recorded without a revision, keep it
		return false
	}
	if snapst == nil || health.Revision != snapst.Current {
		return true
	}
	return snapst.LastRefreshTime != nil && health.Timestamp.Before(*snapst.LastRefreshTime)
}

// All returns the health of all snaps, omitting the health recorded for
// revisions that are not current.
func All(st *state.State) (map[string]*HealthState, error) {
	var hs map[string]*HealthState
	if err := st.Get("health", &hs); err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if len(hs) == 0 {
		return hs, nil
	}
	snapStates, err := snapstate.All(st)
	if err != nil {
		return nil, err
	}
	for name, health := range hs {
		if health == nil {
			continue
		}
		if isStale(health, snapStates[naming.InstanceName(name)]) {
			delete(hs, name)
		}
	}
	return hs, nil
}

func Get(st *state.State, snap string) (*HealthState, error) {
	var hs map[string]json.RawMessage
	if err := st.Get("health", &hs); err != nil {
		if !errors.Is(err, state.ErrNoState) {
			return nil, err
		}
		return nil, nil
	}

	buf := hs[snap]
	if len(buf) == 0 {
		return nil, nil
	}

	var health HealthState
	if err := json.Unmarshal(buf, &health); err != nil {
		return nil, err
	}

	snapst := &snapstate.SnapState{}
	if err := snapstate.Get(st, snap, snapst); err != nil {
		if !errors.Is(err, state.ErrNoState) {
			return nil, err
		}
		snapst = nil
	}
	if isStale(&health, snapst) {
		return nil, nil
	}

	return &health, nil
}
