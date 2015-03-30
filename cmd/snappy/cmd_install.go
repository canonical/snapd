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
	"errors"
	"fmt"
	"os"

	"launchpad.net/snappy/priv"
	"launchpad.net/snappy/snappy"
)

type cmdInstall struct {
	AllowUnauthenticated bool `long:"allow-unauthenticated" description:"Install snaps even if the signature can not be verified."`
	Positional           struct {
		PackageName string `positional-arg-name:"package name" description:"Set configuration for a specific installed package"`
		ConfigFile  string `positional-arg-name:"config file" description:"The configuration for the given file"`
	} `positional-args:"yes"`
}

func init() {
	var cmdInstallData cmdInstall
	_, _ = parser.AddCommand("install",
		"Install a snap package",
		"Install a snap package",
		&cmdInstallData)
}

func (x *cmdInstall) Execute(args []string) (err error) {
	pkgName := x.Positional.PackageName
	configFile := x.Positional.ConfigFile

	// FIXME patch goflags to allow for specific n required positional arguments
	if pkgName == "" {
		return errors.New("package name is required")
	}

	privMutex := priv.New()
	if err := privMutex.TryLock(); err != nil {
		return err
	}
	defer privMutex.Unlock()

	var flags snappy.InstallFlags
	if x.AllowUnauthenticated {
		flags |= snappy.AllowUnauthenticated
	}

	fmt.Printf("Installing %s\n", pkgName)
	if err := snappy.Install(pkgName, flags); err == snappy.ErrPackageNotFound {
		return fmt.Errorf("No package '%s' for %s", pkgName, ubuntuCoreChannel())
	} else if err != nil {
		return err
	}

	if configFile != "" {
		if _, err := configurePackage(pkgName, configFile); err != nil {
			return err
		}
	}

	// call show versions afterwards
	installed, err := snappy.ListInstalled()
	if err != nil {
		return err
	}

	showInstalledList(installed, os.Stdout)

	return nil
}
