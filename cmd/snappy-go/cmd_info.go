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

	"launchpad.net/snappy/logger"
	"launchpad.net/snappy/snappy"
)

type cmdInfo struct {
	Verbose    bool `short:"v" long:"verbose" description:"Provides more detailed information"`
	Positional struct {
		PackageName string `positional-arg-name:"package name" description:"Provide information about a specific installed package"`
	} `positional-args:"yes"`
}

const shortInfoHelp = `Display a summary of key attributes of the snappy system.`

const longInfoHelp = `A concise summary of key attributes of the snappy system, such as the release and channel.

The verbose output includes the specific version information for the factory image, the running image and the image that will be run on reboot, together with a list of the available channels for this image.

Providing a package name will display information about a specific installed package.

The verbose version of the info command for a package will also tell you the available channels for that package, when it was installed for the first time, disk space utilization, and in the case of frameworks, which apps are able to use the framework.`

func init() {
	var cmdInfoData cmdInfo
	if _, err := parser.AddCommand("info", shortInfoHelp, longInfoHelp, &cmdInfoData); err != nil {
		// panic here as something must be terribly wrong if there is an
		// error here
		logger.LogAndPanic(err)
	}
}

func (x *cmdInfo) Execute(args []string) (err error) {
	if x.Positional.PackageName != "" {
		return snapInfo(x.Positional.PackageName, x.Verbose)
	}

	return info()
}

func snapInfo(pkgname string, verbose bool) error {
	snap := snappy.ActiveSnapByName(pkgname)
	if snap == nil {
		return fmt.Errorf("No snap '%s' found", pkgname)
	}

	fmt.Printf("channel: %s\n", snap.Channel())
	fmt.Printf("version: %s\n", snap.Version())
	fmt.Printf("updated: %s\n", snap.Date())
	if verbose {
		fmt.Printf("installed: %s\n", "n/a")
		fmt.Printf("binary-size: %v\n", snap.InstalledSize())
		fmt.Printf("data-size: %s\n", "n/a")
		// FIXME: implement backup list per spec
	}

	return nil
}

func ubuntuCoreChannel() string {
	parts, err := snappy.InstalledSnapsByType(snappy.SnapTypeCore)
	if len(parts) == 1 && err == nil {
		return parts[0].Channel()
	}

	return "unknown"
}

func info() error {
	release := ubuntuCoreChannel()
	frameworks, _ := snappy.InstalledSnapNamesByType(snappy.SnapTypeFramework)
	apps, _ := snappy.InstalledSnapNamesByType(snappy.SnapTypeApp)

	fmt.Printf("release: %s\n", release)
	fmt.Printf("architecture: %s\n", snappy.Architecture())
	fmt.Printf("frameworks: %s\n", strings.Join(frameworks, ", "))
	fmt.Printf("apps: %s\n", strings.Join(apps, ", "))

	return nil
}
