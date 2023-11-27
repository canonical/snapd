// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package interfaces

import (
	"fmt"
	"path/filepath"
	"strings"
)

// EnsureDirSpec contains the information required to ensure the existence of a directory.
// MustExistDir is the prefix of EnsureDir and must exist as prerequisite for creation for
// the remainder of missing directories of EnsureDir.
type EnsureDirSpec struct {
	MustExistDir string
	EnsureDir    string
}

// Validate returns an error if the ensure directory specification is not valid.
func (spec *EnsureDirSpec) Validate() error {
	if spec.MustExistDir != filepath.Clean(spec.MustExistDir) {
		return fmt.Errorf("directory that must exist %q is not a clean path", spec.MustExistDir)
	}
	if spec.EnsureDir != filepath.Clean(spec.EnsureDir) {
		return fmt.Errorf("directory to ensure %q is not a clean path", spec.EnsureDir)
	}

	hasEnvPrefix := func(path string) (hasPrefix bool, envPrefix string) {
		pathElements := strings.Split(path, string(filepath.Separator))
		if strings.HasPrefix(pathElements[0], "$") {
			return true, pathElements[0]
		}
		return false, ""
	}

	// Extend this allowed list as required
	allowedEnvPrefixes := []string{"$HOME"}
	isAllowedEnvPrefix := func(envPrefix string) (isAllowed bool) {
		for _, allowedEnvPrefix := range allowedEnvPrefixes {
			if envPrefix == allowedEnvPrefix {
				return true
			}
		}
		return false
	}

	if doCheck, envPrefix := hasEnvPrefix(spec.MustExistDir); doCheck {
		if !isAllowedEnvPrefix(envPrefix) {
			return fmt.Errorf("directory that must exist %q prefix %q is not allowed", spec.MustExistDir, envPrefix)
		}
	} else if !filepath.IsAbs(spec.MustExistDir) {
		return fmt.Errorf("directory that must exist %q is not an absolute path", spec.MustExistDir)
	}
	if doCheck, envPrefix := hasEnvPrefix(spec.EnsureDir); doCheck {
		if !isAllowedEnvPrefix(envPrefix) {
			return fmt.Errorf("directory to ensure %q prefix %q is not allowed", spec.EnsureDir, envPrefix)
		}
	} else if !filepath.IsAbs(spec.EnsureDir) {
		return fmt.Errorf("directory to ensure %q is not an absolute path", spec.EnsureDir)
	}

	isParent := spec.EnsureDir != spec.MustExistDir && (spec.MustExistDir == "/" || strings.HasPrefix(spec.EnsureDir, spec.MustExistDir+string(filepath.Separator)))
	if !isParent {
		return fmt.Errorf("directory that must exist %q is not a parent of directory to ensure %q", spec.MustExistDir, spec.EnsureDir)
	}
	return nil
}
