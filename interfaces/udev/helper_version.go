// -*- Mode: Go; indent-tabs-mode: t -*-

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

package udev

import (
	"bufio"
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
)

func helperVersion() (string, error) {
	snap := filepath.Join(dirs.GlobalRootDir, "/usr/bin/snap")
	cmd := exec.Command(snap, "--version")
	cmd.Env = append(cmd.Environ(), "SNAP_REEXEC=0")
	output, _, err := osutil.RunCmd(cmd)
	if err != nil {
		return "", err
	}

	reader := bufio.NewReader(bytes.NewReader(output))
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		if !strings.HasPrefix(line, "snap ") {
			continue
		}
		line = strings.TrimPrefix(line, "snap ")
		line = strings.TrimLeft(line, " ")
		line = strings.TrimRight(line, "\n")
		return line, nil
	}
}

var useOldCallCache *bool

func useOldCall() bool {
	if useOldCallCache != nil {
		return *useOldCallCache
	}
	version, err := helperVersion()
	if err != nil {
		logger.Noticef("WARNING: could not find the version of the helper: %v", err)
		v := false
		useOldCallCache = &v
		return *useOldCallCache
	}
	cmp, err := strutil.VersionCompare(version, "2.62")
	if err != nil {
		logger.Noticef("WARNING: could parse the version of the helper: %v", err)
		v := false
		useOldCallCache = &v
		return *useOldCallCache
	}
	v := cmp < 0
	useOldCallCache = &v
	return *useOldCallCache
}
