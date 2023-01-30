// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package configcore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/sysconfig"
)

// For mocking in tests
var (
	osutilEnsureFileState = osutil.EnsureFileState
	osutilDirExists       = osutil.DirExists

	apparmorUpdateHomedirsTunable = apparmor.UpdateHomedirsTunable
	apparmorReloadAllSnapProfiles = apparmor.ReloadAllSnapProfiles
)

var (
	// No path located under any of these top-level directories can be used.
	// This is not a security measure (the configuration can only be changed by
	// the system administrator anyways) but rather a check around unintended
	// mistakes.
	invalidPrefixes = []string{
		"/bin/",
		"/boot/",
		"/dev/",
		"/etc/",
		"/lib/",
		"/proc/",
		"/root/",
		"/sbin/",
		"/sys/",
		"/tmp/",
	}

	// TODO: remove this once we mount the root FS of a snap as tmpfs and
	// become able to create any mount points. But for the time being, let's
	// allow only those locations that can be supported without creating a
	// mimic of "/".
	validPrefixes = []string{
		"/home/",
	}
)

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.homedirs"] = true
}

func updateHomedirsConfig(config string, opts *fsOnlyContext) error {
	// if opts is not nil this is image build time
	rootDir := dirs.GlobalRootDir
	if opts != nil {
		rootDir = opts.RootDir
	}
	snapStateDir := dirs.SnapdStateDir(rootDir)
	if err := os.MkdirAll(snapStateDir, 0755); err != nil {
		return err
	}

	configPath := filepath.Join(snapStateDir, "system-params")
	contents := fmt.Sprintf("homedirs=%s\n", config)
	desiredState := &osutil.MemoryFileState{
		Content: []byte(contents),
		Mode:    0644,
	}
	if err := osutilEnsureFileState(configPath, desiredState); errors.Is(err, osutil.ErrSameState) {
		// The state is unchanged, nothing to do
		return nil
	} else if err != nil {
		return err
	}

	var homedirs []string
	if len(config) > 0 {
		homedirs = strings.Split(config, ",")
	}
	if err := apparmorUpdateHomedirsTunable(homedirs); err != nil {
		return err
	}

	// Only if run time
	if opts == nil {
		// We must reload the apparmor profiles in order for our changes to become
		// effective. In theory, all profiles are affected; in practice, we are a
		// bit egoist and only care about snapd.
		if err := apparmorReloadAllSnapProfiles(); err != nil {
			return err
		}
	}

	return nil
}

func handleHomedirsConfiguration(_ sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	config, err := coreCfg(tr, "homedirs")
	if err != nil {
		return err
	}

	if err := updateHomedirsConfig(config, opts); err != nil {
		return err
	}

	return nil
}

func validateHomedirsConfiguration(tr ConfGetter) error {
	config, err := coreCfg(tr, "homedirs")
	if err != nil {
		return err
	}

	if config == "" {
		return nil
	}

	homedirs := strings.Split(config, ",")
	for _, dir := range homedirs {
		if !filepath.IsAbs(dir) {
			return fmt.Errorf("path %q is not absolute", dir)
		}

		// Ensure that the paths are not too fancy, as this could cause
		// AppArmor to interpret them as patterns
		if err := apparmor.ValidateNoAppArmorRegexp(dir); err != nil {
			return fmt.Errorf("home path invalid: %v", err)
		}

		// Also make sure that the path is not going to interfere with standard
		// locations
		if dir[len(dir)-1] != '/' {
			dir += "/"
		}
		for _, prefix := range invalidPrefixes {
			if strings.HasPrefix(dir, prefix) {
				return fmt.Errorf("path %q uses reserved root directory %q", dir, prefix)
			}
		}

		// Temporary: see the comment on validPrefixes
		isValid := false
		for _, prefix := range validPrefixes {
			if strings.HasPrefix(dir, prefix) {
				isValid = true
				break
			}
		}
		if !isValid {
			formattedList := strings.Join(validPrefixes, ", ")
			return fmt.Errorf("path %q unsupported: must start with one of: %s", dir, formattedList)
		}

		exists, isDir, err := osutilDirExists(dir)
		if err != nil {
			return fmt.Errorf("cannot get directory info for %q: %v", dir, err)
		}
		if !exists {
			// There's no harm in letting this pass even if the directory does
			// not exist, as long as snap-confine handles it properly. But for
			// the time being let's err on the safe side.
			return fmt.Errorf("path %q does not exist", dir)
		}
		if !isDir {
			return fmt.Errorf("path %q is not a directory", dir)
		}
	}

	return err
}
