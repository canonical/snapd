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

// Repair holds an repair assertion which allows running repair
// code to fixup broken systems. It can be limited by series and models.
type Repair struct {
	assertionBase

	series []string
	models []string
	since  time.Time
	until  time.Time
}

// RepairID returns the "id" of the repair. It should be a short string
// that follows a convention like "REPAIR-123". Similar to a CVE there
// should be a public place to look up details about the repair-id
// (e.g. the snapcraft forum).
func (em *Repair) RepairID() string {
	return em.HeaderString("repair-id")
}

// Series returns the series that this assertion is valid for.
func (em *Repair) Series() []string {
	return em.series
}

// Models returns the models that this assertion is valid for.
// It is a list of "brand-id/model-name" strings.
func (em *Repair) Models() []string {
	return em.models
}

// Script returns the full script that should run.
func (em *Repair) Script() string {
	return em.HeaderString("script")
}

// Since returns the time since the assertion is valid.
func (em *Repair) Since() time.Time {
	return em.since
}

// Until returns the time until the assertion is valid.
func (em *Repair) Until() time.Time {
	return em.until
}

// ValidAt returns whether the repair is valid at 'when' time.
func (em *Repair) ValidAt(when time.Time) bool {
	valid := when.After(em.since) || when.Equal(em.since)
	if valid {
		valid = when.Before(em.until)
	}
	return valid
}

// Implement further consistency checks.
func (em *Repair) checkConsistency(db RODatabase, acck *AccountKey) error {
	if !db.IsTrustedAccount(em.AuthorityID()) {
		return fmt.Errorf("repair assertion for %q is not signed by a directly trusted authority: %s", em.RepairID(), em.AuthorityID())
	}

	return nil
}

// sanity
var _ consistencyChecker = (*Repair)(nil)

func assembleRepair(assert assertionBase) (Assertion, error) {
	series, err := checkStringList(assert.headers, "series")
	if err != nil {
		return nil, err
	}
	models, err := checkStringList(assert.headers, "models")
	if err != nil {
		return nil, err
	}
	if _, err := checkExistsString(assert.headers, "script"); err != nil {
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

	return &Repair{
		assertionBase: assert,
		series:        series,
		models:        models,
		since:         since,
		until:         until,
	}, nil
}
