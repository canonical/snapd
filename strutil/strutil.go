// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"bytes"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

func init() {
	// golang does not init Seed() itself
	rand.Seed(time.Now().UTC().UnixNano())
}

const letters = "BCDFGHJKLMNPQRSTVWXYbcdfghjklmnpqrstvwxy0123456789"

// MakeRandomString returns a random string of length length
//
// The vowels are omitted to avoid that words are created by pure
// chance. Numbers are included.
func MakeRandomString(length int) string {
	out := ""
	for i := 0; i < length; i++ {
		out += string(letters[rand.Intn(len(letters))])
	}

	return out
}

// Convert the given size in btes to a readable string
func SizeToStr(size int64) string {
	suffixes := []string{"B", "kB", "MB", "GB", "TB", "PB", "EB"}
	for _, suf := range suffixes {
		if size < 1000 {
			return fmt.Sprintf("%d%s", size, suf)
		}
		size /= 1000
	}
	panic("SizeToStr got a size bigger than math.MaxInt64")
}

// Quoted formats a slice of strings to a quoted list of
// comma-separated strings, e.g. `"snap1", "snap2"`
func Quoted(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = strconv.Quote(name)
	}

	return strings.Join(quoted, ", ")
}

// WordWrap takes a input string and word wraps after `n` chars
// into a new slice.
//
// Caveats:
// - If a single word that is biger than max will not get wrapped
// - Extra whitespace will be removed
func WordWrap(s string, max int) []string {
	n := 0

	var out []string
	line := bytes.NewBuffer(nil)
	// FIXME: we want to be smarter here. to quote Gustavo: "The
	// logic here is corrupting the spacing of the original line,
	// which means indentation and tabling will be gone. A better
	// approach would be finding the break point and then using
	// the original content instead of rewriting it."
	for _, word := range strings.Fields(s) {
		if n+len(word) > max && n > 0 {
			out = append(out, line.String())
			line.Truncate(0)
			n = 0
		} else if n > 0 {
			fmt.Fprintf(line, " ")
			n += 1
		}
		fmt.Fprintf(line, word)
		n += len(word)
	}
	if line.Len() > 0 {
		out = append(out, line.String())
	}

	return out
}
