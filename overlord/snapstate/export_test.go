// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2019 Canonical Ltd
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

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type ManagerBackend managerBackend

func SetSnapManagerBackend(s *SnapManager, b ManagerBackend) {
	s.backend = b
}

func MockSnapReadInfo(mock func(name string, si *snap.SideInfo) (*snap.Info, error)) (restore func()) {
	old := snapReadInfo
	snapReadInfo = mock
	return func() { snapReadInfo = old }
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

func MockErrtrackerReport(mock func(string, string, string, map[string]string) (string, error)) (restore func()) {
	prev := errtrackerReport
	errtrackerReport = mock
	return func() { errtrackerReport = prev }
}

func MockPrerequisitesRetryTimeout(d time.Duration) (restore func()) {
	old := prerequisitesRetryTimeout
	prerequisitesRetryTimeout = d
	return func() { prerequisitesRetryTimeout = old }
}

func MockFindUid(mock func(name string) (uint64, error)) (restore func()) {
	old := findUid
	findUid = mock
	return func() { findUid = old }
}

func MockFindGid(mock func(name string) (uint64, error)) (restore func()) {
	old := findGid
	findGid = mock
	return func() { findGid = old }
}

var (
	CoreInfoInternal       = coreInfo
	CheckSnap              = checkSnap
	CanRemove              = canRemove
	CanDisable             = canDisable
	CachedStore            = cachedStore
	DefaultRefreshSchedule = defaultRefreshSchedule
	DoInstall              = doInstall
	UserFromUserID         = userFromUserID
	ValidateFeatureFlags   = validateFeatureFlags
	ResolveChannel         = resolveChannel

	DefaultContentPlugProviders = defaultContentPlugProviders

	HasOtherInstances = hasOtherInstances
)

func PreviousSideInfo(snapst *SnapState) *snap.SideInfo {
	return snapst.previousSideInfo()
}

// aliases v2
var (
	ApplyAliasesChange    = applyAliasesChange
	AutoAliasesDelta      = autoAliasesDelta
	RefreshAliases        = refreshAliases
	CheckAliasesConflicts = checkAliasesConflicts
	DisableAliases        = disableAliases
	SwitchSummary         = switchSummary
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
)

func MockNextRefresh(ar *autoRefresh, when time.Time) {
	ar.nextRefresh = when
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

// re-refresh related
var (
	RefreshedSnaps  = refreshedSnaps
	ReRefreshFilter = reRefreshFilter
)

type UpdateFilter = updateFilter

func MockReRefreshUpdateMany(f func(context.Context, *state.State, []string, int, UpdateFilter, *Flags, string) ([]string, []*state.TaskSet, error)) (restore func()) {
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
