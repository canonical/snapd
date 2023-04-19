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
func FilterKernelCmdline(cmdline string, allowedSl []osutil.KernelArgument) (argsAllowed, argsDenied string) {
	// Set of allowed arguments
	allowed := kernelArgsSet{}
	wildcards := map[string]bool{}
	for _, p := range allowedSl {
		if p.Value == "*" && !p.Quoted {
			// Currently only allowed globbing
			wildcards[p.Param] = true
		} else {
			allowed[kargKey{par: p.Param, val: p.Value}] = true
		}
	}

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
		if allowed[kargKey{par: p.Param, val: p.Value}] || wildcards[p.Param] {
			in = append(in, buildArg(p))
		} else {
			out = append(out, buildArg(p))
		}
	}

	argsAllowed = strutil.JoinNonEmpty(in, " ")
	argsDenied = strutil.JoinNonEmpty(out, " ")
	return argsAllowed, argsDenied
}
