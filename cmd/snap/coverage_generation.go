// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build coveragegeneration

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

package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

const spreadCoverageDir = "/var/tmp/snapd-tools/coverage"
const hostfsPrefix = "/var/lib/snapd/hostfs"

// exposeGoCoverDir rewrites host /tmp paths to hostfs so strict snaps can
// access a shared coverage directory.
func exposeGoCoverDir() (string, error) {
	goCoverDir := osGetenv("GOCOVERDIR")
	if goCoverDir == "" {
		return "", nil
	}

	path := filepath.Clean(goCoverDir)
	if strings.HasPrefix(path, hostfsPrefix+spreadCoverageDir) {
		return strings.TrimPrefix(path, hostfsPrefix), nil
	}
	if strings.HasPrefix(path, spreadCoverageDir) {
		return path, nil
	}

	return "", fmt.Errorf("unsupported GOCOVERDIR: %s", goCoverDir)
}
