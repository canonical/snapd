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

package configstate

import (
	"sort"
	"strings"
)

func sortPatchKeys(patch map[string]interface{}) []string {
	if len(patch) == 0 {
		return nil
	}
	depths := make(map[string]int, len(patch))
	keys := make([]string, 0, len(patch))
	for k := range patch {
		depths[k] = strings.Count(k, ".")
		keys = append(keys, k)
	}

	sort.Slice(keys, func(i, j int) bool {
		return depths[keys[i]] < depths[keys[j]]
	})
	return keys
}
