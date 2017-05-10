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

// ExecEnv returns the full environment that is required for
// snap-{confine,exec}(like SNAP_{NAME,REVISION} etc are all set).
//
// It merges it with the existing os.Environ() and ensures the SNAP_*
// overrides the any pre-existing environment variables.
//
// With the extra parameter additional environment variables can be
// supplied which will be set in the execution environment.
func ExecEnv(info *snap.Info, extra map[string]string) []string {
	// merge environment and the snap environment, note that the
	// snap environment overrides pre-existing env entries
	env := envMap(os.Environ())
	snapEnv := snapEnv(info)
	for k, v := range snapEnv {
		env[k] = v
	}
	for k, v := range extra {
		env[k] = v
	}
	return envFromMap(env)
}

// snapEnv returns the extra environment that is required for
// snap-{confine,exec} to work.
func snapEnv(info *snap.Info) map[string]string {
	var home string

	usr, err := user.Current()
	if err == nil {
		home = usr.HomeDir
	}

	env := basicEnv(info)
	if home != "" {
		for k, v := range userEnv(info, home) {
			env[k] = v
		}
	}
	return env
}

// basicEnv returns the app-level environment variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func basicEnv(info *snap.Info) map[string]string {
	return map[string]string{
		"SNAP":          info.MountDir(),
		"SNAP_COMMON":   info.CommonDataDir(),
		"SNAP_DATA":     info.DataDir(),
		"SNAP_NAME":     info.Name(),
		"SNAP_VERSION":  info.Version,
		"SNAP_REVISION": info.Revision.String(),
		"SNAP_ARCH":     arch.UbuntuArchitecture(),
		// see https://github.com/snapcore/snapd/pull/2732#pullrequestreview-18827193
		"SNAP_LIBRARY_PATH": "/var/lib/snapd/lib/gl:/var/lib/snapd/void",
		"SNAP_REEXEC":       os.Getenv("SNAP_REEXEC"),
	}
}

// userEnv returns the user-level environment variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func userEnv(info *snap.Info, home string) map[string]string {
	result := map[string]string{
		"SNAP_USER_COMMON": info.UserCommonDataDir(home),
		"SNAP_USER_DATA":   info.UserDataDir(home),
		"XDG_RUNTIME_DIR":  info.UserXdgRuntimeDir(os.Geteuid()),
	}
	// For non-classic snaps, we set HOME but on classic allow snaps to see real HOME
	if !info.NeedsClassic() {
		result["HOME"] = info.UserDataDir(home)
	}
	return result
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
