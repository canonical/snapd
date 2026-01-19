// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package main

import (
	"fmt"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/snap/squashfs"
)

var shortDeltaHelp = i18n.G("Apply/Generate snap detla")
var longDeltaHelp = i18n.G(`
Generate / apply 'smart' delta between source and target snaps

Operations:
  generate : generate delta between source and target
  apply    : apply delta on the source

  delta tool, for generate operation one of the following tools has to be spefified
	--hdiff:   use hdiffz(hpatchz) as delta tool to generate and apply delta on squashfs pseudo file definition
	           As this delta tool does not support streaming, pseudo file definition is processed per file within the stream.
	--xdelta3: use xdelta3 as delta tool to generate and apply delta on squashfs pseudo file definition

Examples:
  $ snap delta generate --hdiffz --source <SNAP>  --target <SNAP> --delta <DELTA>
  $ snap delta apply             --source <SNAP>  --target <SNAP> --delta <DELTA>
`)

type cmdDelta struct {
	clientMixin
	Positional struct {
		Operation string `positional-args:"yes" choices:"apply|generate"`
	} `positional-args:"yes" required:"yes"`

	Source      string `long:"source" short:"s" required:"yes"`
	Target      string `long:"target" short:"t" required:"yes"`
	Delta       string `long:"delta" short:"d" required:"yes"`
	Xdelta3Tool bool   `long:"xdelta3" short:"x"`
	HdiffzTool  bool   `long:"hdiffz" short:"h"`
}

func init() {
	addCommand("delta", shortDeltaHelp, longDeltaHelp, func() flags.Commander { return &cmdDelta{} },
		map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"source": i18n.G("Source snap package"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"target": i18n.G("Target snap package"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"delta": i18n.G("Delta between source and target snap package"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"xdelta3": i18n.G("Use xdelta3 tool for delta"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"hdiffz": i18n.G("Use hdiffz tool for delta generation"),
		}, []argDesc{
			{
				// TRANSLATORS: This needs to begin with < and end with >
				name: i18n.G("<operation>"),
				// TRANSLATORS: This should not start with a lowercase letter.
				desc: i18n.G("The delta operation to perform, one of: apply|generate"),
			},
		})
}

func (x *cmdDelta) Execute(args []string) error {
	algo, _, _, _, _, _, err := squashfs.CheckSupportedDeltaFormats(nil)
	if err != nil {
		return fmt.Errorf(i18n.G("snap delta not supported: %v"), err)
	}
	fmt.Fprintf(Stdout, i18n.G("Using snap delta algorithm '%s'\n"), strings.Split(algo, ";")[0])
	if "generate" == x.Positional.Operation {
		var deltaTool uint16
		if x.HdiffzTool {
			deltaTool = squashfs.DeltaToolHdiffz
		} else if x.Xdelta3Tool {
			deltaTool = squashfs.DeltaToolXdelta3
		} else {
			return fmt.Errorf(i18n.G("missing delta tool setting for generate operation"))
		}
		fmt.Fprintf(Stdout, i18n.G("Generating delta...\n"))
		return squashfs.GenerateSnapDelta(x.Source, x.Target, x.Delta, deltaTool)
	} else if "apply" == x.Positional.Operation {
		if x.Xdelta3Tool || x.HdiffzTool {
			return fmt.Errorf(i18n.G("cannot define delta tool (xdelta3/hdiffz) for apply operation"))
		}
		fmt.Fprintf(Stdout, i18n.G("Applying delta...\n"))
		return squashfs.ApplySnapDelta(x.Source, x.Delta, x.Target)
	}
	return fmt.Errorf(i18n.G("unknown operation: %s"), x.Positional.Operation)
}
