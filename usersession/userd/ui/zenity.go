// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package ui

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/ddkwork/golibrary/mylog"
)

// Zenity provides a zenity based UI interface
type Zenity struct{}

// YesNo asks a yes/no question using zenity
func (*Zenity) YesNo(primary, secondary string, options *DialogOptions) bool {
	if options == nil {
		options = &DialogOptions{}
	}

	txt := fmt.Sprintf("<big><b>%s</b></big>\n\n%s", primary, secondary)
	if options.Footer != "" {
		txt += fmt.Sprintf("\n\n<span size=\"x-small\">%s</span>", options.Footer)
	}
	args := []string{"--question", "--modal", "--text=" + txt}
	if options.Timeout > 0 {
		args = append(args, fmt.Sprintf("--timeout=%d", int(options.Timeout/time.Second)))
	}
	// Gtk is not doing a good job with long labels. It will
	// create a very narrow and long window (minimal width to fit
	// the buttons).  So force a bigger width here. See also LP:
	// 1778484. The primary number is lower because the header is
	// in a bigger font.
	if len(primary) > 10 || len(secondary) > 20 {
		args = append(args, "--width=500")
	}

	cmd := exec.Command("zenity", args...)
	mylog.Check(cmd.Start())
	mylog.Check(cmd.Wait())

	return true
}
