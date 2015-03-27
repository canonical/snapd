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
	pkgname := x.Positional.PackageName
	if pkgname == "" {
		return errors.New("Config needs a packagename")
	}

	var configInput []byte
	configFile := x.Positional.ConfigFile
	switch {
	case configFile == "-":
		if configInput, err = ioutil.ReadAll(os.Stdin); err != nil {
			return err
		}
	case configFile != "":
		if configInput, err = ioutil.ReadFile(configFile); err != nil {
			return err
		}
	}

	snap := snappy.ActiveSnapByName(pkgname)
	if snap == nil {
		return fmt.Errorf("No snap: '%s' found", pkgname)
	}

	newConfig, err := snap.Config(configInput)
	if err != nil {
		return err
	}
	// output the new configuration
	fmt.Println(newConfig)

	return nil
}
