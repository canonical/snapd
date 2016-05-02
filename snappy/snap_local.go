// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v2"

	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
)

// Snap represents a generic snap type
type Snap struct {
	info *snap.Info

	isActive bool
}

// NewInstalledSnap returns a new Snap from the given yamlPath
func NewInstalledSnap(yamlPath string) (*Snap, error) {
	mountDir := filepath.Dir(filepath.Dir(yamlPath))

	// XXX: hack the name and revision out of the path for now
	// snapstate primitives shouldn't need this
	name := filepath.Base(filepath.Dir(mountDir))
	revnoStr := filepath.Base(mountDir)
	revno, err := strconv.Atoi(revnoStr)
	if err != nil {
		return nil, fmt.Errorf("broken snap directory path: %q", mountDir)
	}

	s := &Snap{}

	// check if the snap is active
	allRevnosDir := filepath.Dir(mountDir)
	p, err := filepath.EvalSymlinks(filepath.Join(allRevnosDir, "current"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	if p == mountDir {
		s.isActive = true
	}

	yamlBits, err := ioutil.ReadFile(yamlPath)
	if err != nil {
		return nil, err
	}

	info, err := snap.InfoFromSnapYaml(yamlBits)
	if err != nil {
		return nil, err
	}

	s.info = info

	if revno != 0 {
		mfPath := manifestPath(name, revno)
		if osutil.FileExists(mfPath) {
			content, err := ioutil.ReadFile(mfPath)
			if err != nil {
				return nil, err
			}

			var manifest snap.SideInfo
			if err := yaml.Unmarshal(content, &manifest); err != nil {
				return nil, &ErrInvalidYaml{File: mfPath, Err: err, Yaml: content}
			}
			info.SideInfo = manifest
		}
	}

	if info.Developer == "" {
		info.Developer = SideloadedDeveloper
	}
	if info.Channel == "" {
		// default for compat with older installs
		info.Channel = "stable"
	}

	return s, nil
}

// Type returns the type of the Snap (app, gadget, ...)
func (s *Snap) Type() snap.Type {
	return s.info.Type
}

// Name returns the name
func (s *Snap) Name() string {
	return s.info.Name()
}

// Version returns the version
func (s *Snap) Version() string {
	return s.info.Version
}

// Revision returns the revision
func (s *Snap) Revision() int {
	return s.info.Revision

}

// Developer returns the developer
func (s *Snap) Developer() string {
	return s.info.Developer

}

// IsActive returns true if the snap is active
func (s *Snap) IsActive() bool {
	return s.isActive
}

// Info returns the snap.Info data.
func (s *Snap) Info() *snap.Info {
	return s.info
}

// NeedsReboot returns true if the snap becomes active on the next reboot
func (s *Snap) NeedsReboot() bool {
	return kernelOrOsRebootRequired(s.info)
}
