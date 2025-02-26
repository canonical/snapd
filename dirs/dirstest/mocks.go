// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
package dirstest

import (
	"fmt"
	"os"
	"path/filepath"
)

// MustMockClassicConfinementAltDirSupport set up classic confinement support with
// alternative snap mount directory under a given root.
func MustMockClassicConfinementAltDirSupport(root string) {
	if err := os.Symlink(
		filepath.Join(root, "/var/lib/snapd/snap"),
		filepath.Join(root, "/snap"),
	); err != nil {
		panic(fmt.Errorf("cannot set up symlink: %w", err))
	}
}

// MustMockAltSnapMountDir set up alternative snap mount directory in a given root.
func MustMockAltSnapMountDir(root string) {
	if err := os.MkdirAll(filepath.Join(root, "/var/lib/snapd/snap"), 0o755); err != nil {
		panic(fmt.Errorf("cannot mkdir path: %w", err))
	}
}

// MustMockCanonicalSnapMountDir set up canonical snap mount directory in a given root.
func MustMockCanonicalSnapMountDir(root string) {
	if err := os.Mkdir(filepath.Join(root, "/snap"), 0o755); err != nil {
		panic(fmt.Errorf("cannot set up symlink: %w", err))
	}
}
