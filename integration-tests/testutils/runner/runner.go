// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015 Canonical Ltd
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

package runner

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"gopkg.in/check.v1"
)

var (
	// TODO: define more flags when needed
	filterFlag  string
	verboseFlag bool
	streamFlag  bool
	listFlag    bool
)

// TestingT uses the same structure of check.TestingT plus allowing
// to pass a custom io.Writer for the output
func TestingT(testingT *testing.T, output io.Writer) {
	initFlags()

	conf := &check.RunConf{
		// TODO: include more fields when needed
		Output:  output,
		Filter:  filterFlag,
		Verbose: verboseFlag,
		Stream:  streamFlag,
	}

	if listFlag {
		w := bufio.NewWriter(os.Stdout)
		for _, name := range check.ListAll(conf) {
			fmt.Fprintln(w, name)
		}
		w.Flush()
		return
	}

	result := check.RunAll(conf)
	println(result.String())
	if !result.Passed() {
		testingT.Fail()
	}
}

func initFlags() {
	visitor := func(f *flag.Flag) {
		elements := strings.Split(f.Name, ".")
		if len(elements) == 2 {
			// TODO: initialize more flags when needed
			switch elements[1] {
			case "f":
				filterFlag = f.Value.String()
			case "v":
				verboseFlag = boolValue(f.Value.String())
			case "vv":
				streamFlag = boolValue(f.Value.String())
			case "list":
				listFlag = boolValue(f.Value.String())
			}
		}
	}
	flag.Visit(visitor)
}

func boolValue(input string) bool {
	return input == "true"
}
