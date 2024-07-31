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
	"fmt"
	"os"

	"github.com/jessevdk/go-flags"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snapdtool"
)

type cmdDebugExec struct {
	Tool        string `long:"tool"`
	Positionals struct {
		What string `required:"yes" positional-arg-name:"<what>"`
	} `positional-args:"true"`
}

func init() {
	addDebugCommand("execution",
		"Obtain information about execution aspects of snap toolchain commands",
		`Display debugging information about aspects of snap toolchain execution, such
as reexecution, tools location etc.`,
		func() flags.Commander { return &cmdDebugExec{} },
		map[string]string{
			"tool": "Internal tool name",
		}, []argDesc{{
			name: "<what>",
			desc: "What aspect, one of: snap, apparmor, internal-tool",
		}})
}

func (x *cmdDebugExec) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	switch x.Positionals.What {
	case "snap":
		fmt.Fprintf(Stdout, "distro-supports-reexec: %v\n", snapdtool.DistroSupportsReExec())
		fmt.Fprintf(Stdout, "is-reexec-enabled: %v\n", snapdtool.IsReexecEnabled())
		fmt.Fprintf(Stdout, "is-reexec-explicitly-enabled: %v\n", snapdtool.IsReexecExplicitlyEnabled())

		isReexecd, err := snapdtool.IsReexecd()
		if err == nil {
			fmt.Fprintf(Stdout, "is-reexecd: %v\n", isReexecd)
		} else {
			fmt.Fprintf(Stdout, "is-reexecd: error:%v\n", err)
		}

		exe, err := os.Readlink("/proc/self/exe")
		if err != nil {
			return err
		}

		fmt.Fprintf(Stdout, "self-exe: %v\n", exe)

	case "apparmor":
		cmd, internal, err := apparmor.AppArmorParser()
		if err != nil {
			fmt.Fprintf(Stdout, "apparmor-parser: error:%s\n", err.Error())
			fmt.Fprint(Stdout, "internal: false\n")
			return err
		} else {
			fmt.Fprintf(Stdout, "apparmor-parser: %s\n", cmd.Path)
			fmt.Fprintf(Stdout, "internal: %v\n", internal)
		}

	case "internal-tool":
		if x.Tool == "" {
			return fmt.Errorf("tool name is missing")
		}

		tool, err := snapdtool.InternalToolPath(x.Tool)
		if err != nil {
			return err
		}

		fmt.Fprintf(Stdout, "%s: %s\n", x.Tool, tool)
	}
	return nil
}
