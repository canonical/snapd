// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"encoding/base64"
	"fmt"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/sandbox/lsm"
)

type cmdDebugLSM struct{}

func init() {
	addDebugCommand("lsm",
		"(internal) obtain status information on LSMs",
		"(internal) obtain status information on LSMs",
		func() flags.Commander {
			return &cmdDebugLSM{}
		}, nil, nil)
}

func (x *cmdDebugLSM) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	lsms, err := lsm.List()
	if err != nil {
		return err
	}

	fmt.Fprintf(Stdout, "found %v active LSMs\n", len(lsms))
	for _, id := range lsms {
		fmt.Fprintf(Stdout, "- (%4d) %s\n", id, id)
	}

	entries, err := lsm.CurrentContext()
	if err != nil {
		return err
	}

	fmt.Fprintf(Stdout, "context entries: %v\n", len(entries))

	for _, e := range entries {
		currentName := e.LsmID.String()
		if e.LsmID.HasStringContext() {
			fmt.Fprintf(Stdout, "current %v LSM context: %q\n", currentName, lsm.ContextAsString(e.Context))
		} else {
			fmt.Fprintf(Stdout, "current %v LSM context (binary): %v\n", currentName, base64.StdEncoding.EncodeToString(e.Context))
		}
	}

	return nil
}
