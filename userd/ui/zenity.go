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

package ui

import (
	"fmt"
	"os/exec"
	"time"
)

type Zenity struct{}

func (*Zenity) YesNo(primary, secondary string, options *Options) bool {
	if options == nil {
		options = &Options{}
	}

	txt := fmt.Sprintf(`<big><b>%s</b></big>

%s`, primary, secondary)
	if options.Footer != "" {
		txt += fmt.Sprintf(`\n\n<span size="x-small">%s</span>`, options.Footer)
	}
	cmd := exec.Command("zenity", "--question", "--modal", "--text="+txt)
	if err := cmd.Start(); err != nil {
		return false
	}

	var err error
	if options.Timeout > 0 {
		done := make(chan error)
		go func() { done <- cmd.Wait() }()
		select {
		case err = <-done:
			// normal exit
		case <-time.After(options.Timeout * time.Second):
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
