// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build linux

/*
 * Copyright (C) 2024 Canonical Ltd
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

package snapdtool

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/fips"
	"github.com/snapcore/snapd/release"
)

func findFIPSLibsAndModules(snapRoot string) (opensslLib, module string) {
	// with OpenSSL supported by 22.04 (the snapd snap build base), the FIPS
	// module is available as an *.so library loaded by libcrypto.so.3 at
	// runtime.
	var fipsLibAndModulePathInSnapdSnap = []struct {
		opensslLib, moduleFile string
	}{
		// TODO update paths for core24, which uses: usr/lib/x86_64-linux-gnu/ossl-modules/fips.so
		{"usr/lib/x86_64-linux-gnu/libcrypto.so.3", "usr/lib/x86_64-linux-gnu/ossl-modules-3/fips.so"},
		{"usr/lib/aarch64-linux-gnu/libcrypto.so.3", "usr/lib/aarch64-linux-gnu/ossl-modules-3/fips.so"},
		{"usr/lib/arm-linux-gnueabihf/libcrypto.so.3", "usr/lib/arm-linux-gnueabihf/ossl-modules-3/fips.so"},
		{"usr/lib/i386-linux-gnu/libcrypto.so.3", "usr/lib/i386-linux-gnu/ossl-modules-3/fips.so"},
		{"usr/lib/riscv64-linux-gnu/libcrypto.so.3", "usr/lib/riscv64-linux-gnu/ossl-modules-3/fips.so"},
		{"usr/lib/s390x-linux-gnu/libcrypto.so.3", "usr/lib/s390x-linux-gnu/ossl-modules-3/fips.so"},
	}

	for _, bundle := range fipsLibAndModulePathInSnapdSnap {
		lib := filepath.Join(snapRoot, bundle.opensslLib)
		module := filepath.Join(snapRoot, bundle.moduleFile)
		if osutil.FileExists(lib) && osutil.FileExists(module) {
			return lib, module
		}
	}
	return "", ""
}

// MaybeCompleteFIPSSetup run completion and cleanup steps if the process was
// invoked through FIPS execution dispatch mechanism.
func MaybeCompleteFIPSSetup() {
	if os.Getenv("SNAPD_FIPS_BOOTSTRAP") == "1" {
		// we've already been reexeced into FIPS mode and bootstrap was
		// performed
		logger.Debugf("FIPS bootstrap complete")

		// if we reached this place, then the initialization was
		// completed successfully and we can drop the environment
		// variables, other processes which may be invoked by snapd will
		// perform the initialization cycle on their own when needed
		os.Unsetenv("GOFIPS")
		os.Unsetenv("SNAPD_FIPS_BOOTSTRAP")
		os.Unsetenv("OPENSSL_MODULES")
		os.Unsetenv("GO_OPENSSL_VERSION_OVERRIDE")
	}
}

// DispatchWithFIPS checks whether system-wide FIPS mode is enabled,
// sets up the environment for FIPS compliance, and execs into targetExe.
func DispatchWithFIPS(targetExe string) error {
	enabled, err := fips.IsEnabled()
	if err != nil {
		return fmt.Errorf("cannot obtain FIPS status: %w", err)
	}

	if enabled {
		logger.Debugf("FIPS mode enabled system wide")
	}

	rootDir, exe, err := exeAndRoot()
	if err != nil {
		return err
	}

	logger.Debugf("self exe: %s", exe)
	logger.Debugf("exe root dir: %q", rootDir)

	snapdRev, err := osReadlink(snapdSnap)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	var currentRevSnapdSnap string
	if snapdRev != "" {
		currentRevSnapdSnap = filepath.Join(dirs.SnapMountDir, "snapd", snapdRev)
		logger.Debugf("snapd snap: %s", currentRevSnapdSnap)
	}

	env := append(os.Environ(), "SNAPD_FIPS_BOOTSTRAP=1")

	if enabled {
		env = append(env,
			// make FIPS mode required at runtime, if FIPS support in Go
			// runtime cannot be completed successfully the startup will
			// fail in a predictable manner
			"GOFIPS=1",
		)
	}

	execOrErr := func(target string, args, env []string) error {
		if err := syscallExec(target, args, env); err != nil {
			return fmt.Errorf("cannot exec %s: %w", target, err)
		}
		return nil
	}

	if release.OnClassic && rootDir != currentRevSnapdSnap {
		// on classic and NOT reexecuted from the snapd snap, most likely a
		// native package which should have been built such that the native
		// OpenSSL libraries will automatically locate the provider module
		return execOrErr(filepath.Join(rootDir, targetExe), os.Args, env)
	}

	// on Core or when reexeced from the snapd snap, we need to set up the
	// environment such that the OpenSSL library will be able to locate a
	// matching FIPS provider module bundled with the snapd snap.

	lib, mod := findFIPSLibsAndModules(currentRevSnapdSnap)

	if mod != "" {
		// version override uses the version suffix right after *.so.
		libVer := strings.TrimPrefix(filepath.Ext(lib), ".")
		logger.Debugf("found FIPS library and module at %s (ver %s) and %s", lib, libVer, mod)
		env = append(env, []string{
			// be specific about where the modules come from
			fmt.Sprintf("OPENSSL_MODULES=%s", filepath.Dir(mod)),
			// and the openssl lib version
			fmt.Sprintf("GO_OPENSSL_VERSION_OVERRIDE=%s", libVer),
		}...)
	}

	// TODO how to ensure that we only load the library from the snapd snap?
	return execOrErr(filepath.Join(rootDir, targetExe), os.Args, env)
}
