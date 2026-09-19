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

package osutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
)

// ReadProcLSMCurrent returns the current LSM attribute of pid from
// /proc/<pid>/attr/<lsm>/current. If that LSM subdirectory is missing,
// attr/current is used instead so stacked LSMs do not share that file.
// An empty string and a nil error mean no candidate existed.
//
// This lives in osutil so sandbox/apparmor can use it without an import
// cycle through sandbox or sandbox/lsm.
func ReadProcLSMCurrent(pid int, lsm string) (string, error) {
	candidates := []string{
		fmt.Sprintf("proc/%d/attr/%s/current", pid, lsm),
	}
	lsmAttrDir := filepath.Join(dirs.GlobalRootDir, fmt.Sprintf("proc/%d/attr/%s", pid, lsm))
	if !FileExists(lsmAttrDir) {
		candidates = append(candidates, fmt.Sprintf("proc/%d/attr/current", pid))
	}

	for _, candidate := range candidates {
		procFile := filepath.Join(dirs.GlobalRootDir, candidate)
		if !FileExists(procFile) {
			continue
		}
		contents, err := os.ReadFile(procFile)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		return strings.TrimRight(string(contents), "\n"), nil
	}
	return "", nil
}
