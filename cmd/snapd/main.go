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
	"runtime"
	"syscall"
	"time"

	"github.com/snapcore/snapd/cmd"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/errtracker"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sanity"
	"github.com/snapcore/snapd/systemd"
)

var (
	sanityCheck = sanity.Check
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
	cmd.ExecInSnapdOrCoreSnap()

	// The Go scheduler by default has a single operating system
	// thread per processor core, and does its own goroutine
	// scheduling inside of that thread.  For I/O operations that
	// the Go runtime knows about, it has mechanisms to reschedule
	// goroutines so the system thread isn't blocked waiting for
	// I/O.  If a goroutine performs a blocking system call which
	// the go runtime doesn't have special optimizations for, the
	// system thread can become blocked waiting for the syscall.
	// This can dramatically reduce runtime performance, and the
	// problem is much worse on single processor systems because
	// there is normally only a single system thread.
	//
	// We workaround by increasing the number of procs to a
	// minimum of two.
	if runtime.GOMAXPROCS(-1) == 1 {
		runtime.GOMAXPROCS(2)
	}

	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	if err := run(ch); err != nil {
		if err == daemon.ErrRestartSocket {
			// Note that we don't prepend: "error: " here because
			// ErrRestartSocket is not an error as such.
			fmt.Fprintf(os.Stdout, "%v\n", err)
			// the exit code must be in sync with
			// data/systemd/snapd.service.in:SuccessExitStatus=
			os.Exit(42)
		}
		fmt.Fprintf(os.Stderr, "cannot run daemon: %v\n", err)
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
				return
			}
		}
	}()

	return wt, nil
}

var checkRunningConditionsRetryDelay = 300 * time.Second

func run(ch chan os.Signal) error {
	t0 := time.Now().Truncate(time.Millisecond)
	httputil.SetUserAgentFromVersion(cmd.Version)

	d, err := daemon.New()
	if err != nil {
		return err
	}
	if err := d.Init(); err != nil {
		return err
	}

	// Run sanity check now, if anything goes wrong with the
	// check we go into "degraded" mode where we always report
	// the given error to any snap client.
	var checkTicker <-chan time.Time
	var tic *time.Ticker
	if err := sanityCheck(); err != nil {
		degradedErr := fmt.Errorf("system does not fully support snapd: %s", err)
		logger.Noticef("%s", degradedErr)
		d.SetDegradedMode(degradedErr)
		tic = time.NewTicker(checkRunningConditionsRetryDelay)
		checkTicker = tic.C
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

out:
	for {
		select {
		case sig := <-ch:
			logger.Noticef("Exiting on %s signal.\n", sig)
			break out
		case <-d.Dying():
			// something called Stop()
			break out
		case <-checkTicker:
			if err := sanityCheck(); err == nil {
				d.SetDegradedMode(nil)
				tic.Stop()
			}
		}
	}

	return d.Stop(ch)
}
