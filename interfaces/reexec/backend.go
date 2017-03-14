// -*- Mode: Go; indent-tabs-mode: t -*-

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

// Package reexec implements a backend which puts host security profiles
// in place for snapd when it re-execs.
package reexec

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

type Backend struct{}

func (b *Backend) Name() interfaces.SecuritySystem {
	return "reexec"
}

func (b *Backend) Setup(snapInfo *snap.Info, confinement interfaces.ConfinementOptions, repo *interfaces.Repository) error {
	// this is a very special interface
	if !release.OnClassic {
		return nil
	}
	if release.ReleaseInfo.ForceDevMode() {
		return nil
	}
	if snapInfo.Name() != "core" {
		return nil
	}

	coreRoot := snapInfo.MountDir()
	snapConfineInCore := filepath.Join(coreRoot, "usr/lib/snapd/snap-confine")
	apparmorProfilePath := filepath.Join(dirs.SystemApparmorDir, strings.Replace(snapConfineInCore[1:], "/", ".", -1))

	apparmorProfile, err := ioutil.ReadFile(filepath.Join(coreRoot, "/etc/apparmor.d/usr.lib.snapd.snap-confine"))
	if err != nil {
		return err
	}
	apparmorProfileForCore := strings.Replace(string(apparmorProfile), "/usr/lib/snapd/snap-confine", snapConfineInCore, -1)

	// /etc/apparmor.d is read/write OnClassic, so write out the
	// new core's profile there
	if err := osutil.AtomicWriteFile(apparmorProfilePath, []byte(apparmorProfileForCore), 0644, 0); err != nil {
		return err
	}

	// not using apparmor.LoadProfile() because it uses a different cachedir
	if output, err := exec.Command("apparmor_parser", "--replace", "--write-cache", apparmorProfilePath, "--cache-loc", dirs.SystemApparmorCacheDir).CombinedOutput(); err != nil {
		return fmt.Errorf("cannot replace snap-confine apparmor profile: %v", osutil.OutputErr(output, err))
	}

	return nil
}

func (b *Backend) Remove(snapName string) error {
	// FIXME: add cleanup
	return nil
}

func (b *Backend) NewSpecification() interfaces.Specification {
	return nil
}
