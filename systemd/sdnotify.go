// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package systemd

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/snapcore/snapd/logger"
)

func RunWatchdog() (*time.Ticker, error) {
	wu := os.Getenv("WATCHDOG_USEC")
	if wu == "" {
		return nil, fmt.Errorf("cannot get WATCHDOG_USEC environment")
	}
	usec, err := strconv.Atoi(wu)
	if err != nil {
		return nil, fmt.Errorf("cannot parse WATCHDOG_USEC: %s", err)
	}
	dur := time.Duration(usec/2) * time.Microsecond
	logger.Debugf("Setting up sd_notify() watchdog timer every %s", dur)
	wt := time.NewTicker(dur)

	go func() {
		for {
			select {
			case <-wt.C:
				sdNotify("WATCHDOG=1")
			}
		}
	}()

	return wt, nil
}

func sdNotify(state string) error {
	if state == "" {
		return fmt.Errorf("cannot use empty state")
	}
	e := os.Getenv("NOTIFY_SOCKET")
	if e == "" {
		return fmt.Errorf("cannot find NOTIFY_SOCKET environment")
	}
	if !strings.HasPrefix(e, "@") && !strings.HasPrefix(e, "/") {
		return fmt.Errorf("cannot use NOTIFY_SOCKET %q", e)
	}

	raddr := &net.UnixAddr{
		Name: e,
		Net:  "unixgram",
	}
	conn, err := net.DialUnix("unixgram", nil, raddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = conn.Write([]byte(state))
	return err
}
