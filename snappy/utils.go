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
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/snap/snapenv"
)

// takes a directory and removes the global root, this is needed
// when the SetRoot option is used and we need to generate
// content for the "Apps" section
var stripGlobalRootDir = stripGlobalRootDirImpl

func stripGlobalRootDirImpl(dir string) string {
	if dirs.GlobalRootDir == "/" {
		return dir
	}

	return dir[len(dirs.GlobalRootDir):]
}

// makeSnapHookEnv returns an environment suitable for passing to
// os/exec.Cmd.Env
//
// The returned environment contains additional SNAP_* variables that
// are required when calling a meta/hook/ script and that will override
// any already existing SNAP_* variables in os.Environment()
func makeSnapHookEnv(snap *Snap) (env []string) {
	desc := struct {
		SnapName    string
		SnapArch    string
		SnapPath    string
		Version     string
		Revision    int
		UdevAppName string
	}{
		snap.Name(),
		arch.UbuntuArchitecture(),
		snap.Info().MountDir(),
		snap.Version(),
		snap.Revision(),
		snap.Name(),
	}

	vars := snapenv.GetBasicSnapEnvVars(desc)
	snapEnv := snapenv.MakeMapFromEnvList(vars)

	// merge regular env and new snapEnv
	envMap := snapenv.MakeMapFromEnvList(os.Environ())
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

// firstErr returns the first error of the given error list
func firstErr(err ...error) error {
	for _, e := range err {
		if e != nil {
			return e
		}
	}
	return nil
}
