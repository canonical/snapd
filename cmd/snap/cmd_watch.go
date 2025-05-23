// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type cmdWatch struct {
	changeIDMixin
	mustWaitMixin
}

var shortWatchHelp = i18n.G("Watch a change in progress")
var longWatchHelp = i18n.G(`
The watch command waits for the given change-id to finish and shows progress
(if available).
`)

func init() {
	addCommand("watch", shortWatchHelp, longWatchHelp, func() flags.Commander {
		return &cmdWatch{mustWaitMixin: mustWaitMixin{skipAbort: true, waitForTasksInWaitStatus: true}}
	}, changeIDMixinOptDesc, changeIDMixinArgDesc)
}

func (x *cmdWatch) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	id, err := x.GetChangeID()
	if err != nil {
		if err == noChangeFoundOK {
			return nil
		}
		return err
	}

	_, err = x.wait(id)
	return err
}

func (x *cmdWatch) setClient(c *client.Client) {
	x.changeIDMixin.setClient(c)
	x.mustWaitMixin.setClient(c)
}
