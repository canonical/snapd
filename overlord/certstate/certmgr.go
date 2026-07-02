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
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/state"
	"gopkg.in/tomb.v2"
)

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

	bootID, err := osutilBootID()
	if err != nil {
		return err
	}
	return garbageCollectCertificateGenerations(bootID)
}

func (m *CertManager) Ensure() error {
	st := m.state
	st.Lock()
	defer st.Unlock()

	if m.oneTimeChecksRan {
		return nil
	}

	// Expect the system to be seeded, otherwise we ignore this.
	var seeded bool
	if err := st.Get("seeded", &seeded); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if !seeded {
		return nil
	}

	// The reason we set it already, before any of the checks have actually run, is
	// that in the case of errors we don't want to keep trying the below things. They are
	// meant to run just once per boot (of snapd is fine too).
	m.oneTimeChecksRan = true

	// If the ssl certs directory is missing, nothing to do.
	if !hasSystemCertsDir() {
		logger.Debugf("/etc/ssl/certs is not available on this system, skipping ca-certificates generation")
		return nil
	}

	// If the CA certificate database is already present, nothing to do.
	// Remove the old style merged folder if it exists. If the merged folder exists, and
	// it's not a symlink, we remove the folder and regenerate the database
	mergedDir := CurrentCertificateDir()
	if osutil.IsSymlink(mergedDir) {
		// The merged directory is already a symlink, we assume it's correctly set up and skip generation.
		logger.Debugf("merged certificate database exists, running garbage collection")
		return m.ensureGarbageCollectionRun()
	} else {
		if err := os.RemoveAll(mergedDir); err != nil {
			// In case of weird errors in this case, log it and skip generation. If it was
			// a weird FS error, then we'll just retry again on next snapd startup.
			logger.Noticef("error checking merged certificate database: %v", err)
			return nil
		}
	}

	// Run garbage collection for the cert generations, but only after the symlink check.
	if err := m.ensureGarbageCollectionRun(); err != nil {
		return err
	}

	// Create the update CA certificate database, this is likely a first
	// run on a pre-existing system after this was introduced.
	// TODO: The database will be generated with the lock being held here, which is not
	// the best, as there is some overhead in file-generation and hashing.
	logger.Noticef("No CA certificate database found, generating it now")
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

	// Record the current generation before updating, so that
	// if the system reboots during the update, we have a known
	// generation to roll back to.
	// The function will do it's own lock/unlock to force a commit to the underlying
	// state to disk.
	_, err := recordCurrentCertificateGeneration(t, previousGenerationTaskKey)
	if err != nil {
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
