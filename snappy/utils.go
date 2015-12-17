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
	"fmt"
	"os"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/helpers"
)

// FIXME: can we kill this?
func addCoreFmk(fmks []string) []string {
	fmkCore := false
	for _, a := range fmks {
		if a == "ubuntu-core-15.04-dev1" {
			fmkCore = true
			break
		}
	}

	if !fmkCore {
		fmks = append(fmks, "ubuntu-core-15.04-dev1")
	}

	return fmks
}

// makeSnapHookEnv returns an environment suitable for passing to
// os/exec.Cmd.Env
//
// The returned environment contains additional SNAP_* variables that
// are required when calling a meta/hook/ script and that will override
// any already existing SNAP_* variables in os.Environment()
func makeSnapHookEnv(part *SnapPart) (env []string) {
	desc := struct {
		AppName     string
		AppArch     string
		AppPath     string
		Version     string
		UdevAppName string
		Origin      string
	}{
		part.Name(),
		arch.UbuntuArchitecture(),
		part.basedir,
		part.Version(),
		QualifiedName(part),
		part.Origin(),
	}
	snapEnv := helpers.MakeMapFromEnvList(helpers.GetBasicSnapEnvVars(desc))

	// merge regular env and new snapEnv
	envMap := helpers.MakeMapFromEnvList(os.Environ())
	for k, v := range snapEnv {
		envMap[k] = v
	}

	// force default locale
	envMap["LC_ALL"] = "C.UTF-8"

	// flatten
	for k, v := range envMap {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	return env
}
