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

package snapstate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
)

// iconDownloadFilename returns the filepath of the icon in the icons pool
// directory for the given snap ID.
func iconDownloadFilename(snapID string) string {
	if snapID == "" {
		return ""
	}
	return filepath.Join(dirs.SnapIconsPoolDir, fmt.Sprintf("%s.icon", snapID))
}

// iconInstallFilename returns the filepath of the icon in the icons directory
// for the given snap ID. This is where the icon should be hard-linked from the
// iconDownloadFilename when the snap is installed on the system.
func iconInstallFilename(snapID string) string {
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
	poolPath := iconDownloadFilename(snapID)
	installPath := iconInstallFilename(snapID)
	if err := os.Link(poolPath, installPath); err != nil {
		return fmt.Errorf("cannot link snap icon for snap %s: %w", snapID, err)
	}
	return nil
}

// unlinkSnapIcon removes the hardlink from the downloaded icons pool to the
// icons directory for the given snap ID.
func unlinkSnapIcon(snapID string) error {
	if snapID == "" {
		return nil
	}
	if err := os.Remove(iconInstallFilename(snapID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("cannot unlink snap icon for snap %s: %w", snapID, err)
	}
	return nil
}

// discardSnapIcon removes the icon for the given snap from the downloaded icons
// pool.
func discardSnapIcon(snapID string) error {
	if snapID == "" {
		return nil
	}
	if err := os.Remove(iconDownloadFilename(snapID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("cannot remove snap icon from pool for snap %s: %v", snapID, err)
	}
	return nil
}
