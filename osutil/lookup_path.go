// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"os/exec"
)

var LookPath func(name string) (string, error) = exec.LookPath

// LookupPath searches for a given command name in all directories listed
// in the environment variable PATH and returns the found path or an
// empty path.
func LookupPath(name string) string {
	return LookupPathWithDefault(name, "")
}

// LookupPathWithDefault searches for a given command name in all directories
// listed in the environment variable PATH and returns the found path or the
// provided default path.
func LookupPathWithDefault(name string, defaultPath string) string {
	p, err := LookPath(name)
	if err != nil {
		return defaultPath
	}
	return p
}
