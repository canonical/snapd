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

package snapenv

import (
	"fmt"
	"os"
	"os/user"
	"strings"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/snap"
)

// ExecEnv returns the environment that is important for the later stages
// of execution (like SNAP_REVISION that snap-exec requires to work)
func ExecEnv(info *snap.Info) []string {
	// merge environment and the snap environment, note that the
	// snap environment overrides pre-existing env entries
	env := envMap(os.Environ())
	snapEnv := envMap(snapEnv(info))
	for k, v := range snapEnv {
		env[k] = v
	}
	return envFromMap(env)
}

// basicEnv returns the app-level environment variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func basicEnv(info *snap.Info) []string {
	return []string{
		fmt.Sprintf("SNAP=%s", info.MountDir()),
		fmt.Sprintf("SNAP_COMMON=%s", info.CommonDataDir()),
		fmt.Sprintf("SNAP_DATA=%s", info.DataDir()),
		fmt.Sprintf("SNAP_NAME=%s", info.Name()),
		fmt.Sprintf("SNAP_VERSION=%s", info.Version),
		fmt.Sprintf("SNAP_REVISION=%s", info.Revision),
		fmt.Sprintf("SNAP_ARCH=%s", arch.UbuntuArchitecture()),
		"SNAP_LIBRARY_PATH=/var/lib/snapd/lib/gl:",
		fmt.Sprintf("SNAP_REEXEC=%s", os.Getenv("SNAP_REEXEC")),
	}
}

// userEnv returns the user-level environment variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func userEnv(info *snap.Info, home string) []string {
	return []string{
		fmt.Sprintf("HOME=%s", info.UserDataDir(home)),
		fmt.Sprintf("SNAP_USER_COMMON=%s", info.UserCommonDataDir(home)),
		fmt.Sprintf("SNAP_USER_DATA=%s", info.UserDataDir(home)),
	}
}

// envMap creates a map from the given environment string list, e.g. the
// list returned from os.Environ()
func envMap(env []string) map[string]string {
	envMap := map[string]string{}
	for _, kv := range env {
		l := strings.SplitN(kv, "=", 2)
		if len(l) < 2 {
			continue // strange
		}
		k, v := l[0], l[1]
		envMap[k] = v
	}
	return envMap
}

// envFromMap creates a list of strings of the form k=v from a dict. This is
// useful in combination with envMap to create an environment suitable to
// pass to e.g. syscall.Exec()
func envFromMap(em map[string]string) []string {
	var out []string
	for k, v := range em {
		out = append(out, fmt.Sprintf("%s=%s", k, v))
	}
	return out
}

// returns the environment that is important for the later stages of execution
// (like SNAP_REVISION that snap-exec requires to work)
func snapEnv(info *snap.Info) []string {
	home := os.Getenv("HOME")
	// HOME is not set for systemd services, so pull it out of passwd
	if home == "" {
		user, err := user.Current()
		if err == nil {
			home = user.HomeDir
		}
	}

	env := basicEnv(info)
	if home != "" {
		env = append(env, userEnv(info, home)...)
	}
	return env
}
