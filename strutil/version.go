// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2017 Canonical Ltd
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
	"strconv"
	"strings"
)

// golang: seriously? that's sad!
func max(a, b int) int {
	if a < b {
		return b
	}
	return a
}

// version number compare, inspired by the libapt/python-debian code
func cmpInt(intA, intB int) int {
	if intA < intB {
		return -1
	} else if intA > intB {
		return 1
	}
	return 0
}

/*

Generated with the old implementation:

var (
	matchDigit = regexp.MustCompile("[0-9]").Match
	matchAlpha = regexp.MustCompile("[a-zA-Z]").Match
)

func chOrder(ch uint8) int {
	// "~" is lower than everything else
	if ch == '~' {
		return -10
	}
	// empty is higher than "~" but lower than everything else
	if ch == 0 {
		return -5
	}
	if matchAlpha([]byte{ch}) {
		return int(ch)
	}

	// can only happen if cmpString sets '0' because there is no fragment
	if matchDigit([]byte{ch}) {
		return 0
	}

	return int(ch) + 256
}

func main() {
	fmt.Println("var chOrder = [...]int{")
	for i := 0; i < 16; i++ {
		for j := 0; j < 16; j++ {
			fmt.Printf("%d, ", chOrder(uint8(i*16+j)))
		}
		fmt.Println()
	}
	fmt.Println("}")
}

*/
var chOrder = [...]int{
	-5, 257, 258, 259, 260, 261, 262, 263, 264, 265, 266, 267, 268, 269, 270, 271,
	272, 273, 274, 275, 276, 277, 278, 279, 280, 281, 282, 283, 284, 285, 286, 287,
	288, 289, 290, 291, 292, 293, 294, 295, 296, 297, 298, 299, 300, 301, 302, 303,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 314, 315, 316, 317, 318, 319,
	320, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79,
	80, 81, 82, 83, 84, 85, 86, 87, 88, 89, 90, 347, 348, 349, 350, 351,
	352, 97, 98, 99, 100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111,
	112, 113, 114, 115, 116, 117, 118, 119, 120, 121, 122, 379, 380, 381, -10, 383,
	384, 385, 386, 387, 388, 389, 390, 391, 392, 393, 394, 395, 396, 397, 398, 399,
	400, 401, 402, 403, 404, 405, 406, 407, 408, 409, 410, 411, 412, 413, 414, 415,
	416, 417, 418, 419, 420, 421, 422, 423, 424, 425, 426, 427, 428, 429, 430, 431,
	432, 433, 434, 435, 436, 437, 438, 439, 440, 441, 442, 443, 444, 445, 446, 447,
	448, 449, 450, 451, 452, 453, 454, 455, 456, 457, 458, 459, 460, 461, 462, 463,
	464, 465, 466, 467, 468, 469, 470, 471, 472, 473, 474, 475, 476, 477, 478, 479,
	480, 481, 482, 483, 484, 485, 486, 487, 488, 489, 490, 491, 492, 493, 494, 495,
	496, 497, 498, 499, 500, 501, 502, 503, 504, 505, 506, 507, 508, 509, 510, 511,
}

func cmpString(as, bs string) int {
	for i := 0; i < max(len(as), len(bs)); i++ {
		var a uint8
		var b uint8
		if i < len(as) {
			a = as[i]
		}
		if i < len(bs) {
			b = bs[i]
		}
		if chOrder[a] < chOrder[b] {
			return -1
		}
		if chOrder[a] > chOrder[b] {
			return +1
		}
	}
	return 0
}

func cmpFragment(a, b string) int {
	intA, errA := strconv.Atoi(a)
	intB, errB := strconv.Atoi(b)
	if errA == nil && errB == nil {
		return cmpInt(intA, intB)
	}
	res := cmpString(a, b)
	//fmt.Println(a, b, res)
	return res
}

func matchEpoch(a string) bool {
	if len(a) == 0 {
		return false
	}
	if a[0] < '0' || a[0] > '9' {
		return false
	}
	i := 0
	for i = 1; i < len(a) && a[i] >= '0' && a[i] <= '9'; i++ {
	}
	return i < len(a) && a[i] == ':'
}

// VersionIsValid returns true if the given string is a valid
// version number according to the debian policy
func VersionIsValid(a string) bool {
	if matchEpoch(a) {
		return false
	}
	if strings.Count(a, "-") > 1 {
		return false
	}
	return true
}

func nextFrag(s string) (frag, rest string) {
	if len(s) == 0 {
		return "", ""
	}

	var i int
	if s[0] >= 48 && s[0] <= 57 {
		// is digit
		for i = 1; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		}
	} else {
		// not digit
		for i = 1; i < len(s) && (s[i] < '0' || s[i] > '9'); i++ {
		}
	}
	return s[:i], s[i:]
}

func compareSubversion(va, vb string) int {
	var a, b string
	for {
		a, va = nextFrag(va)
		b, vb = nextFrag(vb)
		if a == "" && b == "" {
			break
		}
		res := cmpFragment(a, b)
		//fmt.Println(a, b, res)
		if res != 0 {
			return res
		}
	}
	return 0
}

// VersionCompare compare two version strings that follow the debian
// version policy and
// Returns:
//   -1 if a is smaller than b
//    0 if a equals b
//   +1 if a is bigger than b
func VersionCompare(va, vb string) (res int, err error) {
	// FIXME: return err here instead
	if !VersionIsValid(va) {
		return 0, fmt.Errorf("invalid version %q", va)
	}
	if !VersionIsValid(vb) {
		return 0, fmt.Errorf("invalid version %q", vb)
	}

	ia := strings.IndexByte(va, '-')
	if ia < 0 {
		ia = len(va)
		va += "-0"
	}
	ib := strings.IndexByte(vb, '-')
	if ib < 0 {
		ib = len(vb)
		vb += "-0"
	}

	// the main version number (before the "-")
	mainA := va[:ia]
	mainB := vb[:ib]
	res = compareSubversion(mainA, mainB)
	if res != 0 {
		return res, nil
	}

	// the subversion revision behind the "-"
	revA := strings.Split(va, "-")[1]
	revB := strings.Split(vb, "-")[1]
	return compareSubversion(revA, revB), nil
}
