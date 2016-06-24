// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2016 Canonical Ltd
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

package refresh

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
)

// CallFakeSnap calls snappy update after faking a new version available for the specified snap.
// The fake is made copying the currently installed snap.
func CallFakeSnap(snaps []string, blobDir string) error {
	for _, snap := range snaps {
		if err := makeFakeRefreshForSnap(snap, blobDir); err != nil {
			return err
		}
	}
	return nil
}

func makeFakeRefreshForSnap(snap, targetDir string) error {
	// make a fake update snap in /var/tmp (which is not a tempfs)
	fakeUpdateDir, err := ioutil.TempDir("/var/tmp", "snap-build-")
	if err != nil {
		return fmt.Errorf("creating tmp for fake update: %v", err)
	}
	// ensure the "." of the squashfs has sane owner/permissions
	err = exec.Command("sudo", "chown", "root:root", fakeUpdateDir).Run()
	if err != nil {
		return fmt.Errorf("changing owner of fake update dir: %v", err)
	}
	err = exec.Command("sudo", "chmod", "0755", fakeUpdateDir).Run()
	if err != nil {
		return fmt.Errorf("changing permissions of fake update dir: %v", err)
	}
	defer exec.Command("sudo", "rm", "-rf", fakeUpdateDir)

	err = copySnap(snap, fakeUpdateDir)
	if err != nil {
		return fmt.Errorf("copying snap: %v", err)
	}

	// fake new version
	err = exec.Command("sudo", "sed", "-i", `s/version:\(.*\)/version:\1+fake1/`, filepath.Join(fakeUpdateDir, "meta/snap.yaml")).Run()
	if err != nil {
		return fmt.Errorf("changing fake snap version: %v", err)
	}
	return buildSnap(fakeUpdateDir, targetDir)
}

func copySnap(snap, targetDir string) error {
	// check for sideloaded snaps
	// XXX: simplify this down to consider only the name (and not origin)
	// in the directory once everything is moved to that
	baseDir := filepath.Join(dirs.SnapSnapsDir, snap)
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		snapName := strings.Split(snap, ".")[0]
		baseDir = filepath.Join(dirs.SnapSnapsDir, snapName)
		if _, err := os.Stat(baseDir); os.IsNotExist(err) {
			baseDir = filepath.Join(dirs.SnapSnapsDir, snapName+".sideload")
			_, err = os.Stat(baseDir)
			if err != nil {
				return fmt.Errorf("%s not found from it's original source not sideloaded", snap)
			}
		}
	}
	sourceDir := filepath.Join(baseDir, "current")
	files, err := filepath.Glob(filepath.Join(sourceDir, "*"))
	if err != nil {
		return err
	}
	for _, m := range files {
		if err = exec.Command("sudo", "cp", "-a", m, targetDir).Run(); err != nil {
			return err
		}
	}
	return nil
}

func buildSnap(snapDir, targetDir string) error {
	// build in /var/tmp (which is not a tempfs)
	cmd := exec.Command("snapbuild", snapDir, targetDir)
	cmd.Env = append(os.Environ(), "TMPDIR=/var/tmp")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("building fake snap :%s : %v", output, err)
	}
	return nil
}
