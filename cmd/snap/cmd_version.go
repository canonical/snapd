// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/snapdtool"
)

var (
	shortVersionHelp = i18n.G("Show version details")
	longVersionHelp  = i18n.G(`
The version command displays the versions of the running client, server,
and operating system.
`)
)

type cmdVersion struct {
	clientMixin
}

func init() {
	addCommand("version", shortVersionHelp, longVersionHelp, func() flags.Commander { return &cmdVersion{} }, nil, nil)
}

func (cmd cmdVersion) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	return printVersions(cmd.client)
}

func printVersions(cli *client.Client) error {
	sv := serverVersion(cli)
	w := tabWriter()

	fmt.Fprintf(w, "snap\t%s\n", snapdtool.Version)
	fmt.Fprintf(w, "snapd\t%s\n", sv.Version)
	fmt.Fprintf(w, "series\t%s\n", sv.Series)
	if sv.OnClassic {
		if sv.OSVersionID == "" {
			sv.OSVersionID = "-"
		}
		fmt.Fprintf(w, "%s\t%s\n", sv.OSID, sv.OSVersionID)
	}
	if sv.KernelVersion != "" {
		fmt.Fprintf(w, "kernel\t%s\n", sv.KernelVersion)
	}

	w.Flush()

	return nil
}
