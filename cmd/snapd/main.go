// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"os/signal"
	"syscall"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/cmd"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/daemon/user"
	"github.com/snapcore/snapd/errtracker"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
)

func init() {
	err := logger.SimpleSetup()
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to activate logging: %s\n", err)
	}
	// set here to avoid accidental submits in e.g. unit tests
	errtracker.CrashDbURLBase = "https://daisy.ubuntu.com/"
	errtracker.SnapdVersion = cmd.Version
}

// commandline args
var opts struct {
	User bool `long:"user" description:"Start the user session instance of snapd"`
}

// Daemon describes all operations necessary to deal with a daemon instance
type Daemon interface {
	Init() error
	Start()
	Stop() error
	Dying() <-chan struct{}
	SetVersion(version string)
}

func main() {
	cmd.ExecInCoreSnap()

	parser := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash|flags.PassAfterNonOption)
	if _, err := parser.ParseArgs(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	var d Daemon
	var err error
	if opts.User {
		d, err = user.NewDaemon()
	} else {
		httputil.SetUserAgentFromVersion(cmd.Version)
		d, err = daemon.New()
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if err := run(d); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(d Daemon) error {
	if err := d.Init(); err != nil {
		return err
	}
	d.SetVersion(cmd.Version)

	d.Start()

	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-ch:
		logger.Noticef("Exiting on %s signal.\n", sig)
	case <-d.Dying():
		// something called Stop()
	}

	return d.Stop()
}
