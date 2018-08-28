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

package snapstate

import (
	"time"

	"github.com/snapcore/snapd/asserts"
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

var (
	CheckSnap              = checkSnap
	CanRemove              = canRemove
	CanDisable             = canDisable
	CachedStore            = cachedStore
	DefaultRefreshSchedule = defaultRefreshSchedule
	NameAndRevnoFromSnap   = nameAndRevnoFromSnap
	DoInstall              = doInstall
	UserFromUserID         = userFromUserID
	ValidateFeatureFlags   = validateFeatureFlags

	DefaultContentPlugProviders = defaultContentPlugProviders

	OtherSnapsLike = otherSnapsLike
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
	NewCatalogRefresh             = newCatalogRefresh
	CanRefreshOnMeteredConnection = canRefreshOnMeteredConnection
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

func MockModelWithBase(baseName string) (restore func()) {
	return mockModel(map[string]string{"base": baseName})
}

func MockModelWithKernelTrack(kernelTrack string) (restore func()) {
	return mockModel(map[string]string{"kernel": "kernel=" + kernelTrack})
}

func MockModelWithGadgetTrack(gadgetTrack string) (restore func()) {
	return mockModel(map[string]string{"gadget": "brand-gadget=" + gadgetTrack})
}

func MockModel() (restore func()) {
	return mockModel(nil)
}

func mockModel(override map[string]string) (restore func()) {
	oldModel := Model

	model := map[string]interface{}{
		"type":              "model",
		"authority-id":      "brand",
		"series":            "16",
		"brand-id":          "brand",
		"model":             "baz-3000",
		"architecture":      "armhf",
		"gadget":            "brand-gadget",
		"kernel":            "kernel",
		"timestamp":         "2018-01-01T08:00:00+00:00",
		"sign-key-sha3-384": "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
	}
	for k, v := range override {
		model[k] = v
	}

	a, err := asserts.Assemble(model, nil, nil, []byte("AXNpZw=="))
	if err != nil {
		panic(err)
	}

	Model = func(*state.State) (*asserts.Model, error) {
		return a.(*asserts.Model), nil
	}
	return func() {
		Model = oldModel
	}
}
