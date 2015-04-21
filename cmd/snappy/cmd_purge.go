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

	"launchpad.net/snappy/helpers"
	"launchpad.net/snappy/priv"
	"launchpad.net/snappy/snappy"
)

type cmdPurge struct {
	Installed bool `long:"installed" description:"Purge an installed package."`
}

const (
	shortPurgeHelp = `Remove all the data from the listed packages`
	longPurgeHelp  = `Remove all the data from the listed packages. Normally this is used for packages that have been removed and attempting to purge data for an installed package will result in an error. The --installed option  overrides that and enables the administrator to purge all data for an installed package (effectively resetting the package completely).`
)

func init() {
	_, err := parser.AddCommand("purge", shortPurgeHelp, longPurgeHelp, &cmdPurge{})
	if err != nil {
		panic(err)
	}
}

func (x *cmdPurge) Execute(args []string) (err error) {
	privMutex := priv.New()
	if err := privMutex.TryLock(); err != nil {
		return err
	}
	defer privMutex.Unlock()

	var flags snappy.PurgeFlags
	if x.Installed {
		flags = snappy.DoPurgeActive
	}

	for _, part := range args {
		fmt.Printf("Purging %s\n", part)

		if err := snappy.Purge(part, flags, helpers.MakeProgressBar(part)); err != nil {
			return err
		}
	}

	return nil
}
