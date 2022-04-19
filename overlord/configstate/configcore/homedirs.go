// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers
// +build !nomanagers

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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/sandbox/apparmor"
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
	// allow only those locations that can be supported without creating mimics
	validPrefixes = []string{
		"/home/",
	}
)

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.homedirs"] = true
}

func updateHomedirsConfig(config string) error {
	configPath := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "system.info")
	f, err := os.OpenFile(configPath, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "Homedirs=%s\n", config); err != nil {
		return err
	}

	homedirs := strings.Split(config, ",")
	if err := apparmor.UpdateHomedirsTunable(homedirs); err != nil {
		return err
	}

	return nil
}

func handleHomedirsConfiguration(tr config.Conf, opts *fsOnlyContext) error {
	config, err := coreCfg(tr, "homedirs")
	if err != nil {
		return err
	}

	if err := updateHomedirsConfig(config); err != nil {
		return err
	}

	return nil
}

func validateHomedirsConfiguration(tr config.Conf) error {
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

		info, err := os.Stat(dir)
		if err != nil {
			// TODO: actually decide if this should be returned as an error;
			// there's no harm in letting this pass even if the directory does
			// not exist, as long as snap-confine handles it properly.
			return fmt.Errorf("cannot get directory info for %q: %v", dir, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("path %q is not a directory", dir)
		}
	}

	return err
}
