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
	"encoding/json"
	"fmt"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/i18n"

	"github.com/jessevdk/go-flags"
)

type cmdKeys struct {
	JSON bool `long:"json"`
}

func init() {
	cmd := addCommand("keys",
		i18n.G("List cryptographic keys"),
		i18n.G("List cryptographic keys that can be used for signing assertions."),
		func() flags.Commander {
			return &cmdKeys{}
		}, map[string]string{"json": i18n.G("Output results in JSON format")}, nil)
	cmd.hidden = true
}

// Key represents a key that can be used for signing assertions.
type Key struct {
	Name     string `json:"name"`
	Sha3_384 string `json:"sha3-384"`
}

func (x *cmdKeys) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	w := tabWriter()
	if !x.JSON {
		fmt.Fprintln(w, i18n.G("Name\tSHA3-384"))
		defer w.Flush()
	}
	keys := []Key{}

	manager := asserts.NewGPGKeypairManager()
	display := func(privk asserts.PrivateKey, fpr string, uid string) error {
		key := Key{
			Name:     uid,
			Sha3_384: privk.PublicKey().ID(),
		}
		if x.JSON {
			keys = append(keys, key)
		} else {
			fmt.Fprintf(w, "%s\t%s\n", key.Name, key.Sha3_384)
		}
		return nil
	}
	err := manager.Walk(display)
	if err != nil {
		return err
	}
	if x.JSON {
		obj, err := json.Marshal(keys)
		if err != nil {
			return err
		}
		fmt.Fprintf(Stdout, "%s\n", obj)
	}

	return nil
}
