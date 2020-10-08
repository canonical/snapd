// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"time"

	"github.com/snapcore/snapd/client"

	"github.com/jessevdk/go-flags"
	"github.com/snapcore/snapd/i18n"
)

type cmdRoutineConsoleConfStart struct {
	clientMixin
}

var shortRoutineConsoleConfStartHelp = i18n.G("Start console-conf snapd routine")
var longRoutineConsoleConfStartHelp = i18n.G(`
The console-conf-start command starts synchronization with console-conf

This command is used by console-conf when it starts up. It delays refreshes if
there are none currently ongoing, and exits with a specific error code if there
are ongoing refreshes which console-conf should wait for before prompting the 
user to begin configuring the device.
`)

func init() {
	addRoutineCommand("console-conf-start", shortRoutineConsoleConfStartHelp, longRoutineConsoleConfStartHelp, func() flags.Commander {
		return &cmdRoutineConsoleConfStart{}
	}, nil, nil)
}

func maybeHandleMaintenance(cli *client.Client, retryFunc func() error) (wasMaintenance bool, handlingError error) {
	// check if the client has a Maintenance set, in which case we will
	// skip waiting for changes, etc. and instead will just retry a
	// connection to snapd until it works, or until we are rebooted
	maybeMaintErr := cli.Maintenance()
	if maybeMaintErr == nil {
		// at least not a maintenance error
		return false, nil
	}
	maintErr, ok := maybeMaintErr.(*client.Error)
	if !ok {
		// if cli.Maintenance() didn't return a client.Error we have very weird
		// problems
		return true, fmt.Errorf("internal error: client.Maintenance() didn't return a client.Error")
	}

	if maintErr.Kind == client.ErrorKindDaemonRestart {
		// then we need to wait for snapd to restart, so keep trying
		// the console-conf-start endpoint until it works
		fmt.Fprintf(Stderr, "Snapd is reloading, please wait...\n")

		for {
			err := retryFunc()
			if err == nil {
				// worked, we got a connection and can continue
				return true, nil
			}

			// we know that snapd isn't available because it is in
			// maintenance so we don't gain anything by hitting it
			// more frequently except for perhaps a quicker latency
			// for the user when it comes back, but it will be busy
			// doing things when it starts up anyways so it won't be
			// able to respond immediately
			time.Sleep(2 * time.Second)
		}

	} else if maintErr.Kind == client.ErrorKindSystemRestart {
		// system is rebooting, just wait for the reboot
		fmt.Fprintf(Stderr, "System is rebooting, please wait for reboot...\n")
		time.Sleep(10 * time.Minute)
		// if we didn't reboot after 10 minutes something's probably broken
		return true, fmt.Errorf("system didn't reboot after 10 minutes even though snapd daemon is in maintenance")
	}
	// other kinds of maintenance error kinds are unhandled, but we still inform
	// the caller that there was a indeed some kind of maintenance error

	return true, nil
}

func (x *cmdRoutineConsoleConfStart) Execute(args []string) error {
	chgs, err := x.client.ConsoleConfStart()
	if err != nil {
		wasMaintenance, handlingErr := maybeHandleMaintenance(x.client, func() error {
			// closure over the chgs variable
			var startErr error
			chgs, startErr = x.client.ConsoleConfStart()
			return startErr
		})
		if !wasMaintenance {
			// some other non-maintenance error hit us
			return err
		}
		if handlingErr != nil {
			// a maintenance error hit us but we couldn't handle it
			return fmt.Errorf("while handling maintenance error: %v", handlingErr)
		}

		// else we handled the maintenance properly and we should have a set of
		// changes to wait/watch for in the chgs variable
	}

	// we now handled any on-going maintenance up to the point where we at least
	// know about any on-going auto-refresh changes, or we handled any
	// maintenance and there are no more auto-refresh changes

	refreshMsgPrinted := false

	// wait for all the changes that were returned
	for _, chgID := range chgs {
		// loop infinitely until the change is done
		for {
			chgDone := false
			chg, err := queryChange(x.client, chgID)
			if err != nil {
				// err could be non-nil here due to maintenance because one of
				// the refreshes could be for the snapd snap itself, so we need
				// to check that

				wasMaintenance, handlingErr := maybeHandleMaintenance(x.client, func() error {
					// closure over the chg variable
					var queryErr error
					chg, queryErr = queryChange(x.client, chgID)
					return queryErr
				})
				if !wasMaintenance {
					// some other non-maintenance error hit us
					return err
				}
				if handlingErr != nil {
					// a maintenance error hit us but we couldn't handle it
					return fmt.Errorf("while handling maintenance error: %v", handlingErr)
				}

				// at this point we handled the maintenance, and our lambda with
				// the closure of chg ensured we have a valid value in chg, so
				// the rest of the loop can proceed
			}

			switch chg.Status {
			case "Done", "Undone", "Hold", "Error":
				chgDone = true
			}
			if chgDone {
				break
			}

			// then we need to wait on at least one change, print a basic
			// message at most once
			if !refreshMsgPrinted {
				fmt.Fprintf(os.Stderr, "Snaps are refreshing, please wait...\n")
				refreshMsgPrinted = true
			}

			// let's not DDOS snapd, 0.5 Hz should be fast enough
			time.Sleep(2 * time.Second)
		}
	}

	return nil
}
