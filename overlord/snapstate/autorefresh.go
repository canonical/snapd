// -*- Mode: Go; indent-tabs-mode: t -*-

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

package snapstate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timeutil"
	"github.com/snapcore/snapd/timings"
	userclient "github.com/snapcore/snapd/usersession/client"
)

// the default refresh pattern
const defaultRefreshScheduleStr = "00:00~24:00/4"

// cannot keep without refreshing for more than maxPostponement
const maxPostponement = 95 * 24 * time.Hour

// buffer for maxPostponement when holding snaps with auto-refresh gating
const maxPostponementBuffer = 5 * 24 * time.Hour

// cannot inhibit refreshes for more than maxInhibition;
// deduct 1s so it doesn't look confusing initially when two notifications
// get displayed in short period of time and it immediately goes from "14 days"
// to "13 days" left.
const maxInhibition = 14*24*time.Hour - time.Second

// maxDuration is used to represent "forever" internally (it's 290 years).
const maxDuration = time.Duration(1<<63 - 1)

// hooks setup by devicestate
var (
	CanAutoRefresh        func(st *state.State) (bool, error)
	CanManageRefreshes    func(st *state.State) bool
	IsOnMeteredConnection func() (bool, error)

	defaultRefreshSchedule = func() []*timeutil.Schedule {
		refreshSchedule, err := timeutil.ParseSchedule(defaultRefreshScheduleStr)
		if err != nil {
			panic(fmt.Sprintf("defaultRefreshSchedule cannot be parsed: %s", err))
		}
		return refreshSchedule
	}()
)

// refreshRetryDelay specified the minimum time to retry failed refreshes
var refreshRetryDelay = 20 * time.Minute

// refreshCandidate carries information about a single snap to update as part
// of auto-refresh.
type refreshCandidate struct {
	SnapSetup
	Version string `json:"version,omitempty"`
}

func (rc *refreshCandidate) Type() snap.Type {
	return rc.SnapSetup.Type
}

func (rc *refreshCandidate) SnapBase() string {
	return rc.SnapSetup.Base
}

func (rc *refreshCandidate) DownloadSize() int64 {
	return rc.DownloadInfo.Size
}

func (rc *refreshCandidate) InstanceName() string {
	return rc.SnapSetup.InstanceName()
}

func (rc *refreshCandidate) Prereq(st *state.State) []string {
	return rc.SnapSetup.Prereq
}

func (rc *refreshCandidate) SnapSetupForUpdate(st *state.State, params updateParamsFunc, userID int, globalFlags *Flags) (*SnapSetup, *SnapState, error) {
	var snapst SnapState
	if err := Get(st, rc.InstanceName(), &snapst); err != nil {
		return nil, nil, err
	}
	return &rc.SnapSetup, &snapst, nil
}

// soundness check
var _ readyUpdateInfo = (*refreshCandidate)(nil)

// autoRefresh will ensure that snaps are refreshed automatically
// according to the refresh schedule.
type autoRefresh struct {
	state *state.State

	lastRefreshSchedule string
	nextRefresh         time.Time
	lastRefreshAttempt  time.Time
	managedDeniedLogged bool

	restoredMonitoring bool
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
	return getTime(m.state, "last-refresh")
}

// EffectiveRefreshHold returns the time until to which refreshes are
// held if refresh.hold configuration is set.
func (m *autoRefresh) EffectiveRefreshHold() (time.Time, error) {
	var holdValue string

	tr := config.NewTransaction(m.state)
	err := tr.Get("core", "refresh.hold", &holdValue)
	if err != nil && !config.IsNoOption(err) {
		return time.Time{}, err
	}

	if holdValue == "forever" {
		return timeNow().Add(maxDuration), nil
	}

	var holdTime time.Time
	if holdValue != "" {
		if holdTime, err = time.Parse(time.RFC3339, holdValue); err != nil {
			return time.Time{}, err
		}
	}

	return holdTime, nil
}

func (m *autoRefresh) ensureRefreshHoldAtLeast(duration time.Duration) error {
	now := time.Now()

	// get the effective refresh hold and check if it is sooner than the
	// specified duration in the future
	effective, err := m.EffectiveRefreshHold()
	if err != nil {
		return err
	}

	if effective.IsZero() || effective.Sub(now) < duration {
		// the effective refresh hold is sooner than the desired delay, so
		// move it out to the specified duration
		holdTime := now.Add(duration)
		tr := config.NewTransaction(m.state)
		err := tr.Set("core", "refresh.hold", &holdTime)
		if err != nil && !config.IsNoOption(err) {
			return err
		}
		tr.Commit()
	}

	return nil
}

// clearRefreshHold clears refresh.hold configuration.
func (m *autoRefresh) clearRefreshHold() {
	tr := config.NewTransaction(m.state)
	tr.Set("core", "refresh.hold", nil)
	tr.Commit()
}

// AtSeed configures refresh policies at end of seeding.
func (m *autoRefresh) AtSeed() error {
	// on classic hold refreshes for 2h after seeding
	if release.OnClassic {
		var t1 time.Time
		tr := config.NewTransaction(m.state)
		err := tr.Get("core", "refresh.hold", &t1)
		if !config.IsNoOption(err) {
			// already set or error
			return err
		}
		// TODO: have a policy that if the snapd exe itself
		// is older than X weeks/months we skip the holding?
		now := time.Now().UTC()
		tr.Set("core", "refresh.hold", now.Add(2*time.Hour))
		tr.Commit()
		m.nextRefresh = now
	}
	return nil
}

func canRefreshOnMeteredConnection(st *state.State) (bool, error) {
	tr := config.NewTransaction(st)
	var onMetered string
	err := tr.GetMaybe("core", "refresh.metered", &onMetered)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return false, err
	}

	return onMetered != "hold", nil
}

func (m *autoRefresh) canRefreshRespectingMetered(now, lastRefresh time.Time) (can bool, err error) {
	can, err = canRefreshOnMeteredConnection(m.state)
	if err != nil {
		return false, err
	}
	if can {
		return true, nil
	}

	// ignore any errors that occurred while checking if we are on a metered
	// connection
	metered, _ := IsOnMeteredConnection()
	if !metered {
		return true, nil
	}

	if now.Sub(lastRefresh) >= maxPostponement {
		// TODO use warnings when the infra becomes available
		logger.Noticef("Auto refresh disabled while on metered connections, but pending for too long (%d days). Trying to refresh now.", int(maxPostponement.Hours()/24))
		return true, nil
	}

	logger.Debugf("Auto refresh disabled on metered connections")

	return false, nil
}

// Ensure ensures that we refresh all installed snaps periodically
func (m *autoRefresh) Ensure() (err error) {
	m.state.Lock()
	defer m.state.Unlock()

	m.restoreMonitoring()

	// see if it even makes sense to try to refresh
	if CanAutoRefresh == nil {
		return nil
	}
	if ok, err := CanAutoRefresh(m.state); err != nil || !ok {
		return err
	}

	// is there a previously partially inhibited auto-refresh that can now be continued?
	if attempt, ok := canContinueAutoRefresh(m.state); ok {
		// override the auto-refresh delay if we're continuing an inhibited auto-refresh
		// for the first time (because the snap just closed after we notified the user)
		overrideDelay := attempt == 1
		err := m.launchAutoRefresh(overrideDelay)
		if err != nil {
			if errors.Is(err, tooSoonError{}) {
				// ignore error, retry the auto-refresh later
				return nil
			}

			// we didn't auto-refresh, so keep flag but increase attempt counter
			m.state.Cache("auto-refresh-continue-attempt", attempt+1)
			return err
		}
		// clear the continue flag if the auto-refresh was scheduled successfully
		m.state.Cache("auto-refresh-continue-attempt", nil)
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

	now := time.Now()
	// compute next refresh attempt time (if needed)
	if m.nextRefresh.IsZero() {
		// store attempts in memory so that we can backoff
		if !lastRefresh.IsZero() {
			delta := timeutil.Next(refreshSchedule, lastRefresh, maxPostponement)
			now = time.Now()
			m.nextRefresh = now.Add(delta)
		} else {
			// make sure either seed-time or last-refresh
			// are set for hold code below
			m.ensureLastRefreshAnchor()
			// immediate
			m.nextRefresh = now
		}
		logger.Debugf("Next refresh scheduled for %s.", m.nextRefresh.Format(time.RFC3339))
	}

	held, holdTime, err := m.isRefreshHeld()
	if err != nil {
		return err
	}

	// do refresh attempt (if needed)
	if !held {
		if !holdTime.IsZero() {
			// expired hold case
			m.clearRefreshHold()
			if m.nextRefresh.Before(holdTime) {
				// next refresh is obsolete, compute the next one
				delta := timeutil.Next(refreshSchedule, holdTime, maxPostponement)
				now = time.Now()
				m.nextRefresh = now.Add(delta)
			}
		}

		// refresh is also "held" if the next time is in the future
		// note that the two times here could be exactly equal, so we use
		// !After() because that is true in the case that the next refresh is
		// before now, and the next refresh is equal to now without requiring an
		// or operation
		if !m.nextRefresh.After(now) {
			var can bool
			can, err = m.canRefreshRespectingMetered(now, lastRefresh)
			if err != nil {
				return err
			}
			if !can {
				// clear nextRefresh so that another refresh time is calculated
				m.nextRefresh = time.Time{}
				return nil
			}

			overrideDelay := false
			err = m.launchAutoRefresh(overrideDelay)
			if _, ok := err.(*httputil.PersistentNetworkError); ok {
				// refresh will be retried after refreshRetryDelay
				return err
			} else if errors.Is(err, tooSoonError{}) {
				// ignore error, retry the auto-refresh later
				return nil
			}

			// refreshed or hit an non-persistent network error, so reset nextRefresh
			m.nextRefresh = time.Time{}
		}
	}

	return err
}

func canContinueAutoRefresh(st *state.State) (int, bool) {
	if cachedAttempt := st.Cached("auto-refresh-continue-attempt"); cachedAttempt != nil {
		return cachedAttempt.(int), true
	}

	return 0, false
}

func (m *autoRefresh) restoreMonitoring() {
	if m.restoredMonitoring {
		return
	}

	var monitored []string
	if err := m.state.Get("monitored-snaps", &monitored); err != nil && !errors.Is(err, state.ErrNoState) {
		logger.Noticef("cannot restore monitoring: %v", err)
		return
	}

	defer func() { m.restoredMonitoring = true }()
	if len(monitored) == 0 {
		return
	}

	monitoring := make(map[string]chan<- bool)
	for _, snap := range monitored {
		done := make(chan string, 1)
		if err := cgroupMonitorSnapEnded(snap, done); err != nil {
			logger.Noticef("cannot restore monitoring for snap %q closure: %v", snap, err)
			continue
		}

		abort := make(chan bool, 1)
		monitoring[snap] = abort

		go continueRefreshOnSnapClose(m.state, snap, done, abort)
	}
	updateMonitoringState(m.state, monitoring)
}

// isRefreshHeld returns whether an auto-refresh is currently held back or not,
// as indicated by m.EffectiveRefreshHold().
func (m *autoRefresh) isRefreshHeld() (bool, time.Time, error) {
	now := time.Now()
	// should we hold back refreshes?
	holdTime, err := m.EffectiveRefreshHold()
	if err != nil {
		return false, time.Time{}, err
	}
	if holdTime.After(now) {
		return true, holdTime, nil
	}

	return false, holdTime, nil
}

func (m *autoRefresh) ensureLastRefreshAnchor() {
	seedTime, _ := getTime(m.state, "seed-time")
	if !seedTime.IsZero() {
		return
	}

	// last core refresh
	coreRefreshDate := snap.InstallDate("core")
	if !coreRefreshDate.IsZero() {
		m.state.Set("last-refresh", coreRefreshDate)
		return
	}

	// fallback to executable time
	st, err := os.Stat("/proc/self/exe")
	if err == nil {
		m.state.Set("last-refresh", st.ModTime())
		return
	}
}

func getRefreshScheduleConf(st *state.State) (confStr string, legacy bool, err error) {
	tr := config.NewTransaction(st)

	err = tr.Get("core", "refresh.timer", &confStr)
	if err != nil && !config.IsNoOption(err) {
		return "", false, err
	}

	// if not set, fallback to refresh.schedule
	if confStr == "" {
		if err := tr.Get("core", "refresh.schedule", &confStr); err != nil && !config.IsNoOption(err) {
			return "", false, err
		}
		legacy = true
	}

	return confStr, legacy, nil
}

// refreshScheduleWithDefaultsFallback returns the current refresh schedule
// and refresh string.
func (m *autoRefresh) refreshScheduleWithDefaultsFallback() (sched []*timeutil.Schedule, scheduleConf string, legacy bool, err error) {
	scheduleConf, legacy, err = getRefreshScheduleConf(m.state)
	if err != nil {
		return nil, "", false, err
	}

	// user requests refreshes to be managed by an external snap
	if scheduleConf == "managed" {
		if CanManageRefreshes == nil || !CanManageRefreshes(m.state) {
			// there's no snap to manage refreshes so use default schedule
			if !m.managedDeniedLogged {
				logger.Noticef("managed refresh schedule denied, no properly configured snapd-control")
				m.managedDeniedLogged = true
			}

			return defaultRefreshSchedule, defaultRefreshScheduleStr, false, nil
		}

		if m.lastRefreshSchedule != "managed" {
			logger.Noticef("refresh is managed via the snapd-control interface")
			m.lastRefreshSchedule = "managed"
		}
		m.managedDeniedLogged = false

		return nil, "managed", legacy, nil
	}
	m.managedDeniedLogged = false

	if scheduleConf == "" {
		return defaultRefreshSchedule, defaultRefreshScheduleStr, false, nil
	}

	// if we read the newer 'refresh.timer' option
	var errPrefix string
	if !legacy {
		sched, err = timeutil.ParseSchedule(scheduleConf)
		errPrefix = "cannot use refresh.timer configuration"
	} else {
		sched, err = timeutil.ParseLegacySchedule(scheduleConf)
		errPrefix = "cannot use refresh.schedule configuration"
	}

	if err != nil {
		// log instead of fail in order not to prevent auto-refreshes
		logger.Noticef("%s: %v", errPrefix, err)
		return defaultRefreshSchedule, defaultRefreshScheduleStr, false, nil
	}

	return sched, scheduleConf, legacy, nil
}

func autoRefreshSummary(updated []string) string {
	var msg string
	switch len(updated) {
	case 0:
		return ""
	case 1:
		msg = fmt.Sprintf(i18n.G("Auto-refresh snap %q"), updated[0])
	case 2, 3:
		quoted := strutil.Quoted(updated)
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		msg = fmt.Sprintf(i18n.G("Auto-refresh snaps %s"), quoted)
	default:
		msg = fmt.Sprintf(i18n.G("Auto-refresh %d snaps"), len(updated))
	}
	return msg
}

type tooSoonError struct{}

func (e tooSoonError) Error() string {
	return "cannot auto-refresh so soon"
}

func (tooSoonError) Is(err error) bool {
	_, ok := err.(tooSoonError)
	return ok
}

// launchAutoRefresh creates the auto-refresh taskset and a change for it.
func (m *autoRefresh) launchAutoRefresh(overrideDelay bool) error {
	// Check that we have reasonable delays between attempts.
	// If the store is under stress we need to make sure we do not
	// hammer it too often
	now := timeNow()
	minAttempt := m.lastRefreshAttempt.Add(refreshRetryDelay)
	if !overrideDelay && !m.lastRefreshAttempt.IsZero() && minAttempt.After(now) {
		return tooSoonError{}
	}
	m.lastRefreshAttempt = now

	perfTimings := timings.New(map[string]string{"ensure": "auto-refresh"})
	tm := perfTimings.StartSpan("auto-refresh", "query store and setup auto-refresh change")
	defer func() {
		tm.Stop()
		perfTimings.Save(m.state)
	}()

	// NOTE: this will unlock and re-lock state for network ops
	updated, updateTss, err := AutoRefresh(auth.EnsureContextTODO(), m.state, &AutoRefreshOptions{IsContinuedAutoRefresh: overrideDelay})

	// TODO: we should have some way to lock just creating and starting changes,
	//       as that would alleviate this race condition we are guarding against
	//       with this check and probably would eliminate other similar race
	//       conditions elsewhere

	// re-check if the refresh is held because it could have been re-held and
	// pushed back, in which case we need to abort the auto-refresh and wait
	held, _, holdErr := m.isRefreshHeld()
	if holdErr != nil {
		return holdErr
	}

	if held {
		// then a request came in that pushed the refresh out, so we will need
		// to try again later
		logger.Noticef("Auto-refresh was delayed mid-way through launching, aborting to try again later")
		return nil
	}

	if _, ok := err.(*httputil.PersistentNetworkError); ok {
		logger.Noticef("Cannot prepare auto-refresh change due to a permanent network error: %s", err)
		return err
	}
	m.state.Set("last-refresh", timeNow())
	if err != nil {
		logger.Noticef("Cannot prepare auto-refresh change: %s", err)
		return err
	}

	if _, err := createPreDownloadChange(m.state, updateTss); err != nil {
		return err
	}

	if len(updateTss.Refresh) == 0 {
		return nil
	}

	msg := autoRefreshSummary(updated)
	if msg == "" {
		logger.Noticef(i18n.G("auto-refresh: all snaps are up-to-date"))
		return nil
	}

	chg := m.state.NewChange("auto-refresh", msg)
	for _, ts := range updateTss.Refresh {
		chg.AddAll(ts)
	}
	chg.Set("snap-names", updated)
	chg.Set("api-data", map[string]interface{}{"snap-names": updated})
	state.TagTimingsWithChange(perfTimings, chg)

	return nil
}

// createPreDownloadChange creates a pre-download change if any relevant tasksets
// exist in the UpdateTaskSets and returns whether or not a change was created.
func createPreDownloadChange(st *state.State, updateTss *UpdateTaskSets) (bool, error) {
	if updateTss != nil && len(updateTss.PreDownload) > 0 {
		var snapNames []string
		for _, ts := range updateTss.PreDownload {
			task := ts.Tasks()[0]
			var snapsup *SnapSetup
			if err := task.Get("snap-setup", &snapsup); err != nil {
				return false, err
			}
			snapNames = append(snapNames, snapsup.InstanceName())
		}

		chgSummary := fmt.Sprintf(i18n.G("Pre-download %s for auto-refresh"), strutil.Quoted(snapNames))
		preDlChg := st.NewChange("pre-download", chgSummary)
		for _, ts := range updateTss.PreDownload {
			preDlChg.AddAll(ts)
		}
		return true, nil
	}
	return false, nil
}

func autoRefreshInFlight(st *state.State) bool {
	for _, chg := range st.Changes() {
		if chg.Kind() == "auto-refresh" && !chg.Status().Ready() {
			return true
		}
	}
	return false
}

// getTime retrieves a time from a state value.
func getTime(st *state.State, timeKey string) (time.Time, error) {
	var t1 time.Time
	err := st.Get(timeKey, &t1)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return time.Time{}, err
	}
	return t1, nil
}

// asyncPendingRefreshNotification broadcasts desktop notification in a goroutine.
//
// This allows the, possibly slow, communication with each snapd session agent,
// to be performed without holding the snap state lock.
var asyncPendingRefreshNotification = func(context context.Context, client *userclient.Client, refreshInfo *userclient.PendingSnapRefreshInfo) {
	logger.Debugf("notifying agents about pending refresh for snap %q", refreshInfo.InstanceName)
	go func() {
		if err := client.PendingRefreshNotification(context, refreshInfo); err != nil {
			logger.Noticef("Cannot send notification about pending refresh: %v", err)
		}
	}()
}

type timedBusySnapError struct {
	err           *BusySnapError
	timeRemaining time.Duration
}

func (e *timedBusySnapError) PendingSnapRefreshInfo() *userclient.PendingSnapRefreshInfo {
	refreshInfo := e.err.PendingSnapRefreshInfo()
	refreshInfo.TimeRemaining = e.timeRemaining
	return refreshInfo
}

func (e *timedBusySnapError) Error() string {
	return e.err.Error()
}

func (e *timedBusySnapError) Is(err error) bool {
	_, ok := err.(*timedBusySnapError)
	return ok
}

// inhibitRefresh returns an error if refresh is inhibited by running apps.
//
// Internally the snap state is updated to remember when the inhibition first
// took place. Apps can inhibit refreshes for up to "maxInhibition", beyond
// that period the refresh will go ahead despite application activity.
func inhibitRefresh(st *state.State, snapst *SnapState, snapsup *SnapSetup, info *snap.Info) error {
	checkerErr := refreshAppsCheck(info)
	if checkerErr == nil {
		return nil
	}

	// carries the remaining inhibition time along with the BusySnapError
	busyErr := &timedBusySnapError{}

	// if it's not a snap busy error or the refresh is manual, surface the error
	// to the user instead of notifying or delaying the refresh
	if !snapsup.IsAutoRefresh || !errors.As(checkerErr, &busyErr.err) {
		return checkerErr
	}

	// Decide on what to do depending on the state of the snap and the remaining
	// inhibition time.
	now := time.Now()
	switch {
	case snapst.RefreshInhibitedTime == nil:
		// If the snap did not have inhibited refresh yet then commence a new
		// window, during which refreshes are postponed, by storing the current
		// time in the snap state's RefreshInhibitedTime field. This field is
		// reset to nil on successful refresh.
		snapst.RefreshInhibitedTime = &now
		busyErr.timeRemaining = (maxInhibition - now.Sub(*snapst.RefreshInhibitedTime)).Truncate(time.Second)
		Set(st, info.InstanceName(), snapst)
	case now.Sub(*snapst.RefreshInhibitedTime) < maxInhibition:
		// If we are still in the allowed window then just return the error but
		// don't change the snap state again.
		// TODO: as time left shrinks, send additional notifications with
		// increasing frequency, allowing the user to understand the urgency.
		busyErr.timeRemaining = (maxInhibition - now.Sub(*snapst.RefreshInhibitedTime)).Truncate(time.Second)
	default:
		// if the refresh inhibition window has ended, notify the user that the
		// refresh is happening now and ignore the error
		refreshInfo := busyErr.PendingSnapRefreshInfo()
		asyncPendingRefreshNotification(context.TODO(), userclient.New(), refreshInfo)
		// important to return "nil" type here instead of
		// setting busyErr to nil as otherwise we return a nil
		// interface which is not the nil type
		return nil
	}

	return busyErr
}

// for testing outside of snapstate
func MockRefreshCandidate(snapSetup *SnapSetup, version string) interface{} {
	return &refreshCandidate{
		SnapSetup: *snapSetup,
		Version:   version,
	}
}
