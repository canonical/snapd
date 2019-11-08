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

package explain

import (
	"fmt"
	"os"
	"strings"
)

var (
	stdout = os.Stdout
)

// Say prints an explanatory message to standard output.
//
// Say is only effective if Enable was called earlier.
var Say = func(f string, args ...interface{}) {}

// Header prints a spaced header, usually separating subsequent programs.
//
// Header  is only effective if Enable was called earlier.
var Header = func(name string) {}

// Do invokes a function that only serves to explain things.
//
// Do can be used to contain code that is only necessary in explain mode. Do
// is only effective if Enable was called earlier.
var Do = func(f func()) {}

// Enable enables explain mode, making Say and Do effective.
//
// Enable also sets the SNAP_EXPLAIN environment variable.
func Enable() {
	Say = func(f string, args ...interface{}) {
		f = strings.Replace(f, "\t", "  ", -1) + "\n"
		fmt.Fprintf(stdout, f, args...)
		stdout.Sync() // Ignore errors
	}
	Header = func(name string) {
		fmt.Fprintf(stdout, "\n<< %s >>\n\n", name)
		stdout.Sync() // Ignore errors
	}
	Do = func(f func()) { f() }
	os.Setenv("SNAP_EXPLAIN", "1")
}

func Disable() {
	Say = func(f string, args ...interface{}) {}
	Header = func(name string) {}
	Do = func(f func()) {}
	os.Unsetenv("SNAP_EXPLAIN")
}
