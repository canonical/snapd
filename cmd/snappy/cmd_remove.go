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

	"launchpad.net/snappy/priv"
	"launchpad.net/snappy/progress"
	"launchpad.net/snappy/snappy"
)

type cmdRemove struct {
	DisableGC bool `long:"no-gc" description:"Do not clean up old versions of the package."`
}

func init() {
	var cmdRemoveData cmdRemove
	_, _ = parser.AddCommand("remove",
		"Remove a snapp part",
		"Remove a snapp part",
		&cmdRemoveData)
}

func (x *cmdRemove) Execute(args []string) (err error) {
	privMutex := priv.New()
	if err := privMutex.TryLock(); err != nil {
		return err
	}
	defer privMutex.Unlock()

	flags := snappy.DoRemoveGC
	if x.DisableGC {
		flags = 0
	}

	for _, part := range args {
		fmt.Printf("Removing %s\n", part)

		pbar := progress.NewTextProgress(part)
		if err := snappy.Remove(part, flags, pbar); err != nil {
			return err
		}
	}

	return nil
}
