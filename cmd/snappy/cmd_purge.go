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
	"fmt"

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snappy"
)

type cmdPurge struct {
	Installed bool `long:"installed"`
}

var (
	shortPurgeHelp = i18n.G("Remove all the data from the listed packages")
	longPurgeHelp  = i18n.G(`Remove all the data from the listed packages. Normally this is used for packages that have been removed and attempting to purge data for an installed package will result in an error. The --installed option  overrides that and enables the administrator to purge all data for an installed package (effectively resetting the package completely).`)
)

func init() {
	arg, err := parser.AddCommand("purge",
		shortPurgeHelp,
		longPurgeHelp,
		&cmdPurge{})
	if err != nil {
		logger.Panicf("Unable to purge: %v", err)
	}
	addOptionDescription(arg, "installed", i18n.G("Purge an installed package."))
}

func (x *cmdPurge) Execute(args []string) error {
	return withMutex(func() error {
		return x.doPurge(args)
	})
}

func (x *cmdPurge) doPurge(args []string) error {
	var flags snappy.PurgeFlags
	if x.Installed {
		flags = snappy.DoPurgeActive
	}

	for _, part := range args {
		// TRANSLATORS: the %s is a pkgname
		fmt.Printf(i18n.G("Purging %s\n"), part)

		if err := snappy.Purge(part, flags, progress.MakeProgressBar()); err != nil {
			return err
		}
	}

	return nil
}
