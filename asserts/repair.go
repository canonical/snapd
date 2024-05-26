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
	"strings"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/strutil"
)

// Repair holds an repair assertion which allows running repair
// code to fixup broken systems. It can be limited by series and models, as well
// as by bases and modes.
type Repair struct {
	assertionBase

	series        []string
	architectures []string
	models        []string

	modes []string
	bases []string

	id int

	disabled  bool
	timestamp time.Time
}

// BrandID returns the brand identifier that signed this assertion.
func (r *Repair) BrandID() string {
	return r.HeaderString("brand-id")
}

// RepairID returns the sequential id of the repair. There
// should be a public place to look up details about the repair
// by brand-id and repair-id.
// (e.g. the snapcraft forum).
func (r *Repair) RepairID() int {
	return r.id
}

// Sequence implements SequenceMember, it returns the same as RepairID.
func (r *Repair) Sequence() int {
	return r.RepairID()
}

// Summary returns the mandatory summary description of the repair.
func (r *Repair) Summary() string {
	return r.HeaderString("summary")
}

// Architectures returns the architectures that this assertions applies to.
func (r *Repair) Architectures() []string {
	return r.architectures
}

// Series returns the series that this assertion is valid for.
func (r *Repair) Series() []string {
	return r.series
}

// Modes returns the modes that this assertion is valid for. It is either a list
// of "run", "recover", or "install", or it is the empty list. The empty list
// is interpreted to mean only "run" mode.
func (r *Repair) Modes() []string {
	return r.modes
}

// Bases returns the bases that this assertion is valid for. It is either a list
// of valid base snaps that Ubuntu Core systems can have or it is the empty
// list. The empty list effectively means all Ubuntu Core systems while "core"
// means Ubuntu Core 16, "core18" means Ubuntu Core 18, etc.
func (r *Repair) Bases() []string {
	return r.bases
}

// Models returns the models that this assertion is valid for.
// It is a list of "brand-id/model-name" strings.
func (r *Repair) Models() []string {
	return r.models
}

// Disabled returns true if the repair has been disabled.
func (r *Repair) Disabled() bool {
	return r.disabled
}

// Timestamp returns the time when the repair was issued.
func (r *Repair) Timestamp() time.Time {
	return r.timestamp
}

// Implement further consistency checks.
func (r *Repair) checkConsistency(db RODatabase, acck *AccountKey) error {
	// Do the cross-checks when this assertion is actually used,
	// i.e. in the future repair code

	return nil
}

// expected interface is implemented
var _ consistencyChecker = (*Repair)(nil)

func assembleRepair(assert assertionBase) (Assertion, error) {
	mylog.Check(checkAuthorityMatchesBrand(&assert))

	repairID := mylog.Check2(checkSequence(assert.headers, "repair-id"))

	summary := mylog.Check2(checkNotEmptyString(assert.headers, "summary"))

	if strings.ContainsAny(summary, "\n\r") {
		return nil, fmt.Errorf(`"summary" header cannot have newlines`)
	}

	series := mylog.Check2(checkStringList(assert.headers, "series"))

	models := mylog.Check2(checkStringList(assert.headers, "models"))

	architectures := mylog.Check2(checkStringList(assert.headers, "architectures"))

	modes := mylog.Check2(checkStringList(assert.headers, "modes"))

	bases := mylog.Check2(checkStringList(assert.headers, "bases"))

	// validate that all base snap names are valid snap names
	for _, b := range bases {
		mylog.Check(naming.ValidateSnap(b))
	}

	// verify that modes is a list of only "run" and "recover"
	if len(modes) != 0 {
		for _, m := range modes {
			// note that we could import boot here to use i.e. boot.ModeRun, but
			// that is rather a heavy package considering that this package is
			// used in many places, so instead just use the values directly,
			// they're unlikely to change now
			if !strutil.ListContains([]string{"run", "recover"}, m) {
				return nil, fmt.Errorf("header \"modes\" contains an invalid element: %q (valid values are run and recover)", m)
			}
		}

		// if modes is non-empty, then bases must be core2X, i.e. core20+
		// however, we don't know what future bases could be UC20-like and named
		// differently yet, so we just fail on bases that we know as of today
		// are _not_ UC20: core and core18

		for _, b := range bases {
			// fail on uc16 and uc18 base snaps
			if b == "core" || b == "core18" || b == "core16" {
				return nil, fmt.Errorf("in the presence of a non-empty \"modes\" header, \"bases\" must only contain base snaps supporting recovery modes")
			}
		}
	}

	disabled := mylog.Check2(checkOptionalBool(assert.headers, "disabled"))

	timestamp := mylog.Check2(checkRFC3339Date(assert.headers, "timestamp"))

	return &Repair{
		assertionBase: assert,
		series:        series,
		architectures: architectures,
		models:        models,
		modes:         modes,
		bases:         bases,
		id:            repairID,
		disabled:      disabled,
		timestamp:     timestamp,
	}, nil
}
