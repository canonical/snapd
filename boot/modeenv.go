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
	"strings"

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
	TryBase        string
	BaseStatus     string
	CurrentKernels []string

	// read is set to true when a modenv was read successfully
	read bool
}

var readModeenv = readModeenvImpl

// ReadModeenv attempts to read the modeenv file at
// <rootdir>/var/iib/snapd/modeenv.
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

func modeenvFile(rootdir string) string {
	if rootdir == "" {
		rootdir = dirs.GlobalRootDir
	}
	return dirs.SnapModeenvFileUnder(rootdir)
}

func readModeenvImpl(rootdir string) (*Modeenv, error) {
	modeenvPath := modeenvFile(rootdir)
	cfg := goconfigparser.New()
	cfg.AllowNoSectionHeader = true
	if err := cfg.ReadFile(modeenvPath); err != nil {
		return nil, err
	}
	// TODO:UC20: should we check these errors and try to do something?
	recoverySystem, _ := cfg.Get("", "recovery_system")
	mode, _ := cfg.Get("", "mode")
	base, _ := cfg.Get("", "base")
	baseStatus, _ := cfg.Get("", "base_status")
	tryBase, _ := cfg.Get("", "try_base")

	// current_kernels is a comma-delimited list in a string
	kernelsString, _ := cfg.Get("", "current_kernels")
	var kernels []string
	if kernelsString != "" {
		kernels = strings.Split(kernelsString, ",")
		// drop empty strings
		nonEmptyKernels := make([]string, 0, len(kernels))
		for _, kernel := range kernels {
			if kernel != "" {
				nonEmptyKernels = append(nonEmptyKernels, kernel)
			}
		}
		kernels = nonEmptyKernels
	}
	return &Modeenv{
		Mode:           mode,
		RecoverySystem: recoverySystem,
		Base:           base,
		TryBase:        tryBase,
		BaseStatus:     baseStatus,
		CurrentKernels: kernels,
		read:           true,
	}, nil
}

// Unset returns true if no modeenv file was read (yet)
func (m *Modeenv) Unset() bool {
	return !m.read
}

// Write outputs the modeenv to the file at <rootdir>/var/lib/snapd/modeenv.
func (m *Modeenv) Write(rootdir string) error {
	modeenvPath := modeenvFile(rootdir)

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
	if m.TryBase != "" {
		fmt.Fprintf(buf, "try_base=%s\n", m.TryBase)
	}
	if m.BaseStatus != "" {
		fmt.Fprintf(buf, "base_status=%s\n", m.BaseStatus)
	}
	if len(m.CurrentKernels) != 0 {
		fmt.Fprintf(buf, "current_kernels=%s\n", strings.Join(m.CurrentKernels, ","))
	}

	if err := osutil.AtomicWriteFile(modeenvPath, buf.Bytes(), 0644, 0); err != nil {
		return err
	}
	return nil
}
