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
	"github.com/ubuntu-core/snappy/logger"
)

type cmdListCaps struct {
}

var (
	shortListCapsHelp = i18n.G("List system capabilities")
	longListCapsHelp  = i18n.G("This command shows all capabilities and their allocation")
)

func init() {
	_, err := parser.AddCommand("list-caps", shortListCapsHelp, longListCapsHelp, &cmdListCaps{})
	if err != nil {
		logger.Panicf("Unable to list capabilities: %v", err)
	}
}

func (x *cmdListCaps) Execute(args []string) error {
	cli := client.New()
	caps, err := cli.GetCapabilities()
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
