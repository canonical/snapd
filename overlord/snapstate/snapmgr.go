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
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/errtracker"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/store"
)

var (
	snapdTransitionDelayWithRandomess = 3*time.Hour + randutil.RandomDuration(4*time.Hour)
)

// overridden in the tests
var errtrackerReport = errtracker.Report

// SnapManager is responsible for the installation and removal of snaps.
type SnapManager struct {
	state   *state.State
	backend managerBackend

	autoRefresh    *autoRefresh
	refreshHints   *refreshHints
	catalogRefresh *catalogRefresh

	lastUbuntuCoreTransitionAttempt time.Time

	preseed bool
}

// SnapSetup holds the necessary snap details to perform most snap manager tasks.
type SnapSetup struct {
	// FIXME: rename to RequestedChannel to convey the meaning better
	Channel string    `json:"channel,omitempty"`
	UserID  int       `json:"user-id,omitempty"`
	Base    string    `json:"base,omitempty"`
	Type    snap.Type `json:"type,omitempty"`
	// PlugsOnly indicates whether the relevant revisions for the
	// operation have only plugs (#plugs >= 0), and absolutely no
	// slots (#slots == 0).
	PlugsOnly bool `json:"plugs-only,omitempty"`

	CohortKey string `json:"cohort-key,omitempty"`

	// FIXME: implement rename of this as suggested in
	//  https://github.com/snapcore/snapd/pull/4103#discussion_r169569717
	//
	// Prereq is a list of snap-names that need to get installed
	// together with this snap. Typically used when installing
	// content-snaps with default-providers.
	Prereq []string `json:"prereq,omitempty"`

	Flags

	SnapPath string `json:"snap-path,omitempty"`

	// LastActiveDisabledServices is a list of services that were disabled right
	// before the snap was unlinked to be used for re-disabling those services
	// right after re-linking a different revision
	LastActiveDisabledServices []string `json:"last-active-disabled-services,omitempty"`

	DownloadInfo *snap.DownloadInfo `json:"download-info,omitempty"`
	SideInfo     *snap.SideInfo     `json:"side-info,omitempty"`
	auxStoreInfo

	// InstanceKey is set by the user during installation and differs for
	// each instance of given snap
	InstanceKey string `json:"instance-key,omitempty"`
}

func (snapsup *SnapSetup) InstanceName() string {
	return snap.InstanceName(snapsup.SnapName(), snapsup.InstanceKey)
}

func (snapsup *SnapSetup) SnapName() string {
	if snapsup.SideInfo.RealName == "" {
		panic("SnapSetup.SideInfo.RealName not set")
	}
	return snapsup.SideInfo.RealName
}

func (snapsup *SnapSetup) Revision() snap.Revision {
	return snapsup.SideInfo.Revision
}

func (snapsup *SnapSetup) placeInfo() snap.PlaceInfo {
	return snap.MinimalPlaceInfo(snapsup.InstanceName(), snapsup.Revision())
}

func (snapsup *SnapSetup) MountDir() string {
	return snap.MountDir(snapsup.InstanceName(), snapsup.Revision())
}

func (snapsup *SnapSetup) MountFile() string {
	return snap.MountFile(snapsup.InstanceName(), snapsup.Revision())
}

// SnapState holds the state for a snap installed in the system.
type SnapState struct {
	SnapType string           `json:"type"` // Use Type and SetType
	Sequence []*snap.SideInfo `json:"sequence"`
	Active   bool             `json:"active,omitempty"`

	// LastActiveDisabledServices is a list of services that were disabled in
	// this snap when it was last active - i.e. when it was disabled, before
	// it was reverted, or before a refresh happens.
	// It is set during unlink-snap and unlink-current-snap and reset during
	// link-snap since it is only meant to be saved when snapd needs to remove
	// systemd units.
	// Note that to handle potential service renames, only services that exist
	// in the snap are removed from this list on link-snap, so that we can
	// remember services that were disabled in another revision and then renamed
	// or otherwise removed from the snap in a future refresh.
	LastActiveDisabledServices []string `json:"last-active-disabled-services,omitempty"`

	// Current indicates the current active revision if Active is
	// true or the last active revision if Active is false
	// (usually while a snap is being operated on or disabled)
	Current         snap.Revision `json:"current"`
	TrackingChannel string        `json:"channel,omitempty"`
	Flags
	// aliases, see aliasesv2.go
	Aliases             map[string]*AliasTarget `json:"aliases,omitempty"`
	AutoAliasesDisabled bool                    `json:"auto-aliases-disabled,omitempty"`
	AliasesPending      bool                    `json:"aliases-pending,omitempty"`

	// UserID of the user requesting the install
	UserID int `json:"user-id,omitempty"`

	// InstanceKey is set by the user during installation and differs for
	// each instance of given snap
	InstanceKey string `json:"instance-key,omitempty"`
	CohortKey   string `json:"cohort-key,omitempty"`

	// RefreshInhibitedime records the time when the refresh was first
	// attempted but inhibited because the snap was busy. This value is
	// reset on each successful refresh.
	RefreshInhibitedTime *time.Time `json:"refresh-inhibited-time,omitempty"`
}

func (snapst *SnapState) SetTrackingChannel(s string) error {
	s, err := channel.Full(s)
	if err != nil {
		return err
	}
	snapst.TrackingChannel = s
	return nil
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

// IsInstalled returns whether the snap is installed, i.e. snapst represents an installed snap with Current revision set.
func (snapst *SnapState) IsInstalled() bool {
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

// CurrentSideInfo returns the side info for the revision indicated by snapst.Current in the snap revision sequence if there is one.
func (snapst *SnapState) CurrentSideInfo() *snap.SideInfo {
	if !snapst.IsInstalled() {
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

const (
	errorOnBroken = 1 << iota
	withAuxStoreInfo
)

var snapReadInfo = snap.ReadInfo

// AutomaticSnapshot allows to hook snapshot manager's AutomaticSnapshot.
var AutomaticSnapshot func(st *state.State, instanceName string) (ts *state.TaskSet, err error)
var AutomaticSnapshotExpiration func(st *state.State) (time.Duration, error)

func readInfo(name string, si *snap.SideInfo, flags int) (*snap.Info, error) {
	info, err := snapReadInfo(name, si)
	if err != nil && flags&errorOnBroken != 0 {
		return nil, err
	}
	if err != nil {
		logger.Noticef("cannot read snap info of snap %q at revision %s: %s", name, si.Revision, err)
	}
	if bse, ok := err.(snap.BrokenSnapError); ok {
		_, instanceKey := snap.SplitInstanceName(name)
		info = &snap.Info{
			SuggestedName: name,
			Broken:        bse.Broken(),
			InstanceKey:   instanceKey,
		}
		info.Apps = snap.GuessAppsForBroken(info)
		if si != nil {
			info.SideInfo = *si
		}
		err = nil
	}
	if err == nil && flags&withAuxStoreInfo != 0 {
		if err := retrieveAuxStoreInfo(info); err != nil {
			logger.Debugf("cannot read auxiliary store info for snap %q: %v", name, err)
		}
	}
	return info, err
}

var revisionDate = revisionDateImpl

// revisionDate returns a good approximation of when a revision reached the system.
func revisionDateImpl(info *snap.Info) time.Time {
	fi, err := os.Lstat(info.MountFile())
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// CurrentInfo returns the information about the current active revision or the last active revision (if the snap is inactive). It returns the ErrNoCurrent error if snapst.Current is unset.
func (snapst *SnapState) CurrentInfo() (*snap.Info, error) {
	cur := snapst.CurrentSideInfo()
	if cur == nil {
		return nil, ErrNoCurrent
	}

	name := snap.InstanceName(cur.RealName, snapst.InstanceKey)
	return readInfo(name, cur, withAuxStoreInfo)
}

func (snapst *SnapState) InstanceName() string {
	cur := snapst.CurrentSideInfo()
	if cur == nil {
		return ""
	}
	return snap.InstanceName(cur.RealName, snapst.InstanceKey)
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

// Store returns the store service provided by the optional device context or
// the one used by the snapstate package if the former has no
// override.
func Store(st *state.State, deviceCtx DeviceContext) StoreService {
	if deviceCtx != nil {
		sto := deviceCtx.Store()
		if sto != nil {
			return sto
		}
	}
	if cachedStore := cachedStore(st); cachedStore != nil {
		return cachedStore
	}
	panic("internal error: needing the store before managers have initialized it")
}

// Manager returns a new snap manager.
func Manager(st *state.State, runner *state.TaskRunner) (*SnapManager, error) {
	preseed := release.PreseedMode()
	m := &SnapManager{
		state:          st,
		autoRefresh:    newAutoRefresh(st),
		refreshHints:   newRefreshHints(st),
		catalogRefresh: newCatalogRefresh(st),
		preseed:        preseed,
	}
	if preseed {
		m.backend = backend.NewForPreseedMode()
	} else {
		m.backend = backend.Backend{}
	}

	if err := os.MkdirAll(dirs.SnapCookieDir, 0700); err != nil {
		return nil, fmt.Errorf("cannot create directory %q: %v", dirs.SnapCookieDir, err)
	}

	if err := genRefreshRequestSalt(st); err != nil {
		return nil, fmt.Errorf("cannot generate request salt: %v", err)
	}

	// this handler does nothing
	runner.AddHandler("nop", func(t *state.Task, _ *tomb.Tomb) error {
		return nil
	}, nil)

	// install/update related

	// TODO: no undo handler here, we may use the GC for this and just
	// remove anything that is not referenced anymore
	runner.AddHandler("prerequisites", m.doPrerequisites, nil)
	runner.AddHandler("prepare-snap", m.doPrepareSnap, m.undoPrepareSnap)
	runner.AddHandler("download-snap", m.doDownloadSnap, m.undoPrepareSnap)
	runner.AddHandler("mount-snap", m.doMountSnap, m.undoMountSnap)
	runner.AddHandler("unlink-current-snap", m.doUnlinkCurrentSnap, m.undoUnlinkCurrentSnap)
	runner.AddHandler("copy-snap-data", m.doCopySnapData, m.undoCopySnapData)
	runner.AddCleanup("copy-snap-data", m.cleanupCopySnapData)
	runner.AddHandler("link-snap", m.doLinkSnap, m.undoLinkSnap)
	runner.AddHandler("start-snap-services", m.startSnapServices, m.stopSnapServices)
	runner.AddHandler("switch-snap-channel", m.doSwitchSnapChannel, nil)
	runner.AddHandler("toggle-snap-flags", m.doToggleSnapFlags, nil)
	runner.AddHandler("check-rerefresh", m.doCheckReRefresh, nil)

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
	runner.AddHandler("set-auto-aliases", m.doSetAutoAliases, m.undoRefreshAliases)
	runner.AddHandler("setup-aliases", m.doSetupAliases, m.doRemoveAliases)
	runner.AddHandler("refresh-aliases", m.doRefreshAliases, m.undoRefreshAliases)
	runner.AddHandler("prune-auto-aliases", m.doPruneAutoAliases, m.undoRefreshAliases)
	runner.AddHandler("remove-aliases", m.doRemoveAliases, m.doSetupAliases)
	runner.AddHandler("alias", m.doAlias, m.undoRefreshAliases)
	runner.AddHandler("unalias", m.doUnalias, m.undoRefreshAliases)
	runner.AddHandler("disable-aliases", m.doDisableAliases, m.undoRefreshAliases)
	runner.AddHandler("prefer-aliases", m.doPreferAliases, m.undoRefreshAliases)

	// misc
	runner.AddHandler("switch-snap", m.doSwitchSnap, nil)

	// control serialisation
	runner.AddBlocked(m.blockedTask)

	return m, nil
}

// StartUp implements StateStarterUp.Startup.
func (m *SnapManager) StartUp() error {
	writeSnapReadme()

	m.state.Lock()
	defer m.state.Unlock()
	if err := m.SyncCookies(m.state); err != nil {
		return fmt.Errorf("failed to generate cookies: %q", err)
	}
	return nil
}

func (m *SnapManager) CanStandby() bool {
	if n, err := NumSnaps(m.state); err == nil && n == 0 {
		return true
	}
	return false
}

func genRefreshRequestSalt(st *state.State) error {
	var refreshPrivacyKey string

	st.Lock()
	defer st.Unlock()

	if err := st.Get("refresh-privacy-key", &refreshPrivacyKey); err != nil && err != state.ErrNoState {
		return err
	}
	if refreshPrivacyKey != "" {
		// nothing to do
		return nil
	}

	refreshPrivacyKey = randutil.RandomString(16)
	st.Set("refresh-privacy-key", refreshPrivacyKey)

	return nil
}

func (m *SnapManager) blockedTask(cand *state.Task, running []*state.Task) bool {
	// Serialize "prerequisites", the state lock is not enough as
	// Install() inside doPrerequisites() will unlock to talk to
	// the store.
	if cand.Kind() == "prerequisites" {
		for _, t := range running {
			if t.Kind() == "prerequisites" {
				return true
			}
		}
	}

	return false
}

// NextRefresh returns the time the next update of the system's snaps
// will be attempted.
// The caller should be holding the state lock.
func (m *SnapManager) NextRefresh() time.Time {
	return m.autoRefresh.NextRefresh()
}

// EffectiveRefreshHold returns the time until to which refreshes are
// held if refresh.hold configuration is set and accounting for the
// max postponement since the last refresh.
// The caller should be holding the state lock.
func (m *SnapManager) EffectiveRefreshHold() (time.Time, error) {
	return m.autoRefresh.EffectiveRefreshHold()
}

// LastRefresh returns the time the last snap update.
// The caller should be holding the state lock.
func (m *SnapManager) LastRefresh() (time.Time, error) {
	return m.autoRefresh.LastRefresh()
}

// RefreshSchedule returns the current refresh schedule as a string suitable for
// display to a user and a flag indicating whether the schedule is a legacy one.
// The caller should be holding the state lock.
func (m *SnapManager) RefreshSchedule() (string, bool, error) {
	return m.autoRefresh.RefreshSchedule()
}

// ensureForceDevmodeDropsDevmodeFromState undoes the forced devmode
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

// changeInFlight returns true if there is any change in the state
// in non-ready state.
func changeInFlight(st *state.State) bool {
	for _, chg := range st.Changes() {
		if !chg.IsReady() {
			// another change already in motion
			return true
		}
	}
	return false
}

// ensureSnapdSnapTransition will migrate systems to use the "snapd" snap
func (m *SnapManager) ensureSnapdSnapTransition() error {
	m.state.Lock()
	defer m.state.Unlock()

	// we only auto-transition people on classic systems, for core we
	// will need to do a proper re-model
	if !release.OnClassic {
		return nil
	}

	// check if snapd snap is installed
	var snapst SnapState
	err := Get(m.state, "snapd", &snapst)
	if err != nil && err != state.ErrNoState {
		return err
	}
	// nothing to do
	if snapst.IsInstalled() {
		return nil
	}

	// check if the user opts into the snapd snap
	optedIntoSnapdTransition, err := optedIntoSnapdSnap(m.state)
	if err != nil {
		return err
	}
	// nothing to do: the user does not want the snapd snap yet
	if !optedIntoSnapdTransition {
		return nil
	}

	// ensure we only transition systems that have snaps already
	installedSnaps, err := NumSnaps(m.state)
	if err != nil {
		return err
	}
	// no installed snaps (yet): do nothing (fresh classic install)
	if installedSnaps == 0 {
		return nil
	}

	// get current core snap and use same channel/user for the snapd snap
	err = Get(m.state, "core", &snapst)
	// Note that state.ErrNoState should never happen in practise. However
	// if it *does* happen we still want to fix those systems by installing
	// the snapd snap.
	if err != nil && err != state.ErrNoState {
		return err
	}
	coreChannel := snapst.TrackingChannel
	// snapd/core are never blocked on auth so we don't need to copy
	// the userID from the snapst here
	userID := 0

	if changeInFlight(m.state) {
		// check that there is no change in flight already, this is a
		// precaution to ensure the snapd transition is safe
		return nil
	}

	// ensure we limit the retries in case something goes wrong
	var lastSnapdTransitionAttempt time.Time
	err = m.state.Get("snapd-transition-last-retry-time", &lastSnapdTransitionAttempt)
	if err != nil && err != state.ErrNoState {
		return err
	}
	now := time.Now()
	if !lastSnapdTransitionAttempt.IsZero() && lastSnapdTransitionAttempt.Add(snapdTransitionDelayWithRandomess).After(now) {
		return nil
	}
	m.state.Set("snapd-transition-last-retry-time", now)

	var retryCount int
	err = m.state.Get("snapd-transition-retry", &retryCount)
	if err != nil && err != state.ErrNoState {
		return err
	}
	m.state.Set("snapd-transition-retry", retryCount+1)

	ts, err := Install(context.Background(), m.state, "snapd", &RevisionOptions{Channel: coreChannel}, userID, Flags{})
	if err != nil {
		return err
	}

	msg := i18n.G("Transition to the snapd snap")
	chg := m.state.NewChange("transition-to-snapd-snap", msg)
	chg.AddAll(ts)

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
	if changeInFlight(m.state) {
		// another change already in motion
		return nil
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

	tss, trErr := TransitionCore(m.state, "ubuntu-core", "core")
	if _, ok := trErr.(*ChangeConflictError); ok {
		// likely just too early, retry at next Ensure
		return nil
	}

	m.state.Set("ubuntu-core-transition-last-retry-time", now)

	var retryCount int
	err = m.state.Get("ubuntu-core-transition-retry", &retryCount)
	if err != nil && err != state.ErrNoState {
		return err
	}
	m.state.Set("ubuntu-core-transition-retry", retryCount+1)

	if trErr != nil {
		return trErr
	}

	msg := i18n.G("Transition ubuntu-core to core")
	chg := m.state.NewChange("transition-ubuntu-core", msg)
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	return nil
}

// atSeed implements at seeding policy for refreshes.
func (m *SnapManager) atSeed() error {
	m.state.Lock()
	defer m.state.Unlock()
	var seeded bool
	err := m.state.Get("seeded", &seeded)
	if err != state.ErrNoState {
		// already seeded or other error
		return err
	}
	if err := m.autoRefresh.AtSeed(); err != nil {
		return err
	}
	if err := m.refreshHints.AtSeed(); err != nil {
		return err
	}
	return nil
}

var (
	localInstallCleanupWait = time.Duration(24 * time.Hour)
	localInstallLastCleanup time.Time
)

// localInstallCleanup removes files that might've been left behind by an
// old aborted local install.
//
// They're usually cleaned up, but if they're created and then snapd
// stops before writing the change to disk (killed, light cut, etc)
// it'll be left behind.
//
// The code that creates the files is in daemon/api.go's postSnaps
func (m *SnapManager) localInstallCleanup() error {
	m.state.Lock()
	defer m.state.Unlock()

	now := time.Now()
	cutoff := now.Add(-localInstallCleanupWait)
	if localInstallLastCleanup.After(cutoff) {
		return nil
	}
	localInstallLastCleanup = now

	d, err := os.Open(dirs.SnapBlobDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer d.Close()

	var filenames []string
	var fis []os.FileInfo
	for err == nil {
		// TODO: if we had fstatat we could avoid a bunch of stats
		fis, err = d.Readdir(100)
		// fis is nil if err isn't
		for _, fi := range fis {
			name := fi.Name()
			if !strings.HasPrefix(name, dirs.LocalInstallBlobTempPrefix) {
				continue
			}
			if fi.ModTime().After(cutoff) {
				continue
			}
			filenames = append(filenames, name)
		}
	}
	if err != io.EOF {
		return err
	}
	return osutil.UnlinkManyAt(d, filenames)
}

// Ensure implements StateManager.Ensure.
func (m *SnapManager) Ensure() error {
	if m.preseed {
		return nil
	}

	// do not exit right away on error
	errs := []error{
		m.atSeed(),
		m.ensureAliasesV2(),
		m.ensureForceDevmodeDropsDevmodeFromState(),
		m.ensureUbuntuCoreTransition(),
		m.ensureSnapdSnapTransition(),
		// we should check for full regular refreshes before
		// considering issuing a hint only refresh request
		m.autoRefresh.Ensure(),
		m.refreshHints.Ensure(),
		m.catalogRefresh.Ensure(),
		m.localInstallCleanup(),
	}

	//FIXME: use firstErr helper
	for _, e := range errs {
		if e != nil {
			return e
		}
	}

	return nil
}
