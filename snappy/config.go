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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// can be overriden by tests
var aaExec = "aa-exec"

// snapConfig configures a installed snap in the given directory
//
// It takes a rawConfig string that is passed as the new configuration
// This string can be empty.
//
// It returns the newConfig or an error
func snapConfig(snapDir, namespace, rawConfig string) (newConfig string, err error) {
	configScript := filepath.Join(snapDir, "meta", "hooks", "config")
	if _, err := os.Stat(configScript); err != nil {
		return "", ErrConfigNotFound
	}

	part, err := NewInstalledSnapPart(filepath.Join(snapDir, "meta", "package.yaml"), namespace)
	if err != nil {
		return "", ErrPackageNotFound
	}

	name := part.Name()
	if part.Type() != SnapTypeFramework {
		name += "." + namespace
	}
	appArmorProfile := fmt.Sprintf("%s_%s_%s", name, "snappy-config", part.Version())

	return runConfigScript(configScript, appArmorProfile, rawConfig, makeSnapHookEnv(part))
}

var runConfigScript = runConfigScriptImpl

// runConfigScript is a helper that just runs the config script and passes
// the rawConfig via stdin and reads/returns the output
func runConfigScriptImpl(configScript, appArmorProfile, rawConfig string, env []string) (newConfig string, err error) {
	cmd := exec.Command(aaExec, "-p", appArmorProfile, configScript)
	cmd.Stdin = strings.NewReader(rawConfig)
	cmd.Env = env

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("config failed with: '%s' (%v)", output, err)
	}

	return string(output), nil
}
