// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package ctlcmd

import (
	"fmt"
	"regexp"
	"time"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/healthstate"
)

var (
	shortHealthHelp = i18n.G("Report on snap's health")
	longHealthHelp  = i18n.G(`
The set-health command can signal to the system and the user that something is
not well with the snap.

Note the health is of the snap, not of the apps it contains; it’s up to the
snap developer to determine how the health of the individual apps add up to
the health of the snap as a whole.

status can be one of

- okay: the snap is healthy. This status takes no message and no code.

- waiting: some resource (e.g. a device, network, or service) the snap needs
  isn’t ready yet; the user just needs to wait.  The message (and optionally
  the code) must explain what it’s waiting for.

- blocked: the user needs to do something for the snap to do something; the
  message (and optionally the code) must say what it is needs doing.

- error: something is broken; the message (and optionally the code) must
  explain what.
`)
)

func init() {
	addCommand("set-health", shortHealthHelp, longHealthHelp, func() command { return &healthCommand{} })
}

type healthPositional struct {
	Status  string `positional-arg-name:"<status>" required:"yes" description:"a valid health status; required."`
	Message string `positional-arg-name:"<message>" description:"a short human-readable explanation of the status (when not okay). Must be longer than 7 characters, and will be truncated if over 70. Message cannot be provided if status is okay, and is required otherwise."`
}

type healthCommand struct {
	baseCommand
	healthPositional `positional-args:"yes"`
	Code             string `long:"code" value-name:"<code>" description:"optional tool-friendly value representing the problem that makes the snap unhealthy.  Not a number, but a word with 3-30 bytes matching [a-z](?:-?[a-z0-9]){2,}"`
}

var (
	validCode = regexp.MustCompile(`^[a-z](?:-?[a-z0-9]){2,}$`).MatchString
)

func (c *healthCommand) Execute([]string) error {
	if c.Status == "okay" && (len(c.Message) > 0 || len(c.Code) > 0) {
		return fmt.Errorf(`when status is "okay", message and code should be empty`)
	}

	status, err := healthstate.StatusLookup(c.Status)
	if err != nil {
		return err
	}
	if status == healthstate.UnknownStatus {
		return fmt.Errorf(`status cannot be manually set to "unknown"`)
	}

	if len(c.Code) > 0 {
		if len(c.Code) < 3 || len(c.Code) > 30 {
			return fmt.Errorf("error code should have between 3 and 30 bytes, got %d", len(c.Code))
		}
		if !validCode(c.Code) {
			return fmt.Errorf("invalid error code %q", c.Code)
		}
	}

	if status != healthstate.OkayStatus {
		if len(c.Message) == 0 {
			return fmt.Errorf(`when status is not "okay", message is required`)
		}

		rmsg := []rune(c.Message)
		if len(rmsg) < 7 {
			return fmt.Errorf(`message must be at least 7 characters long (%q has %d)`, c.Message, len(rmsg))
		}
		if len(rmsg) > 70 {
			c.Message = string(rmsg[:69]) + "…"
		}
	}

	ctx := c.context()
	if ctx == nil {
		// reuses the i18n'ed error message from service ctl
		return fmt.Errorf(i18n.G("cannot %s without a context"), "set-health")
	}
	ctx.Lock()
	defer ctx.Unlock()

	health := &healthstate.HealthState{
		Revision:  ctx.SnapRevision(), // will be "unset" for unasserted installs, and trys
		Timestamp: time.Now(),
		Status:    status,
		Message:   c.Message,
		Code:      c.Code,
	}

	ctx.Set("health", health)
	return nil
}
