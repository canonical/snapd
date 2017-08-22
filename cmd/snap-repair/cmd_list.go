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
	"os"
	"text/tabwriter"
)

func init() {
	const (
		short = "List repairs run on this device"
		long  = ""
	)

	if _, err := parser.AddCommand("list", short, long, &cmdList{}); err != nil {
		panic(err)
	}

}

type cmdList struct{}

func (c *cmdList) Execute(args []string) error {
	runner := NewRunner()
	err := runner.readState()
	if os.IsNotExist(err) {
		fmt.Fprintf(Stdout, "no repairs yet\n")
		return nil
	}
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(Stdout, 5, 3, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprintf(w, "Issuer\tSeq\tRev\tStatus\n")
	for issuer, repairs := range runner.state.Sequences {
		for _, repairState := range repairs {
			fmt.Fprintf(w, "%s\t%d\t%d\t%s\n", issuer, repairState.Sequence, repairState.Revision, repairState.Status)
		}
	}

	return nil
}
