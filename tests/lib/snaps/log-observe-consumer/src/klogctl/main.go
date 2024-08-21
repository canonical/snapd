// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

	"golang.org/x/sys/unix"
)

var cmdResolver = map[string]int{
	"SYSLOG_ACTION_CLOSE":         unix.SYSLOG_ACTION_CLOSE,
	"SYSLOG_ACTION_OPEN":          unix.SYSLOG_ACTION_OPEN,
	"SYSLOG_ACTION_READ":          unix.SYSLOG_ACTION_READ,
	"SYSLOG_ACTION_READ_ALL":      unix.SYSLOG_ACTION_READ_ALL,
	"SYSLOG_ACTION_READ_CLEAR":    unix.SYSLOG_ACTION_READ_CLEAR,
	"SYSLOG_ACTION_CLEAR":         unix.SYSLOG_ACTION_CLEAR,
	"SYSLOG_ACTION_CONSOLE_OFF":   unix.SYSLOG_ACTION_CONSOLE_OFF,
	"SYSLOG_ACTION_CONSOLE_ON":    unix.SYSLOG_ACTION_CONSOLE_ON,
	"SYSLOG_ACTION_CONSOLE_LEVEL": unix.SYSLOG_ACTION_CONSOLE_LEVEL,
	"SYSLOG_ACTION_SIZE_UNREAD":   unix.SYSLOG_ACTION_SIZE_UNREAD,
	"SYSLOG_ACTION_SIZE_BUFFER":   unix.SYSLOG_ACTION_SIZE_BUFFER,
	// A fake action for testing bad arguments
	"SYSLOG_ACTION_BAD": 99,
}

func main() {

	if len(os.Args) != 2 {
		panic("usage: klogctl SYSLOG_ACTION_*")
	}

	cmd := os.Args[1]

	cmdNo, ok := cmdResolver[cmd]
	if !ok {
		panic("klogctl: unknown SYSLOG_ACTION_* (see `man 2 syslog`)")
	}

	len := 64
	if cmdNo == unix.SYSLOG_ACTION_CONSOLE_LEVEL {
		// in this case, the len is actually the console level (the buf itself is ignored)
		len = 6
	}

	buf := make([]byte, len)

	// This is just what glibc and go call syslog(2) to avoid
	// confusion with syslog(3)
	_, err := unix.Klogctl(cmdNo, buf)

	if err == nil {
		fmt.Println("SUCCESS")
	} else {
		fmt.Printf("ERROR %s\n", err)
	}

	os.Exit(0)
}
