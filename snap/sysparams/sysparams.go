// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package sysparams

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/snapcore/snapd/osutil"
)

// For mocking in tests
var (
	osutilAtomicWriteFile = osutil.AtomicWriteFile
)

// SystemParams manages certain system configuration parameters
// aspects like the homedirs configuration that must be available
// for other binaries, as snap-confine.
type SystemParams struct {
	// path stored to allow for updating the same path
	path string
	// Homedirs is the comma-delimited list of user specified home
	// directories that should be mounted
	Homedirs string
}

func parseSystemParams(contents string) (*SystemParams, error) {
	params := &SystemParams{}
	scanner := bufio.NewScanner(strings.NewReader(contents))
	for scanner.Scan() {
		line := scanner.Text()

		// ignore empty lines and comments
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "homedirs=") {
			params.Homedirs = strings.TrimPrefix(line, "homedirs=")
		} else {
			return params, fmt.Errorf("cannot parse invalid line: %s", line)
		}
	}
	return params, nil
}

// Open either opens the existing file at the given path, and parses
// the file, or in case the file does not exist, it will initialize
// and return a new SnapdSystemParams structure.
func Open(path string) (*SystemParams, error) {
	if !osutil.FileExists(path) {
		return &SystemParams{path: path}, nil
	}

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	params, err := parseSystemParams(string(data))
	if err != nil {
		return params, err
	}
	params.path = path
	return params, nil
}

// Write updates the system-params file with the values in the
// SystemParams instance.
func (ssp *SystemParams) Write() error {
	contents := fmt.Sprintf("homedirs=%s\n", ssp.Homedirs)
	return osutilAtomicWriteFile(ssp.path, []byte(contents), 0644, 0)
}
