// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package cli_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snapd/cli"
	"github.com/snapcore/snapd/systemd/logind"
)

func (s *SnapSuite) TestRoutineUserServicePreconditionNonGreeter(c *C) {
	// All non-greeter session classes should exit 0
	for _, class := range []string{
		"user",
		"user-early",
		"user-incomplete",
		"user-light",
		"user-early-light",
		"lock-screen",
		"background",
		"background-light",
		"manager",
		"manager-early",
		"none",
	} {
		restore := snap.MockLogindSessionClass(func(ctx context.Context) (string, error) {
			return class, nil
		})
		_, err := snap.Parser(snap.Client()).ParseArgs([]string{"routine", "user-service-precondition"})
		c.Assert(err, IsNil, Commentf("class %q", class))
		c.Check(s.Stdout(), Equals, "", Commentf("class %q", class))
		c.Check(s.Stderr(), Equals, "", Commentf("class %q", class))
		restore()
		s.ResetStdStreams()
	}
}

func (s *SnapSuite) TestRoutineUserServicePreconditionGreeter(c *C) {
	restore := snap.MockLogindSessionClass(func(ctx context.Context) (string, error) {
		return "greeter", nil
	})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"routine", "user-service-precondition"})
	c.Assert(err, NotNil)
	c.Check(snap.ExitCodeFromError(err), Equals, 1)
	c.Check(err.Error(), Equals, "session is a greeter session")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestRoutineUserServicePreconditionGreeterWithErrorExitCode(c *C) {
	restore := snap.MockLogindSessionClass(func(ctx context.Context) (string, error) {
		return "greeter", nil
	})
	defer restore()

	for _, exitCode := range []int{1, 2, 3, 254} {
		_, err := snap.Parser(snap.Client()).ParseArgs([]string{"routine", "user-service-precondition", "--error-exit-code", fmt.Sprintf("%d", exitCode)})
		c.Assert(err, NotNil)
		c.Check(snap.ExitCodeFromError(err), Equals, exitCode)
		c.Check(err.Error(), Equals, "session is a greeter session")
		c.Check(s.Stderr(), Equals, "")
	}
}

func (s *SnapSuite) TestRoutineUserServicePreconditionGreeterInvalidErrorExitCode(c *C) {
	restore := snap.MockLogindSessionClass(func(ctx context.Context) (string, error) {
		return "user", nil // shouldn't matter
	})
	defer restore()

	for _, exitCode := range []string{"-1", "0", "255", "256"} {
		_, err := snap.Parser(snap.Client()).ParseArgs([]string{"routine", "user-service-precondition", "--error-exit-code", exitCode})
		c.Assert(err, NotNil)
		c.Check(snap.ExitCodeFromError(err), Equals, 2)
		c.Check(err.Error(), Equals, "invalid --error-exit-code: must be in range 1-254")
		c.Check(s.Stderr(), Equals, "")
	}
}

func (s *SnapSuite) TestRoutineUserServicePreconditionNoSession(c *C) {
	restoreSleep := snap.MockTimeSleep(func(time.Duration) {})
	defer restoreSleep()

	restore := snap.MockLogindSessionClass(func(ctx context.Context) (string, error) {
		return "", fmt.Errorf("cannot find display-eligible session for the current user: 1000")
	})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"routine", "user-service-precondition"})
	c.Assert(err, NotNil)
	c.Check(snap.ExitCodeFromError(err), Equals, 1)
	c.Check(err.Error(), Equals, "cannot determine session class: cannot find display-eligible session for the current user: 1000")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestRoutineUserServicePreconditionNoSessionWithErrorExitCode(c *C) {
	restoreSleep := snap.MockTimeSleep(func(time.Duration) {})
	defer restoreSleep()

	restore := snap.MockLogindSessionClass(func(ctx context.Context) (string, error) {
		return "", fmt.Errorf("cannot find display-eligible session for the current user: 1000")
	})
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"routine", "user-service-precondition", "--error-exit-code", "3"})
	c.Assert(err, NotNil)
	c.Check(snap.ExitCodeFromError(err), Equals, 3)
	c.Check(err.Error(), Equals, "cannot determine session class: cannot find display-eligible session for the current user: 1000")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestRoutineUserServicePreconditionRetriesNoDisplayEligibleSession(c *C) {
	calls := 0
	restore := snap.MockLogindSessionClass(func(ctx context.Context) (string, error) {
		calls++
		if calls < 3 {
			return "", fmt.Errorf("%w: 1000", logind.ErrNoDisplayEligibleSession)
		}
		return "user", nil
	})
	defer restore()

	sleeps := 0
	restoreSleep := snap.MockTimeSleep(func(time.Duration) {
		sleeps++
	})
	defer restoreSleep()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"routine", "user-service-precondition"})
	c.Assert(err, IsNil)
	c.Check(calls, Equals, 3)
	c.Check(sleeps, Equals, 2)
}

func (s *SnapSuite) TestRoutineUserServicePreconditionDoesNotRetryOtherErrors(c *C) {
	calls := 0
	restore := snap.MockLogindSessionClass(func(ctx context.Context) (string, error) {
		calls++
		return "", errors.New("cannot call loginctl")
	})
	defer restore()

	sleeps := 0
	restoreSleep := snap.MockTimeSleep(func(time.Duration) {
		sleeps++
	})
	defer restoreSleep()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"routine", "user-service-precondition"})
	c.Assert(err, NotNil)
	c.Check(calls, Equals, 1)
	c.Check(sleeps, Equals, 0)
}
