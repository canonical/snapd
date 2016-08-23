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
	"fmt"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/i18n"
)

type cmdExportKey struct {
	Positional struct {
		KeyName string `positional-arg-name:"<key-name>" description:"name of key to export"`
	} `positional-args:"true" required:"true"`
}

func init() {
	cmd := addCommand("export-key",
		i18n.G("Export cryptographic public key"),
		i18n.G("Export a public key assertion body that may be imported by other systems."),
		func() flags.Commander {
			return &cmdExportKey{}
		})
	cmd.hidden = true
}

func (x *cmdExportKey) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	manager := asserts.NewGPGKeypairManager("")
	encoded, err := manager.Export(x.Positional.KeyName)
	if err != nil {
		return err
	}
	fmt.Fprintf(Stdout, "%s\n", encoded)
	return nil
}
