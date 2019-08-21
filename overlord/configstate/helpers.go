// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

func sortPatchKeysByDepth(patch map[string]interface{}) []string {
	depths := make(map[string]int, len(patch))
	var keys []string
	for k := range patch {
		parts := strings.Split(k, ".")
		depths[k] = len(parts)
		keys = append(keys, k)
	}

	sort.Slice(keys, func(i, j int) bool {
		return depths[keys[i]] < depths[keys[j]]
	})
	return keys
}
