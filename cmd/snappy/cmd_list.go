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
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snappy"
)

type cmdList struct {
	Updates bool `short:"u" long:"updates"`
	Verbose bool `short:"v" long:"verbose"`
}

var shortListHelp = i18n.G("List active components installed on a snappy system")

var longListHelp = i18n.G(`Provides a list of all active components installed on a snappy system.

If requested, the command will find out if there are updates for any of the components and indicate that by appending a * to the date. This will be slower as it requires a round trip to the app store on the network.

The developer information refers to non-mainline versions of a package (much like PPAs in deb-based Ubuntu). If the package is the primary version of that package in Ubuntu then the developer info is not shown. This allows one to identify packages which have custom, non-standard versions installed. As a special case, the “sideload” developer refers to packages installed manually on the system.

When a verbose listing is requested, information about the channel used is displayed; which is one of alpha, beta, rc or stable, and all fields are fully expanded too. In some cases, older (inactive) versions of snappy packages will be installed, these will be shown in the verbose output and the active version indicated with a * appended to the name of the component.`)

func init() {
	cmd, err := parser.AddCommand("list",
		shortListHelp,
		longListHelp,
		&cmdList{})
	if err != nil {
		logger.Panicf("Unable to list: %v", err)
	}

	cmd.Aliases = append(cmd.Aliases, "li")
	addOptionDescription(cmd, "updates", i18n.G("Show available updates (requires network)"))
	addOptionDescription(cmd, "verbose", i18n.G("Show channel information and expand all fields"))
}

func (x *cmdList) Execute(args []string) (err error) {
	return x.list()
}

func (x cmdList) list() error {
	installed, err := snappy.ListInstalled()
	if err != nil {
		return err
	}

	if x.Updates {
		updates, err := snappy.ListUpdates()
		if err != nil {
			return err
		}
		showUpdatesList(installed, updates, os.Stdout)
	} else if x.Verbose {
		showVerboseList(installed, os.Stdout)
	} else {
		showInstalledList(installed, os.Stdout)
	}

	return err
}

func formatDate(t time.Time) string {
	return fmt.Sprintf("%v-%02d-%02d", t.Year(), int(t.Month()), t.Day())
}

func showInstalledList(installed []*snappy.Snap, o io.Writer) {
	w := tabwriter.NewWriter(o, 5, 3, 1, ' ', 0)

	fmt.Fprintln(w, "Name\tDate\tVersion\tDeveloper\t")
	for _, part := range installed {
		if part.IsActive() {
			fmt.Fprintln(w, fmt.Sprintf("%s\t%s\t%s\t%s\t", part.Name(), formatDate(part.Date()), part.Version(), part.Developer()))
		}
	}
	w.Flush()

	showRebootMessage(installed, o)
}

func showVerboseList(installed []*snappy.Snap, o io.Writer) {
	w := tabwriter.NewWriter(o, 5, 3, 1, ' ', 0)

	fmt.Fprintln(w, i18n.G("Name\tDate\tVersion\tDeveloper\t"))
	for _, part := range installed {
		active := ""
		if part.IsActive() {
			active = "*"
		}

		needsReboot := ""
		if part.NeedsReboot() {
			active = "!"
		}

		fmt.Fprintln(w, fmt.Sprintf("%s%s\t%s\t%s\t%s%s\t", part.Name(), needsReboot, formatDate(part.Date()), part.Version(), part.Developer(), active))
	}
	w.Flush()

	showRebootMessage(installed, o)
}

func showRebootMessage(installed []*snappy.Snap, o io.Writer) {
	// display all parts that require a reboot
	for _, part := range installed {
		if !part.NeedsReboot() {
			continue
		}

		// TRANSLATORS: the first %s is a pkgname the second a version
		fmt.Fprintln(o, fmt.Sprintf(i18n.G("Reboot to use %s version %s."), part.Name(), part.Version()))
	}
}

func showUpdatesList(installed []*snappy.Snap, updates []*snap.Info, o io.Writer) {
	// TODO tabwriter and output in general to adapt to the spec
	w := tabwriter.NewWriter(o, 5, 3, 1, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, i18n.G("Name\tDate\tVersion\t"))
	for _, installed := range installed {
		if !installed.IsActive() {
			continue
		}
		hasUpdate := ""
		ver := installed.Version()
		date := installed.Date()
		for _, update := range updates {
			if update.Name() == installed.Name() {
				hasUpdate = "*"
				ver = update.Version
				break
			}
		}
		fmt.Fprintln(w, fmt.Sprintf("%s%s\t%v\t%s\t", installed.Name(), hasUpdate, formatDate(date), ver))
	}
}
