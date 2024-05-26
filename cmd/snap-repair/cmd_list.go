// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"text/tabwriter"

	"github.com/ddkwork/golibrary/mylog"
)

func init() {
	const (
		short = "Lists repairs run on this device"
		long  = ""
	)
	mylog.Check2(parser.AddCommand("list", short, long, &cmdList{}))
}

type cmdList struct{}

func (c *cmdList) Execute([]string) error {
	w := tabwriter.NewWriter(Stdout, 5, 3, 2, ' ', 0)
	defer w.Flush()

	// FIXME: this will not currently list the repairs that are
	//        skipped because of e.g. wrong architecture

	// directory structure is:
	// var/lib/snapd/run/repairs/
	//  canonical/
	//    1/
	//      r0.retry
	//      r0.script
	//      r1.done
	//      r1.script
	//    2/
	//      r3.done
	//      r3.script
	repairTraces := mylog.Check2(newRepairTraces("*", "*"))

	if len(repairTraces) == 0 {
		fmt.Fprintf(Stderr, "no repairs yet\n")
		return nil
	}

	fmt.Fprintf(w, "Repair\tRev\tStatus\tSummary\n")
	for _, t := range repairTraces {
		fmt.Fprintf(w, "%s\t%v\t%s\t%s\n", t.Repair(), t.Revision(), t.Status(), t.Summary())
	}

	return nil
}
