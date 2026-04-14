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
	"github.com/snapcore/snapd/overlord/state"
	"gopkg.in/tomb.v2"
)

type CertManager struct {
	state                               *state.State
	ensureEarlyCertificateGenerationRan bool
}

const doUpdateCertificateDatabaseMarker = ".certdb-do-refresh"
const undoUpdateCertificateDatabaseMarker = ".certdb-undo-refresh"

func Manager(st *state.State, runner *state.TaskRunner) *CertManager {
	m := &CertManager{
		state: st,
	}

	// register tasks to update the certificate database
	runner.AddHandler("update-cert-db", m.doUpdateCertificateDatabase, m.undoUpdateCertificateDatabase)
	runner.AddCleanup("update-cert-db", m.cleanupUpdateCertificateDatabase)

	return m
}

func (m *CertManager) Ensure() error {
	st := m.state
	st.Lock()
	defer st.Unlock()

	if m.ensureEarlyCertificateGenerationRan {
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

	m.ensureEarlyCertificateGenerationRan = true

	// If the CA certificate database is already present, nothing to do.
	certDbPath := filepath.Join(dirs.SnapdPKIV1Dir, "merged", "ca-certificates.crt")
	if osutil.FileExists(certDbPath) {
		logger.Debugf("ca-certificate database has already been generated, skipping generation")
		return nil
	}

	// If the ssl certs directory is missing, nothing to do.
	if !hasSystemCertsDir() {
		logger.Debugf("/etc/ssl/certs is not available on this system, skipping ca-certificates generation")
		return nil
	}

	// Create the update CA certificate database, this is likely a first
	// run on a pre-existing system after this was introduced.
	logger.Noticef("No CA certificate database found, generating it now")
	return RefreshCertificateDatabase()
}

func (m *CertManager) doUpdateCertificateDatabase(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	if !hasSystemCertsDir() {
		t.Logf("/etc/ssl/certs is not available on this system, skipping certificate database update")
		return nil
	}

	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	stagedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged.staged")

	markerInMerged := filepath.Join(mergedDir, doUpdateCertificateDatabaseMarker)
	markerInStaged := filepath.Join(stagedDir, doUpdateCertificateDatabaseMarker)

	// A retry after reboot may observe the marker already moved into
	// merged. In that case the swap already happened and we must preserve the
	// staged backup for undo instead of refreshing again.
	if osutil.FileExists(markerInMerged) {
		return nil
	}

	// Start with a clean staged directory.
	if err := os.RemoveAll(stagedDir); err != nil {
		return err
	}

	// SwapDirs requires both paths to exist, so create an empty merged
	// directory when there is no prior certificate database yet.
	if err := os.MkdirAll(mergedDir, 0o755); err != nil {
		return fmt.Errorf("cannot create merged certificates directory: %v", err)
	}

	if err := GenerateCertificateDatabase(stagedDir); err != nil {
		return err
	}

	if err := os.WriteFile(markerInStaged, nil, 0o600); err != nil {
		return fmt.Errorf("cannot create certificate database swap marker %v", err)
	}

	// Swap the new certificate database into place.
	if err := osutil.SwapDirs(stagedDir, mergedDir); err != nil {
		return fmt.Errorf("cannot swap certificate database directories: %v", err)
	}
	return nil
}

func (m *CertManager) undoUpdateCertificateDatabase(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	defer t.State().Unlock()

	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	stagedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged.staged")

	markerInMerged := filepath.Join(mergedDir, undoUpdateCertificateDatabaseMarker)
	markerInStaged := filepath.Join(stagedDir, undoUpdateCertificateDatabaseMarker)

	// The marker moves with the restored directory across the swap, so a retry
	// after a reboot can tell whether undo already flipped merged and only needs
	// to finish cleanup.
	if osutil.FileExists(markerInMerged) {
		return nil
	}

	if exists, isDir, err := osutil.DirExists(stagedDir); err != nil {
		return err
	} else if !exists || !isDir {
		t.Logf("cannot undo certificate database update: missing backup directory %q", stagedDir)
		return nil
	}

	if err := os.WriteFile(markerInStaged, nil, 0o600); err != nil {
		return err
	}
	if err := osutil.SwapDirs(stagedDir, mergedDir); err != nil {
		return err
	}

	return nil
}

func (m *CertManager) cleanupUpdateCertificateDatabase(t *state.Task, _ *tomb.Tomb) error {
	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	stagedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged.staged")

	if err := os.RemoveAll(stagedDir); err != nil {
		return err
	}

	for _, marker := range []string{doUpdateCertificateDatabaseMarker, undoUpdateCertificateDatabaseMarker} {
		markerInMerged := filepath.Join(mergedDir, marker)
		if err := os.Remove(markerInMerged); err != nil && !os.IsNotExist(err) {
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

// Cleanup marker + staged on task cleanup handlers
// Mount namespace - snapshot the certs into a tmpfs
