// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.network.disable-ipv6"] = true
}

func validateNetworkSettings(tr Conf) error {
	return validateBoolFlag(tr, "network.disable-ipv6")
}

func handleNetworkConfiguration(tr Conf) error {
	dir := filepath.Join(dirs.GlobalRootDir, "/etc/sysctl.d")
	name := "10-snapd-network.conf"
	content := bytes.NewBuffer(nil)

	if !osutil.FileExists(dir) {
		return nil
	}

	var sysctl string
	output, err := coreCfg(tr, "network.disable-ipv6")
	if err != nil {
		return nil
	}
	switch output {
	case "true":
		sysctl = "net.ipv6.conf.all.disable_ipv6=1"
		content.WriteString(sysctl + "\n")
	case "false":
		sysctl = "net.ipv6.conf.all.disable_ipv6=0"
		content.WriteString(sysctl + "\n")
	case "":
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	default:
		return fmt.Errorf("unsupported disable-ipv6 option: %q", output)
	}

	if sysctl != "" {
		// write the new config
		dirContent := map[string]*osutil.FileState{
			name: &osutil.FileState{
				Content: content.Bytes(),
				Mode:    0644,
			},
		}
		glob := name
		if _, _, err = osutil.EnsureDirState(dir, glob, dirContent); err != nil {
			return err
		}

		// load the new config into the kernel
		output, err := exec.Command("sysctl", "-p", filepath.Join(dir, name)).CombinedOutput()
		if err != nil {
			return osutil.OutputErr(output, err)
		}
	}

	return nil
}
