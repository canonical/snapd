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

package snapstate

import (
	"errors"
	"fmt"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/errtracker"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timeutil"
)

// FIXME: what we actually want is a more flexible schedule spec that is
// user configurable  like:
// """
// tue
// tue,thu
// tue-thu
// 9:00
// 9:00,15:00
// 9:00-15:00
// tue,thu@9:00-15:00
// tue@9:00;thu@15:00
// mon,wed-fri@9:00-11:00,13:00-15:00
// """
// where 9:00 is implicitly taken as 9:00-10:00
// and tue is implicitly taken as tue@<our current setting?>
//
// it is controlled via:
// $ snap refresh --schedule=<time spec>
// which is a shorthand for
// $ snap set core refresh.schedule=<time spec>
// and we need to validate the time-spec, ideally internally by
// intercepting the set call

const defaultRefreshSchedule = "00:00-04:59/5:00-10:59/11:00-16:59/17:00-23:59"

// overridden in the tests
var errtrackerReport = errtracker.Report

// SnapManager is responsible for the installation and removal of snaps.
type SnapManager struct {
	state   *state.State
	backend managerBackend

	currentRefreshSchedule string
	nextRefresh            time.Time
	lastRefreshAttempt     time.Time

	lastUbuntuCoreTransitionAttempt time.Time

	runner *state.TaskRunner
}

// SnapSetup holds the necessary snap details to perform most snap manager tasks.
type SnapSetup struct {
	// FIXME: rename to RequestedChannel to convey the meaning better
	Channel string `json:"channel,omitempty"`
	UserID  int    `json:"user-id,omitempty"`

	Flags

	SnapPath string `json:"snap-path,omitempty"`

	DownloadInfo *snap.DownloadInfo `json:"download-info,omitempty"`
	SideInfo     *snap.SideInfo     `json:"side-info,omitempty"`
}

func (snapsup *SnapSetup) Name() string {
	if snapsup.SideInfo.RealName == "" {
		panic("SnapSetup.SideInfo.RealName not set")
	}
	return snapsup.SideInfo.RealName
}

func (snapsup *SnapSetup) Revision() snap.Revision {
	return snapsup.SideInfo.Revision
}

func (snapsup *SnapSetup) placeInfo() snap.PlaceInfo {
	return snap.MinimalPlaceInfo(snapsup.Name(), snapsup.Revision())
}

func (snapsup *SnapSetup) MountDir() string {
	return snap.MountDir(snapsup.Name(), snapsup.Revision())
}

func (snapsup *SnapSetup) MountFile() string {
	return snap.MountFile(snapsup.Name(), snapsup.Revision())
}

// SnapState holds the state for a snap installed in the system.
type SnapState struct {
	SnapType string           `json:"type"` // Use Type and SetType
	Sequence []*snap.SideInfo `json:"sequence"`
	Active   bool             `json:"active,omitempty"`
	// Current indicates the current active revision if Active is
	// true or the last active revision if Active is false
	// (usually while a snap is being operated on or disabled)
	Current snap.Revision `json:"current"`
	Channel string        `json:"channel,omitempty"`
	Flags
	// aliases, see aliasesv2.go
	Aliases             map[string]*AliasTarget `json:"aliases,omitempty"`
	AutoAliasesDisabled bool                    `json:"auto-aliases-disabled,omitempty"`
	AliasesPending      bool                    `json:"aliases-pending,omitempty"`
}

// Type returns the type of the snap or an error.
// Should never error if Current is not nil.
func (snapst *SnapState) Type() (snap.Type, error) {
	if snapst.SnapType == "" {
		return snap.Type(""), fmt.Errorf("snap type unset")
	}
	return snap.Type(snapst.SnapType), nil
}

// SetType records the type of the snap.
func (snapst *SnapState) SetType(typ snap.Type) {
	snapst.SnapType = string(typ)
}

// HasCurrent returns whether snapst.Current is set.
func (snapst *SnapState) HasCurrent() bool {
	if snapst.Current.Unset() {
		if len(snapst.Sequence) > 0 {
			panic(fmt.Sprintf("snapst.Current and snapst.Sequence out of sync: %#v %#v", snapst.Current, snapst.Sequence))
		}

		return false
	}
	return true
}

// LocalRevision returns the "latest" local revision. Local revisions
// start at -1 and are counted down.
func (snapst *SnapState) LocalRevision() snap.Revision {
	var local snap.Revision
	for _, si := range snapst.Sequence {
		if si.Revision.Local() && si.Revision.N < local.N {
			local = si.Revision
		}
	}
	return local
}

// TODO: unexport CurrentSideInfo and HasCurrent?

// CurrentSideInfo returns the side info for the revision indicated by snapst.Current in the snap revision sequence if there is one.
func (snapst *SnapState) CurrentSideInfo() *snap.SideInfo {
	if !snapst.HasCurrent() {
		return nil
	}
	if idx := snapst.LastIndex(snapst.Current); idx >= 0 {
		return snapst.Sequence[idx]
	}
	panic("cannot find snapst.Current in the snapst.Sequence")
}

func (snapst *SnapState) previousSideInfo() *snap.SideInfo {
	n := len(snapst.Sequence)
	if n < 2 {
		return nil
	}
	// find "current" and return the one before that
	currentIndex := snapst.LastIndex(snapst.Current)
	if currentIndex <= 0 {
		return nil
	}
	return snapst.Sequence[currentIndex-1]
}

// LastIndex returns the last index of the given revision in the
// snapst.Sequence
func (snapst *SnapState) LastIndex(revision snap.Revision) int {
	for i := len(snapst.Sequence) - 1; i >= 0; i-- {
		if snapst.Sequence[i].Revision == revision {
			return i
		}
	}
	return -1
}

// Block returns revisions that should be blocked on refreshes,
// computed from Sequence[currentRevisionIndex+1:].
func (snapst *SnapState) Block() []snap.Revision {
	// return revisions from Sequence[currentIndex:]
	currentIndex := snapst.LastIndex(snapst.Current)
	if currentIndex < 0 || currentIndex+1 == len(snapst.Sequence) {
		return nil
	}
	out := make([]snap.Revision, len(snapst.Sequence)-currentIndex-1)
	for i, si := range snapst.Sequence[currentIndex+1:] {
		out[i] = si.Revision
	}
	return out
}

var ErrNoCurrent = errors.New("snap has no current revision")

// Retrieval functions
var readInfo = readInfoAnyway

func readInfoAnyway(name string, si *snap.SideInfo) (*snap.Info, error) {
	info, err := snap.ReadInfo(name, si)
	if _, ok := err.(*snap.NotFoundError); ok {
		reason := fmt.Sprintf("cannot read snap %q: %s", name, err)
		info := &snap.Info{
			SuggestedName: name,
			Broken:        reason,
		}
		info.Apps = snap.GuessAppsForBroken(info)
		if si != nil {
			info.SideInfo = *si
		}
		return info, nil
	}
	return info, err
}

// CurrentInfo returns the information about the current active revision or the last active revision (if the snap is inactive). It returns the ErrNoCurrent error if snapst.Current is unset.
func (snapst *SnapState) CurrentInfo() (*snap.Info, error) {
	cur := snapst.CurrentSideInfo()
	if cur == nil {
		return nil, ErrNoCurrent
	}
	return readInfo(cur.RealName, cur)
}

func revisionInSequence(snapst *SnapState, needle snap.Revision) bool {
	for _, si := range snapst.Sequence {
		if si.Revision == needle {
			return true
		}
	}
	return false
}

type cachedStoreKey struct{}

// ReplaceStore replaces the store used by the manager.
func ReplaceStore(state *state.State, store StoreService) {
	state.Cache(cachedStoreKey{}, store)
}

func cachedStore(st *state.State) StoreService {
	ubuntuStore := st.Cached(cachedStoreKey{})
	if ubuntuStore == nil {
		return nil
	}
	return ubuntuStore.(StoreService)
}

// the store implementation has the interface consumed here
var _ StoreService = (*store.Store)(nil)

// Store returns the store service used by the snapstate package.
func Store(st *state.State) StoreService {
	if cachedStore := cachedStore(st); cachedStore != nil {
		return cachedStore
	}
	panic("internal error: needing the store before managers have initialized it")
}

// Manager returns a new snap manager.
func Manager(st *state.State) (*SnapManager, error) {
	runner := state.NewTaskRunner(st)

	m := &SnapManager{
		state:   st,
		backend: backend.Backend{},
		runner:  runner,
	}

	// this handler does nothing
	runner.AddHandler("nop", func(t *state.Task, _ *tomb.Tomb) error {
		return nil
	}, nil)

	// install/update related
	runner.AddHandler("prepare-snap", m.doPrepareSnap, m.undoPrepareSnap)
	runner.AddHandler("download-snap", m.doDownloadSnap, m.undoPrepareSnap)
	runner.AddHandler("mount-snap", m.doMountSnap, m.undoMountSnap)
	runner.AddHandler("unlink-current-snap", m.doUnlinkCurrentSnap, m.undoUnlinkCurrentSnap)
	runner.AddHandler("copy-snap-data", m.doCopySnapData, m.undoCopySnapData)
	runner.AddCleanup("copy-snap-data", m.cleanupCopySnapData)
	runner.AddHandler("link-snap", m.doLinkSnap, m.undoLinkSnap)
	runner.AddHandler("start-snap-services", m.startSnapServices, m.stopSnapServices)
	runner.AddHandler("switch-snap-channel", m.doSwitchSnapChannel, nil)

	// FIXME: drop the task entirely after a while
	// (having this wart here avoids yet-another-patch)
	runner.AddHandler("cleanup", func(*state.Task, *tomb.Tomb) error { return nil }, nil)

	// remove related
	runner.AddHandler("stop-snap-services", m.stopSnapServices, m.startSnapServices)
	runner.AddHandler("unlink-snap", m.doUnlinkSnap, nil)
	runner.AddHandler("clear-snap", m.doClearSnapData, nil)
	runner.AddHandler("discard-snap", m.doDiscardSnap, nil)

	// alias related
	// FIXME: drop the task entirely after a while
	runner.AddHandler("clear-aliases", func(*state.Task, *tomb.Tomb) error { return nil }, nil)
	runner.AddHandler("set-auto-aliases", m.doSetAutoAliasesV2, m.undoRefreshAliasesV2)
	runner.AddHandler("setup-aliases", m.doSetupAliasesV2, m.doRemoveAliasesV2)
	runner.AddHandler("refresh-aliases", m.doRefreshAliasesV2, m.undoRefreshAliasesV2)
	runner.AddHandler("prune-auto-aliases", m.doPruneAutoAliasesV2, m.undoRefreshAliasesV2)
	runner.AddHandler("remove-aliases", m.doRemoveAliasesV2, m.doSetupAliasesV2)
	runner.AddHandler("alias", m.doAliasV2, m.undoRefreshAliasesV2)
	runner.AddHandler("unalias", m.doUnaliasV2, m.undoRefreshAliasesV2)
	runner.AddHandler("disable-aliases", m.doDisableAliasesV2, m.undoRefreshAliasesV2)

	// control serialisation
	runner.SetBlocked(m.blockedTask)

	// test handlers
	runner.AddHandler("fake-install-snap", func(t *state.Task, _ *tomb.Tomb) error {
		return nil
	}, nil)
	runner.AddHandler("fake-install-snap-error", func(t *state.Task, _ *tomb.Tomb) error {
		return fmt.Errorf("fake-install-snap-error errored")
	}, nil)

	return m, nil
}

func (m *SnapManager) blockedTask(cand *state.Task, running []*state.Task) bool {
	return false
}

var CanAutoRefresh func(st *state.State) (bool, error)

func refreshScheduleNoWeekdays(rs []*timeutil.Schedule) error {
	for _, s := range rs {
		if s.Weekday != "" {
			return fmt.Errorf("%q uses weekdays which is currently not supported", s)
		}
	}
	return nil
}

func (m *SnapManager) getRefreshSchedule() ([]*timeutil.Schedule, error) {
	refreshScheduleStr := defaultRefreshSchedule

	tr := config.NewTransaction(m.state)
	err := tr.Get("core", "refresh.schedule", &refreshScheduleStr)
	if err != nil && !config.IsNoOption(err) {
		return nil, err
	}
	refreshSchedule, err := timeutil.ParseSchedule(refreshScheduleStr)
	if err == nil {
		err = refreshScheduleNoWeekdays(refreshSchedule)
	}
	if err != nil {
		logger.Noticef("cannot use refresh.schedule configuration: %s", err)
		refreshSchedule, err = timeutil.ParseSchedule(defaultRefreshSchedule)
		if err != nil {
			panic(fmt.Sprintf("defaultRefreshSchedule cannot be parsed: %s", err))
		}
		tr.Set("core", "refresh.schedule", defaultRefreshSchedule)
		tr.Commit()
	}

	// we already have a refresh time, check if we got a new config
	if !m.nextRefresh.IsZero() {
		if m.currentRefreshSchedule != refreshScheduleStr {
			// the refresh schedule has changed
			logger.Debugf("Option refresh.schedule changed.")
			m.nextRefresh = time.Time{}
		}
	}
	m.currentRefreshSchedule = refreshScheduleStr

	return refreshSchedule, nil
}

func (m *SnapManager) launchAutoRefresh() error {
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
		logger.Noticef(i18n.G("No snaps to auto-refresh found"))
		return nil
	case 1:
		msg = fmt.Sprintf(i18n.G("Auto-refresh snap %q"), updated[0])
	case 2:
	case 3:
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

func autoRefreshInFlight(st *state.State) bool {
	for _, chg := range st.Changes() {
		if chg.Kind() == "auto-refresh" && !chg.Status().Ready() {
			return true
		}
	}
	return false
}

func lastRefresh(st *state.State) (time.Time, error) {
	var lastRefresh time.Time
	err := st.Get("last-refresh", &lastRefresh)
	if err != nil && err != state.ErrNoState {
		return time.Time{}, err
	}
	return lastRefresh, nil
}

// ensureRefreshes ensures that we refresh all installed snaps periodically
func (m *SnapManager) ensureRefreshes() error {
	m.state.Lock()
	defer m.state.Unlock()

	// see if it even makes sense to try to refresh
	if CanAutoRefresh == nil {
		return nil
	}
	if ok, err := CanAutoRefresh(m.state); err != nil || !ok {
		return err
	}

	// get lastRefresh and schedule
	lastRefresh, err := lastRefresh(m.state)
	if err != nil {
		return err
	}
	refreshSchedule, err := m.getRefreshSchedule()
	if err != nil {
		return err
	}

	// ensure nothing is in flight already
	if autoRefreshInFlight(m.state) {
		return nil
	}

	// compute next refresh attempt time (if needed)
	if m.nextRefresh.IsZero() {
		// store attempts in memory so that we can backoff
		delta := timeutil.Next(refreshSchedule, lastRefresh)
		m.nextRefresh = time.Now().Add(delta)
		logger.Debugf("Next refresh scheduled for %s.", m.nextRefresh)
	}

	// Check that we have reasonable delays between unsuccessful attempts.
	// If the store is under stress we need to make sure we do not
	// hammer it too often
	if !m.lastRefreshAttempt.IsZero() && m.lastRefreshAttempt.Add(10*time.Minute).After(time.Now()) {
		return nil
	}

	// do refresh attempt (if needed)
	if m.nextRefresh.Before(time.Now()) {
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

// ensureForceDevmodeDropsDevmodeFromState undoes the froced devmode
// in snapstate for forced devmode distros.
func (m *SnapManager) ensureForceDevmodeDropsDevmodeFromState() error {
	if !release.ReleaseInfo.ForceDevMode() {
		return nil
	}

	m.state.Lock()
	defer m.state.Unlock()

	// int because we might want to come back and do a second pass at cleanup
	var fixed int
	if err := m.state.Get("fix-forced-devmode", &fixed); err != nil && err != state.ErrNoState {
		return err
	}

	if fixed > 0 {
		return nil
	}

	for _, name := range []string{"core", "ubuntu-core"} {
		var snapst SnapState
		if err := Get(m.state, name, &snapst); err == state.ErrNoState {
			// nothing to see here
			continue
		} else if err != nil {
			// bad
			return err
		}
		if info := snapst.CurrentSideInfo(); info == nil || info.SnapID == "" {
			continue
		}
		snapst.DevMode = false
		Set(m.state, name, &snapst)
	}
	m.state.Set("fix-forced-devmode", 1)

	return nil
}

func (m *SnapManager) NextRefresh() time.Time {
	return m.nextRefresh
}

// ensureUbuntuCoreTransition will migrate systems that use "ubuntu-core"
// to the new "core" snap
func (m *SnapManager) ensureUbuntuCoreTransition() error {
	m.state.Lock()
	defer m.state.Unlock()

	var snapst SnapState
	err := Get(m.state, "ubuntu-core", &snapst)
	if err == state.ErrNoState {
		return nil
	}
	if err != nil && err != state.ErrNoState {
		return err
	}

	// check that there is no change in flight already, this is a
	// precaution to ensure the core transition is safe
	for _, chg := range m.state.Changes() {
		if !chg.Status().Ready() {
			// another change already in motion
			return nil
		}
	}

	// ensure we limit the retries in case something goes wrong
	var lastUbuntuCoreTransitionAttempt time.Time
	err = m.state.Get("ubuntu-core-transition-last-retry-time", &lastUbuntuCoreTransitionAttempt)
	if err != nil && err != state.ErrNoState {
		return err
	}
	now := time.Now()
	if !lastUbuntuCoreTransitionAttempt.IsZero() && lastUbuntuCoreTransitionAttempt.Add(6*time.Hour).After(now) {
		return nil
	}
	m.state.Set("ubuntu-core-transition-last-retry-time", now)

	var retryCount int
	err = m.state.Get("ubuntu-core-transition-retry", &retryCount)
	if err != nil && err != state.ErrNoState {
		return err
	}
	m.state.Set("ubuntu-core-transition-retry", retryCount+1)

	tss, err := TransitionCore(m.state, "ubuntu-core", "core")
	if err != nil {
		return err
	}

	msg := fmt.Sprintf(i18n.G("Transition ubuntu-core to core"))
	chg := m.state.NewChange("transition-ubuntu-core", msg)
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	return nil
}

// Ensure implements StateManager.Ensure.
func (m *SnapManager) Ensure() error {
	// do not exit right away on error
	errs := []error{
		m.ensureAliasesV2(),
		m.ensureForceDevmodeDropsDevmodeFromState(),
		m.ensureUbuntuCoreTransition(),
		m.ensureRefreshes(),
	}

	m.runner.Ensure()

	//FIXME: use firstErr helper
	for _, e := range errs {
		if e != nil {
			return e
		}
	}

	return nil
}

// Wait implements StateManager.Wait.
func (m *SnapManager) Wait() {
	m.runner.Wait()
}

// Stop implements StateManager.Stop.
func (m *SnapManager) Stop() {
	m.runner.Stop()
}
