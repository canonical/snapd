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
	"io/ioutil"
	"os"

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/snappy"
)

type cmdConfig struct {
	Positional struct {
		PackageName string `positional-arg-name:"package name"`
		ConfigFile  string `positional-arg-name:"config file"`
	} `positional-args:"yes"`
}

var shortConfigHelp = i18n.G("Set configuration for an installed package.")

var longConfigHelp = i18n.G("Configures a package. The configuration is a YAML file, provided in the specified file which can be \"-\" for stdin. Output of the command is the current configuration, so running this command with no input file provides a snapshot of the app's current config.")

func init() {
	arg, err := parser.AddCommand("config",
		shortConfigHelp,
		longConfigHelp,
		&cmdConfig{})
	if err != nil {
		logger.Panicf("Unable to config: %v", err)
	}
	addOptionDescription(arg, "package name", i18n.G("Set configuration for a specific installed package"))
	addOptionDescription(arg, "config file", i18n.G("The configuration for the given file"))
}

func (x *cmdConfig) Execute(args []string) (err error) {
	pkgName := x.Positional.PackageName
	configFile := x.Positional.ConfigFile

	// FIXME transform this into something that returns the config for
	// the full system
	if pkgName == "" {
		return errors.New(i18n.G("package name is required"))
	}

	newConfig, err := configurePackage(pkgName, configFile)
	if err == snappy.ErrPackageNotFound {
		// TRANSLATORS: the %s is a pkgname
		return fmt.Errorf(i18n.G("No snap: '%s' found"), pkgName)
	} else if err != nil {
		return err
	}

	// output the new configuration
	os.Stdout.Write(newConfig)

	return nil
}

func configurePackage(pkgName, configFile string) ([]byte, error) {
	config, err := readConfiguration(configFile)
	if err != nil {
		return nil, err
	}

	snap := snappy.ActiveSnapByName(pkgName)
	if snap == nil {
		return nil, snappy.ErrPackageNotFound
	}

	overlord := &snappy.Overlord{}
	return overlord.Configure(snap, config)
}

func readConfiguration(configInput string) (config []byte, err error) {
	switch configInput {
	case "-":
		return ioutil.ReadAll(os.Stdin)
	case "":
		return nil, nil
	default:
		return ioutil.ReadFile(configInput)
	}
}
