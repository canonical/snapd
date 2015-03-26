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
	"os"

	"launchpad.net/snappy/priv"
	"launchpad.net/snappy/snappy"

	"github.com/jessevdk/go-flags"
)

// fixed errors the command can return
var ErrRequiresRoot = errors.New("command requires sudo (root)")

type options struct {
	// No global options yet
}

var optionsData options

var parser = flags.NewParser(&optionsData, flags.Default)

func init() {
	if os.Getenv("SNAPPY_DEBUG") != "" {
		// FIXME: need a global logger!
		//setupLogger()
	}
}

func main() {
	if _, err := parser.Parse(); err != nil {
		if err == priv.ErrNeedRoot {
			// make the generic root error more specific for
			// the CLI user.
			err = snappy.ErrNeedRoot
		}
		os.Exit(1)
	}
}
