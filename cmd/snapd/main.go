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
	"time"

	"github.com/snapcore/snapd/cmd"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/errtracker"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/selftest"
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

func main() {
	cmd.ExecInCoreSnap()
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	t0 := time.Now().Truncate(time.Millisecond)
	httputil.SetUserAgentFromVersion(cmd.Version)

	if err := selftest.Run(); err != nil {
		return fmt.Errorf("cannot start snapd: %v", err)
	}

	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	d, err := daemon.New()
	if err != nil {
		return err
	}
	if err := d.Init(); err != nil {
		return err
	}
	d.Version = cmd.Version

	d.Start()

	logger.Debugf("activation done in %v", time.Now().Truncate(time.Millisecond).Sub(t0))

	select {
	case sig := <-ch:
		logger.Noticef("Exiting on %s signal.\n", sig)
	case <-d.Dying():
		// something called Stop()
	}

	return d.Stop()
}
