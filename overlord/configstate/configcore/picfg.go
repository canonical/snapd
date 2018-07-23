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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// valid pi config keys
var piConfigKeys = map[string]bool{
	"disable_overscan":         true,
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
	"hdmi_group":               true,
	"hdmi_mode":                true,
	"hdmi_drive":               true,
	"avoid_warnings":           true,
	"gpu_mem_256":              true,
	"gpu_mem_512":              true,
	"gpu_mem":                  true,
	"sdtv_aspect":              true,
	"config_hdmi_boost":        true,
	"hdmi_force_hotplug":       true,
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

func piConfigFile() string {
	return filepath.Join(dirs.GlobalRootDir, "/boot/uboot/config.txt")
}

func handlePiConfiguration(tr Conf) error {
	if osutil.FileExists(piConfigFile()) {
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
		if err := updatePiConfig(piConfigFile(), config); err != nil {
			return err
		}
	}
	return nil
}
