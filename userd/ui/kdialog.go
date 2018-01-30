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
	"html"
	"os/exec"
	"time"
)

// Kdialog provides a kdialog based UI interface
type Kdialog struct{}

// YesNo asks a yes/no question using kdialog
func (*Kdialog) YesNo(primary, secondary string, options *Options) bool {
	if options == nil {
		options = &Options{}
	}

	txt := fmt.Sprintf(`<p><big><b>%s</b></big></p><p>%s</p>`, html.EscapeString(primary), html.EscapeString(secondary))
	if options.Footer != "" {
		txt += fmt.Sprintf(`<p><small>%s</small></p>`, html.EscapeString(options.Footer))
	}
	cmd := exec.Command("kdialog", "--yesno="+txt)
	if err := cmd.Start(); err != nil {
		return false
	}

	var err error
	if options.Timeout > 0 {
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err = <-done:
			// normal exit
		case <-time.After(time.Duration(options.Timeout) * time.Second):
			// timeout do nothing, the other side will have timed
			// out as well, no need to send a reply.
			cmd.Process.Kill()
			return false
		}
	} else {
		err = cmd.Wait()
	}
	if err == nil {
		return true
	}

	return false
}
