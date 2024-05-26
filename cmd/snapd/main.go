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
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sandbox"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/syscheck"
	"github.com/snapcore/snapd/systemd"
)

var syscheckCheckSystem = syscheck.CheckSystem

func init() {
	mylog.Check(logger.SimpleSetup(nil))
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
	mylog.Check(run(ch))

	// Note that we don't prepend: "error: " here because
	// ErrRestartSocket is not an error as such.

	// the exit code must be in sync with
	// data/systemd/snapd.service.in:SuccessExitStatus=
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

	d := mylog.Check2(daemon.New())
	mylog.Check(d.Init())

	// Run syscheck check now, if anything goes wrong with the
	// check we go into "degraded" mode where we always report
	// the given error to any snap client.
	var checkTicker <-chan time.Time
	var tic *time.Ticker
	mylog.Check(syscheckCheckSystem())

	d.Version = snapdtool.Version
	mylog.Check(d.Start())

	watchdog := mylog.Check2(runWatchdog(d))

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
			if mylog.Check(syscheckCheckSystem()); err == nil {
				d.SetDegradedMode(nil)
				tic.Stop()
			}
		}
	}

	return d.Stop(ch)
}
