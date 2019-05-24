// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package mount

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/cmd"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

// mountNsPath returns path of the system-wide mount namespace file of a given snap.
func mountNsPath(snapName string) string {
	// NOTE: This value has to be synchronized with snap-confine
	return filepath.Join(dirs.SnapRunNsDir, fmt.Sprintf("%s.mnt", snapName))
}

// mountNsPathForUser returns path of the per-user mount namespace file of a given snap and user ID.
func mountNsPathForUser(snapName string, uid int) string {
	// NOTE: This value has to be synchronized with snap-confine
	return filepath.Join(dirs.SnapRunNsDir, fmt.Sprintf("%s.mnt", snapName))
}

// mountNsGlobForUser returns a glob pattern for per-user mount namespaces of a given snap.
func mountNsGlobForUser(snapName string) string {
	return filepath.Join(dirs.SnapRunNsDir, snapName+".*.mnt")
}

// runSnapDiscardNs runs snap-discard-ns with a given snap name.
func runSnapDiscardNs(snapName string) error {
	// Discard is unconditional because it affects .fstab, .user-fstab and .mnt files.
	toolPath, err := cmd.InternalToolPath("snap-discard-ns")
	if err != nil {
		return err
	}
	cmd := exec.Command(toolPath, snapName)
	logger.Debugf("running snap-discard-ns %q", snapName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot discard preserved namespace of snap %q: %s", snapName, osutil.OutputErr(output, err))
	}
	return nil
}

// runSnapUpdateNs runs snap-update-ns with a given snap name.
func runSnapUpdateNs(snapName string) error {
	if !osutil.FileExists(mountNsPath(snapName)) {
		return nil
	}
	toolPath, err := cmd.InternalToolPath("snap-update-ns")
	if err != nil {
		return err
	}
	cmd := exec.Command(toolPath, snapName)
	logger.Debugf("running snap-update-ns %q", snapName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot update preserved namespace of snap %q: %s", snapName, osutil.OutputErr(output, err))
	}
	return nil
}

// runSnapUpdateNsForUser runs snap-update-ns with a given snap name and user identifier.
func runSnapUpdateNsForUser(snapName string, uid int) error {
	if !osutil.FileExists(mountNsPathForUser(snapName, uid)) {
		return nil
	}
	toolPath, err := cmd.InternalToolPath("snap-update-ns")
	if err != nil {
		return err
	}
	cmd := exec.Command(toolPath, "--user-mounts", "-u", strconv.Itoa(uid), snapName)
	logger.Debugf("running snap-update-ns --user-mounts -u %d %q", uid, snapName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot update preserved namespace of snap %q for user %d: %s", snapName, uid, osutil.OutputErr(output, err))
	}
	return nil
}

// Discard the mount namespace of a given snap.
func DiscardSnapNamespace(snapName string) error {
	return runSnapDiscardNs(snapName)
}

// Update the mount namespace of a given snap.
func UpdateSnapNamespace(snapName string) error {
	// TODO: switch to --inherit-lock and lock here.
	if err := runSnapUpdateNs(snapName); err != nil {
		return err
	}
	if !features.PerUserMountNamespace.IsEnabled() {
		return nil
	}
	matches, err := filepath.Glob(mountNsGlobForUser(snapName))
	if err != nil {
		return err
	}
	for _, match := range matches {
		fi, err := os.Stat(match)
		if err != nil {
			logger.Noticef("cannot stat preserved mount namespace: %s", err)
			continue
		}
		if !fi.Mode().IsRegular() {
			continue
		}
		_, fname := filepath.Split(match)
		// fname has the form "$SNAP_NAME.$UID.mnt"
		d1 := strings.IndexByte(fname, '.')
		d2 := strings.LastIndexByte(fname, '.')
		if d1 == -1 || d2 == -1 || d1 == d2 {
			logger.Noticef("malformed mount namespace name %q", fname)
			continue
		}
		uid, err := strconv.Atoi(fname[d1+1 : d2])
		if err != nil {
			logger.Noticef("cannot parse user ID: %s", err)
		}
		if err := runSnapUpdateNsForUser(snapName, uid); err != nil {
			return err
		}
	}
	return nil
}
