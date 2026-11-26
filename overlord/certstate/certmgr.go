// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2026 Canonical Ltd
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

package certstate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/swfeats"
	"github.com/snapcore/snapd/release"
	"gopkg.in/tomb.v2"
)

func init() {
	swfeats.RegisterEnsure("CertManager", "ensureGarbageCollectionRun")
}

type CertManager struct {
	state            *state.State
	oneTimeChecksRan bool
}

const previousGenerationTaskKey = "cert-db-prev-generation"

var osutilBootID = osutil.BootID

func Manager(st *state.State, runner *state.TaskRunner) *CertManager {
	m := &CertManager{
		state: st,
	}

	// register tasks to update the certificate database
	runner.AddHandler("update-cert-db", m.doUpdateCertificateDatabase, m.undoUpdateCertificateDatabase)

	return m
}

// hasTaskInProgress is meant to be used from non-task contexts,
// so it doesn't take the current task as an argument. It checks if
// there is a task with the given name that is part of a change yet to
// be completed, which will be interpreted as "in progress".
func hasTaskInProgress(st *state.State, taskName string) bool {
	for _, t := range st.Tasks() {
		if t.Kind() != taskName {
			continue
		}
		if chg := t.Change(); chg != nil && !chg.IsReady() {
			return true
		}
	}
	return false
}

func (m *CertManager) ensureGarbageCollectionRun() error {
	// Skip garbage collection if there is a "update-cert-db" change in flight
	if hasTaskInProgress(m.state, "update-cert-db") {
		logger.Debugf("skipping certificate database garbage collection as update-cert-db change is in flight")
		return nil
	}

	logger.Trace("ensure", "manager", "CertManager", "func", "ensureGarbageCollectionRun")

	bootID, err := osutilBootID()
	if err != nil {
		return err
	}
	return garbageCollectCertificateGenerations(bootID)
}

// removeLegacyCertificateDatabase removes the legacy merged certificate path
// if the merged path is not a symlink.
func (m *CertManager) removeLegacyCertificateDatabase() error {
	mergedDir := CurrentCertificateDir()
	if osutil.IsSymlink(mergedDir) {
		return nil
	}
	// the legacy setup used plain directories instead of symlinks
	return os.RemoveAll(mergedDir)
}

func (m *CertManager) Ensure() error {
	st := m.state
	st.Lock()
	defer st.Unlock()

	// Do not perform automatic db generation on classic, or if ensure
	// has already run
	if m.oneTimeChecksRan || release.OnClassic {
		return nil
	}

	// Expect the system to be seeded, otherwise we ignore this.
	seeded, err := snapstate.SystemSeeded(st)
	if err != nil {
		return err
	}
	if !seeded {
		return nil
	}

	// If the ssl certs directory is missing, nothing to do.
	if !hasSystemCertsDir() {
		logger.Debugf("/etc/ssl/certs is not available on this system, skipping ca-certificates generation")
		return nil
	}

	// Skip reconciliation while an update-cert-db change is in flight so we do
	// not race the generation that the task's undo expects to restore. This is
	// checked before marking the one-time checks as run so that a later Ensure
	// (e.g. once the change has finished or been aborted) still reconciles the
	// store instead of waiting for the next snapd restart.
	if hasTaskInProgress(st, "update-cert-db") {
		logger.Debugf("skipping certificate database reconciliation as update-cert-db change is in flight")
		return nil
	}

	// At this point make sure we dont run again
	m.oneTimeChecksRan = true

	// If the merged certificate path is a plain directory (the old layout from
	// before generations were published), remove it so it can be regenerated.
	if err := m.removeLegacyCertificateDatabase(); err != nil {
		// In case of weird errors in this case, log it and skip generation. If it was
		// a weird FS error, then we'll just retry again on next snapd startup.
		logger.Noticef("error checking merged certificate database: %v", err)
		return nil
	}

	// Run garbage collection for the cert generations before reconciliation so a
	// currently-active generation that is about to be displaced has any stale
	// inactivity marker cleared first, and obsolete trees are reclaimed before
	// rendering. A GC failure (e.g. a failed boot-ID read or a full disk) must
	// not block the reconciliation below, so log it and carry on.
	if err := m.ensureGarbageCollectionRun(); err != nil {
		logger.Noticef("cannot garbage collect certificate generations: %v", err)
	}

	// Reconcile the managed store with the current inputs. This covers both the
	// first run on a pre-existing system (no merged symlink yet) and the case
	// where merged already points at a generation. The latter matters because a
	// refresh of the model base may have been planned by a snapd that predates
	// the managed certificate store (and so scheduled no update-cert-db task),
	// leaving the published generation rendered from the previous base.
	//
	// Note: RefreshCertificateDatabase is content-addressed in what it
	// *publishes* (the merged pointer only moves when the cert set changes), but
	// it always parses every certificate and renders a full staged tree, so on
	// Ubuntu Core this work now happens under the state lock on every snapd
	// start, not just the first run.
	//
	// TODO: The database will be generated with the lock being held here, which is not
	// the best, as there is some overhead in file-generation and hashing.
	return RefreshCertificateDatabase()
}

// recordCurrentCertificateGeneration is a helper function to make sure
// the current certificate generation is recorded before updating.
// OBS: This function will lock the state and commit to disk.
func recordCurrentCertificateGeneration(t *state.Task, key string) (string, error) {
	// Make sure we commit the changes here to disk immediately.
	st := t.State()
	st.Lock()
	defer st.Unlock()

	var current string
	err := t.Get(key, &current)
	if err != nil {
		if !errors.Is(err, state.ErrNoState) {
			return "", err
		}
		current, err = resolveCurrentCertificateTarget()
		if err != nil {
			return "", err
		}
		// Record the rollback target before publishing a new generation so a
		// reboot during refresh does not lose the generation undo must return to.
		t.Set(key, current)
	}
	return current, nil
}

func (m *CertManager) doUpdateCertificateDatabase(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()

	if !hasSystemCertsDir() {
		st.Lock()
		defer st.Unlock()
		t.Logf("/etc/ssl/certs is not available on this system, skipping certificate database update")
		return nil
	}

	// Remove the old-style merged directory (a plain directory from before
	// generations were published) before recording the previous generation.
	// Without this, recording the previous generation would fail with
	// "readlink .../merged: invalid argument" (merged is a directory, not a
	// symlink), which would fail the whole base refresh. Note: if a legacy
	// directory was removed here and the change is later undone, the recorded
	// previous target is empty and undo removes merged, leaving no store until
	// the next Ensure reconciles it again.
	st.Lock()
	err := m.removeLegacyCertificateDatabase()
	st.Unlock()
	if err != nil {
		return fmt.Errorf("cannot remove old certificate database: %v", err)
	}

	// Record the current generation before updating, so that
	// if the system reboots during the update, we have a known
	// generation to roll back to.
	// The function will do it's own lock/unlock to force a commit to the underlying
	// state to disk.
	if _, err := recordCurrentCertificateGeneration(t, previousGenerationTaskKey); err != nil {
		return err
	}

	st.Lock()
	defer st.Unlock()
	return RefreshCertificateDatabase()
}

func (m *CertManager) undoUpdateCertificateDatabase(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	var previousTarget string
	err := t.Get(previousGenerationTaskKey, &previousTarget)
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return nil
		}
		return err
	}

	if previousTarget == "" {
		mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
		// When there was no previously published generation, undo cannot point
		// merged back anywhere. Cleanup the merged directory.
		if err := os.RemoveAll(mergedDir); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	currentTarget, err := resolveCurrentCertificateTarget()
	if err != nil {
		return err
	}
	if currentTarget != previousTarget {
		// Ordinary undo just moves merged back to the generation captured before
		// the do-path ran.
		if err := switchCurrentMergedCertificates(previousTarget); err != nil {
			return err
		}
	}
	return nil
}

func hasSystemCertsDir() bool {
	if exists, isDir, err := osutil.DirExists(dirs.SystemCertsDir); !exists || !isDir || err != nil {
		return false
	}
	return true
}
