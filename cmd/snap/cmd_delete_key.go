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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/i18n"
)

type cmdDeleteKey struct {
	Positional struct {
		KeyName keyName
	} `positional-args:"true" required:"true"`
}

func init() {
	cmd := addCommand("delete-key",
		i18n.G("Delete cryptographic key pair"),
		i18n.G("Delete the local cryptographic key pair with the given name."),
		func() flags.Commander {
			return &cmdDeleteKey{}
		}, nil, []argDesc{{
			name: i18n.G("<key-name>"),
			desc: i18n.G("Name of key to delete"),
		}})
	cmd.hidden = true
}

func (x *cmdDeleteKey) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	manager := asserts.NewGPGKeypairManager()
	return manager.Delete(string(x.Positional.KeyName))
}
