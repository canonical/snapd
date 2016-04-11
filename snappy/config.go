// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ubuntu-core/snappy/coreconfig"
)

// can be overriden by tests
var aaExec = "aa-exec"

// for the unit tests
var coreConfig = coreConfigImpl

// coreConfig configure the OS snap
func coreConfigImpl(configuration []byte) (newConfig []byte, err error) {
	if len(configuration) > 0 {
		return coreconfig.Set(configuration)
	}

	return coreconfig.Get()
}

// snapConfig configures a installed snap in the given directory
//
// It takes a rawConfig string that is passed as the new configuration
// This string can be empty.
//
// It returns the newConfig or an error
func snapConfig(snapDir string, rawConfig []byte) (newConfig []byte, err error) {
	configScript := filepath.Join(snapDir, "meta", "hooks", "config")
	if _, err := os.Stat(configScript); err != nil {
		return nil, ErrConfigNotFound
	}

	snap, err := NewInstalledSnap(filepath.Join(snapDir, "meta", "snap.yaml"))
	if err != nil {
		return nil, ErrPackageNotFound
	}

	// XXX: new security will not make this anymore!!
	appArmorProfile := fmt.Sprintf("%s_%s_%s", snap.Name(), "snappy-config", snap.Version())

	return runConfigScript(configScript, appArmorProfile, rawConfig, makeSnapHookEnv(snap))
}

var runConfigScript = runConfigScriptImpl

// runConfigScript is a helper that just runs the config script and passes
// the rawConfig via stdin and reads/returns the output
func runConfigScriptImpl(configScript, appArmorProfile string, rawConfig []byte, env []string) (newConfig []byte, err error) {
	cmd := exec.Command(aaExec, "-p", appArmorProfile, configScript)
	cmd.Stdin = bytes.NewReader(rawConfig)
	cmd.Env = env

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("config failed with: '%s' (%v)", output, err)
	}

	return output, nil
}
