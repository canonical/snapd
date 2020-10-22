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
	"sort"
	"strings"
	"sync"
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
	c := addRoutineCommand("console-conf-start", shortRoutineConsoleConfStartHelp, longRoutineConsoleConfStartHelp, func() flags.Commander {
		return &cmdRoutineConsoleConfStart{}
	}, nil, nil)
	c.hidden = true
}

func printfFunc(msg string, format ...interface{}) func() {
	return func() {
		fmt.Fprintf(Stderr, msg, format...)
	}
}

var (
	snapdReloadMsgDoer  = sync.Once{}
	systemReloadMsgDoer = sync.Once{}
	snapRefreshMsgDoer  = sync.Once{}
)

func (x *cmdRoutineConsoleConfStart) Execute(args []string) error {
	for {
		chgs, snaps, err := x.client.InternalConsoleConfStart()
		if err != nil {
			// snapd may be under maintenance right now, either for base/kernel
			// snap refreshes which result in a reboot, or for snapd itself
			// which just results in a restart of the daemon
			maybeMaintErr := x.client.Maintenance()
			if maybeMaintErr == nil {
				// not a maintenance error, give up
				return err
			}

			maintErr, ok := maybeMaintErr.(*client.Error)
			if !ok {
				// if cli.Maintenance() didn't return a client.Error we have very weird
				// problems
				return fmt.Errorf("internal error: client.Maintenance() didn't return a client.Error")
			}

			if maintErr.Kind == client.ErrorKindDaemonRestart {
				// then we need to wait for snapd to restart, so keep trying
				// the console-conf-start endpoint until it works
				snapdReloadMsgDoer.Do(printfFunc("Snapd is reloading, please wait...\n"))

				// we know that snapd isn't available because it is in
				// maintenance so we don't gain anything by hitting it
				// more frequently except for perhaps a quicker latency
				// for the user when it comes back, but it will be busy
				// doing things when it starts up anyways so it won't be
				// able to respond immediately
				time.Sleep(2 * time.Second)
				continue
			} else if maintErr.Kind == client.ErrorKindSystemRestart {
				// system is rebooting, just wait for the reboot
				systemReloadMsgDoer.Do(printfFunc("System is rebooting, please wait for reboot...\n"))
				time.Sleep(10 * time.Minute)
				// if we didn't reboot after 10 minutes something's probably broken
				return fmt.Errorf("system didn't reboot after 10 minutes even though snapd daemon is in maintenance")
			}
		}

		if len(chgs) == 0 {
			break
		}

		if len(snaps) == 0 {
			// internal error if we have chg id's, but no snaps
			return fmt.Errorf("internal error: returned changes (%v) but no snap names", chgs)
		}

		snapRefreshMsgDoer.Do(func() {
			sort.Strings(snaps)

			var snapNameList string
			switch len(snaps) {
			case 1:
				snapNameList = snaps[0]
			case 2:
				snapNameList = fmt.Sprintf("%s and %s", snaps[0], snaps[1])
			default:
				// don't forget the oxford comma!
				snapNameList = fmt.Sprintf("%s, and %s", strings.Join(snaps[:len(snaps)-1], ", "), snaps[len(snaps)-1])
			}

			fmt.Fprintf(os.Stderr, "Snaps (%s) are refreshing, please wait...\n", snapNameList)
		})

		// let's not DDOS snapd, 0.5 Hz should be fast enough
		time.Sleep(2 * time.Second)
	}

	return nil
}
