// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

	"github.com/snapcore/snapd/i18n"
)

var (
	shortResealHelp = i18n.G("Reseal this device")
	longResealHelp  = i18n.G(`
TODO
`)
)

type cmdReseal struct {
	waitMixin
}

func init() {
	addCommand("reseal",
		shortResealHelp,
		longResealHelp,
		func() flags.Commander {
			return &cmdReseal{}
		},
		waitDescs,
		[]argDesc{},
	)
}

func (x *cmdReseal) Execute(args []string) error {
	const reboot = false
	id, err := x.client.Reseal(false)

	if err != nil {
		return err
	}

	if _, err := x.wait(id); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	return nil
}
