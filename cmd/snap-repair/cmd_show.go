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
	"io"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
)

func init() {
	const (
		short = "Shows specific repairs run on this device"
		long  = ""
	)
	mylog.Check2(parser.AddCommand("show", short, long, &cmdShow{}))
}

type cmdShow struct {
	Positional struct {
		Repair []string `positional-arg-name:"<repair>"`
	} `positional-args:"yes"`
}

func showRepairDetails(w io.Writer, repair string) error {
	i := strings.LastIndex(repair, "-")
	if i < 0 {
		return fmt.Errorf("cannot parse repair %q", repair)
	}
	brand := repair[:i]
	seq := repair[i+1:]

	repairTraces := mylog.Check2(newRepairTraces(brand, seq))

	if len(repairTraces) == 0 {
		return fmt.Errorf("cannot find repair \"%s-%s\"", brand, seq)
	}

	for _, trace := range repairTraces {
		fmt.Fprintf(w, "repair: %s\n", trace.Repair())
		fmt.Fprintf(w, "revision: %s\n", trace.Revision())
		fmt.Fprintf(w, "status: %s\n", trace.Status())
		fmt.Fprintf(w, "summary: %s\n", trace.Summary())

		fmt.Fprintf(w, "script:\n")
		mylog.Check(trace.WriteScriptIndented(w, 2))

		fmt.Fprintf(w, "output:\n")
		mylog.Check(trace.WriteOutputIndented(w, 2))

	}

	return nil
}

func (c *cmdShow) Execute([]string) error {
	for _, repair := range c.Positional.Repair {
		mylog.Check(showRepairDetails(Stdout, repair))

		fmt.Fprintf(Stdout, "\n")
	}

	return nil
}
