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
	"github.com/snapcore/snapd/sysconfig"
)

const (
	motdOptionKey               = "system.motd"
	defaultMotdFilePathReadonly = "/usr/lib/motd.d/50-default"
	defaultMotdFilePathWritable = "/etc/motd.d/50-default"
)

var (
	osMkdirAll            = os.MkdirAll
	osReadFile            = os.ReadFile
	osRemove              = os.Remove
	osutilAtomicWriteFile = osutil.AtomicWriteFile
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
		return fmt.Errorf("cannot set message of the day: size %d KiB exceeds limit of 64 KiB", len(motdBytes))
	}
	return nil
}

func isMotdConfigurationSupported(rootDir string) bool {
	// Restrict this config to systems where defaultMotdFilepathReadonly exists.
	// Generally this means UC24+ systems.
	return osutil.FileExists(filepath.Join(rootDir, defaultMotdFilePathReadonly))
}

func handleMotdConfiguration(_ sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	rootDir := dirs.GlobalRootDir
	if opts != nil {
		rootDir = opts.RootDir
	}

	// Check if MOTD configuration is supported on this system
	if !isMotdConfigurationSupported(rootDir) {
		return errors.New("cannot set message of the day: unsupported on this system, requires UC24+")
	}

	motd, err := coreCfg(tr, motdOptionKey)
	if err != nil {
		return err
	}

	// coreCfg() returns an empty string if the config key was unset
	// or if the value was actually set to an empty string.
	// In either case, we want to reset the MOTD to defaultMotdFilepathReadonly
	// by deleting the defaultMotdFilepathWritable if it exists.
	motdFilePath := filepath.Join(rootDir, defaultMotdFilePathWritable)
	if motd == "" {
		if err := osRemove(motdFilePath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("cannot unset message of the day: %v", err)
		}
		return nil
	}

	if err := osMkdirAll(filepath.Dir(motdFilePath), 0755); err != nil {
		return fmt.Errorf("cannot set message of the day: %v", err)
	}
	motdBytes := []byte(motd + "\n")
	if err := osutilAtomicWriteFile(motdFilePath, motdBytes, 0644, 0); err != nil {
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
	if !isMotdConfigurationSupported(rootDir) {
		return "", nil
	}
	motdFilePath := filepath.Join(rootDir, defaultMotdFilePathWritable)
	if !osutil.FileExists(motdFilePath) {
		motdFilePath = filepath.Join(rootDir, defaultMotdFilePathReadonly)
	}
	motdBytes, err := osReadFile(motdFilePath)
	if err != nil {
		return "", fmt.Errorf("cannot get message of the day: %v", err)
	}
	return string(motdBytes), nil
}
