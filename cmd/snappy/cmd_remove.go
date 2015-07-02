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

	"launchpad.net/snappy/i18n"
	"launchpad.net/snappy/logger"
	"launchpad.net/snappy/progress"
	"launchpad.net/snappy/snappy"
)

type cmdRemove struct {
	DisableGC bool `long:"no-gc" description:"Do not clean up old versions of the package."`
}

func init() {
	_, err := parser.AddCommand("remove",
		i18n.G("Remove a snapp part"),
		i18n.G("Remove a snapp part"),
		&cmdRemove{})
	if err != nil {
		logger.Panicf("Unable to remove: %v", err)
	}
}

func (x *cmdRemove) Execute(args []string) (err error) {
	return withMutex(func() error {
		return x.doRemove(args)
	})
}

func (x *cmdRemove) doRemove(args []string) error {
	flags := snappy.DoRemoveGC
	if x.DisableGC {
		flags = 0
	}

	for _, part := range args {
		// TRANSLATORS: the %s is a pkgname
		fmt.Printf(i18n.G("Removing %s\n"), part)

		if err := snappy.Remove(part, flags, progress.MakeProgressBar()); err != nil {
			return err
		}
	}

	return nil
}
