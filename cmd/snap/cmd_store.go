// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

	"github.com/jessevdk/go-flags"
	"github.com/snapcore/snapd/i18n"
)

type cmdStore struct {
	Revert bool `long:"revert"`

	Positional struct {
		Store string `positional-arg-name:"<store-id>"`
	} `positional-args:"yes"`
}

var shortStoreHelp = i18n.G("Manages store configuration")
var longStoreHelp = i18n.G(`
The store command sets or reverts store configuration for the system.

To configure the system to use an alternative store the command's arguments
must identify a store assertion, by store ID, in the system's assertion
database.

Reverting store configuration updates the system to use the default store.
`)

func init() {
	cmd := addCommand(
		"store",
		shortStoreHelp, longStoreHelp,
		func() flags.Commander { return &cmdStore{} },
		map[string]string{
			"revert": i18n.G("Revert to system default store"),
		}, []argDesc{{
			name: i18n.G("<store id>"),
			desc: i18n.G("ID of the store"),
		}})
	// XXX: hidden for now
	cmd.hidden = true
}

func (cmd *cmdStore) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	if cmd.Revert {
		if cmd.Positional.Store != "" {
			return errors.New("store ID must not be provided when reverting")
		}
		return revert()
	} else {
		return setStore(cmd.Positional.Store)
	}
}

func revert() error {
	cli := Client()
	return cli.UnsetStore()
}

func setStore(store string) error {
	cli := Client()
	_, err := cli.SetStore(store)
	return err
}
