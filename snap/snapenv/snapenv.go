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
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/snap"
)

type preserveUnsafeEnvFlag int8

const (
	discardUnsafeFlag preserveUnsafeEnvFlag = iota
	preserveUnsafeFlag
)

// ExecEnv returns the full environment that is required for
// snap-{confine,exec}(like SNAP_{NAME,REVISION} etc are all set).
//
// It merges it with the existing os.Environ() and ensures the SNAP_*
// overrides the any pre-existing environment variables. For a classic
// snap, environment variables that are usually stripped out by ld.so
// when starting a setuid process are renamed by prepending
// PreservedUnsafePrefix -- which snap-exec will remove, restoring the
// variables to their original names.
//
// With the extra parameter additional environment variables can be
// supplied which will be set in the execution environment.
func ExecEnv(info *snap.Info, extra map[string]string) []string {
	// merge environment and the snap environment, note that the
	// snap environment overrides pre-existing env entries
	preserve := discardUnsafeFlag
	if info.NeedsClassic() {
		preserve = preserveUnsafeFlag
	}
	env := envMap(os.Environ(), preserve)
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
	// TODO parallel-install: use of proper instance/store name
	return map[string]string{
		// This uses CoreSnapMountDir because the computed environment
		// variables are conveyed to the started application process which
		// shall *either* execute with the new mount namespace where snaps are
		// always mounted on /snap OR it is a classically confined snap where
		// /snap is a part of the distribution package.
		//
		// NOTE for parallel-installs the mount namespace setup is
		// making the environment of each snap instance appear as if
		// it's the only snap, i.e. SNAP paths point to the same
		// locations within the mount namespace
		"SNAP":               filepath.Join(dirs.CoreSnapMountDir, info.SnapName(), info.Revision.String()),
		"SNAP_COMMON":        snap.CommonDataDir(info.SnapName()),
		"SNAP_DATA":          snap.DataDir(info.SnapName(), info.Revision),
		"SNAP_NAME":          info.SnapName(),
		"SNAP_INSTANCE_NAME": info.InstanceName(),
		"SNAP_INSTANCE_KEY":  info.InstanceKey,
		"SNAP_VERSION":       info.Version,
		"SNAP_REVISION":      info.Revision.String(),
		"SNAP_ARCH":          arch.UbuntuArchitecture(),
		// see https://github.com/snapcore/snapd/pull/2732#pullrequestreview-18827193
		"SNAP_LIBRARY_PATH": "/var/lib/snapd/lib/gl:/var/lib/snapd/lib/gl32:/var/lib/snapd/void",
		"SNAP_REEXEC":       os.Getenv("SNAP_REEXEC"),
	}
}

// userEnv returns the user-level environment variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func userEnv(info *snap.Info, home string) map[string]string {
	// TODO parallel-install: we do not have a way to make the mounts from
	// instance-specific to snap-specific directories at user controlled
	// location completely safe, make sure we use instance-specific
	// directories always
	result := map[string]string{
		"SNAP_USER_COMMON": info.UserCommonDataDir(home),
		"SNAP_USER_DATA":   info.UserDataDir(home),
		"XDG_RUNTIME_DIR":  info.UserXdgRuntimeDir(sys.Geteuid()),
	}
	// For non-classic snaps, we set HOME but on classic allow snaps to see real HOME
	if !info.NeedsClassic() {
		result["HOME"] = info.UserDataDir(home)
	}
	return result
}

// Environment variables glibc strips out when running a setuid binary.
// Taken from https://sourceware.org/git/?p=glibc.git;a=blob_plain;f=sysdeps/generic/unsecvars.h;hb=HEAD
// TODO: use go generate to obtain this list at build time.
var unsafeEnv = map[string]bool{
	"GCONV_PATH":       true,
	"GETCONF_DIR":      true,
	"GLIBC_TUNABLES":   true,
	"HOSTALIASES":      true,
	"LD_AUDIT":         true,
	"LD_DEBUG":         true,
	"LD_DEBUG_OUTPUT":  true,
	"LD_DYNAMIC_WEAK":  true,
	"LD_HWCAP_MASK":    true,
	"LD_LIBRARY_PATH":  true,
	"LD_ORIGIN_PATH":   true,
	"LD_PRELOAD":       true,
	"LD_PROFILE":       true,
	"LD_SHOW_AUXV":     true,
	"LD_USE_LOAD_BIAS": true,
	"LOCALDOMAIN":      true,
	"LOCPATH":          true,
	"MALLOC_TRACE":     true,
	"NIS_PATH":         true,
	"NLSPATH":          true,
	"RESOLV_HOST_CONF": true,
	"RES_OPTIONS":      true,
	"TMPDIR":           true,
	"TZDIR":            true,
}

const PreservedUnsafePrefix = "SNAP_SAVED_"

// envMap creates a map from the given environment string list,
// e.g. the list returned from os.Environ(). If preserveUnsafeVars
// rename variables that will be stripped out by the dynamic linker
// executing the setuid snap-confine by prepending their names with
// PreservedUnsafePrefix.
func envMap(env []string, preserveUnsafeEnv preserveUnsafeEnvFlag) map[string]string {
	envMap := map[string]string{}
	for _, kv := range env {
		// snap-exec unconditionally renames variables
		// starting with PreservedUnsafePrefix so skip any
		// that are already present in the environment to
		// avoid confusion.
		if strings.HasPrefix(kv, PreservedUnsafePrefix) {
			continue
		}
		l := strings.SplitN(kv, "=", 2)
		if len(l) < 2 {
			continue // strange
		}
		k, v := l[0], l[1]
		if preserveUnsafeEnv == preserveUnsafeFlag && unsafeEnv[k] {
			k = PreservedUnsafePrefix + k
		}
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
