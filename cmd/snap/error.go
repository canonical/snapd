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

package main

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

func fill(para string) string {
	// lazy way of doing it
	buf := make([]byte, 0, len(para))
	scanner := bufio.NewScanner(strings.NewReader(para))
	scanner.Split(bufio.ScanWords)
	l := len("error: ")
	for scanner.Scan() {
		word := scanner.Bytes()
		l += len(word) + 1
		if l <= 72 {
			buf = append(buf, ' ')
		} else {
			l = len(word) + 1
			buf = append(buf, '\n')
		}
		buf = append(buf, word...)
	}

	return string(buf[1:])
}

func clientErrorToCmdMessage(snapName string, err *client.Error) (msg string, isError bool) {
	switch err.Kind {
	case client.ErrorKindSnapAlreadyInstalled:
		return fmt.Sprintf(i18n.G(`snap %q is already installed, see "snap refresh --help"`), snapName), false
	case client.ErrorKindSnapNeedsMode:
		switch err.Value {
		case client.DevModeConfinement:
			msg = i18n.G(`
the developer of snap %q has indicated that they do not consider it to
be of production quality, and is only meant for developers or user
testing at this point.  Installing this snap from an untrusted developer
can put your system at risk.  What's more this developer-mode snap once
installed will need to be manually refreshed.  If all of the above is
agreeable to you please repeat this command with the --devmode option
added.
`)

		case client.ClassicConfinement:
			msg = i18n.G(`
snap %q is a "classic" snap, which means it runs outside of the sandbox
more strict snaps run in.  As this could put your system at risk (like
traditional .deb or .rpm packages could), you must explicitly request
this on installation, by using this same command with the added
--classic option.  After that the snap will be refreshed automatically.
`)
		default:
			msg = "%q: " + err.Message
		}
		return fill(fmt.Sprintf(msg, snapName)), true
	case client.ErrorKindSnapNotInstalled, client.ErrorKindNoUpdateAvailable:
		return err.Message, false
	default:
		return err.Message, true
	}
}
