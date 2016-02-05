// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

	"github.com/ubuntu-core/snappy/client"
	"github.com/ubuntu-core/snappy/i18n"
)

type cmdListCaps struct {
}

var shortListCapsHelp = i18n.G("Lists system capabilities")
var longListCapsHelp = i18n.G(`
The list-caps command shows all capabilities and their allocation.
`)

func init() {
	addCommand("list-caps", shortListCapsHelp, longListCapsHelp, func() interface{} {
		return &cmdListCaps{}
	})
}

func (x *cmdListCaps) Execute(args []string) error {
	cli := client.New(nil)
	caps, err := cli.Capabilities()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 1, ' ', 0)
	fmt.Fprintln(w, "Name\tLabel\tType")
	for _, cap := range caps {
		fmt.Fprintf(w, "%s\t%s\t%s\n", cap.Name, cap.Label, cap.Type)
	}
	w.Flush()
	return nil
}
