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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

var checkTimeout = 30 * time.Second

func init() {
	if s, ok := os.LookupEnv("SNAPD_CHECK_HEALTH_HOOK_TIMEOUT"); ok {
		if to := mylog.Check2(time.ParseDuration(s)); err == nil {
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
	mylog.Check(h.context.Get("health", &health))
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
	mylog.Check(st.Get("health", &hs))

	hs[ctx.InstanceName()] = health
	st.Set("health", hs)

	return nil
}

// SetFromHookContext extracts the health of a snap from a hook
// context, and saves it in snapd's state.
// Must be called with the context lock held.
func SetFromHookContext(ctx *hookstate.Context) error {
	var health HealthState
	mylog.Check(ctx.Get("health", &health))

	return appendHealth(ctx, &health)
}

func All(st *state.State) (map[string]*HealthState, error) {
	var hs map[string]*HealthState
	if mylog.Check(st.Get("health", &hs)); err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	return hs, nil
}

func Get(st *state.State, snap string) (*HealthState, error) {
	var hs map[string]json.RawMessage
	mylog.Check(st.Get("health", &hs))

	buf := hs[snap]
	if len(buf) == 0 {
		return nil, nil
	}

	var health HealthState
	mylog.Check(json.Unmarshal(buf, &health))

	return &health, nil
}
