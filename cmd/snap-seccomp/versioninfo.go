// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"

	"github.com/seccomp/libseccomp-golang"

	"github.com/snapcore/snapd/cmd/snap-seccomp/syscalls"
	"github.com/snapcore/snapd/osutil"
)

var seccompSyscalls = syscalls.SeccompSyscalls

func versionInfo() (string, error) {
	myBuildID, err := osutil.MyBuildID()
	if err != nil {
		return "", fmt.Errorf("cannot get build-id of snap-seccomp: %v", err)
	}
	// Calculate the checksum of all syscall names supported by libseccomp
	// library. We add that to the version info to cover the case when
	// libseccomp version does not change, but the set of supported syscalls
	// does due to distro patches.
	sh := sha256.New()
	newline := []byte("\n")
	for _, syscallName := range seccompSyscalls {
		if _, err := seccomp.GetSyscallFromName(syscallName); err != nil {
			// syscall is unsupported by this version of libseccomp
			continue
		}
		sh.Write([]byte(syscallName))
		sh.Write(newline)
	}

	major, minor, micro := seccomp.GetLibraryVersion()
	features := goSeccompFeatures()

	return fmt.Sprintf("%s %d.%d.%d %x %s", myBuildID, major, minor, micro, sh.Sum(nil), features), nil
}

func showVersionInfo() error {
	vi, err := versionInfo()
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, vi)
	return nil
}

func goSeccompFeatures() string {
	var features []string
	if actLogSupported() {
		features = append(features, "bpf-actlog")
	}

	if len(features) == 0 {
		return "-"
	}
	return strings.Join(features, ":")
}
