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
	Model          string
	BrandID        string
	Grade          string

	// read is set to true when a modenv was read successfully
	read bool

	// originRootdir is set to the root whence the modeenv was
	// read from, and where it will be written back to
	originRootdir string
}

func modeenvFile(rootdir string) string {
	if rootdir == "" {
		rootdir = dirs.GlobalRootDir
	}
	return dirs.SnapModeenvFileUnder(rootdir)
}

// ReadModeenv attempts to read the modeenv file at
// <rootdir>/var/iib/snapd/modeenv.
func ReadModeenv(rootdir string) (*Modeenv, error) {
	modeenvPath := modeenvFile(rootdir)
	cfg := goconfigparser.New()
	cfg.AllowNoSectionHeader = true
	if err := cfg.ReadFile(modeenvPath); err != nil {
		return nil, err
	}
	// TODO:UC20: should we check these errors and try to do something?
	recoverySystem, _ := cfg.Get("", "recovery_system")
	mode, _ := cfg.Get("", "mode")
	if mode == "" {
		return nil, fmt.Errorf("internal error: mode is unset")
	}
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
	brand := ""
	model := ""
	brandSlashModel, _ := cfg.Get("", "model")
	if bsmSplit := strings.SplitN(brandSlashModel, "/", 2); len(bsmSplit) == 2 {
		if bsmSplit[0] != "" && bsmSplit[1] != "" {
			brand = bsmSplit[0]
			model = bsmSplit[1]
		}
	}
	// expect the caller to validate the grade
	grade, _ := cfg.Get("", "grade")

	return &Modeenv{
		Mode:           mode,
		RecoverySystem: recoverySystem,
		Base:           base,
		TryBase:        tryBase,
		BaseStatus:     baseStatus,
		CurrentKernels: kernels,
		BrandID:        brand,
		Grade:          grade,
		Model:          model,
		read:           true,
		originRootdir:  rootdir,
	}, nil
}

// Write outputs the modeenv to the file where it was read, only valid on
// modeenv that has been read.
func (m *Modeenv) Write() error {
	if m.read {
		return m.WriteTo(m.originRootdir)
	}
	return fmt.Errorf("internal error: must use WriteTo with modeenv not read from disk")
}

// WriteTo outputs the modeenv to the file at <rootdir>/var/lib/snapd/modeenv.
func (m *Modeenv) WriteTo(rootdir string) error {
	modeenvPath := modeenvFile(rootdir)

	if err := os.MkdirAll(filepath.Dir(modeenvPath), 0755); err != nil {
		return err
	}
	buf := bytes.NewBuffer(nil)
	if m.Mode == "" {
		return fmt.Errorf("internal error: mode is unset")
	}
	fmt.Fprintf(buf, "mode=%s\n", m.Mode)

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
	if m.Model != "" || m.Grade != "" {
		if m.Model == "" {
			return fmt.Errorf("internal error: model is unset")
		}
		if m.BrandID == "" {
			return fmt.Errorf("internal error: brand is unset")
		}
		fmt.Fprintf(buf, "model=%s/%s\n", m.BrandID, m.Model)
	}
	if m.Grade != "" {
		fmt.Fprintf(buf, "grade=%s\n", m.Grade)
	}

	if err := osutil.AtomicWriteFile(modeenvPath, buf.Bytes(), 0644, 0); err != nil {
		return err
	}
	return nil
}
