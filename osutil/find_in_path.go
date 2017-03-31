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
	"fmt"
	"os"
	"strings"
)

var Getenv func(key string) string = os.Getenv

// FindInPath searches for a given command name in all directories listed
// in the environment variable PATH and returns the found path or an
// empty path.
func FindInPath(name string) string {
	return FindInPathOrDefault(name, "")
}

// FindInPathOrDefault searches for a given command name in all directories
// listed in the environment variable PATH and returns the found path or the
// provided default path.
func FindInPathOrDefault(name string, defaultPath string) string {
	paths := strings.Split(Getenv("PATH"), ":")
	for _, p := range paths {
		candidate := fmt.Sprintf("%s/%s", p, name)
		_, err := os.Stat(candidate)
		if err == nil {
			return candidate
		}
	}
	return defaultPath
}
