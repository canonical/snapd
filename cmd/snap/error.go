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
	"bytes"
	"fmt"
	"go/doc"
	"os"
	"os/user"
	"strings"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
)

var errorPrefix = i18n.G("error: %v\n")

func termSize() (width, height int) {
	if f, ok := Stdout.(*os.File); ok {
		width, height, _ = terminal.GetSize(int(f.Fd()))
	}

	if width <= 0 {
		width = int(osutil.GetenvInt64("COLUMNS"))
	}

	if height <= 0 {
		height = int(osutil.GetenvInt64("COLUMNS"))
	}

	if width < 40 {
		width = 80
	}

	if height < 15 {
		height = 25
	}

	return width, height
}

func fill(para string) string {
	width, _ := termSize()

	if width > 100 {
		width = 100
	}

	width--

	// 3 is the %v\n, which will be present in any locale
	indent := len(errorPrefix) - 3
	var buf bytes.Buffer
	doc.ToText(&buf, para, strings.Repeat(" ", indent), "", width)

	return strings.TrimSpace(buf.String())
}

func clientErrorToCmdMessage(snapName string, err *client.Error) (msg string, isError bool) {
	isError = true
	switch err.Kind {
	case client.ErrorKindSnapAlreadyInstalled:
		isError = false
		msg = i18n.G(`snap %q is already installed, see "snap refresh --help"`)
	case client.ErrorKindSnapNeedsDevMode:
		msg = i18n.G(`
The publisher of snap %q has indicated that they do not consider this revision
to be of production quality and that it is only meant for development or testing
at this point. As a consequence this snap will not refresh automatically and may
perform arbitrary system changes outside of the security sandbox snaps are
generally confined to, which may put your system at risk.

If you understand and want to proceed repeat the command including --devmode;
if instead you want to install the snap forcing it into strict confinement
repeat the command including --jailmode.`)
	case client.ErrorKindSnapNeedsClassic:
		msg = i18n.G(`
This revision of snap %q was published using classic confinement and thus may
perform arbitrary system changes outside of the security sandbox that snaps are
usually confined to, which may put your system at risk.

If you understand and want to proceed repeat the command including --classic.
`)
	case client.ErrorKindLoginRequired:
		u, _ := user.Current()
		if u != nil && u.Username == "root" {
			msg = fmt.Sprintf(i18n.G(`%s (see "snap login --help")`), err.Message)
		} else {
			msg = fmt.Sprintf(i18n.G(`%s (try with sudo)`), err.Message)
		}
		return fill(msg), true
	case client.ErrorKindSnapNotInstalled, client.ErrorKindNoUpdateAvailable:
		isError = false
		fallthrough
	default:
		return fill(err.Message), isError
	}

	return fill(fmt.Sprintf(msg, snapName)), isError
}
