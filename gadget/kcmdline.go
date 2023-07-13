// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package gadget

import (
	"fmt"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
)

type kargKey struct{ par, val string }
type kernelArgsSet map[kargKey]bool

// FilterKernelCmdline returns a filtered command line, removing
// arguments that are not on a list of allowed kernel arguments. A
// wild card ('*') can be used in the allow list for the
// values. Additionally, a string with the arguments that have been
// filtered out is also returned.
func FilterKernelCmdline(cmdline string, allowedSl []osutil.KernelArgumentPattern) (argsAllowed, argsDenied string) {
	matcher := osutil.NewKernelArgumentMatcher(allowedSl)

	proposed := osutil.ParseKernelCommandline(cmdline)

	buildArg := func(arg osutil.KernelArgument) string {
		if arg.Value == "" {
			return arg.Param
		} else {
			val := arg.Value
			if arg.Quoted {
				val = "\"" + arg.Value + "\""
			}
			return fmt.Sprintf("%s=%s", arg.Param, val)
		}
	}

	in := []string{}
	out := []string{}
	for _, p := range proposed {
		if matcher.Match(p) {
			in = append(in, buildArg(p))
		} else {
			out = append(out, buildArg(p))
		}
	}

	argsAllowed = strutil.JoinNonEmpty(in, " ")
	argsDenied = strutil.JoinNonEmpty(out, " ")
	return argsAllowed, argsDenied
}
