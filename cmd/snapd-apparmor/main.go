// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

// This tool is provided for integration with systemd on distributions where
// apparmor profiles generated and managed by snapd are not loaded by the
// system-wide apparmor systemd integration on early boot-up.
//
// Only the start operation is provided as all other activity is managed by
// snapd as a part of the life-cycle of particular snaps.
//
// In addition this tool assumes that the system-wide apparmor service has
// already executed, initializing apparmor file-systems as necessary.
//
// NOTE: This tool ignores failures in some scenarios as the intent is to
// simply load application profiles ahead of time, as many as we can (for
// performance reasons), even if for whatever reason some of those fail.

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/release"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snapdtool"
)

// Checks to see if the current container is capable of having internal AppArmor
// profiles that should be loaded.
//
// The only known container environments capable of supporting internal policy
// are LXD, LXC and incus environments.
//
// Returns true if the container environment is capable of having its own internal
// policy and false otherwise.
//
// IMPORTANT: This function will return true in the case of a
// non-LXD/non-LXC/non-incus system container technology being nested inside of
// a LXD/LXC/incus container that utilized an AppArmor namespace and profile
// stacking. The reason true will be returned is because .ns_stacked will be
// "yes" and .ns_name will still match "(lx[dc]|incus)-*" since the nested
// system container technology will not have set up a new AppArmor profile
// namespace. This will result in the nested system container's boot process to
// experience failed policy loads but the boot process should continue without
// any loss of functionality. This is an unsupported configuration that cannot
// be properly handled by this function.
func isContainerWithInternalPolicy() bool {
	if release.OnWSL {
		return true
	}

	var appArmorSecurityFSPath = filepath.Join(dirs.GlobalRootDir, "/sys/kernel/security/apparmor")
	var nsStackedPath = filepath.Join(appArmorSecurityFSPath, ".ns_stacked")
	var nsNamePath = filepath.Join(appArmorSecurityFSPath, ".ns_name")

	contents, err := os.ReadFile(nsStackedPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		logger.Noticef("Failed to read %s: %v", nsStackedPath, err)
		return false
	}

	if strings.TrimSpace(string(contents)) != "yes" {
		return false
	}

	contents, err = os.ReadFile(nsNamePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		logger.Noticef("Failed to read %s: %v", nsNamePath, err)
		return false
	}

	// LXD, LXC and incus set up AppArmor namespaces starting with "lxd-",
	// "lxc-" and "incus-" respectively. Return false for all other
	// namespace identifiers.
	name := strings.TrimSpace(string(contents))
	if !strings.HasPrefix(name, "lxd-") && !strings.HasPrefix(name, "lxc-") && !strings.HasPrefix(name, "incus-") {
		return false
	}
	return true
}

func loadAppArmorProfiles() error {
	candidates, err := filepath.Glob(dirs.SnapAppArmorDir + "/*")
	if err != nil {
		return fmt.Errorf("Failed to glob profiles from snap apparmor dir %s: %v", dirs.SnapAppArmorDir, err)
	}

	profiles := make([]string, 0, len(candidates))
	for _, profile := range candidates {
		// Filter out profiles with names ending with ~, those are
		// temporary files created by snapd.
		if strings.HasSuffix(profile, "~") {
			continue
		}
		profiles = append(profiles, profile)
	}
	if len(profiles) == 0 {
		logger.Noticef("No profiles to load")
		return nil
	}
	logger.Noticef("Loading profiles %v", profiles)
	return apparmor_sandbox.LoadProfiles(profiles, apparmor_sandbox.SystemCacheDir, 0)
}

func isContainer() bool {
	// systemd's implementation may fail on WSL2 with custom kernels
	return release.OnWSL || (exec.Command("systemd-detect-virt", "--quiet", "--container").Run() == nil)
}

func validateArgs(args []string) error {
	if len(args) != 1 || args[0] != "start" {
		return errors.New("Expected to be called with a single 'start' argument.")
	}
	return nil
}

func init() {
	if err := logger.SimpleSetup(nil); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to activate logging: %v\n", err)
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	snapdtool.ExecInSnapdOrCoreSnap()

	if err := validateArgs(os.Args[1:]); err != nil {
		return err
	}

	if isContainer() {
		logger.Debugf("inside container environment")
		// in container environment - see if container has own
		// policy that we need to manage otherwise get out of the
		// way
		if !isContainerWithInternalPolicy() {
			logger.Noticef("Inside container environment without internal policy")
			return nil
		}
	}

	return loadAppArmorProfiles()
}

func mockParserSearchPath(parserSearchPath string) (restore func()) {
	return apparmor_sandbox.MockParserSearchPath(parserSearchPath)
}
