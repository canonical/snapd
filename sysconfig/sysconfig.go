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
	"fmt"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/snap"
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

	// GadgetSnap is a snap.Container of the gadget snap. This is used in
	// priority over GadgetDir if set.
	GadgetSnap snap.Container
}

// Device carries information about the device model and mode that is
// relevant to sysconfig.
type Device interface {
	RunMode() bool
	Classic() bool

	Kernel() string
	// Base() string

	HasModeenv() bool

	// Model() *asserts.Model
}

type configedDevice struct {
	model *asserts.Model
}

func (di *configedDevice) RunMode() bool {
	// the functions in sysconfig are used to configure not yet
	// running systems.
	return false
}

func (d *configedDevice) Classic() bool {
	return d.model.Classic()
}

func (d *configedDevice) Kernel() string {
	return d.model.Kernel()
}

func (d *configedDevice) HasModeenv() bool {
	return d.model.Grade() != asserts.ModelGradeUnset
}

// ApplyFilesystemOnlyDefaultsImpl is initialized by init() of configcore.
var ApplyFilesystemOnlyDefaultsImpl = func(dev Device, rootDir string, defaults map[string]interface{}) error {
	panic("ApplyFilesystemOnlyDefaultsImpl is unset, import overlord/configstate/configcore")
}

// ApplyFilesystemOnlyDefaults applies (via configcore.filesystemOnlyApply())
// filesystem modifications under rootDir, according to the defaults.
// This is a subset of core config options that is important
// early during boot, before all the configuration is applied as part of
// normal execution of configure hook.
func ApplyFilesystemOnlyDefaults(model *asserts.Model, rootDir string, defaults map[string]interface{}) error {
	dev := &configedDevice{model: model}
	return ApplyFilesystemOnlyDefaultsImpl(dev, rootDir, defaults)
}

// ConfigureTargetSystem configures the ubuntu-data partition with
// any configuration needed from e.g. the gadget or for cloud-init (and also for
// cloud-init from the gadget).
// It is okay to use both from install mode for run mode, as well as from the
// initramfs for recover mode.
// It is only meant to be used with models that have a grade (i.e. UC20+).
func ConfigureTargetSystem(model *asserts.Model, opts *Options) error {
	// check that we have a uc20 model
	if model.Grade() == asserts.ModelGradeUnset {
		return fmt.Errorf("internal error: ConfigureTargetSystem can only be used with a model with a grade")
	}
	mylog.Check(configureCloudInit(model, opts))

	var gadgetInfo *gadget.Info

	switch {
	case opts.GadgetSnap != nil:
		// we do not perform consistency validation here because
		// such unlikely problems are better surfaced in different
		// and less surprising contexts like the seeding itself
		gadgetInfo = mylog.Check2(gadget.ReadInfoFromSnapFileNoValidate(opts.GadgetSnap, nil))
	case opts.GadgetDir != "":
		gadgetInfo = mylog.Check2(gadget.ReadInfo(opts.GadgetDir, nil))
	}

	if gadgetInfo != nil {
		defaults := gadget.SystemDefaults(gadgetInfo.Defaults)
		if len(defaults) > 0 {
			mylog.Check(
				// TODO for classic with modes we do not want it under
				// _writable_defaults folder.
				ApplyFilesystemOnlyDefaults(model, WritableDefaultsDir(opts.TargetRootDir), defaults))
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
