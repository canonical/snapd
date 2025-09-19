// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

import "strings"

// RSplitN slices s into substrings separated by sep and returns a slice of
// the substrings between those separators, starting from the right.
//
// The count determines the number of substrings to return:
//   - n > 0: at most n substrings; the leftmost substring will be the unsplit remainder;
//   - n == 0: the result is nil (zero substrings);
//   - n < 0: all substrings.
func RSplitN(s, sep string, n int) []string {
	if n == 0 {
		return nil
	}

	parts := strings.Split(s, sep)
	if n >= len(parts) || n < 0 {
		return parts
	}

	leftmost := strings.Join(parts[:len(parts)-n+1], sep)
	return append([]string{leftmost}, parts[len(parts)-n+1:]...)
}
