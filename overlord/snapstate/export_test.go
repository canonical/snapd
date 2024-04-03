// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
	userclient "github.com/snapcore/snapd/usersession/client"
	"github.com/snapcore/snapd/wrappers"
)

type (
	ManagerBackend managerBackend

	MinimalInstallInfo  = minimalInstallInfo
	InstallSnapInfo     = installSnapInfo
	ByType              = byType
	DirMigrationOptions = dirMigrationOptions
	Migration           = migration

	ReRefreshSetup = reRefreshSetup

	TooSoonError = tooSoonError
)

const (
	None         = none
	Full         = full
	Hidden       = hidden
	Home         = home
	RevertHidden = revertHidden
	DisableHome  = disableHome
	RevertFull   = revertFull
)

func SetSnapManagerBackend(s *SnapManager, b ManagerBackend) {
	s.backend = b
}

func MockSnapReadInfo(mock func(name string, si *snap.SideInfo) (*snap.Info, error)) (restore func()) {
	old := snapReadInfo
	snapReadInfo = mock
	return func() { snapReadInfo = old }
}

func MockReadComponentInfo(mock func(compMntDir string) (*snap.ComponentInfo, error)) (restore func()) {
	old := readComponentInfo
	readComponentInfo = mock
	return func() { readComponentInfo = old }
}

func MockMountPollInterval(intv time.Duration) (restore func()) {
	old := mountPollInterval
	mountPollInterval = intv
	return func() { mountPollInterval = old }
}

func MockRevisionDate(mock func(info *snap.Info) time.Time) (restore func()) {
	old := revisionDate
	if mock == nil {
		mock = revisionDateImpl
	}
	revisionDate = mock
	return func() { revisionDate = old }
}

func MockOpenSnapFile(mock func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error)) (restore func()) {
	prevOpenSnapFile := openSnapFile
	openSnapFile = mock
	return func() { openSnapFile = prevOpenSnapFile }
}

func MockPrerequisitesRetryTimeout(d time.Duration) (restore func()) {
	old := prerequisitesRetryTimeout
	prerequisitesRetryTimeout = d
	return func() { prerequisitesRetryTimeout = old }
}

func MockOsutilEnsureSnapUserGroup(mock func(name string, id uint32, extraUsers bool) error) (restore func()) {
	old := osutilEnsureSnapUserGroup
	osutilEnsureSnapUserGroup = mock
	return func() { osutilEnsureSnapUserGroup = old }
}

var (
	CoreInfoInternal       = coreInfo
	CheckSnap              = checkSnap
	CanRemove              = canRemove
	CanDisable             = canDisable
	CachedStore            = cachedStore
	DefaultRefreshSchedule = defaultRefreshScheduleStr
	DoInstall              = doInstall
	UserFromUserID         = userFromUserID
	ValidateFeatureFlags   = validateFeatureFlags
	ResolveChannel         = resolveChannel

	CurrentSnaps = currentSnaps

	HasOtherInstances = hasOtherInstances

	SafetyMarginDiskSpace = safetyMarginDiskSpace

	AffectedByRefresh = affectedByRefresh

	GetDirMigrationOpts                  = getDirMigrationOpts
	WriteSeqFile                         = writeSeqFile
	TriggeredMigration                   = triggeredMigration
	TaskSetsByTypeForEssentialSnaps      = taskSetsByTypeForEssentialSnaps
	SetDefaultRestartBoundaries          = setDefaultRestartBoundaries
	DeviceModelBootBase                  = deviceModelBootBase
	SplitTaskSetByRebootEdges            = splitTaskSetByRebootEdges
	ArrangeSnapToWaitForBaseIfPresent    = arrangeSnapToWaitForBaseIfPresent
	ArrangeSnapTaskSetsLinkageAndRestart = arrangeSnapTaskSetsLinkageAndRestart
	ReRefreshSummary                     = reRefreshSummary
)

const (
	NoRestartBoundaries = noRestartBoundaries
)

func PreviousSideInfo(snapst *SnapState) *snap.SideInfo {
	return snapst.previousSideInfo()
}

// helpers
var InstallSize = installSize

// aliases v2
var (
	ApplyAliasesChange    = applyAliasesChange
	AutoAliasesDelta      = autoAliasesDelta
	RefreshAliases        = refreshAliases
	CheckAliasesConflicts = checkAliasesConflicts
	DisableAliases        = disableAliases
	SwitchSummary         = switchSummary
)

// dbus
var (
	CheckDBusServiceConflicts = checkDBusServiceConflicts
)

// readme files
var (
	WriteSnapReadme = writeSnapReadme
	SnapReadme      = snapReadme
)

// refreshes
var (
	NewAutoRefresh                = newAutoRefresh
	NewRefreshHints               = newRefreshHints
	CanRefreshOnMeteredConnection = canRefreshOnMeteredConnection

	NewCatalogRefresh            = newCatalogRefresh
	CatalogRefreshDelayBase      = catalogRefreshDelayBase
	CatalogRefreshDelayWithDelta = catalogRefreshDelayWithDelta

	SoftCheckNothingRunningForRefresh     = softCheckNothingRunningForRefresh
	HardEnsureNothingRunningDuringRefresh = hardEnsureNothingRunningDuringRefresh
)

// cleanup
var (
	CleanSnapDownloads = cleanSnapDownloads
	CleanDownloads     = cleanDownloads
)

func MockMaxUnusedDownloadRetention(t time.Duration) func() {
	old := maxUnusedDownloadRetention
	maxUnusedDownloadRetention = t
	return func() {
		maxUnusedDownloadRetention = old
	}
}

func MockCleanDownloads(mock func(st *state.State) error) func() {
	old := cleanDownloads
	cleanDownloads = mock
	return func() {
		cleanDownloads = old
	}
}

func MockCleanSnapDownloads(mock func(st *state.State, snapName string) error) func() {
	old := cleanSnapDownloads
	cleanSnapDownloads = mock
	return func() {
		cleanSnapDownloads = old
	}
}

// install
var HasAllContentAttrs = hasAllContentAttrs

func MockNextRefresh(ar *autoRefresh, when time.Time) {
	ar.nextRefresh = when
}

func (snapmgr *SnapManager) MockNextRefresh(when time.Time) {
	snapmgr.autoRefresh.nextRefresh = when
}

func MockLastRefreshSchedule(ar *autoRefresh, schedule string) {
	ar.lastRefreshSchedule = schedule
}

func MockCatalogRefreshNextRefresh(cr *catalogRefresh, when time.Time) {
	cr.nextCatalogRefresh = when
}

func NextCatalogRefresh(cr *catalogRefresh) time.Time {
	return cr.nextCatalogRefresh
}

func MockRefreshRetryDelay(d time.Duration) func() {
	origRefreshRetryDelay := refreshRetryDelay
	refreshRetryDelay = d
	return func() {
		refreshRetryDelay = origRefreshRetryDelay
	}
}

func MockIsOnMeteredConnection(mock func() (bool, error)) func() {
	old := IsOnMeteredConnection
	IsOnMeteredConnection = mock
	return func() {
		IsOnMeteredConnection = old
	}
}

func MockLocalInstallCleanupWait(d time.Duration) (restore func()) {
	old := localInstallCleanupWait
	localInstallCleanupWait = d
	return func() {
		localInstallCleanupWait = old
	}
}

func MockLocalInstallLastCleanup(t time.Time) (restore func()) {
	old := localInstallLastCleanup
	localInstallLastCleanup = t
	return func() {
		localInstallLastCleanup = old
	}
}

func MockAsyncPendingRefreshNotification(fn func(context.Context, *userclient.PendingSnapRefreshInfo)) (restore func()) {
	old := asyncPendingRefreshNotification
	asyncPendingRefreshNotification = fn
	return func() {
		asyncPendingRefreshNotification = old
	}
}

func MockHasActiveConnection(fn func(st *state.State, iface string) (bool, error)) (restore func()) {
	old := HasActiveConnection
	HasActiveConnection = fn
	return func() {
		HasActiveConnection = old
	}
}

// re-refresh related
var (
	RefreshedSnaps     = refreshedSnaps
	ReRefreshFilter    = reRefreshFilter
	UpdateManyFiltered = updateManyFiltered

	MaybeRestoreValidationSetsAndRevertSnaps = maybeRestoreValidationSetsAndRevertSnaps
)

type UpdateFilter = updateFilter

func MockReRefreshUpdateMany(f func(context.Context, *state.State, []string, []*RevisionOptions, int, UpdateFilter, *Flags, string) ([]string, *UpdateTaskSets, error)) (restore func()) {
	old := reRefreshUpdateMany
	reRefreshUpdateMany = f
	return func() {
		reRefreshUpdateMany = old
	}
}

func MockReRefreshRetryTimeout(d time.Duration) (restore func()) {
	old := reRefreshRetryTimeout
	reRefreshRetryTimeout = d
	return func() {
		reRefreshRetryTimeout = old
	}
}

// aux store info
var (
	AuxStoreInfoFilename = auxStoreInfoFilename
	RetrieveAuxStoreInfo = retrieveAuxStoreInfo
	KeepAuxStoreInfo     = keepAuxStoreInfo
	DiscardAuxStoreInfo  = discardAuxStoreInfo
)

type AuxStoreInfo = auxStoreInfo

// link, misc handlers
var (
	MissingDisabledServices = missingDisabledServices
)

func (m *SnapManager) MaybeUndoRemodelBootChanges(t *state.Task) (restartRequested, rebootRequired bool, err error) {
	restartPoss, err := m.maybeUndoRemodelBootChanges(t)
	if restartPoss != nil {
		return true, restartPoss.RebootRequired, nil
	}
	return false, false, err
}

func MockEnsuredDesktopFilesUpdated(m *SnapManager, ensured bool) (restore func()) {
	old := m.ensuredDesktopFilesUpdated
	m.ensuredDesktopFilesUpdated = ensured
	return func() {
		m.ensuredDesktopFilesUpdated = old
	}
}

func MockEnsuredDownloadsCleaned(m *SnapManager, ensured bool) (restore func()) {
	old := m.ensuredDownloadsCleaned
	m.ensuredDownloadsCleaned = ensured
	return func() {
		m.ensuredDownloadsCleaned = old
	}
}

func MockPidsOfSnap(f func(instanceName string) (map[string][]int, error)) func() {
	old := pidsOfSnap
	pidsOfSnap = f
	return func() {
		pidsOfSnap = old
	}
}

func MockCurrentSnaps(f func(st *state.State) ([]*store.CurrentSnap, error)) func() {
	old := currentSnaps
	currentSnaps = f
	return func() {
		currentSnaps = old
	}
}

func MockInstallSize(f func(st *state.State, snaps []minimalInstallInfo, userID int, preqt PrereqTracker) (uint64, error)) func() {
	old := installSize
	installSize = f
	return func() {
		installSize = old
	}
}

func MockGenerateSnapdWrappers(f func(snapInfo *snap.Info, opts *backend.GenerateSnapdWrappersOptions) (wrappers.SnapdRestart, error)) func() {
	old := generateSnapdWrappers
	generateSnapdWrappers = f
	return func() {
		generateSnapdWrappers = old
	}
}

var (
	NotifyLinkParticipants = notifyLinkParticipants
)

// autorefresh
var (
	InhibitRefresh                       = inhibitRefresh
	MaxInhibition                        = maxInhibition
	MaxDuration                          = maxDuration
	MaybeAddRefreshInhibitNotice         = maybeAddRefreshInhibitNotice
	MaybeAsyncPendingRefreshNotification = maybeAsyncPendingRefreshNotification
)

type RefreshCandidate = refreshCandidate
type TimedBusySnapError = timedBusySnapError

func NewBusySnapError(info *snap.Info, pids []int, busyAppNames, busyHookNames []string) *BusySnapError {
	return &BusySnapError{
		SnapInfo:      info,
		pids:          pids,
		busyAppNames:  busyAppNames,
		busyHookNames: busyHookNames,
	}
}

func MockRefreshAppsCheck(fn func(info *snap.Info) error) (restore func()) {
	old := refreshAppsCheck
	refreshAppsCheck = fn
	return func() { refreshAppsCheck = old }
}

func (m *autoRefresh) EnsureRefreshHoldAtLeast(d time.Duration) error {
	return m.ensureRefreshHoldAtLeast(d)
}

func MockSecurityProfilesDiscardLate(fn func(snapName string, rev snap.Revision, typ snap.Type) error) (restore func()) {
	old := SecurityProfilesRemoveLate
	SecurityProfilesRemoveLate = fn
	return func() {
		SecurityProfilesRemoveLate = old
	}
}

type HoldState = holdState

var (
	HoldDurationLeft           = holdDurationLeft
	LastRefreshed              = lastRefreshed
	PruneRefreshCandidates     = pruneRefreshCandidates
	UpdateRefreshCandidates    = updateRefreshCandidates
	ResetGatingForRefreshed    = resetGatingForRefreshed
	PruneGating                = pruneGating
	PruneSnapsHold             = pruneSnapsHold
	CreateGateAutoRefreshHooks = createGateAutoRefreshHooks
	AutoRefreshPhase1          = autoRefreshPhase1
	RefreshRetain              = refreshRetain
	RefreshCheck               = refreshAppsCheck

	ExcludeFromRefreshAppAwareness = excludeFromRefreshAppAwareness
)

func MockTimeNow(f func() time.Time) (restore func()) {
	old := timeNow
	timeNow = f
	return func() {
		timeNow = old
	}
}

func MockHoldState(firstHeld string, holdUntil string) *HoldState {
	first, err := time.Parse(time.RFC3339, firstHeld)
	if err != nil {
		panic(err)
	}
	var until time.Time
	if holdUntil != "forever" {
		var err error
		until, err = time.Parse(time.RFC3339, holdUntil)
		if err != nil {
			panic(err)
		}
	} else {
		until = first.Add(maxDuration)
	}
	return &holdState{
		FirstHeld: first,
		HoldUntil: until,
	}
}

func MockSnapsToRefresh(f func(gatingTask *state.Task) ([]*refreshCandidate, error)) (restore func()) {
	old := snapsToRefresh
	snapsToRefresh = f
	return func() {
		snapsToRefresh = old
	}
}

func MockExcludeFromRefreshAppAwareness(f func(t snap.Type) bool) (restore func()) {
	r := testutil.Backup(&excludeFromRefreshAppAwareness)
	excludeFromRefreshAppAwareness = f
	return r
}

func MockAddCurrentTrackingToValidationSetsStack(f func(st *state.State) error) (restore func()) {
	old := AddCurrentTrackingToValidationSetsStack
	AddCurrentTrackingToValidationSetsStack = f
	return func() {
		AddCurrentTrackingToValidationSetsStack = old
	}
}

func MockRestoreValidationSetsTracking(f func(*state.State) error) (restore func()) {
	old := RestoreValidationSetsTracking
	RestoreValidationSetsTracking = f
	return func() {
		RestoreValidationSetsTracking = old
	}
}

func MockMaybeRestoreValidationSetsAndRevertSnaps(f func(st *state.State, refreshedSnaps []string, fromChange string) ([]*state.TaskSet, error)) (restore func()) {
	old := maybeRestoreValidationSetsAndRevertSnaps
	maybeRestoreValidationSetsAndRevertSnaps = f
	return func() {
		maybeRestoreValidationSetsAndRevertSnaps = old
	}
}

func MockGetHiddenDirOptions(f func(*state.State, *SnapState, *SnapSetup) (*dirMigrationOptions, error)) (restore func()) {
	old := getDirMigrationOpts
	getDirMigrationOpts = f
	return func() {
		getDirMigrationOpts = old
	}
}

func MockEnforcedValidationSets(f func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error)) func() {
	old := EnforcedValidationSets
	EnforcedValidationSets = f
	return func() {
		EnforcedValidationSets = old
	}
}

func MockEnforceValidationSets(f func(*state.State, map[string]*asserts.ValidationSet, map[string]int, []*snapasserts.InstalledSnap, map[string]bool, int) error) func() {
	old := EnforceValidationSets
	EnforceValidationSets = f
	return func() {
		EnforceValidationSets = old
	}
}

func MockEnforceLocalValidationSets(f func(*state.State, map[string][]string, map[string]int, []*snapasserts.InstalledSnap, map[string]bool) error) func() {
	old := EnforceLocalValidationSets
	EnforceLocalValidationSets = f
	return func() {
		EnforceLocalValidationSets = old
	}
}

func MockCgroupMonitorSnapEnded(f func(string, chan<- string) error) func() {
	old := cgroupMonitorSnapEnded
	cgroupMonitorSnapEnded = f
	return func() {
		cgroupMonitorSnapEnded = old
	}
}

func SetRestoredMonitoring(snapmgr *SnapManager, value bool) {
	snapmgr.autoRefresh.restoredMonitoring = value
}

func SetPreseed(snapmgr *SnapManager, value bool) {
	snapmgr.preseed = value
}
