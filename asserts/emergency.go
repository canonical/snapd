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

package asserts

import (
	"fmt"
	"time"
)

// Emergency holds an emergency assertion which allows running emergency
// code to fixup broken systems. It can be limited by series and models.
type Emergency struct {
	assertionBase

	series []string
	models []string
	cmd    string
	since  time.Time
	until  time.Time
}

// EmergencyID returns the "id" of the emergency. It should be a string
// that points to a public description of the emergency in the snapcraft
// forum (or a similar place).
func (em *Emergency) EmergencyID() string {
	return em.HeaderString("emergency-id")
}

// Series returns the series that this assertion is valid for.
func (em *Emergency) Series() []string {
	return em.series
}

// Models returns the models that this assertion is valid for.
func (em *Emergency) Models() []string {
	return em.models
}

// Cmd returns the full command that is to be run.
func (em *Emergency) Cmd() string {
	return em.cmd
}

// Since returns the time since the assertion is valid.
func (em *Emergency) Since() time.Time {
	return em.since
}

// Until returns the time until the assertion is valid.
func (em *Emergency) Until() time.Time {
	return em.until
}

// ValidAt returns whether the emergency is valid at 'when' time.
func (em *Emergency) ValidAt(when time.Time) bool {
	valid := when.After(em.since) || when.Equal(em.since)
	if valid {
		valid = when.Before(em.until)
	}
	return valid
}

// Implement further consistency checks.
func (em *Emergency) checkConsistency(db RODatabase, acck *AccountKey) error {
	if !db.IsTrustedAccount(em.AuthorityID()) {
		return fmt.Errorf("emergency assertion for %q is not signed by a directly trusted authority: %s", em.EmergencyID(), em.AuthorityID())
	}

	return nil
}

// sanity
var _ consistencyChecker = (*Emergency)(nil)

func assembleEmergency(assert assertionBase) (Assertion, error) {
	series, err := checkStringList(assert.headers, "series")
	if err != nil {
		return nil, err
	}
	models, err := checkStringList(assert.headers, "models")
	if err != nil {
		return nil, err
	}
	cmd, err := checkExistsString(assert.headers, "cmd")
	if err != nil {
		return nil, err
	}
	since, err := checkRFC3339Date(assert.headers, "since")
	if err != nil {
		return nil, err
	}
	until, err := checkRFC3339Date(assert.headers, "until")
	if err != nil {
		return nil, err
	}
	if until.Before(since) {
		return nil, fmt.Errorf("'until' time cannot be before 'since' time")
	}

	// emegency assertion can only be valid for 1 month
	if until.After(since.AddDate(0, 1, 0)) {
		return nil, fmt.Errorf("'until' time cannot be more than month in the future")
	}

	return &Emergency{
		assertionBase: assert,
		series:        series,
		models:        models,
		cmd:           cmd,
		since:         since,
		until:         until,
	}, nil
}
