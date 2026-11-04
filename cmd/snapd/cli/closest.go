// -*- Mode: Go; indent-tabs-mode: t -*-

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

package cli

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/strutil"
)

// closestCommand returns the name, or one of the aliases, of the sub-command
// of parent that is the closest match for the mistyped command name, if any is
// close enough. Hidden commands are considered too, since they are still
// runnable. A candidate is only returned when the edit distance relative to
// its length is small enough so that clearly unrelated commands do not yield a
// suggestion.
func closestCommand(parent *flags.Command, typo string) (string, bool) {
	if parent == nil {
		return "", false
	}

	var names []string
	for _, cmd := range parent.Commands() {
		names = append(names, cmd.Name)
		names = append(names, cmd.Aliases...)
	}
	// sort to have a deterministic choice when several candidates are
	// equally close
	sort.Strings(names)

	// no command name uses uppercase characters, but a user may type one
	// in any case, so normalize the typo before comparing
	typo = strings.ToLower(typo)

	best := ""
	bestDist := -1
	for _, name := range names {
		dist := strutil.LevenshteinDistance(typo, name)
		// keep candidates where fewer than half of the runes need to
		// be edited; clearly unrelated ones are not useful
		// suggestions, and must not shadow a closer eligible one
		if float64(dist)/float64(utf8.RuneCountInString(name)) >= 0.5 {
			continue
		}
		if bestDist < 0 || dist < bestDist {
			best, bestDist = name, dist
		}
	}
	if bestDist < 0 {
		return "", false
	}
	return best, true
}
