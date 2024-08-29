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
	"errors"
	"fmt"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/signtool"
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
		i18n.G(`
The delete-key command deletes the local cryptographic key pair with
the given name.
`),
		func() flags.Commander {
			return &cmdDeleteKey{}
		}, nil, []argDesc{{
			// TRANSLATORS: This needs to begin with < and end with >
			name: i18n.G("<key-name>"),
			// TRANSLATORS: This should not start with a lowercase letter.
			desc: i18n.G("Name of key to delete"),
		}})
	cmd.hidden = true
	cmd.completeHidden = true
}

func (x *cmdDeleteKey) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	keypairMgr, err := signtool.GetKeypairManager()
	if err != nil {
		return err
	}
	keyName := string(x.Positional.KeyName)
	err = keypairMgr.DeleteByName(keyName)
	if _, ok := err.(*asserts.ExternalUnsupportedOpError); ok {
		return errors.New(i18n.G("cannot delete external keypair manager key via snap command, use the appropriate external procedure"))
	}
	if err != nil {
		return fmt.Errorf("cannot delete key named %q: %v", keyName, err)
	}
	return nil
}
