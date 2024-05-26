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
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/healthstate"
	"github.com/snapcore/snapd/overlord/state"
)

var (
	shortHealthHelp = i18n.G("Report the health status of a snap")
	longHealthHelp  = i18n.G(`
The set-health command is called from within a snap to inform the system of the
snap's overall health.

It can be called from any hook, and even from the apps themselves. A snap can
optionally provide a 'check-health' hook to better manage these calls, which is
then called periodically and with increased frequency while the snap is
"unhealthy". Any health regression will issue a warning to the user.

Note: the health is of the snap only, not of the apps it contains; it’s up to
      the snap developer to determine how the health of the individual apps is
      reflected in the overall health of the snap.

status can be one of:

- okay: the snap is healthy. This status takes no message and no code.

- waiting: a resource needed by the snap (e.g. a device, network, or service) is
  not ready and the user will need to wait.  The message must explain what
  resource is being waited for.

- blocked: something needs doing to unblock the snap (e.g. a service needs to be
  configured); the message must be sufficient to point the user in the right
  direction.

- error: something is broken; the message must explain what.
`)
)

func init() {
	addCommand("set-health", shortHealthHelp, longHealthHelp, func() command { return &healthCommand{} })
}

type healthPositional struct {
	Status  string `positional-arg-name:"<status>" required:"yes" description:"a valid health status; required."`
	Message string `positional-arg-name:"<message>" description:"a short human-readable explanation of the status (when not okay). Must be longer than 7 characters, and will be truncated if over 70. Message cannot be provided if status is okay, but is required otherwise."`
}

type healthCommand struct {
	baseCommand
	healthPositional `positional-args:"yes"`
	Code             string `long:"code" value-name:"<code>" description:"optional tool-friendly value representing the problem that makes the snap unhealthy.  Not a number, but a word with 3-30 characters matching [a-z](-?[a-z0-9])+"`
}

var validCode = regexp.MustCompile(`^[a-z](?:-?[a-z0-9])+$`).MatchString

func (c *healthCommand) Execute([]string) error {
	if c.Status == "okay" && (len(c.Message) > 0 || len(c.Code) > 0) {
		return fmt.Errorf(`when status is "okay", message and code must be empty`)
	}

	status := mylog.Check2(healthstate.StatusLookup(c.Status))

	if status == healthstate.UnknownStatus {
		return fmt.Errorf(`status cannot be manually set to "unknown"`)
	}

	if len(c.Code) > 0 {
		if len(c.Code) < 3 || len(c.Code) > 30 {
			return fmt.Errorf("code must have between 3 and 30 characters, got %d", len(c.Code))
		}
		if !validCode(c.Code) {
			return fmt.Errorf("invalid code %q (code must start with lowercase ASCII letters, and contain only ASCII letters and numbers, optionally separated by single dashes)", c.Code) // technically not dashes but hyphen-minuses
		}
	}

	if status != healthstate.OkayStatus {
		if len(c.Message) == 0 {
			return fmt.Errorf(`when status is not "okay", message is required`)
		}

		rmsg := []rune(c.Message)
		if len(rmsg) < 7 {
			return fmt.Errorf(`message must be at least 7 characters long (got %d)`, len(rmsg))
		}
		if len(rmsg) > 70 {
			c.Message = string(rmsg[:69]) + "…"
		}
	}

	ctx := mylog.Check2(c.ensureContext())

	ctx.Lock()
	defer ctx.Unlock()

	var v struct{}

	// if 'health' is there we've either already added an OnDone (and the
	// following Set("health"), or we're in the set-health hook itself
	// (which sets it to a fake entry for this purpose).
	if mylog.Check(ctx.Get("health", &v)); errors.Is(err, state.ErrNoState) {
		ctx.OnDone(func() error {
			return healthstate.SetFromHookContext(ctx)
		})
	}

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
