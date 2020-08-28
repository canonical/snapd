// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package sysconfig

import (
	"path/filepath"

	"github.com/snapcore/snapd/gadget"
)

// See https://github.com/snapcore/core20/pull/46
const writableDefaultsDir = "_writable_defaults"

// Options is the set of options used to configure the run system
type Options struct {
	// CloudInitSrcDir is where to find the cloud-init data when installing it,
	// i.e. in early boot install mode it could be something like
	// filepath.Join(boot.InitramfsUbuntuSeedDir,"data")
	CloudInitSrcDir string

	// TargetRootDir is the root directory where to install configure
	// data, i.e. for cloud-init during the initramfs it will be something like
	// boot.InstallHostWritableDir
	TargetRootDir string

	// AllowCloudInit is whether to allow cloud-init to run or not in the
	// TargetRootDir.
	AllowCloudInit bool

	// GadgetDir is the path of the mounted gadget snap.
	GadgetDir string
}

// FilesystemOnlyApplyOptions is the set of options for
// ApplyFilesystemOnlyDefaults.
type FilesystemOnlyApplyOptions struct {
	// Classic is true when the system in rootdir is a classic system
	Classic bool
}

// ApplyFilesystemOnlyDefaultsImpl is initialized by init() of configcore.
var ApplyFilesystemOnlyDefaultsImpl = func(rootDir string, defaults map[string]interface{}, options *FilesystemOnlyApplyOptions) error {
	panic("ApplyFilesystemOnlyDefaultsImpl is unset, import overlord/configstate/configcore")
}

// ApplyFilesystemOnlyDefaults applies (via configcore.filesystemOnlyApply())
// filesystem modifications under rootDir, according to the defaults.
// This is a subset of core config options that is important
// early during boot, before all the configuration is applied as part of
// normal execution of configure hook.
func ApplyFilesystemOnlyDefaults(rootDir string, defaults map[string]interface{}, options *FilesystemOnlyApplyOptions) error {
	return ApplyFilesystemOnlyDefaultsImpl(rootDir, defaults, options)
}

// ConfigureRunSystem configures the ubuntu-data partition with any
// configuration needed from e.g. the gadget or for cloud-init.
func ConfigureRunSystem(opts *Options) error {
	if err := configureCloudInit(opts); err != nil {
		return err
	}

	if opts.GadgetDir != "" {
		ginf, err := gadget.ReadInfo(opts.GadgetDir, nil)
		if err != nil {
			return err
		}
		defaults := gadget.SystemDefaults(ginf.Defaults)
		if len(defaults) > 0 {
			// options are nil which implies core system
			var options *FilesystemOnlyApplyOptions
			if err := ApplyFilesystemOnlyDefaults(WritableDefaultsDir(opts.TargetRootDir), defaults, options); err != nil {
				return err
			}
		}
	}

	return nil
}

// WritableDefaultsDir returns the full path of the joined subdir under the
// subtree for default content for system data living at rootdir,
// i.e. rootdir/_writable_defaults/subdir...
func WritableDefaultsDir(rootdir string, subdir ...string) string {
	return filepath.Join(rootdir, writableDefaultsDir, filepath.Join(subdir...))
}
