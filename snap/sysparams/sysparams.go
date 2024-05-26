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
	"os"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
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
	// rootdir is stored to allow for updating the same
	// system-params file.
	rootdir string
	// Homedirs is the comma-delimited list of user specified home
	// directories that should be mounted.
	Homedirs string
}

func parseSystemParams(contents string) (*SystemParams, error) {
	params := &SystemParams{}
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(strings.NewReader(contents))
	for scanner.Scan() {
		line := scanner.Text()

		// ignore empty lines and comments
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			continue
		}

		tokens := strings.SplitN(line, "=", 2)
		if len(tokens) != 2 {
			return nil, fmt.Errorf("invalid line: %q", line)
		}

		// ensure that each configuration value only appears once
		if ok := seen[tokens[0]]; ok {
			return nil, fmt.Errorf("duplicate entry found: %q", tokens[0])
		}
		seen[tokens[0]] = true

		if tokens[0] == "homedirs" {
			params.Homedirs = tokens[1]
		} else {
			return nil, fmt.Errorf("invalid line: %q", line)
		}
	}
	mylog.Check(scanner.Err())

	return params, nil
}

func sysparamsFile(rootdir string) string {
	if rootdir == "" {
		rootdir = dirs.GlobalRootDir
	}
	return dirs.SnapSystemParamsUnder(rootdir)
}

// Open either reads the existing file at <rootdir>/var/lib/snapd/system-params
// or in case the file does not exist, it will initialize and return a
// new SystemParams structure.
func Open(rootdir string) (*SystemParams, error) {
	sspFile := sysparamsFile(rootdir)
	if !osutil.FileExists(sspFile) {
		return &SystemParams{rootdir: rootdir}, nil
	}

	data := mylog.Check2(os.ReadFile(sspFile))

	params := mylog.Check2(parseSystemParams(string(data)))

	params.rootdir = rootdir
	return params, nil
}

// Write updates the system-params file with the values in the
// SystemParams instance.
func (ssp *SystemParams) Write() error {
	sspFile := sysparamsFile(ssp.rootdir)
	contents := fmt.Sprintf("homedirs=%s\n", ssp.Homedirs)
	mylog.Check(osutilAtomicWriteFile(sspFile, []byte(contents), 0644, 0))

	return nil
}
