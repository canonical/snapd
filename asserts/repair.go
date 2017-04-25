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
	"regexp"
)

// Repair holds an repair assertion which allows running repair
// code to fixup broken systems. It can be limited by series and models.
type Repair struct {
	assertionBase

	series []string
	models []string
}

// BrandID returns the brand identifier that signed this assertion.
func (em *Repair) BrandID() string {
	return em.HeaderString("brand-id")
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

// Implement further consistency checks.
func (em *Repair) checkConsistency(db RODatabase, acck *AccountKey) error {
	if !db.IsTrustedAccount(em.AuthorityID()) {
		return fmt.Errorf("repair assertion for %q is not signed by a directly trusted authority: %s", em.RepairID(), em.AuthorityID())
	}

	return nil
}

// sanity
var _ consistencyChecker = (*Repair)(nil)

var validRepairID = regexp.MustCompile("^[a-z]+-[0-9]+")

func assembleRepair(assert assertionBase) (Assertion, error) {
	series, err := checkStringList(assert.headers, "series")
	if err != nil {
		return nil, err
	}
	models, err := checkStringList(assert.headers, "models")
	if err != nil {
		return nil, err
	}
	if _, err = checkStringMatchesWhat(assert.headers, "repair-id", "header", validRepairID); err != nil {
		return nil, err
	}

	return &Repair{
		assertionBase: assert,
		series:        series,
		models:        models,
	}, nil
}
