// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package apparmor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

var securityLabelProbeLevel = ProbedLevel

// SecurityLabelFromPid returns the AppArmor security label of the process with
// the given pid. When AppArmor is not in use on the system, "unconfined" is
// returned.
func SecurityLabelFromPid(pid int) (string, error) {
	if securityLabelProbeLevel() == Unsupported {
		return "unconfined", nil
	}

	candidates := []string{
		fmt.Sprintf("proc/%d/attr/apparmor/current", pid),
	}
	apparmorAttrDir := filepath.Join(dirs.GlobalRootDir, fmt.Sprintf("proc/%d/attr/apparmor", pid))
	if !osutil.FileExists(apparmorAttrDir) {
		// Legacy AppArmor kernels expose the profile via attr/current.
		candidates = append(candidates, fmt.Sprintf("proc/%d/attr/current", pid))
	}

	for _, candidate := range candidates {
		procFile := filepath.Join(dirs.GlobalRootDir, candidate)
		if !osutil.FileExists(procFile) {
			continue
		}
		label, err := readAppArmorSecurityLabel(procFile)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		return label, nil
	}
	return "unconfined", nil
}

func readAppArmorSecurityLabel(procFile string) (string, error) {
	contents, err := os.ReadFile(procFile)
	if err != nil {
		return "", err
	}
	label := strings.TrimRight(string(contents), "\n")
	// Trim off the AppArmor mode suffix.
	if strings.HasSuffix(label, ")") {
		if pos := strings.LastIndex(label, " ("); pos != -1 {
			label = label[:pos]
		}
	}
	return label, nil
}

func DecodeLabel(label string) (snap, app, hook string, err error) {
	parts := strings.Split(label, ".")
	if parts[0] != "snap" {
		return "", "", "", fmt.Errorf("security label %q does not belong to a snap", label)
	}
	if len(parts) == 3 {
		return parts[1], parts[2], "", nil
	}
	if len(parts) == 4 && parts[2] == "hook" {
		return parts[1], "", parts[3], nil
	}
	return "", "", "", fmt.Errorf("unknown snap related security label %q", label)
}

func SnapAppFromPid(pid int) (snap, app, hook string, err error) {
	label, err := SecurityLabelFromPid(pid)
	if err != nil {
		return "", "", "", err
	}
	return DecodeLabel(label)
}
