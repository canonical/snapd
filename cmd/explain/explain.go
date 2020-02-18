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
	"sort"
	"strings"
)

var (
	stdout  = os.Stdout
	enabled = false
)

type explainState struct {
	indent int
}

var cs explainState

// Say prints an explanatory message to standard output.
//
// Say is only effective if Enable was called earlier.
func Say(f string, args ...interface{}) {
	if !enabled {
		return
	}

	for i := cs.indent; i > 0; i-- {
		f = "\t" + f
	}
	f = strings.Replace(f, "\t", "  ", -1) + "\n"
	fmt.Fprintf(stdout, f, args...)
	stdout.Sync() // Ignore errors
}

func StartSection(f string, args ...interface{}) {
	Say(f, args...)
	cs.indent++
}

func EndSection() {
	cs.indent--
}

func ListItem(f string, args ...interface{}) {
	f = "- " + f
	Say(f, args...)
}

func SayExtraEnv(env []string) {
	envCopy := make([]string, len(env))
	copy(envCopy, env)
	sort.Strings(envCopy)
	extraEnv := make([]string, 0, len(env))
	for _, envItem := range envCopy {
		keyValue := strings.SplitN(envItem, "=", 2)
		key, value := keyValue[0], keyValue[1]
		if os.Getenv(key) != value {
			extraEnv = append(extraEnv, envItem)
		}
	}
	if len(extraEnv) > 0 {
		StartSection("with environment additions")
		for _, ee := range extraEnv {
			Say(ee)
		}
		EndSection()
	}
}

// Header prints a spaced header, usually separating subsequent programs.
//
// Header  is only effective if Enable was called earlier.
// Depending on the number of extras used the format is as follows:
//
// Zero: << $name >>
// One:  << $name ($extra[0]) >>
// Two:  << $extra[1] $name ($extra[0]) >>
func Header(name string, extras ...string) {
	if !enabled {
		return
	}
	switch len(extras) {
	case 0:
		fmt.Fprintf(stdout, "\n<< %s >>\n\n", name)
	case 1:
		fmt.Fprintf(stdout, "\n<< %s (%s) >>\n\n", name, extras[0])
	case 2:
		fmt.Fprintf(stdout, "\n<< %s %s (%s) >>\n\n", extras[1], name, extras[0])
	}
	stdout.Sync() // Ignore errors
}

// Do invokes a function that only serves to explain things.
//
// Do can be used to contain code that is only necessary in explain mode. Do
// is only effective if Enable was called earlier.
func Do(f func()) {
	if !enabled {
		return
	}
	f()
}

// Enable enables explain mode, making Say and Do effective.
//
// Enable also sets the SNAP_EXPLAIN environment variable.
func Enable() {
	enabled = true
	os.Setenv("SNAP_EXPLAIN", "1")
}

func Disable() {
	enabled = false
	os.Unsetenv("SNAP_EXPLAIN")
}
