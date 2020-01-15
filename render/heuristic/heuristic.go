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

package heuristic

import (
	"unicode"
)

// RuneWidth returns the width of a given rune.
//
// Non-graphic runes have width of zero. Most graphical runes have the width of
// one.  Some graphics runes have width of two.  Those include hiragana,
// katakana, han/kanji and hangul.
func RuneWidth(r rune) int {
	if !unicode.IsGraphic(r) {
		return 0
	}
	if !unicode.In(r, unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Han) {
		return 1
	}
	return 2
}

// TerminalRenderSize returns the size of given text as rendered on a terminal.
//
// This is a heuristic because terminal emulators have bugs and features that
// make it impossible to determine the size perfectly. The return value should
// be correct in all practical cases, though, and is perfectly sufficient for
// computing alignment and positioning.
//
// The heuristic is based on the following observation:
// - graphic have certain width (see RuneWidth)
// - some control characters are handled (\n, \r, \t, \v and \b)
func TerminalRenderSize(text string) (width, height int) {
	width = 0
	height = 1
	n := 0
	for _, r := range text {
		switch {
		case unicode.IsGraphic(r):
			n += RuneWidth(r)
		case unicode.IsControl(r):
			switch r {
			case '\n':
				if n > width {
					width = n
				}
				height++
				n = 0
			case '\r':
				if n > width {
					width = n
				}
				n = 0
			case '\t':
				n += 8
			case '\v':
				height++
			case '\b':
				if n > 0 {
					n--
				}
			}
		}
	}
	if n > width {
		width = n
	}
	return width, height
}
