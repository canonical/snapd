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

	"github.com/snapcore/snapd/i18n"

	"github.com/jessevdk/go-flags"
)

var shortIsManagedHelp = i18n.G("internal")
var longIsManagedHelp = i18n.G(`
internal
`)

type cmdIsManaged struct{}

func init() {
	cmd := addCommand("is-managed", shortIsManagedHelp, longIsManagedHelp, func() flags.Commander { return &cmdIsManaged{} }, nil, nil)
	cmd.hidden = true
}

func (cmd cmdIsManaged) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	is, err := Client().SysInfo()
	if err != nil {
		return fmt.Errorf("cannot get system information: %s", err)
	}

	if is.Managed {
		fmt.Fprintf(Stdout, "system is managed\n")
		return nil
	}

	return fmt.Errorf("system is not managed")
}
