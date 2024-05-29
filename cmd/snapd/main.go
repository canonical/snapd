// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sandbox"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/syscheck"
	"github.com/snapcore/snapd/systemd"
)

var (
	syscheckCheckSystem = syscheck.CheckSystem
)

func init() {
	err := logger.SimpleSetup(nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to activate logging: %s\n", err)
	}
}

func main() {
	// When preseeding re-exec is not used
	if snapdenv.Preseeding() {
		logger.Noticef("running for preseeding")
	} else {
		snapdtool.ExecInSnapdOrCoreSnap()
	}

	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	if err := run(ch); err != nil {
		if errors.Is(err, daemon.ErrRestartSocket) {
			// Note that we don't prepend: "error: " here because
			// ErrRestartSocket is not an error as such.
			fmt.Fprintf(os.Stdout, "%v\n", err)
			// the exit code must be in sync with
			// data/systemd/snapd.service.in:SuccessExitStatus=
			os.Exit(42)
		} else if errors.Is(err, daemon.ErrNoFailureRecoveryNeeded) {
			// Similar consideration as above.
			fmt.Fprintf(os.Stdout, "%v\n", err)
			// We were invoked from a failure handler, but there is
			// nothing to recover from in the state, as such the
			// failure handling was successful.
			return
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
	snapdenv.SetUserAgentFromVersion(snapdtool.Version, sandbox.ForceDevMode)

	d, err := daemon.New()
	if err != nil {
		return err
	}
	if err := d.Init(); err != nil {
		return err
	}

	// Run syscheck check now, if anything goes wrong with the
	// check we go into "degraded" mode where we always report
	// the given error to any snap client.
	var checkTicker <-chan time.Time
	var tic *time.Ticker
	if err := syscheckCheckSystem(); err != nil {
		degradedErr := fmt.Errorf("system does not fully support snapd: %s", err)
		logger.Noticef("%s", degradedErr)
		d.SetDegradedMode(degradedErr)
		tic = time.NewTicker(checkRunningConditionsRetryDelay)
		checkTicker = tic.C
	}

	d.Version = snapdtool.Version

	if err := d.Start(); err != nil {
		return err
	}

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
			if err := syscheckCheckSystem(); err == nil {
				d.SetDegradedMode(nil)
				tic.Stop()
			}
		}
	}

	return d.Stop(ch)
}
