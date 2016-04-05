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
	"strings"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/release"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snappy"
)

type cmdInfo struct {
	Verbose       bool `short:"v" long:"verbose"`
	IncludeRemote bool `long:"include-remote"`
	Positional    struct {
		PackageName string `positional-arg-name:"package name"`
	} `positional-args:"yes"`
}

var shortInfoHelp = i18n.G("Display a summary of key attributes of the snappy system.")

// FIXME: gettext does not understand ``
var longInfoHelp = i18n.G(`A concise summary of key attributes of the snappy system, such as the release and channel.

The verbose output includes the specific version information for the factory image, the running image and the image that will be run on reboot, together with a list of the available channels for this image.

Providing a package name will display information about a specific installed package.

The verbose version of the info command for a package will also tell you the available channels for that package, when it was installed for the first time, disk space utilization, and in the case of frameworks, which apps are able to use the framework.`)

func init() {
	arg, err := parser.AddCommand("info",
		shortInfoHelp,
		longInfoHelp,
		&cmdInfo{})
	if err != nil {
		logger.Panicf("Unable to info: %v", err)
	}
	addOptionDescription(arg, "verbose", i18n.G("Provides more detailed information"))
	addOptionDescription(arg, "include-remote", i18n.G("Include information about packages from the snappy store"))
	addOptionDescription(arg, "package name", i18n.G("Provide information about a specific installed package"))
}

func (x *cmdInfo) Execute(args []string) (err error) {
	if x.Positional.PackageName != "" {
		return snapInfo(x.Positional.PackageName, x.IncludeRemote, x.Verbose)
	}

	return info()
}

func snapInfo(pkgname string, includeStore, verbose bool) error {
	snap := snappy.ActiveSnapByName(pkgname)
	if snap == nil && includeStore {
		m := snappy.NewConfiguredUbuntuStoreSnapRepository()
		remote, err := m.Snap(pkgname, release.Get().Channel)
		if err != nil {
			return fmt.Errorf("cannot get details for snap %q: %s", pkgname, err)
		}

		// TRANSLATORS: the %s is a channel name
		fmt.Printf(i18n.G("channel: %s\n"), remote.Channel)
		// TRANSLATORS: the %s is a version string
		fmt.Printf(i18n.G("version: %s\n"), remote.Version)
		if verbose {
			// TRANSLATORS: the %s is a date
			fmt.Printf(i18n.G("installed: %s\n"), "n/a")
			// TRANSLATORS: the %s is a size
			fmt.Printf(i18n.G("data-size: %s\n"), "n/a")
		}

	}

	if snap == nil {
		return fmt.Errorf("no snap '%s' found", pkgname)
	}

	// TRANSLATORS: the %s is a channel name
	fmt.Printf(i18n.G("channel: %s\n"), snap.Channel())
	// TRANSLATORS: the %s is a version string
	fmt.Printf(i18n.G("version: %s\n"), snap.Version())
	// TRANSLATORS: the %s is a date
	fmt.Printf(i18n.G("updated: %s\n"), snap.Date())
	if verbose {
		// TRANSLATORS: the %s is a date
		fmt.Printf(i18n.G("installed: %s\n"), "n/a")
		// TRANSLATORS: the %s is a size
		fmt.Printf(i18n.G("binary-size: %v\n"), snap.InstalledSize())
		// TRANSLATORS: the %s is a size
		fmt.Printf(i18n.G("data-size: %s\n"), "n/a")
		// FIXME: implement backup list per spec
	}

	return nil
}

func info() error {
	rel := release.Get()
	release := fmt.Sprintf("%s/%s", rel.Flavor, rel.Series)
	apps, _ := snappy.ActiveSnapIterByType(snappy.FullName, snap.TypeApp)

	// TRANSLATORS: the %s release string
	fmt.Printf(i18n.G("release: %s\n"), release)
	// TRANSLATORS: the %s an architecture string
	fmt.Printf(i18n.G("architecture: %s\n"), arch.UbuntuArchitecture())
	//TRANSLATORS: the %s represents a list of installed appnames
	//             (e.g. "apps: foo, bar, baz")
	fmt.Printf(i18n.G("apps: %s\n"), strings.Join(apps, ", "))

	return nil
}
