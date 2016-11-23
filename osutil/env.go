// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"os"
	"strconv"
)

// GetenvBool returns whether the given key may be considered "set" in the environment
// (i.e. it is set to one of "1", "true", etc)
func GetenvBool(key string) bool {
	val := os.Getenv(key)
	if val == "" {
		return false
	}

	b, err := strconv.ParseBool(val)
	if err != nil {
		return false
	}

	return b
}
