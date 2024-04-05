// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/sysconfig"
)

// valid pi config keys
var piConfigKeys = map[string]bool{
	"disable_overscan":         true,
	"force_turbo":              true,
	"framebuffer_width":        true,
	"framebuffer_height":       true,
	"framebuffer_depth":        true,
	"framebuffer_ignore_alpha": true,
	"overscan_left":            true,
	"overscan_right":           true,
	"overscan_top":             true,
	"overscan_bottom":          true,
	"overscan_scale":           true,
	"display_rotate":           true,
	"hdmi_cvt":                 true,
	"hdmi_group":               true,
	"hdmi_mode":                true,
	"hdmi_timings":             true,
	"hdmi_drive":               true,
	"avoid_warnings":           true,
	"gpu_mem_256":              true,
	"gpu_mem_512":              true,
	"gpu_mem":                  true,
	"sdtv_aspect":              true,
	"sdtv_mode":                true,
	"config_hdmi_boost":        true,
	"hdmi_force_hotplug":       true,
	"start_x":                  true,
}

func init() {
	// add supported config keys
	for k := range piConfigKeys {
		s := fmt.Sprintf("core.pi-config.%s", strings.Replace(k, "_", "-", -1))
		supportedConfigurations[s] = true
	}
}

func updatePiConfig(path string, config map[string]string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	toWrite, err := updateKeyValueStream(f, piConfigKeys, config)
	if err != nil {
		return err
	}

	if toWrite != nil {
		s := strings.Join(toWrite, "\n")
		// ensure we have a final newline in the file
		s += "\n"
		return osutil.AtomicWriteFile(path, []byte(s), 0644, 0)
	}

	return nil
}

type piConfigNotSupportedError struct {
	reason string
}

func newPiConfigNotSupportedError(msg string) *piConfigNotSupportedError {
	return &piConfigNotSupportedError{msg}
}

func (e *piConfigNotSupportedError) Error() string {
	return fmt.Sprintf("configuration cannot be applied: %s", e.reason)
}

var reIgnorePrefix = regexp.MustCompile(`(?i)^#\s+Snapd-Edit:\s+no\s*$`)

func piConfigFileIgnoreMarkerSet(configFile string) bool {
	f, err := os.Open(configFile)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// read the first line
	scanner.Scan()
	if scanner.Err() != nil {
		return false
	}

	return reIgnorePrefix.Match(scanner.Bytes())
}

// Some of the pi devices (avnet) ship with measured boot enabled and
// the config.txt is part of the measurements. We cannot modify the
// configuration here or measurements are wrong.
var piMeasuredBootKernels = []string{
	// see https://bugs.launchpad.net/denver/+bug/1928613
	"avnet-avt-iiotg20-kernel",
}

func piConfigFile(dev sysconfig.Device, opts *fsOnlyContext) (string, error) {
	rootDir := dirs.GlobalRootDir
	subdir := "/boot/uboot"

	if strutil.ListContains(piMeasuredBootKernels, dev.Kernel()) {
		return "", newPiConfigNotSupportedError("boot measures config.txt")
	}

	if opts != nil {
		rootDir = opts.RootDir
	} else if dev.HasModeenv() {
		// not a filesystem only apply, so we may be operating on a run system
		// on UC20, in which case we shouldn't use the /boot/uboot/ option and
		// instead should use /run/mnt/ubuntu-seed/
		if dev.RunMode() {
			rootDir = boot.InitramfsUbuntuSeedDir
			subdir = ""
		} else {
			// we don't support configuring pi-config in these modes as it is
			// unclear what the right behavior is
			return "", newPiConfigNotSupportedError("unsupported system mode")
		}
	}
	configPath := filepath.Join(rootDir, subdir, "config.txt")
	if piConfigFileIgnoreMarkerSet(configPath) {
		return "", newPiConfigNotSupportedError("no-editing header found")
	}

	return configPath, nil
}

func handlePiConfiguration(dev sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	configFile, err := piConfigFile(dev, opts)
	if _, ok := err.(*piConfigNotSupportedError); ok {
		logger.Noticef("ignoring pi-config settings: %v", err)
		return nil
	}
	if err != nil {
		return err
	}
	if osutil.FileExists(configFile) {
		// snapctl can actually give us the whole dict in
		// JSON, in a single call; use that instead of this.
		config := map[string]string{}
		for key := range piConfigKeys {
			output, err := coreCfg(tr, fmt.Sprintf("pi-config.%s", strings.Replace(key, "_", "-", -1)))
			if err != nil {
				return err
			}
			config[key] = output
		}
		if err := updatePiConfig(configFile, config); err != nil {
			return err
		}
	}
	return nil
}
