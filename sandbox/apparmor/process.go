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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

func labelFromPid(pid int) (string, error) {
	procFile := filepath.Join(rootPath, fmt.Sprintf("proc/%v/attr/current", pid))
	contents, err := ioutil.ReadFile(procFile)
	if os.IsNotExist(err) {
		return "unconfined", nil
	} else if err != nil {
		return "", err
	}
	label := strings.TrimRight(string(contents), "\n")
	// Trim off the mode
	if strings.HasSuffix(label, ")") {
		if pos := strings.LastIndex(label, " ("); pos != -1 {
			label = label[:pos]
		}
	}
	return label, nil
}

func decodeLabel(label string) (snap, app, hook string, err error) {
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
	label, err := labelFromPid(pid)
	if err != nil {
		return "", "", "", err
	}
	return decodeLabel(label)
}
