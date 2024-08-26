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
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jessevdk/go-flags"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snapdtool"
)

type cmdDebugExec struct{}

func init() {
	cmd := addDebugCommand("execution",
		"Obtain information about execution aspects of snap toolchain commands",
		`Display debugging information about aspects of snap toolchain execution, such
as reexecution, tools location etc.`,
		func() flags.Commander { return &cmdDebugExec{} },
		nil,
		nil,
	)
	cmd.extra = func(c *flags.Command) {
		c.AddCommand("apparmor", "Show apparmor", "", &cmdDebugExecAppArmor{})
		c.AddCommand("snap", "Show snap execution info", "", &cmdDebugExecSnap{})
		c.AddCommand("internal-tool", "Show internal tool execution info", "", &cmdDebugExecInternalTool{})
	}
}

func (x *cmdDebugExec) Execute(args []string) error {
	return flag.ErrHelp
}

type cmdDebugExecAppArmor struct{}

func (x *cmdDebugExecAppArmor) Execute(args []string) error {
	cmd, internal, err := apparmor.AppArmorParser()
	if err != nil {
		fmt.Fprintf(Stdout, "apparmor-parser: error:%s\n", err.Error())
		fmt.Fprint(Stdout, "internal: false\n")
		return err
	} else {
		fmt.Fprintf(Stdout, "apparmor-parser: %s\n", cmd.Path)
		fmt.Fprintf(Stdout, "apparmor-parser-command: %s\n", strings.Join(cmd.Args, " "))
		fmt.Fprintf(Stdout, "internal: %v\n", internal)
	}
	return nil
}

type cmdDebugExecSnap struct{}

func (x *cmdDebugExecSnap) Execute(args []string) error {
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

	return nil
}

type cmdDebugExecInternalTool struct {
	Positionals struct {
		Tool string `required:"yes" positional-arg-name:"<tool>" description:"internal tool name"`
	} `positional-args:"true"`
}

func (x *cmdDebugExecInternalTool) Execute(args []string) error {
	if x.Positionals.Tool == "" {
		return fmt.Errorf("tool name is missing")
	}

	tool, err := snapdtool.InternalToolPath(x.Positionals.Tool)
	if err != nil {
		return err
	}

	fmt.Fprintf(Stdout, "%s: %s\n", x.Positionals.Tool, tool)

	return nil
}
