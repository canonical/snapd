// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2024 Canonical Ltd
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
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/snapcore/snapd/features"
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
	// Monitored signals whether this snap is currently being monitored for closure
	// so its auto-refresh can be continued.
	Monitored bool `json:"monitored,omitempty"`
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

func (rc *refreshCandidate) Prereq(*state.State, PrereqTracker) []string {
	return rc.SnapSetup.Prereq
}

func (rc *refreshCandidate) SnapSetupForUpdate(st *state.State, _ updateParamsFunc, _ int, globalFlags *Flags, _ PrereqTracker) (*SnapSetup, *SnapState, error) {
	var snapst SnapState
	if err := Get(st, rc.InstanceName(), &snapst); err != nil {
		return nil, nil, err
	}

	snapsup := &rc.SnapSetup
	if globalFlags != nil {
		snapsup.Flags.IsAutoRefresh = globalFlags.IsAutoRefresh
		snapsup.Flags.IsContinuedAutoRefresh = globalFlags.IsContinuedAutoRefresh
	}

	return snapsup, &snapst, nil
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
	return effectiveRefreshHold(m.state)
}

func effectiveRefreshHold(st *state.State) (time.Time, error) {
	var holdValue string

	tr := config.NewTransaction(st)
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
		holdTime, err := effectiveRefreshHold(m.state)
		if err != nil {
			return err
		}
		if !holdTime.IsZero() {
			// already set
			return nil
		}
		// TODO: have a policy that if the snapd exe itself
		// is older than X weeks/months we skip the holding?
		now := time.Now().UTC()
		tr := config.NewTransaction(m.state)
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

func isStoreOnline(s *state.State) (bool, error) {
	tr := config.NewTransaction(s)

	var access string
	if err := tr.GetMaybe("core", "store.access", &access); err != nil {
		return false, err
	}

	return access != "offline", nil
}

// Ensure ensures that we refresh all installed snaps periodically
func (m *autoRefresh) Ensure() (err error) {
	m.state.Lock()
	defer m.state.Unlock()

	online, err := isStoreOnline(m.state)
	if err != nil || !online {
		return err
	}

	if err := m.restoreMonitoring(); err != nil {
		return fmt.Errorf("cannot restore monitoring: %v", err)
	}

	// see if it even makes sense to try to refresh
	if CanAutoRefresh == nil {
		return nil
	}
	if ok, err := CanAutoRefresh(m.state); err != nil || !ok {
		return err
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

			err = m.launchAutoRefresh()
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

func (m *autoRefresh) restoreMonitoring() error {
	if m.restoredMonitoring {
		return nil
	}

	// clear the old monitoring state; remove in a later snapd version
	m.state.Set("monitored-snaps", nil)

	var refreshHints map[string]*refreshCandidate
	if err := m.state.Get("refresh-candidates", &refreshHints); err != nil && !errors.Is(err, &state.NoStateError{}) {
		return fmt.Errorf("cannot get refresh-candidates: %v", err)
	}

	defer func() { m.restoredMonitoring = true }()

	var monitored []*SnapSetup
	for _, hint := range refreshHints {
		if hint.Monitored {
			monitored = append(monitored, &hint.SnapSetup)
		}
	}

	if len(monitored) == 0 {
		return nil
	}

	aborts := make(map[string]context.CancelFunc, len(monitored))
	for _, snap := range monitored {
		done := make(chan string, 1)
		snapName := snap.InstanceName()
		if err := cgroupMonitorSnapEnded(snapName, done); err != nil {
			logger.Noticef("cannot restore monitoring for snap %q closure: %v", snapName, err)
			continue
		}

		refreshCtx, abort := context.WithCancel(context.Background())
		aborts[snapName] = abort
		go continueRefreshOnSnapClose(m.state, snap.InstanceName(), done, refreshCtx)
	}

	m.state.Cache("monitored-snaps", aborts)
	return nil
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
func (m *autoRefresh) launchAutoRefresh() error {
	// Check that we have reasonable delays between attempts.
	// If the store is under stress we need to make sure we do not
	// hammer it too often
	now := timeNow()
	minAttempt := m.lastRefreshAttempt.Add(refreshRetryDelay)
	if !m.lastRefreshAttempt.IsZero() && minAttempt.After(now) {
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
	updated, updateTss, err := AutoRefresh(auth.EnsureContextTODO(), m.state)

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

	if _, err = createPreDownloadChange(m.state, updateTss); err != nil {
		return err
	}

	if len(updateTss.Refresh) == 0 {
		// NOTE: If all refresh candidates are blocked from auto-refresh by checks
		// in softCheckNothingRunningForRefresh then no auto-refresh change will be
		// created (i.e. len(updateTss.Refresh) == 0) and only a pre-download change
		// is created for those snaps. This still means that the set of inhibited
		// snaps could have changed so we are recording a notice about it here
		// because it cannot be captured in processInhibitedAutoRefresh which only
		// looks for auto-refresh changes.
		return maybeAddRefreshInhibitNotice(m.state)
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
		if chg.Kind() == "auto-refresh" && !chg.IsReady() {
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
var asyncPendingRefreshNotification = func(ctx context.Context, refreshInfo *userclient.PendingSnapRefreshInfo) {
	logger.Debugf("notifying agents about pending refresh for snap %q", refreshInfo.InstanceName)

	go func() {
		client := userclient.New()
		if err := client.PendingRefreshNotification(ctx, refreshInfo); err != nil {
			logger.Noticef("Cannot send notification about pending refresh: %v", err)
		}
	}()
}

// maybeAsyncPendingRefreshNotification broadcasts desktop notification in a goroutine.
//
// The notification is sent only if no snap has the marker "snap-refresh-observe"
// interface connected and the "refresh-app-awareness-ux" experimental flag is disabled.
func maybeAsyncPendingRefreshNotification(ctx context.Context, st *state.State, refreshInfo *userclient.PendingSnapRefreshInfo) {

	sendNotification, err := ShouldSendNotificationsToTheUser(st)
	if err != nil {
		logger.Noticef("Cannot send notification about pending refresh: %v", err)
		return
	}
	if !sendNotification {
		return
	}
	asyncPendingRefreshNotification(ctx, refreshInfo)
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

// inhibitRefresh returns whether a refresh is forced due to inhibition
// timeout or an error if refresh is inhibited by running apps.
//
// Internally the snap state is updated to remember when the inhibition first
// took place. Apps can inhibit refreshes for up to "maxInhibition", beyond
// that period the refresh will go ahead despite application activity.
func inhibitRefresh(st *state.State, snapst *SnapState, snapsup *SnapSetup, info *snap.Info) (inhibitionTimeout bool, err error) {
	checkerErr := refreshAppsCheck(info)
	if checkerErr == nil {
		return false, nil
	}

	// carries the remaining inhibition time along with the BusySnapError
	busyErr := &timedBusySnapError{}

	// if it's not a snap busy error or the refresh is manual, surface the error
	// to the user instead of notifying or delaying the refresh
	if !snapsup.IsAutoRefresh || !errors.As(checkerErr, &busyErr.err) {
		return false, checkerErr
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
		// XXX: should we drop this notification?
		// if the refresh inhibition window has ended, notify the user that the
		// refresh is happening now and ignore the error
		refreshInfo := busyErr.PendingSnapRefreshInfo()
		maybeAsyncPendingRefreshNotification(context.TODO(), st, refreshInfo)
		// important to return "nil" type here instead of
		// setting busyErr to nil as otherwise we return a nil
		// interface which is not the nil type
		return true, nil
	}

	return false, busyErr
}

// IsSnapMonitored checks if there's already a goroutine waiting for this snap to close.
func IsSnapMonitored(st *state.State, snapName string) bool {
	return monitoringAbort(st, snapName) != nil
}

func processInhibitedAutoRefresh(chg *state.Change, old state.Status, new state.Status) {
	if chg.Kind() != "auto-refresh" || !new.Ready() {
		return
	}

	if err := maybeAddRefreshInhibitNotice(chg.State()); err != nil {
		logger.Debugf(`internal error: failed to add "refresh-inhibit" notice: %v`, err)
	}
}

// maybeAddRefreshInhibitNotice records a refresh-inhibit notice if the set of
// inhibited snaps was changed since the last notice.
func maybeAddRefreshInhibitNotice(st *state.State) error {
	var lastRecordedInhibitedSnaps map[string]bool
	if err := st.Get("last-recorded-inhibited-snaps", &lastRecordedInhibitedSnaps); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	snapStates, err := All(st)
	if err != nil {
		return err
	}

	curInhibitedSnaps := make(map[string]bool, len(lastRecordedInhibitedSnaps))
	for _, snapst := range snapStates {
		if snapst.RefreshInhibitedTime == nil {
			continue
		}
		curInhibitedSnaps[snapst.InstanceName()] = true
	}

	changed := len(lastRecordedInhibitedSnaps) != len(curInhibitedSnaps)
	if !changed {
		for snapName := range curInhibitedSnaps {
			if !lastRecordedInhibitedSnaps[snapName] {
				changed = true
				break
			}
		}
	}

	if changed {
		if _, err := st.AddNotice(nil, state.RefreshInhibitNotice, "-", nil); err != nil {
			return err
		}
		st.Set("last-recorded-inhibited-snaps", curInhibitedSnaps)
	}

	if err := maybeAddRefreshInhibitWarningFallback(st, curInhibitedSnaps); err != nil {
		logger.Noticef("Cannot add refresh inhibition warning: %v", err)
	}

	return nil
}

// maybeAddRefreshInhibitWarningFallback records a warning if the set of
// inhibited snaps was changed since the last notice.
//
// The warning is recorded only if:
//  1. There is at least 1 inhibited snap.
//  2. The "refresh-app-awareness-ux" experimental flag is enabled.
//  3. No snap exists with the marker "snap-refresh-observe" interface connected.
//
// Note: If no snaps are inhibited then existing inhibition warning
// will be removed.
func maybeAddRefreshInhibitWarningFallback(st *state.State, inhibitedSnaps map[string]bool) error {
	if len(inhibitedSnaps) == 0 {
		// no more inhibited snaps, remove inhibition warning if it exists.
		return removeRefreshInhibitWarning(st)
	}

	tr := config.NewTransaction(st)
	experimentalRefreshAppAwarenessUX, err := features.Flag(tr, features.RefreshAppAwarenessUX)
	if err != nil && !config.IsNoOption(err) {
		return err
	}
	if !experimentalRefreshAppAwarenessUX {
		// snapd will send notifications directly, check maybeAsyncPendingRefreshNotification
		return nil
	}

	markerExists, err := HasActiveConnection(st, "snap-refresh-observe")
	if err != nil {
		return err
	}
	if markerExists {
		// do nothing
		return nil
	}

	// let's fallback to issuing warnings if no snap exists with the
	// marker snap-refresh-observe interface connected.

	// remove inhibition warning if it exists.
	if err := removeRefreshInhibitWarning(st); err != nil {
		return err
	}

	// building warning message
	var snapsBuf bytes.Buffer
	i := 0
	for snap := range inhibitedSnaps {
		if i > 0 {
			snapsBuf.WriteString(", ")
		}
		snapsBuf.WriteString(snap)
		i++
	}
	message := fmt.Sprintf("cannot refresh (%s) due running apps; close running apps to continue refresh.", snapsBuf.String())

	// wait some time before showing the same warning to the user again after okaying.
	st.AddWarning(message, &state.AddWarningOptions{RepeatAfter: 24 * time.Hour})

	return nil
}

// removeRefreshInhibitWarning removes inhibition warning if it exists.
func removeRefreshInhibitWarning(st *state.State) error {
	// XXX: is it worth it to check for unexpected multiple matches?
	for _, warning := range st.AllWarnings() {
		if !strings.HasSuffix(warning.String(), "close running apps to continue refresh.") {
			continue
		}
		if err := st.RemoveWarning(warning.String()); err != nil && !errors.Is(err, state.ErrNoState) {
			return err
		}
		return nil
	}
	return nil
}

// for testing outside of snapstate
func MockRefreshCandidate(snapSetup *SnapSetup) interface{} {
	return &refreshCandidate{
		SnapSetup: *snapSetup,
	}
}
