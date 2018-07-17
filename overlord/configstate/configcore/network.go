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

	output, err := coreCfg(tr, "network.disable-ipv6")
	if err != nil {
		return nil
	}

	var sysctl string
	switch output {
	case "true":
		sysctl = "net.ipv6.conf.all.disable_ipv6=1"
		content.WriteString(sysctl + "\n")
	case "false", "":
		// Store the sysctl for the code below but don't write it to
		// content so that the file setting this option gets removed.
		sysctl = "net.ipv6.conf.all.disable_ipv6=0"
	default:
		return fmt.Errorf("unsupported disable-ipv6 option: %q", output)
	}
	dirContent := map[string]*osutil.FileState{}
	if content.Len() > 0 {
		dirContent[name] = &osutil.FileState{
			Content: content.Bytes(),
			Mode:    0644,
		}
	}

	// write the new config
	glob := name
	changed, removed, err := osutil.EnsureDirState(dir, glob, dirContent)
	if err != nil {
		return err
	}

	// load the new config into the kernel
	if len(changed) > 0 || len(removed) > 0 {
		output, err := exec.Command("sysctl", "-w", sysctl).CombinedOutput()
		if err != nil {
			return osutil.OutputErr(output, err)
		}
	}

	return nil
}
