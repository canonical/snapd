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
	"io"
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
	m := Modeenv{
		read:          true,
		originRootdir: rootdir,
	}
	unmarshalModeenvValueFromCfg(cfg, "recovery_system", &m.RecoverySystem)
	unmarshalModeenvValueFromCfg(cfg, "mode", &m.Mode)
	if m.Mode == "" {
		return nil, fmt.Errorf("internal error: mode is unset")
	}
	unmarshalModeenvValueFromCfg(cfg, "base", &m.Base)
	unmarshalModeenvValueFromCfg(cfg, "base_status", &m.BaseStatus)
	unmarshalModeenvValueFromCfg(cfg, "try_base", &m.TryBase)

	// current_kernels is a comma-delimited list in a string
	unmarshalModeenvValueFromCfg(cfg, "current_kernels", &m.CurrentKernels)
	var bm modeenvModel
	unmarshalModeenvValueFromCfg(cfg, "model", &bm)
	m.BrandID = bm.brandID
	m.Model = bm.model
	// expect the caller to validate the grade
	unmarshalModeenvValueFromCfg(cfg, "grade", &m.Grade)

	return &m, nil
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
	marshalNonEmptyForModeenvEntry(buf, "mode", m.Mode)
	marshalNonEmptyForModeenvEntry(buf, "recovery_system", m.RecoverySystem)
	marshalNonEmptyForModeenvEntry(buf, "base", m.Base)
	marshalNonEmptyForModeenvEntry(buf, "try_base", m.TryBase)
	marshalNonEmptyForModeenvEntry(buf, "base_status", m.BaseStatus)
	marshalNonEmptyForModeenvEntry(buf, "current_kernels", strings.Join(m.CurrentKernels, ","))
	if m.Model != "" || m.Grade != "" {
		if m.Model == "" {
			return fmt.Errorf("internal error: model is unset")
		}
		if m.BrandID == "" {
			return fmt.Errorf("internal error: brand is unset")
		}
		marshalNonEmptyForModeenvEntry(buf, "model", &modeenvModel{brandID: m.BrandID, model: m.Model})
	}
	marshalNonEmptyForModeenvEntry(buf, "grade", m.Grade)

	if err := osutil.AtomicWriteFile(modeenvPath, buf.Bytes(), 0644, 0); err != nil {
		return err
	}
	return nil
}

type modeenvValueMarshaller interface {
	MarshalModeenvValue() (string, error)
}

type modeenvValueUnmarshaller interface {
	UnmarshalModeenvValue(value string) error
}

func marshalNonEmptyForModeenvEntry(out io.Writer, key string, what interface{}) error {
	var asString string
	switch v := what.(type) {
	case string:
		if v == "" {
			return nil
		}
		asString = v
	case []string:
		if len(v) == 0 {
			return nil
		}
		asString = asModeenvStringList(v)
	default:
		vm, ok := what.(modeenvValueMarshaller)
		if !ok {
			return fmt.Errorf("internal error: cannot marshal value %+v for key %q", what, key)
		}
		marshalled, err := vm.MarshalModeenvValue()
		if err != nil {
			return fmt.Errorf("cannot marshal value for key %q: %v", key, err)
		}
		asString = marshalled
	}
	_, err := fmt.Fprintf(out, "%s=%s\n", key, asString)
	return err
}

func unmarshalModeenvValueFromCfg(cfg *goconfigparser.ConfigParser, key string, where interface{}) error {
	if where == nil {
		return fmt.Errorf("internal error: cannot unmarshal to nil")
	}
	kv, _ := cfg.Get("", key)

	switch v := where.(type) {
	case *string:
		*v = kv
	case *[]string:
		*v = splitModeenvStringList(kv)
	default:
		vm, ok := v.(modeenvValueUnmarshaller)
		if !ok {
			return fmt.Errorf("internal error: cannot unmarshal value %q for unsupported type %T", kv, where)
		}
		if err := vm.UnmarshalModeenvValue(kv); err != nil {
			return fmt.Errorf("cannot unmarshal value %q to %T: %v", kv, where, err)
		}
	}
	return nil
}

func splitModeenvStringList(v string) []string {
	if v == "" {
		return nil
	}
	split := strings.Split(v, ",")
	// drop empty strings
	nonEmpty := make([]string, 0, len(split))
	for _, one := range split {
		if one != "" {
			nonEmpty = append(nonEmpty, one)
		}
	}
	if len(nonEmpty) == 0 {
		return nil
	}
	return nonEmpty
}

func asModeenvStringList(v []string) string {
	return strings.Join(v, ",")
}

type modeenvModel struct {
	brandID, model string
}

func (m *modeenvModel) MarshalModeenvValue() (string, error) {
	return fmt.Sprintf("%s/%s", m.brandID, m.model), nil
}

func (m *modeenvModel) UnmarshalModeenvValue(brandSlashModel string) error {
	if bsmSplit := strings.SplitN(brandSlashModel, "/", 2); len(bsmSplit) == 2 {
		if bsmSplit[0] != "" && bsmSplit[1] != "" {
			m.brandID = bsmSplit[0]
			m.model = bsmSplit[1]
		}
	}
	return nil
}
