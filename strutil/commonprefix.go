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

package strutil

import "errors"

func FindCommonPrefix(patterns []string) (string, error) {
	if len(patterns) == 0 {
		return "", errors.New("no patterns provided")
	}
	if len(patterns) == 1 {
		return patterns[0], nil
	}

	// Find the length of shortest pattern (minSize)
	minSize := len(patterns[0])
	for _, pattern := range patterns[1:] {
		if len(pattern) < minSize {
			minSize = len(pattern)
		}
	}

	// Find the longest prefix common to ALL patterns.
	commonPrefix := ""
findCommonPrefix:
	for charInd := 0; charInd < minSize; charInd++ {
		for _, pattern := range patterns[1:] {
			if pattern[charInd] != patterns[0][charInd] {
				break findCommonPrefix
			}
		}
		commonPrefix = patterns[0][:charInd+1]
	}

	return commonPrefix, nil
}
