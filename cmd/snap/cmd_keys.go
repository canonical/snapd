// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2021 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts/signtool"
	"github.com/snapcore/snapd/i18n"
)

type cmdKeys struct {
	JSON bool `long:"json"`
}

func init() {
	cmd := addCommand("keys",
		i18n.G("List cryptographic keys"),
		i18n.G(`
The keys command lists cryptographic keys that can be used for signing
assertions.
`),
		func() flags.Commander {
			return &cmdKeys{}
		}, map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"json": i18n.G("Output results in JSON format"),
		}, nil)
	cmd.hidden = true
	cmd.completeHidden = true
}

// Key represents a key that can be used for signing assertions.
type Key struct {
	Name     string `json:"name"`
	Sha3_384 string `json:"sha3-384"`
}

func outputJSON(keys []Key) error {
	obj := mylog.Check2(json.Marshal(keys))

	fmt.Fprintf(Stdout, "%s\n", obj)
	return nil
}

func outputText(keys []Key) error {
	if len(keys) == 0 {
		fmt.Fprintf(Stderr, "No keys registered, see `snapcraft create-key`\n")
		return nil
	}

	w := tabWriter()
	defer w.Flush()

	fmt.Fprintln(w, i18n.G("Name\tSHA3-384"))
	for _, key := range keys {
		fmt.Fprintf(w, "%s\t%s\n", key.Name, key.Sha3_384)
	}
	return nil
}

func (x *cmdKeys) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	keypairMgr := mylog.Check2(signtool.GetKeypairManager())

	kinfos := mylog.Check2(keypairMgr.List())

	keys := make([]Key, len(kinfos))
	for i, kinfo := range kinfos {
		keys[i].Name = kinfo.Name
		keys[i].Sha3_384 = kinfo.ID
	}

	if x.JSON {
		return outputJSON(keys)
	}

	return outputText(keys)
}
