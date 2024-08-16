// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sysconfig"
)

const (
	optionCoredumpEnable     = "system.coredump.enable"
	optionCoredumpMaxuse     = "system.coredump.maxuse"
	coreOptionCoredumpEnable = "core." + optionCoredumpEnable
	coreOptionCoredumpMaxuse = "core." + optionCoredumpMaxuse

	coredumpCfgSubdir = "coredump.conf.d"
	coredumpCfgFile   = "ubuntu-core.conf"
)

func init() {
	// add supported configuration of this module
	supportedConfigurations[coreOptionCoredumpEnable] = true
	supportedConfigurations[coreOptionCoredumpMaxuse] = true
}

func validMaxUseSize(sizeStr string) error {
	if sizeStr == "" {
		return nil
	}

	_, err := quantity.ParseSize(sizeStr)
	if err != nil {
		return err
	}

	return nil
}

func validateCoredumpSettings(tr ConfGetter) error {
	if err := validateBoolFlag(tr, optionCoredumpEnable); err != nil {
		return err
	}

	maxUse, err := coreCfg(tr, optionCoredumpMaxuse)
	if err != nil {
		return err
	}

	return validMaxUseSize(maxUse)
}

func handleCoredumpConfiguration(dev sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	// Rule out UC16/18 as we will not backport systemd-coredump there
	if !dev.HasModeenv() {
		return nil
	}

	coreEnabled, err := coreCfg(tr, optionCoredumpEnable)
	if err != nil {
		return err
	}

	cfgContent := "[Coredump]\n"
	switch coreEnabled {
	case "", "false":
		cfgContent += "Storage=none\nProcessSizeMax=0\n"
	case "true":
		maxUse, err := coreCfg(tr, optionCoredumpMaxuse)
		if err != nil {
			return err
		}
		cfgContent += "Storage=external\n"
		if maxUse != "" {
			cfgContent += fmt.Sprintf("MaxUse=%s\n", maxUse)
		}
	}

	var coredumpCfgDir string
	if opts == nil {
		// runtime system
		coredumpCfgDir = dirs.SnapSystemdDir
	} else {
		coredumpCfgDir = dirs.SnapSystemdDirUnder(opts.RootDir)
	}
	coredumpCfgDir = filepath.Join(coredumpCfgDir, coredumpCfgSubdir)
	if err := os.MkdirAll(coredumpCfgDir, 0755); err != nil {
		return err
	}

	// Ensure content of configuration file (path is
	// /etc/systemd/coredump.conf.d/ubuntu-core.conf)
	dirContent := map[string]osutil.FileState{
		coredumpCfgFile: &osutil.MemoryFileState{
			Content: []byte(cfgContent),
			Mode:    0644,
		},
	}
	if _, _, err = osutil.EnsureDirState(coredumpCfgDir, coredumpCfgFile, dirContent); err != nil {
		return err
	}

	return nil
}
