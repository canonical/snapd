// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

// Package snapstate implements the manager and state aspects responsible for the installation and removal of snaps.
package snapstate

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/errtracker"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
)

// FIXME: what we actually want is a schedule spec that is user configurable
// like:
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
var (
	minRefreshInterval = 4 * time.Hour

	// random interval on top of the minmum time between refreshes
	defaultRefreshRandomness = 4 * time.Hour
)

var (
	errtrackerReport = errtracker.Report
)

// SnapManager is responsible for the installation and removal of snaps.
type SnapManager struct {
	state   *state.State
	backend managerBackend

	refreshRandomness  time.Duration
	lastRefreshAttempt time.Time

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

func userFromUserID(st *state.State, userID int) (*auth.UserState, error) {
	if userID == 0 {
		return nil, nil
	}
	return auth.User(st, userID)
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

func updateInfo(st *state.State, snapst *SnapState, channel string, userID int) (*snap.Info, error) {
	user, err := userFromUserID(st, userID)
	if err != nil {
		return nil, err
	}
	curInfo, err := snapst.CurrentInfo()
	if err != nil {
		return nil, err
	}

	if curInfo.SnapID == "" { // covers also trymode
		return nil, fmt.Errorf("cannot refresh local snap %q", curInfo.Name())
	}

	refreshCand := &store.RefreshCandidate{
		// the desired channel
		Channel:  channel,
		SnapID:   curInfo.SnapID,
		Revision: curInfo.Revision,
		Epoch:    curInfo.Epoch,
	}

	theStore := Store(st)
	st.Unlock() // calls to the store should be done without holding the state lock
	res, err := theStore.ListRefresh([]*store.RefreshCandidate{refreshCand}, user)
	st.Lock()
	if err != nil {
		return nil, fmt.Errorf("cannot get refresh information for snap %q: %s", curInfo.Name(), err)
	}
	if len(res) == 0 {
		return nil, &snap.NoUpdateAvailableError{Snap: curInfo.Name()}
	}

	return res[0], nil
}

func snapInfo(st *state.State, name, channel string, revision snap.Revision, userID int) (*snap.Info, error) {
	user, err := userFromUserID(st, userID)
	if err != nil {
		return nil, err
	}
	theStore := Store(st)
	st.Unlock() // calls to the store should be done without holding the state lock
	spec := store.SnapSpec{
		Name:     name,
		Channel:  channel,
		Revision: revision,
	}
	snap, err := theStore.SnapInfo(spec, user)
	st.Lock()
	return snap, err
}

// Manager returns a new snap manager.
func Manager(st *state.State) (*SnapManager, error) {
	runner := state.NewTaskRunner(st)

	m := &SnapManager{
		state:   st,
		backend: backend.Backend{},
		runner:  runner,

		refreshRandomness: time.Duration(rand.Int63n(int64(defaultRefreshRandomness))),
	}
	logger.Debugf("snapmgr refresh randomness %s", m.refreshRandomness)

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
	runner.AddHandler("alias", m.doAlias, m.undoAlias)
	runner.AddHandler("clear-aliases", m.doClearAliases, m.undoClearAliases)
	runner.AddHandler("set-auto-aliases", m.doSetAutoAliases, m.undoClearAliases)
	runner.AddHandler("setup-aliases", m.doSetupAliases, m.undoSetupAliases)
	runner.AddHandler("remove-aliases", m.doRemoveAliases, m.doSetupAliases)

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

func diskAliasTask(t *state.Task) bool {
	kind := t.Kind()
	return kind == "setup-aliases" || kind == "remove-aliases" || kind == "alias"
}

func (m *SnapManager) blockedTask(cand *state.Task, running []*state.Task) bool {
	// aliases are global, serialize tasks operating on them
	if diskAliasTask(cand) {
		for _, t := range running {
			if diskAliasTask(t) {
				return true
			}
		}
	}
	return false
}

var CanAutoRefresh func(st *state.State) (bool, error)

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

	tr := config.NewTransaction(m.state)

	// allow disabling auto-refresh in the tests
	if osutil.GetenvBool("SNAPD_DEBUG") {
		var refreshDisabled bool
		err := tr.Get("core", "refresh.disabled", &refreshDisabled)
		if err != nil && !config.IsNoOption(err) {
			return err
		}
		if refreshDisabled {
			return nil
		}
	}

	var lastRefresh time.Time
	err := tr.Get("core", "refresh.last", &lastRefresh)
	if err != nil && !config.IsNoOption(err) {
		return err
	}

	nextRefresh := lastRefresh.Add(minRefreshInterval).Add(m.refreshRandomness)
	if time.Now().Before(nextRefresh) {
		return nil
	}

	// check that there is no change in flight already
	for _, chg := range m.state.Changes() {
		if chg.Kind() == "auto-refresh" && !chg.Status().Ready() {
			// change already in motion
			return nil
		}
	}

	// Check that we have reasonable delays between unsuccessful attempts.
	// If the store is under stress we need to make sure we do not
	// hammer it too often
	if !m.lastRefreshAttempt.IsZero() && m.lastRefreshAttempt.Add(10*time.Minute).After(time.Now()) {
		return nil
	}

	// store attempts in memory so that we can backoff a
	m.lastRefreshAttempt = time.Now()
	updated, tasksets, err := AutoRefresh(m.state)
	if err != nil {
		return err
	}

	// Do setLastRefresh() only if the store (in AutoRefresh) gave
	// us no error.
	setLastRefresh(m.state)

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
	err1 := m.ensureUbuntuCoreTransition()

	err2 := m.ensureRefreshes()

	m.runner.Ensure()

	//FIXME: use firstErr helper
	if err1 != nil {
		return err1
	}
	return err2
}

// Wait implements StateManager.Wait.
func (m *SnapManager) Wait() {
	m.runner.Wait()
}

// Stop implements StateManager.Stop.
func (m *SnapManager) Stop() {
	m.runner.Stop()
}

// TaskSnapSetup returns the SnapSetup with task params hold by or referred to by the the task.
func TaskSnapSetup(t *state.Task) (*SnapSetup, error) {
	var snapsup SnapSetup

	err := t.Get("snap-setup", &snapsup)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if err == nil {
		return &snapsup, nil
	}

	var id string
	err = t.Get("snap-setup-task", &id)
	if err != nil {
		return nil, err
	}

	ts := t.State().Task(id)
	if err := ts.Get("snap-setup", &snapsup); err != nil {
		return nil, err
	}
	return &snapsup, nil
}

func snapSetupAndState(t *state.Task) (*SnapSetup, *SnapState, error) {
	snapsup, err := TaskSnapSetup(t)
	if err != nil {
		return nil, nil, err
	}
	var snapst SnapState
	err = Get(t.State(), snapsup.Name(), &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, nil, err
	}
	return snapsup, &snapst, nil
}

func (m *SnapManager) doPrepareSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	snapsup, snapst, err := snapSetupAndState(t)
	st.Unlock()
	if err != nil {
		return err
	}

	if snapsup.Revision().Unset() {
		// Local revisions start at -1 and go down.
		revision := snapst.LocalRevision()
		if revision.Unset() || revision.N > 0 {
			revision = snap.R(-1)
		} else {
			revision.N--
		}
		if !revision.Local() {
			panic("internal error: invalid local revision built: " + revision.String())
		}
		snapsup.SideInfo.Revision = revision
	}

	st.Lock()
	t.Set("snap-setup", snapsup)
	st.Unlock()
	return nil
}

func (m *SnapManager) undoPrepareSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	snapsup, err := TaskSnapSetup(t)
	if err != nil {
		return err
	}

	// report this error to an error tracker
	if osutil.GetenvBool("SNAPPY_TESTING") {
		return nil
	}
	if snapsup.SideInfo.RealName == "" {
		return nil
	}

	var logMsg []string
	var snapSetup string
	dupSig := []string{"snap-install:"}
	for _, t := range t.Change().Tasks() {
		// TODO: report only tasks in intersecting lanes?
		tintro := fmt.Sprintf("%s: %s", t.Kind(), t.Status())
		logMsg = append(logMsg, tintro)
		dupSig = append(dupSig, tintro)
		if snapsup, err := TaskSnapSetup(t); err == nil {
			snapSetup1 := fmt.Sprintf(" snap-setup: %q (%v) %q", snapsup.SideInfo.RealName, snapsup.SideInfo.Revision, snapsup.SideInfo.Channel)
			if snapSetup1 != snapSetup {
				snapSetup = snapSetup1
				logMsg = append(logMsg, snapSetup)
				dupSig = append(dupSig, fmt.Sprintf(" snap-setup: %q", snapsup.SideInfo.RealName))
			}
		}
		for _, l := range t.Log() {
			// cut of the rfc339 timestamp to ensure duplicate
			// detection works in daisy
			tStampLen := strings.Index(l, " ")
			if tStampLen < 0 {
				continue
			}
			// not tStampLen+1 because the indent is nice
			entry := l[tStampLen:]
			logMsg = append(logMsg, entry)
			dupSig = append(dupSig, entry)
		}
	}

	var ubuntuCoreTransitionCount int
	err = st.Get("ubuntu-core-transition-retry", &ubuntuCoreTransitionCount)
	if err != nil && err != state.ErrNoState {
		return err
	}
	extra := map[string]string{
		"Channel":  snapsup.Channel,
		"Revision": snapsup.SideInfo.Revision.String(),
	}
	if ubuntuCoreTransitionCount > 0 {
		extra["UbuntuCoreTransitionCount"] = strconv.Itoa(ubuntuCoreTransitionCount)
	}

	st.Unlock()
	oopsid, err := errtrackerReport(snapsup.SideInfo.RealName, strings.Join(logMsg, "\n"), strings.Join(dupSig, "\n"), extra)
	st.Lock()
	if err == nil {
		logger.Noticef("Reported problem as %s", oopsid)
	} else {
		logger.Debugf("Cannot report problem: %s", err)
	}

	return nil
}

func (m *SnapManager) doDownloadSnap(t *state.Task, tomb *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	snapsup, err := TaskSnapSetup(t)
	st.Unlock()
	if err != nil {
		return err
	}

	meter := &TaskProgressAdapter{task: t}

	st.Lock()
	theStore := Store(st)
	user, err := userFromUserID(st, snapsup.UserID)
	st.Unlock()
	if err != nil {
		return err
	}

	targetFn := snapsup.MountFile()
	if snapsup.DownloadInfo == nil {
		var storeInfo *snap.Info
		// COMPATIBILITY - this task was created from an older version
		// of snapd that did not store the DownloadInfo in the state
		// yet.
		spec := store.SnapSpec{
			Name:     snapsup.Name(),
			Channel:  snapsup.Channel,
			Revision: snapsup.Revision(),
		}
		storeInfo, err = theStore.SnapInfo(spec, user)
		if err != nil {
			return err
		}
		err = theStore.Download(tomb.Context(nil), snapsup.Name(), targetFn, &storeInfo.DownloadInfo, meter, user)
		snapsup.SideInfo = &storeInfo.SideInfo
	} else {
		err = theStore.Download(tomb.Context(nil), snapsup.Name(), targetFn, snapsup.DownloadInfo, meter, user)
	}
	if err != nil {
		return err
	}

	snapsup.SnapPath = targetFn

	// update the snap setup for the follow up tasks
	st.Lock()
	t.Set("snap-setup", snapsup)
	st.Unlock()

	return nil
}

func (m *SnapManager) doUnlinkSnap(t *state.Task, _ *tomb.Tomb) error {
	// invoked only if snap has a current active revision

	st := t.State()

	st.Lock()
	defer st.Unlock()

	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	info, err := Info(t.State(), snapsup.Name(), snapsup.Revision())
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	st.Unlock() // pb itself will ask for locking
	err = m.backend.UnlinkSnap(info, pb)
	st.Lock()
	if err != nil {
		return err
	}

	// mark as inactive
	snapst.Active = false
	Set(st, snapsup.Name(), snapst)
	return nil
}

func (m *SnapManager) doClearSnapData(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	snapsup, snapst, err := snapSetupAndState(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	t.State().Lock()
	info, err := Info(t.State(), snapsup.Name(), snapsup.Revision())
	t.State().Unlock()
	if err != nil {
		return err
	}

	if err = m.backend.RemoveSnapData(info); err != nil {
		return err
	}

	// Only remove data common between versions if this is the last version
	if len(snapst.Sequence) == 1 {
		if err = m.backend.RemoveSnapCommonData(info); err != nil {
			return err
		}
	}

	return nil
}

func (m *SnapManager) doDiscardSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()

	st.Lock()
	snapsup, snapst, err := snapSetupAndState(t)
	st.Unlock()
	if err != nil {
		return err
	}

	if snapst.Current == snapsup.Revision() && snapst.Active {
		return fmt.Errorf("internal error: cannot discard snap %q: still active", snapsup.Name())
	}

	if len(snapst.Sequence) == 1 {
		snapst.Sequence = nil
		snapst.Current = snap.Revision{}
	} else {
		newSeq := make([]*snap.SideInfo, 0, len(snapst.Sequence))
		for _, si := range snapst.Sequence {
			if si.Revision == snapsup.Revision() {
				// leave out
				continue
			}
			newSeq = append(newSeq, si)
		}
		snapst.Sequence = newSeq
		if snapst.Current == snapsup.Revision() {
			snapst.Current = newSeq[len(newSeq)-1].Revision
		}
	}

	pb := &TaskProgressAdapter{task: t}
	typ, err := snapst.Type()
	if err != nil {
		return err
	}
	err = m.backend.RemoveSnapFiles(snapsup.placeInfo(), typ, pb)
	if err != nil {
		st.Lock()
		t.Errorf("cannot remove snap file %q, will retry in 3 mins: %s", snapsup.Name(), err)
		st.Unlock()
		return &state.Retry{After: 3 * time.Minute}
	}
	if len(snapst.Sequence) == 0 {
		err = m.backend.DiscardSnapNamespace(snapsup.Name())
		if err != nil {
			st.Lock()
			t.Errorf("cannot discard snap namespace %q, will retry in 3 mins: %s", snapsup.Name(), err)
			st.Unlock()
			return &state.Retry{After: 3 * time.Minute}
		}
	}
	st.Lock()
	Set(st, snapsup.Name(), snapst)
	st.Unlock()
	return nil
}

func (m *SnapManager) undoMountSnap(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	snapsup, err := TaskSnapSetup(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	t.State().Lock()
	var typ snap.Type
	err = t.Get("snap-type", &typ)
	t.State().Unlock()
	// backward compatibility
	if err == state.ErrNoState {
		typ = "app"
	} else if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	return m.backend.UndoSetupSnap(snapsup.placeInfo(), typ, pb)
}

func (m *SnapManager) doMountSnap(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	snapsup, snapst, err := snapSetupAndState(t)
	t.State().Unlock()
	if err != nil {
		return err
	}
	curInfo, err := snapst.CurrentInfo()
	if err != nil && err != ErrNoCurrent {
		return err
	}

	m.backend.CurrentInfo(curInfo)

	if err := checkSnap(t.State(), snapsup.SnapPath, snapsup.SideInfo, curInfo, snapsup.Flags); err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	// TODO Use snapsup.Revision() to obtain the right info to mount
	//      instead of assuming the candidate is the right one.
	if err := m.backend.SetupSnap(snapsup.SnapPath, snapsup.SideInfo, pb); err != nil {
		return err
	}

	// set snapst type for undoMountSnap
	newInfo, err := readInfo(snapsup.Name(), snapsup.SideInfo)
	if err != nil {
		return err
	}
	t.State().Lock()
	t.Set("snap-type", newInfo.Type)
	t.State().Unlock()

	if snapsup.Flags.RemoveSnapPath {
		if err := os.Remove(snapsup.SnapPath); err != nil {
			logger.Noticef("Failed to cleanup %s: %s", snapsup.SnapPath, err)
		}
	}

	return nil
}

func (m *SnapManager) undoUnlinkCurrentSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()

	st.Lock()
	defer st.Unlock()

	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	oldInfo, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}

	snapst.Active = true
	st.Unlock()
	err = m.backend.LinkSnap(oldInfo)
	st.Lock()
	if err != nil {
		return err
	}

	// mark as active again
	Set(st, snapsup.Name(), snapst)

	return nil

}

func (m *SnapManager) doUnlinkCurrentSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()

	st.Lock()
	defer st.Unlock()

	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	oldInfo, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}

	snapst.Active = false

	pb := &TaskProgressAdapter{task: t}
	st.Unlock() // pb itself will ask for locking
	err = m.backend.UnlinkSnap(oldInfo, pb)
	st.Lock()
	if err != nil {
		return err
	}

	// mark as inactive
	Set(st, snapsup.Name(), snapst)
	return nil
}

func (m *SnapManager) undoCopySnapData(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	snapsup, snapst, err := snapSetupAndState(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	newInfo, err := readInfo(snapsup.Name(), snapsup.SideInfo)
	if err != nil {
		return err
	}

	oldInfo, err := snapst.CurrentInfo()
	if err != nil && err != ErrNoCurrent {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	return m.backend.UndoCopySnapData(newInfo, oldInfo, pb)
}

func (m *SnapManager) doCopySnapData(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	snapsup, snapst, err := snapSetupAndState(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	newInfo, err := readInfo(snapsup.Name(), snapsup.SideInfo)
	if err != nil {
		return err
	}

	oldInfo, err := snapst.CurrentInfo()
	if err != nil && err != ErrNoCurrent {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	return m.backend.CopySnapData(newInfo, oldInfo, pb)
}

func (m *SnapManager) doLinkSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()

	st.Lock()
	defer st.Unlock()

	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	cand := snapsup.SideInfo
	m.backend.Candidate(cand)

	oldCandidateIndex := snapst.LastIndex(cand.Revision)

	if oldCandidateIndex < 0 {
		snapst.Sequence = append(snapst.Sequence, cand)
	} else if !snapsup.Revert {
		// remove the old candidate from the sequence, add it at the end
		copy(snapst.Sequence[oldCandidateIndex:len(snapst.Sequence)-1], snapst.Sequence[oldCandidateIndex+1:])
		snapst.Sequence[len(snapst.Sequence)-1] = cand
	}

	oldCurrent := snapst.Current
	snapst.Current = cand.Revision
	snapst.Active = true
	oldChannel := snapst.Channel
	if snapsup.Channel != "" {
		snapst.Channel = snapsup.Channel
	}
	oldTryMode := snapst.TryMode
	snapst.TryMode = snapsup.TryMode
	oldDevMode := snapst.DevMode
	snapst.DevMode = snapsup.DevMode
	oldJailMode := snapst.JailMode
	snapst.JailMode = snapsup.JailMode
	oldClassic := snapst.Classic
	snapst.Classic = snapsup.Classic
	if snapsup.Required { // set only on install and left alone on refresh
		snapst.Required = true
	}

	newInfo, err := readInfo(snapsup.Name(), cand)
	if err != nil {
		return err
	}

	// record type
	snapst.SetType(newInfo.Type)

	st.Unlock()
	// XXX: this block is slightly ugly, find a pattern when we have more examples
	err = m.backend.LinkSnap(newInfo)
	if err != nil {
		pb := &TaskProgressAdapter{task: t}
		err := m.backend.UnlinkSnap(newInfo, pb)
		if err != nil {
			st.Lock()
			t.Errorf("cannot cleanup failed attempt at making snap %q available to the system: %v", snapsup.Name(), err)
			st.Unlock()
		}
	}
	st.Lock()
	if err != nil {
		return err
	}

	// save for undoLinkSnap
	t.Set("old-trymode", oldTryMode)
	t.Set("old-devmode", oldDevMode)
	t.Set("old-jailmode", oldJailMode)
	t.Set("old-classic", oldClassic)
	t.Set("old-channel", oldChannel)
	t.Set("old-current", oldCurrent)
	t.Set("old-candidate-index", oldCandidateIndex)
	// Do at the end so we only preserve the new state if it worked.
	Set(st, snapsup.Name(), snapst)
	// Make sure if state commits and snapst is mutated we won't be rerun
	t.SetStatus(state.DoneStatus)

	// if we just installed a core snap, request a restart
	// so that we switch executing its snapd
	if release.OnClassic && newInfo.Type == snap.TypeOS {
		t.Logf("Requested daemon restart.")
		st.Unlock()
		st.RequestRestart(state.RestartDaemon)
		st.Lock()
	}
	if !release.OnClassic && boot.KernelOrOsRebootRequired(newInfo) {
		t.Logf("Requested system restart.")
		st.Unlock()
		st.RequestRestart(state.RestartSystem)
		st.Lock()
	}

	return nil
}

func (m *SnapManager) undoLinkSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()

	st.Lock()
	defer st.Unlock()

	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	var oldChannel string
	err = t.Get("old-channel", &oldChannel)
	if err != nil {
		return err
	}
	var oldTryMode bool
	err = t.Get("old-trymode", &oldTryMode)
	if err != nil {
		return err
	}
	var oldDevMode bool
	err = t.Get("old-devmode", &oldDevMode)
	if err != nil {
		return err
	}
	var oldJailMode bool
	err = t.Get("old-jailmode", &oldJailMode)
	if err != nil {
		return err
	}
	var oldClassic bool
	err = t.Get("old-classic", &oldClassic)
	if err != nil {
		return err
	}
	var oldCurrent snap.Revision
	err = t.Get("old-current", &oldCurrent)
	if err != nil {
		return err
	}
	var oldCandidateIndex int
	if err := t.Get("old-candidate-index", &oldCandidateIndex); err != nil {
		return err
	}

	isRevert := snapsup.Revert

	// relinking of the old snap is done in the undo of unlink-current-snap
	currentIndex := snapst.LastIndex(snapst.Current)
	if currentIndex < 0 {
		return fmt.Errorf("internal error: cannot find revision %d in %v for undoing the added revision", snapsup.SideInfo.Revision, snapst.Sequence)
	}

	if oldCandidateIndex < 0 {
		snapst.Sequence = append(snapst.Sequence[:currentIndex], snapst.Sequence[currentIndex+1:]...)
	} else if !isRevert {
		oldCand := snapst.Sequence[currentIndex]
		copy(snapst.Sequence[oldCandidateIndex+1:], snapst.Sequence[oldCandidateIndex:])
		snapst.Sequence[oldCandidateIndex] = oldCand
	}
	snapst.Current = oldCurrent
	snapst.Active = false
	snapst.Channel = oldChannel
	snapst.TryMode = oldTryMode
	snapst.DevMode = oldDevMode
	snapst.JailMode = oldJailMode
	snapst.Classic = oldClassic

	newInfo, err := readInfo(snapsup.Name(), snapsup.SideInfo)
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	st.Unlock() // pb itself will ask for locking
	err = m.backend.UnlinkSnap(newInfo, pb)
	st.Lock()
	if err != nil {
		return err
	}

	// mark as inactive
	Set(st, snapsup.Name(), snapst)
	// Make sure if state commits and snapst is mutated we won't be rerun
	t.SetStatus(state.UndoneStatus)
	return nil
}

func (m *SnapManager) doSwitchSnapChannel(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	// switched the tracked channel
	snapst.Channel = snapsup.Channel
	// optionally support switching the current snap channel too, e.g.
	// if a snap is in both stable and candidate with the same revision
	// we can update it here and it will be displayed correctly in the UI
	if snapsup.SideInfo.Channel != "" {
		snapst.CurrentSideInfo().Channel = snapsup.Channel
	}

	Set(st, snapsup.Name(), snapst)
	return nil
}

func (m *SnapManager) startSnapServices(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()

	st.Lock()
	defer st.Unlock()

	_, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	currentInfo, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	st.Unlock()
	err = m.backend.StartSnapServices(currentInfo, pb)
	st.Lock()
	return err
}

func setLastRefresh(st *state.State) {
	tr := config.NewTransaction(st)
	tr.Set("core", "refresh.last", time.Now())
	tr.Commit()
}

func (m *SnapManager) stopSnapServices(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()

	st.Lock()
	defer st.Unlock()

	_, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	currentInfo, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	st.Unlock()
	err = m.backend.StopSnapServices(currentInfo, pb)
	st.Lock()

	return err
}

func (m *SnapManager) cleanupCopySnapData(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()

	st.Lock()
	defer st.Unlock()

	if t.Status() != state.DoneStatus {
		// it failed
		return nil
	}

	_, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	info, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}

	m.backend.ClearTrashedData(info)

	return nil
}
