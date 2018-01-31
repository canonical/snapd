// -*- Mode: Go; indent-tabs-mode: t -*-

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

package snapstate

import (
	"fmt"
	"time"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timeutil"
)

// the default refresh pattern
const defaultRefreshSchedule = "00:00-24:00/4"

// hooks setup by devicestate
var (
	CanAutoRefresh     func(st *state.State) (bool, error)
	CanManageRefreshes func(st *state.State) bool
)

// refreshRetryDelay specified the minimum time to retry failed refreshes
var refreshRetryDelay = 10 * time.Minute

// autoRefresh will ensure that snaps are refreshed automatically
// according to the refresh schedule.
type autoRefresh struct {
	state *state.State

	lastRefreshSchedule string
	nextRefresh         time.Time
	lastRefreshAttempt  time.Time
}

func newAutoRefresh(st *state.State) *autoRefresh {
	return &autoRefresh{
		state: st,
	}
}

// RefreshSchedule will return a user visible string with the current schedule
// for the automatic refreshes and a flag indicating whether the schedule is a
// legacy one.
func (m *autoRefresh) RefreshSchedule() (schedule string, legacy bool, err error) {
	_, schedule, legacy, err = m.refreshScheduleWithDefaultsFallback()
	return schedule, legacy, err
}

// NextRefresh returns when the next automatic refresh will happen.
func (m *autoRefresh) NextRefresh() time.Time {
	return m.nextRefresh
}

// LastRefresh returns when the last refresh happened.
func (m *autoRefresh) LastRefresh() (time.Time, error) {
	var lastRefresh time.Time
	err := m.state.Get("last-refresh", &lastRefresh)
	if err != nil && err != state.ErrNoState {
		return time.Time{}, err
	}
	return lastRefresh, nil
}

// Ensure ensures that we refresh all installed snaps periodically
func (m *autoRefresh) Ensure() error {
	m.state.Lock()
	defer m.state.Unlock()

	// see if it even makes sense to try to refresh
	if CanAutoRefresh == nil {
		return nil
	}
	if ok, err := CanAutoRefresh(m.state); err != nil || !ok {
		return err
	}

	// Check that we have reasonable delays between attempts.
	// If the store is under stress we need to make sure we do not
	// hammer it too often
	if !m.lastRefreshAttempt.IsZero() && m.lastRefreshAttempt.Add(refreshRetryDelay).After(time.Now()) {
		return nil
	}

	// get lastRefresh and schedule
	lastRefresh, err := m.LastRefresh()
	if err != nil {
		return err
	}

	refreshSchedule, refreshScheduleStr, _, err := m.refreshScheduleWithDefaultsFallback()
	if err != nil {
		return err
	}
	if len(refreshSchedule) == 0 {
		m.nextRefresh = time.Time{}
		return nil
	}
	// we already have a refresh time, check if we got a new config
	if !m.nextRefresh.IsZero() {
		if m.lastRefreshSchedule != refreshScheduleStr {
			// the refresh schedule has changed
			logger.Debugf("Refresh timer changed.")
			m.nextRefresh = time.Time{}
		}
	}
	m.lastRefreshSchedule = refreshScheduleStr

	// ensure nothing is in flight already
	if autoRefreshInFlight(m.state) {
		return nil
	}

	// compute next refresh attempt time (if needed)
	if m.nextRefresh.IsZero() {
		// store attempts in memory so that we can backoff
		if !lastRefresh.IsZero() {
			delta := timeutil.Next(refreshSchedule, lastRefresh)
			m.nextRefresh = time.Now().Add(delta)
		} else {
			// immediate
			m.nextRefresh = time.Now()
		}
		logger.Debugf("Next refresh scheduled for %s.", m.nextRefresh)
	}

	// do refresh attempt (if needed)
	if !m.nextRefresh.After(time.Now()) {
		err = m.launchAutoRefresh()
		// clear nextRefresh only if the refresh worked. There is
		// still the lastRefreshAttempt rate limit so things will
		// not go into a busy store loop
		if err == nil {
			m.nextRefresh = time.Time{}
		}
	}

	return err
}

// refreshScheduleWithDefaultsFallback returns the current refresh schedule
// and refresh string. When an invalid refresh schedule is set by the user
// the refresh schedule is automatically reset to the default.
//
// TODO: we can remove the refreshSchedule reset because we have validation
//       of the schedule now.
func (m *autoRefresh) refreshScheduleWithDefaultsFallback() (ts []*timeutil.Schedule, scheduleAsStr string, legacy bool, err error) {
	if refreshScheduleManaged(m.state) {
		if m.lastRefreshSchedule != "managed" {
			logger.Noticef("refresh.schedule is managed via the snapd-control interface")
			m.lastRefreshSchedule = "managed"
		}
		return nil, "managed", true, nil
	}

	tr := config.NewTransaction(m.state)

	// try the new refresh.timer config option first
	err = tr.Get("core", "refresh.timer", &scheduleAsStr)
	if err != nil && !config.IsNoOption(err) {
		return nil, "", false, err
	}
	if scheduleAsStr != "" {
		ts, err = timeutil.ParseSchedule(scheduleAsStr)
		if err != nil {
			logger.Noticef("cannot use refresh.timer configuration: %s", err)
			return refreshScheduleDefault()
		}
		return ts, scheduleAsStr, false, nil
	}

	// fallback to legacy refresh.schedule setting when the new
	// config option is not set
	err = tr.Get("core", "refresh.schedule", &scheduleAsStr)
	if err != nil && !config.IsNoOption(err) {
		return nil, "", false, err
	}
	if scheduleAsStr != "" {
		ts, err = timeutil.ParseLegacySchedule(scheduleAsStr)
		if err != nil {
			logger.Noticef("cannot use refresh.schedule configuration: %s", err)
			return refreshScheduleDefault()
		}
		return ts, scheduleAsStr, true, nil
	}

	return refreshScheduleDefault()
}

// launchAutoRefresh creates the auto-refresh taskset and a change for it.
func (m *autoRefresh) launchAutoRefresh() error {
	m.lastRefreshAttempt = time.Now()
	updated, tasksets, err := AutoRefresh(m.state)
	if err != nil {
		logger.Noticef("Cannot prepare auto-refresh change: %s", err)
		return err
	}

	// Set last refresh time only if the store (in AutoRefresh) gave
	// us no error.
	m.state.Set("last-refresh", time.Now())

	var msg string
	switch len(updated) {
	case 0:
		logger.Noticef(i18n.G("auto-refresh: all snaps are up-to-date"))
		return nil
	case 1:
		msg = fmt.Sprintf(i18n.G("Auto-refresh snap %q"), updated[0])
	case 2, 3:
		quoted := strutil.Quoted(updated)
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		msg = fmt.Sprintf(i18n.G("Auto-refresh snaps %s"), quoted)
	default:
		msg = fmt.Sprintf(i18n.G("Auto-refresh %d snaps"), len(updated))
	}

	chg := m.state.NewChange("auto-refresh", msg)
	for _, ts := range tasksets {
		chg.AddAll(ts)
	}
	chg.Set("snap-names", updated)
	chg.Set("api-data", map[string]interface{}{"snap-names": updated})

	return nil
}

func refreshScheduleDefault() (ts []*timeutil.Schedule, scheduleStr string, legacy bool, err error) {
	refreshSchedule, err := timeutil.ParseSchedule(defaultRefreshSchedule)
	if err != nil {
		panic(fmt.Sprintf("defaultRefreshSchedule cannot be parsed: %s", err))
	}

	return refreshSchedule, defaultRefreshSchedule, false, nil
}

func autoRefreshInFlight(st *state.State) bool {
	for _, chg := range st.Changes() {
		if chg.Kind() == "auto-refresh" && !chg.Status().Ready() {
			return true
		}
	}
	return false
}

// refreshScheduleManaged returns true if the refresh schedule of the
// device is managed by an external snap
func refreshScheduleManaged(st *state.State) bool {
	var refreshScheduleStr string

	// this will only be "nil" if running in tests
	if CanManageRefreshes == nil {
		return false
	}

	tr := config.NewTransaction(st)
	err := tr.Get("core", "refresh.schedule", &refreshScheduleStr)
	if err != nil {
		return false
	}
	if refreshScheduleStr != "managed" {
		return false
	}

	return CanManageRefreshes(st)
}
