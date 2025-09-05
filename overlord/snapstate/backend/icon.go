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

package backend

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// IconDownloadFilename returns the filepath of the icon in the icons pool
// directory for the given snap ID.
func IconDownloadFilename(snapID string) string {
	if snapID == "" {
		return ""
	}
	return filepath.Join(dirs.SnapIconsPoolDir, fmt.Sprintf("%s.icon", snapID))
}

// IconInstallFilename returns the filepath of the icon in the icons directory
// for the given snap ID. This is where the icon should be hard-linked from the
// iconDownloadFilename when the snap is installed on the system.
func IconInstallFilename(snapID string) string {
	if snapID == "" {
		return ""
	}
	return filepath.Join(dirs.SnapIconsDir, fmt.Sprintf("%s.icon", snapID))
}

// linkSnapIcon creates a hardlink from the downloaded icons pool to the icons
// directory for the given snap ID.
func linkSnapIcon(snapID string) error {
	if snapID == "" {
		return nil
	}

	poolPath := IconDownloadFilename(snapID)
	installPath := IconInstallFilename(snapID)

	if !osutil.CanStat(poolPath) {
		return fmt.Errorf("icon for snap: %w", fs.ErrNotExist)
	}

	if err := os.MkdirAll(dirs.SnapIconsDir, 0o755); err != nil {
		return fmt.Errorf("cannot create directory for snap icons: %w", err)
	}

	if err := osutil.AtomicLink(poolPath, installPath); err != nil {
		return fmt.Errorf("cannot link snap icon: %w", err)
	}
	return nil
}

// unlinkSnapIcon removes the hardlink from the downloaded icons pool to the
// icons directory for the given snap ID.
func unlinkSnapIcon(snapID string) error {
	if snapID == "" {
		return nil
	}
	if err := os.Remove(IconInstallFilename(snapID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("cannot unlink snap icon: %w", err)
	}
	return nil
}

// discardSnapIcon removes the icon for the given snap from the downloaded icons
// pool.
func discardSnapIcon(snapID string) error {
	if snapID == "" {
		return nil
	}
	if err := os.Remove(IconDownloadFilename(snapID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("cannot remove snap icon from pool: %w", err)
	}
	return nil
}
