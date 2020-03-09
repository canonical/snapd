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

package strutil

import (
	"fmt"
)

// ParseRawEnvironmentDelta returns a new environment delta parsed from key=value strings.
func ParseRawEnvironmentDelta(entries []string) (*EnvironmentDelta, error) {
	om := NewOrderedMap()
	for _, entry := range entries {
		key, value, err := parseEnvEntry(entry)
		if err != nil {
			return nil, err
		}
		if om.Get(key) != "" {
			return nil, fmt.Errorf("cannot overwrite earlier value of %q", key)
		}
		om.Set(key, value)
	}
	return &EnvironmentDelta{OrderedMap: *om}, nil
}
