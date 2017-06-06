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
	"strings"
)

// SdNotify sends the given state string notification to systemd.
//
// inspired by libsystemd/sd-daemon/sd-daemon.c from the systemd source
func SdNotify(notifyState string) error {
	if notifyState == "" {
		return fmt.Errorf("cannot use empty notify state")
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

	_, err = conn.Write([]byte(notifyState))
	return err
}
