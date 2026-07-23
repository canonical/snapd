// -*- Mode: Go; indent-tabs-mode: t -*-

//go:build linux

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/cmd/snapd/cli"
	"github.com/snapcore/snapd/cmd/snapd/daemon"
	"github.com/snapcore/snapd/cmd/snapd/tool/snap-gpio-helper"
	"github.com/snapcore/snapd/cmd/snapd/tool/snap-preseed"
	"github.com/snapcore/snapd/cmd/snapd/tool/snapd-apparmor"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/strutil"
)

var (
	// toolMains maps tool names (as passed in argv[1] by snapd-tool-wrap) to the
	// corresponding Main() entry point.
	toolMains = map[string]func(){
		"snap-preseed":     snap_preseed.Main,
		"snapd-apparmor":   snapd_apparmor.Main,
		"snap-gpio-helper": snap_gpio_helper.Main,
	}

	// list of snapd tools that expect reexec
	//
	// could use a simple list instead of adding to a map to avoid exporting
	// types to the test suite
	reexecTools = []string{
		"snapd-apparmor",
		"snap-gpio-helper",
	}

	daemonMain = daemon.Main
	cliMain    = cli.Main
)

func main() {
	argv0 := filepath.Base(os.Args[0])

	// TODO: unify reexec call so that we have a single entrypoint for snapd,
	// snapd tools and snap

	switch argv0 {
	case "snapd":
		// Tool dispatch: the C wrapper (snapd-tool-wrap) sets
		// argv[0]="snapd" and argv[1]=<tool-name>. Check argv[1]
		// for known tool names before falling through to the daemon.
		if len(os.Args) > 1 {
			toolName := os.Args[1]
			if toolMain, ok := toolMains[toolName]; ok {
				runTool(toolName, toolMain, os.Args)
				return
			}
		}
		// no tool invoked, proceed to executing snapd daemon main()
		daemonMain()
	default:
		cliMain()
	}
}

func runTool(toolName string, toolMain func(), fullArgv []string) {
	// setup logger so that we have logs from reexec, the tool can install a new
	// logger by reinitializing it.
	logger.SimpleSetup(nil)

	if strutil.ListContains(reexecTools, toolName) {
		// reexec, if enabled, must be applied **before** any args modifications
		snapdtool.ExecInSnapdOrCoreSnap()
	}

	// Strip argv[1] (the tool name) so the tool sees its own args.
	os.Args = append([]string{toolName}, fullArgv[2:]...)

	toolMain()
}
