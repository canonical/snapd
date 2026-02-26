// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"io/fs"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/sysconfig"
)

const (
	motdOptionKey               = "system.motd"
	defaultMotdFilePathReadonly = "/usr/lib/motd.d/50-default"
	defaultMotdFilePathWritable = "/etc/motd.d/50-default"
)

func init() {
	// add supported configuration of this module
	supportedConfigurations["core."+motdOptionKey] = true
	config.RegisterExternalConfig("core", motdOptionKey, getMotdFromSystemHelper)
}

func validateMotdConfiguration(tr ConfGetter) error {
	motd, err := coreCfg(tr, motdOptionKey)
	if err != nil {
		return err
	}
	if motd == "" {
		return nil
	}

	// If MOTD is greater than 64 KiB, return an error
	// See `man 8 pam_motd`
	motdBytes := []byte(motd + "\n")
	if len(motdBytes) > 64*1024 {
		return fmt.Errorf("cannot set message of the day: size %d bytes exceeds limit of 65536 bytes", len(motdBytes))
	}
	return nil
}

func isMotdConfigurationSupported(base string) bool {
	// Restrict this config to UC24+ systems
	coreVersion, err := naming.CoreVersion(base)
	if err != nil {
		return false
	}
	return coreVersion >= 24
}

func handleMotdConfiguration(dev sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	var motd, rootDir string
	if opts == nil {
		// runtime system
		// if there is no change in MOTD configuration, then do nothing
		currentMotd, err := getMotdFromSystem()
		if err != nil {
			return err
		}
		// here coreCfg returns current value (always exists) overridden by changed value (if exists)
		motd, err = coreCfg(tr, motdOptionKey)
		if err != nil {
			return err
		}
		if currentMotd == motd {
			return nil
		}
		rootDir = dirs.GlobalRootDir
	} else {
		// here tr.Get returns changed value (if exists), otherwise returns NoOptionError
		err := tr.Get("core", motdOptionKey, &motd)
		if config.IsNoOption(err) {
			// there is no change in MOTD configuration, so do nothing
			return nil
		}
		if err != nil {
			return err
		}
		rootDir = opts.RootDir
	}

	// Check if MOTD configuration is supported on this system
	// Note: It's important to check if the motd option is actually being changed before checking
	// if it is supported because these handlers are called even if there is no change in the
	// configuration they are handling. So, this ensures that only when someone is trying to set
	// the motd option and if it is unsupported, then throw the error
	if !isMotdConfigurationSupported(dev.Base()) {
		return errors.New("cannot set message of the day: unsupported on this system, requires UC24+")
	}

	// coreCfg() returns an empty string if the config key was unset
	// or if the value was actually set to an empty string.
	// In either case, we want to reset the MOTD to defaultMotdFilepathReadonly
	// by deleting the defaultMotdFilepathWritable if it exists.
	motdFilePath := filepath.Join(rootDir, defaultMotdFilePathWritable)
	if motd == "" {
		if err := os.Remove(motdFilePath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("cannot unset message of the day: %v", err)
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(motdFilePath), 0755); err != nil {
		return fmt.Errorf("cannot set message of the day: %v", err)
	}
	motdBytes := []byte(motd + "\n")
	if err := osutil.AtomicWriteFile(motdFilePath, motdBytes, 0644, 0); err != nil {
		return fmt.Errorf("cannot set message of the day: %v", err)
	}
	return nil
}

func getMotdFromSystemHelper(key string) (any, error) {
	if key != motdOptionKey {
		return nil, fmt.Errorf("cannot get message of the day: invalid key %q, expected %s", key, motdOptionKey)
	}
	return getMotdFromSystem()
}

func getMotdFromSystem() (string, error) {
	rootDir := dirs.GlobalRootDir
	motdFilePath := filepath.Join(rootDir, defaultMotdFilePathWritable)
	if !osutil.FileExists(motdFilePath) {
		motdFilePath = filepath.Join(rootDir, defaultMotdFilePathReadonly)
		if !osutil.FileExists(motdFilePath) {
			// as both don't exist, motd option is not supported (no error)
			return "", nil
		}
	}
	motdBytes, err := os.ReadFile(motdFilePath)
	if err != nil {
		return "", fmt.Errorf("cannot get message of the day: %v", err)
	}
	return string(motdBytes), nil
}
