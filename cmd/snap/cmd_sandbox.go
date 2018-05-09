// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"sort"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
)

var shortSandboxHelp = i18n.G("Print the details of the sandbox available on the system")
var longSandboxHelp = i18n.G(`
The sandbox command prints tags describing features of individual sandbox
components used by snapd on a given system.
`)

type cmdSandbox struct{}

func init() {
	addDebugCommand("sandbox", shortSandboxHelp, longSandboxHelp, func() flags.Commander { return &cmdSandbox{} })
}

func (cmd cmdSandbox) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	cli := Client()
	sysInfo, err := cli.SysInfo()
	if err != nil {
		return err
	}
	sandbox := sysInfo.Sandbox
	keys := make([]string, 0, len(sandbox))
	for key := range sandbox {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	w := tabWriter()
	defer w.Flush()
	for _, key := range keys {
		fmt.Fprintf(w, "%s:\t%s\n", key, strings.Join(sandbox[key], " "))
	}

	return nil
}
