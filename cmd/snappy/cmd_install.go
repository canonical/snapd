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
	"strings"

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/release"
	"github.com/ubuntu-core/snappy/snappy"
)

type cmdInstall struct {
	AllowUnauthenticated bool `long:"allow-unauthenticated"`
	DisableGC            bool `long:"no-gc"`
	Positional           struct {
		PackageName string `positional-arg-name:"package name"`
		ConfigFile  string `positional-arg-name:"config file"`
	} `positional-args:"yes"`
}

func init() {
	arg, err := parser.AddCommand("install",
		i18n.G("Install a snap package"),
		i18n.G("Install a snap package"),
		&cmdInstall{})
	if err != nil {
		logger.Panicf("Unable to install: %v", err)
	}
	addOptionDescription(arg, "allow-unauthenticated", i18n.G("Install snaps even if the signature can not be verified."))
	addOptionDescription(arg, "no-gc", i18n.G("Do not clean up old versions of the package."))
	addOptionDescription(arg, "package name", i18n.G("The Package to install (name or path)"))
	addOptionDescription(arg, "config file", i18n.G("The configuration for the given install"))
}

func (x *cmdInstall) Execute(args []string) error {
	return withMutexAndRetry(x.doInstall)
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

	channel := release.Get().Channel
	if idx := strings.IndexByte(pkgName, '/'); idx > -1 && !osutil.FileExists(pkgName) {
		pkgName, channel = pkgName[:idx], pkgName[idx+1:]
	}

	realPkgName, err := snappy.Install(pkgName, channel, flags, progress.MakeProgressBar())
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
