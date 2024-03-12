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
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/strutil"
)

// FilterKernelCmdline returns a filtered command line, removing
// arguments that are not on a list of allowed kernel arguments. A
// wild card ('*') can be used in the allow list for the
// values. Additionally, a string with the arguments that have been
// filtered out is also returned.
func FilterKernelCmdline(cmdline string, allowedSl []kcmdline.ArgumentPattern) (argsAllowed, argsDenied string) {
	matcher := kcmdline.NewMatcher(allowedSl)

	proposed := kcmdline.Parse(cmdline)

	in := []string{}
	out := []string{}
	for _, p := range proposed {
		if matcher.Match(p) {
			in = append(in, p.String())
		} else {
			out = append(out, p.String())
		}
	}

	argsAllowed = strutil.JoinNonEmpty(in, " ")
	argsDenied = strutil.JoinNonEmpty(out, " ")
	return argsAllowed, argsDenied
}
