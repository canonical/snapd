// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package boot

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mvo5/goconfigparser"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// Modeenv is a file on UC20 that provides additional information
// about the current mode (run,recover,install)
type Modeenv struct {
	Mode           string
	RecoverySystem string
	Base           string
	// XXX: we may need to revisit setting the kernel in modeenv
	Kernel string

	// read is set to true when a modenv was read successfully
	read bool
}

var readModeenv = readModeenvImpl

// ReadModeenv attempts to read the file specified by
// "rootdir/dirs.SnapModeenvFile" as a Modeenv.
func ReadModeenv(rootdir string) (*Modeenv, error) {
	return readModeenv(rootdir)
}

// MockReadModeenv replaces the current implementation of ReadModeenv with a
// mocked one. For use in tests.
func MockReadModeenv(f func(rootdir string) (*Modeenv, error)) (restore func()) {
	old := readModeenv
	readModeenv = f
	return func() {
		readModeenv = old
	}
}

func readModeenvImpl(rootdir string) (*Modeenv, error) {
	modeenvPath := filepath.Join(rootdir, dirs.SnapModeenvFile)
	cfg := goconfigparser.New()
	cfg.AllowNoSectionHeader = true
	if err := cfg.ReadFile(modeenvPath); err != nil {
		return nil, err
	}
	recoverySystem, _ := cfg.Get("", "recovery_system")
	mode, _ := cfg.Get("", "mode")
	base, _ := cfg.Get("", "base")
	kernel, _ := cfg.Get("", "kernel")
	return &Modeenv{
		Mode:           mode,
		RecoverySystem: recoverySystem,
		Base:           base,
		Kernel:         kernel,
		read:           true,
	}, nil
}

// Unset returns true if no modeenv file was read (yet)
func (m *Modeenv) Unset() bool {
	return !m.read
}

// Write outputs the modeenv to the file specified by
// "rootdir/dirs.SnapModeenvFile".
func (m *Modeenv) Write(rootdir string) error {
	modeenvPath := filepath.Join(rootdir, dirs.SnapModeenvFile)

	if err := os.MkdirAll(filepath.Dir(modeenvPath), 0755); err != nil {
		return err
	}
	buf := bytes.NewBuffer(nil)
	if m.Mode != "" {
		fmt.Fprintf(buf, "mode=%s\n", m.Mode)
	}
	if m.RecoverySystem != "" {
		fmt.Fprintf(buf, "recovery_system=%s\n", m.RecoverySystem)
	}
	if m.Base != "" {
		fmt.Fprintf(buf, "base=%s\n", m.Base)
	}
	if m.Kernel != "" {
		fmt.Fprintf(buf, "kernel=%s\n", m.Kernel)
	}
	if err := osutil.AtomicWriteFile(modeenvPath, buf.Bytes(), 0644, 0); err != nil {
		return err
	}
	return nil
}
