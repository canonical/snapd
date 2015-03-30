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

	"launchpad.net/snappy/logger"
	"launchpad.net/snappy/snappy"
)

type cmdConfig struct {
	Positional struct {
		PackageName string `positional-arg-name:"package name" description:"Set configuration for a specific installed package"`
		ConfigFile  string `positional-arg-name:"config file" description:"The configuration for the given file"`
	} `positional-args:"yes"`
}

const shortConfigHelp = `Set configuraion for a installed package.`

const longConfigHelp = `Configures a package. The configuration is a
YAML file, provided in the specified file which can be “-” for
stdin. Output of the command is the current configuration, so running
this command with no input file provides a snapshot of the app’s
current config.  `

func init() {
	var cmdConfigData cmdConfig
	if _, err := parser.AddCommand("config", shortConfigHelp, longConfigHelp, &cmdConfigData); err != nil {
		// panic here as something must be terribly wrong if there is an
		// error here
		logger.LogAndPanic(err)
	}
}

func (x *cmdConfig) Execute(args []string) (err error) {
	pkgName := x.Positional.PackageName
	configFile := x.Positional.ConfigFile

	// FIXME transform this into something that returns the config for
	// the full system
	if pkgName == "" {
		return errors.New("package name is required")
	}

	newConfig, err := configurePackage(pkgName, configFile)
	if err == snappy.ErrPackageNotFound {
		return fmt.Errorf("No snap: '%s' found", pkgName)
	} else if err != nil {
		return err
	}

	// output the new configuration
	fmt.Println(newConfig)

	return nil
}

func configurePackage(pkgName, configFile string) (string, error) {
	config, err := readConfiguration(configFile)
	if err != nil {
		return "", err
	}

	snap := snappy.ActiveSnapByName(pkgName)
	if snap == nil {
		return "", snappy.ErrPackageNotFound
	}

	return snap.Config(config)
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
