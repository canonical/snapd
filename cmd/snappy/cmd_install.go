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
	"errors"
	"fmt"
	"os"

	"launchpad.net/snappy/i18n"
	"launchpad.net/snappy/logger"
	"launchpad.net/snappy/progress"
	"launchpad.net/snappy/snappy"
)

type cmdInstall struct {
	AllowUnauthenticated bool `long:"allow-unauthenticated" description:"Install snaps even if the signature can not be verified."`
	DisableGC            bool `long:"no-gc" description:"Do not clean up old versions of the package."`
	Positional           struct {
		PackageName string `positional-arg-name:"package name" description:"The Package to install (name or path)"`
		ConfigFile  string `positional-arg-name:"config file" description:"The configuration for the given install"`
	} `positional-args:"yes"`
}

func init() {
	_, err := parser.AddCommand("install",
		i18n.G("Install a snap package"),
		i18n.G("Install a snap package"),
		&cmdInstall{})
	if err != nil {
		logger.Panicf("Unable to install: %v", err)
	}
}

func (x *cmdInstall) Execute(args []string) error {
	return withMutex(x.doInstall)
}

func (x *cmdInstall) doInstall() error {
	pkgName := x.Positional.PackageName
	configFile := x.Positional.ConfigFile

	// FIXME patch goflags to allow for specific n required positional arguments
	if pkgName == "" {
		return errors.New(i18n.G("package name is required"))
	}

	flags := snappy.DoInstallGC
	if x.DisableGC {
		flags = 0
	}
	if x.AllowUnauthenticated {
		flags |= snappy.AllowUnauthenticated
	}
	// TRANSLATORS: the %s is a pkgname
	fmt.Printf(i18n.G("Installing %s\n"), pkgName)

	realPkgName, err := snappy.Install(pkgName, flags, progress.MakeProgressBar())
	if err != nil {
		return err
	}

	if configFile != "" {
		if _, err := configurePackage(realPkgName, configFile); err != nil {
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
