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
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/selftest"
	"github.com/snapcore/snapd/systemd"
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

func runWatchdog(d *daemon.Daemon) (*time.Ticker, error) {
	// not running under systemd
	if os.Getenv("WATCHDOG_USEC") == "" {
		return nil, nil
	}
	usec := osutil.GetenvInt64("WATCHDOG_USEC")
	if usec == 0 {
		return nil, fmt.Errorf("cannot parse WATCHDOG_USEC: %q", os.Getenv("WATCHDOG_USEC"))
	}
	dur := time.Duration(usec/2) * time.Microsecond
	logger.Debugf("Setting up sd_notify() watchdog timer every %s", dur)
	wt := time.NewTicker(dur)

	go func() {
		for {
			select {
			case <-wt.C:
				// TODO: poke the snapd API here and
				//       only report WATCHDOG=1 if it
				//       replies with valid data
				systemd.SdNotify("WATCHDOG=1")
			case <-d.Dying():
				break
			}
		}
	}()

	return wt, nil
}

func run() error {
	t0 := time.Now().Truncate(time.Millisecond)
	httputil.SetUserAgentFromVersion(cmd.Version)

	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	d, err := daemon.New()
	if err != nil {
		return err
	}
	if err := d.Init(); err != nil {
		return err
	}

	// Run selftest now, if anything goes wrong with the selftest we go
	// into "degraded" mode where we always report the given error to
	// any snap client.
	if err := selftest.Run(); err != nil {
		degradedErr := fmt.Errorf("selftest failed: %s", err)
		logger.Noticef("%s", degradedErr)
		logger.Noticef("Entering degraded mode")
		d.DegradedMode(degradedErr)
	}

	d.Version = cmd.Version

	d.Start()

	watchdog, err := runWatchdog(d)
	if err != nil {
		return fmt.Errorf("cannot run software watchdog: %v", err)
	}
	if watchdog != nil {
		defer watchdog.Stop()
	}

	logger.Debugf("activation done in %v", time.Now().Truncate(time.Millisecond).Sub(t0))

	select {
	case sig := <-ch:
		logger.Noticef("Exiting on %s signal.\n", sig)
	case <-d.Dying():
		// something called Stop()
	}

	return d.Stop(ch)
}
