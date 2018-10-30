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

//go:generate go run github.com/snapcore/snapd/strutil/chrorder -package=strutil -output=chrorder.go

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
