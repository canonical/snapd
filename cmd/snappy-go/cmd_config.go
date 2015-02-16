package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"

	"launchpad.net/snappy/snappy"
)

type cmdConfig struct {
	Positional struct {
		PackageName string `positional-arg-name:"package name" description:"Set configuration for a specific installed package"`
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
		panic(err)
	}
}

func (x *cmdConfig) Execute(args []string) (err error) {
	pkgname := x.Positional.PackageName
	if pkgname == "" {
		return errors.New("Config needs a packagename")
	}

	input, err := ioutil.ReadAll(os.Stdin)
	snap := snappy.ActiveSnapByName(pkgname)
	if snap == nil {
		return fmt.Errorf("No snap: '%s' found", pkgname)
	}
	return snap.Config(input)
}
