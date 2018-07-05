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
	"errors"
	"fmt"
	"sort"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type ManagerBackend managerBackend

func SetSnapManagerBackend(s *SnapManager, b ManagerBackend) {
	s.backend = b
}

type ForeignTaskTracker interface {
	ForeignTask(kind string, status state.Status, snapsup *SnapSetup)
}

// AddForeignTaskHandlers registers handlers for tasks handled outside of the snap manager.
func (m *SnapManager) AddForeignTaskHandlers(tracker ForeignTaskTracker) {
	// Add fake handlers for tasks handled by interfaces manager
	fakeHandler := func(task *state.Task, _ *tomb.Tomb) error {
		task.State().Lock()
		kind := task.Kind()
		status := task.Status()
		snapsup, err := TaskSnapSetup(task)
		task.State().Unlock()
		if err != nil {
			return err
		}

		tracker.ForeignTask(kind, status, snapsup)

		return nil
	}
	m.runner.AddHandler("setup-profiles", fakeHandler, fakeHandler)
	m.runner.AddHandler("auto-connect", fakeHandler, nil)
	m.runner.AddHandler("remove-profiles", fakeHandler, fakeHandler)
	m.runner.AddHandler("discard-conns", fakeHandler, fakeHandler)
	m.runner.AddHandler("validate-snap", fakeHandler, nil)
	m.runner.AddHandler("transition-ubuntu-core", fakeHandler, nil)

	// Add handler to test full aborting of changes
	erroringHandler := func(task *state.Task, _ *tomb.Tomb) error {
		return errors.New("error out")
	}
	m.runner.AddHandler("error-trigger", erroringHandler, nil)

	m.runner.AddHandler("run-hook", func(task *state.Task, _ *tomb.Tomb) error {
		return nil
	}, nil)
	m.runner.AddHandler("configure-snapd", func(t *state.Task, _ *tomb.Tomb) error {
		return nil
	}, nil)

}

// AddAdhocTaskHandlers registers handlers for ad hoc test handler
func (m *SnapManager) AddAdhocTaskHandler(adhoc string, do, undo func(*state.Task, *tomb.Tomb) error) {
	m.runner.AddHandler(adhoc, do, undo)
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

func ByKindOrder(snaps ...*snap.Info) []*snap.Info {
	sort.Sort(byKind(snaps))
	return snaps
}

func MockModelWithBase(baseName string) (restore func()) {
	return mockModel(baseName)
}

func MockModel() (restore func()) {
	return mockModel("")
}

func mockModel(baseName string) (restore func()) {
	oldModel := Model

	base := ""
	if baseName != "" {
		base = fmt.Sprintf("\nbase: %s", baseName)
	}
	mod := fmt.Sprintf(`type: model
authority-id: brand
series: 16
brand-id: brand
model: baz-3000
architecture: armhf
gadget: brand-gadget
kernel: kernel%s
timestamp: 2018-01-01T08:00:00+00:00
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==
`, base)
	a, err := asserts.Decode([]byte(mod))
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
