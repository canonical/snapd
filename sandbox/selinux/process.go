// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package selinux

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// SecurityLabelFromPid returns the SELinux security context of the process with
// the given pid. When SELinux is not in use on the system, an empty string is
// returned.
func SecurityLabelFromPid(pid int) (string, error) {
	if ProbedLevel() == Unsupported {
		return "", nil
	}

	candidates := []string{
		fmt.Sprintf("proc/%d/attr/selinux/current", pid),
	}
	selinuxAttrDir := filepath.Join(dirs.GlobalRootDir, fmt.Sprintf("proc/%d/attr/selinux", pid))
	if !osutil.FileExists(selinuxAttrDir) {
		// Legacy SELinux kernels expose the context via attr/current.
		candidates = append(candidates, fmt.Sprintf("proc/%d/attr/current", pid))
	}

	for _, candidate := range candidates {
		procFile := filepath.Join(dirs.GlobalRootDir, candidate)
		if !osutil.FileExists(procFile) {
			continue
		}
		label, err := readSELinuxSecurityLabel(procFile)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		return label, nil
	}
	return "", nil
}

func readSELinuxSecurityLabel(procFile string) (string, error) {
	contents, err := os.ReadFile(procFile)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(contents), "\n"), nil
}
