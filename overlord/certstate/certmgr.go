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

func Manager(st *state.State, runner *state.TaskRunner) *CertManager {
	m := &CertManager{
		state: st,
	}

	// register tasks to update the certificate database
	runner.AddHandler("update-cert-db", m.doUpdateCertificateDatabase, m.undoUpdateCertificateDatabase)

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
	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	return GenerateCertificateDatabase(mergedDir)
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

	// Swap the new certificate database into place.
	return osutil.SwapDirs(stagedDir, mergedDir)
}

func (m *CertManager) undoUpdateCertificateDatabase(_ *state.Task, _ *tomb.Tomb) error {
	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	stagedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged.staged")

	if exists, isDir, err := osutil.DirExists(stagedDir); err != nil {
		return err
	} else if !exists || !isDir {
		return fmt.Errorf("cannot undo certificate database update: missing backup directory %q", stagedDir)
	}

	if err := osutil.SwapDirs(stagedDir, mergedDir); err != nil {
		return err
	}

	if err := os.RemoveAll(stagedDir); err != nil {
		return err
	}
	return nil
}

func hasSystemCertsDir() bool {
	if exists, isDir, err := osutil.DirExists(dirs.SystemCertsDir); !exists || !isDir || err != nil {
		return false
	}
	return true
}
