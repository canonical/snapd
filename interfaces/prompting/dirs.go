// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
package prompting

import (
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
)

// StateDir returns the path to the prompting state directory.
func StateDir() string {
	return filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "interfaces-requests")
}

// EnsureStateDir creates the state directory with appropriate permissions.
func EnsureStateDir() error {
	return os.MkdirAll(StateDir(), 0o755)
}
