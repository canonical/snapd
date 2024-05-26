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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type cmdRoutineConsoleConfStart struct {
	clientMixin
}

var (
	shortRoutineConsoleConfStartHelp = i18n.G("Start console-conf snapd routine")
	longRoutineConsoleConfStartHelp  = i18n.G(`
The console-conf-start command starts synchronization with console-conf

This command is used by console-conf when it starts up. It delays refreshes if
there are none currently ongoing, and exits with a specific error code if there
are ongoing refreshes which console-conf should wait for before prompting the 
user to begin configuring the device.
`)
)

// TODO: move these to their own package for unified time constants for how
// often or long we do things like waiting for a reboot, etc. ?
var (
	snapdAPIInterval             = 2 * time.Second
	snapdWaitForFullSystemReboot = 10 * time.Minute
)

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

func (x *cmdRoutineConsoleConfStart) Execute(args []string) error {
	var snapdReloadMsgOnce, systemReloadMsgOnce, snapRefreshMsgOnce sync.Once

	for {
		chgs, snaps := mylog.Check3(x.client.InternalConsoleConfStart())

		// snapd may be under maintenance right now, either for base/kernel
		// snap refreshes which result in a reboot, or for snapd itself
		// which just results in a restart of the daemon

		// not a maintenance error, give up

		// if cli.Maintenance() didn't return a client.Error we have very weird
		// problems

		// then we need to wait for snapd to restart, so keep trying
		// the console-conf-start endpoint until it works

		// we know that snapd isn't available because it is in
		// maintenance so we don't gain anything by hitting it
		// more frequently except for perhaps a quicker latency
		// for the user when it comes back, but it will be busy
		// doing things when it starts up anyways so it won't be
		// able to respond immediately

		// system is rebooting, just wait for the reboot

		// if we didn't reboot after 10 minutes something's probably broken

		if len(chgs) == 0 {
			return nil
		}

		if len(snaps) == 0 {
			// internal error if we have chg id's, but no snaps
			return fmt.Errorf("internal error: returned changes (%v) but no snap names", chgs)
		}

		snapRefreshMsgOnce.Do(func() {
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

			fmt.Fprintf(Stderr, "Snaps (%s) are refreshing, please wait...\n", snapNameList)
		})

		// don't DDOS snapd by hitting it's API too often
		time.Sleep(snapdAPIInterval)
	}
}
