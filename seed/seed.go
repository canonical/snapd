// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2021 Canonical Ltd
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

// Package seed implements loading and validating of seed data.
package seed

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/seed/internal"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
)

var (
	ErrNoAssertions = errors.New("no seed assertions")
	ErrNoMeta       = errors.New("no seed metadata")
)

// Snap holds the details of a snap in a seed.
type Snap struct {
	Path string

	SideInfo *snap.SideInfo

	// EssentialType is the type of the snap as specified by the model.
	// Provided only for essential snaps (Essential = true).
	EssentialType snap.Type

	Essential bool
	Required  bool

	// options
	Channel string
	DevMode bool
	Classic bool
}

func (s *Snap) SnapName() string {
	return s.SideInfo.RealName
}

func (s *Snap) ID() string {
	return s.SideInfo.SnapID
}

// PlaceInfo returns a PlaceInfo for the seed snap.
func (s *Snap) PlaceInfo() snap.PlaceInfo {
	return &snap.Info{SideInfo: *s.SideInfo}
}

// Seed supports loading assertions and seed snaps' metadata.
type Seed interface {
	// LoadAssertions loads all assertions from the seed with
	// cross-checks.  A read-only view on an assertions database
	// can be passed in together with a commitTo function which
	// will be used to commit the assertions to the underlying
	// database. If db is nil an internal temporary database will
	// be setup instead. ErrNoAssertions will be returned if there
	// is no assertions directory in the seed, this is legitimate
	// only on classic.
	LoadAssertions(db asserts.RODatabase, commitTo func(*asserts.Batch) error) error

	// Model returns the seed provided model assertion.
	// It will panic if called before LoadAssertions.
	Model() *asserts.Model

	// Brand returns the brand information of the seed.
	// It will panic if called before LoadAssertions.
	Brand() (*asserts.Account, error)

	// LoadEssentialMeta loads the seed's snaps metadata for the
	// essential snaps with types in the essentialTypes set while
	// verifying them against assertions. It can return ErrNoMeta
	// if there is no metadata nor snaps in the seed, this is
	// legitimate only on classic. It can be called multiple times
	// if needed before invoking LoadMeta.
	// It will panic if called before LoadAssertions.
	LoadEssentialMeta(essentialTypes []snap.Type, tm timings.Measurer) error

	// LoadMeta loads the seed and seed's snaps metadata while
	// verifying the underlying snaps against assertions. It can
	// return ErrNoMeta if there is no metadata nor snaps in the
	// seed, this is legitimate only on classic.
	// It will panic if called before LoadAssertions.
	LoadMeta(tm timings.Measurer) error

	// UsesSnapdSnap returns whether the system as defined by the
	// seed will use the snapd snap, after LoadMeta.
	UsesSnapdSnap() bool

	// EssentialSnaps returns the essential snaps as defined by
	// the seed, after LoadMeta.
	EssentialSnaps() []*Snap

	// ModeSnaps returns the snaps that should be available
	// in the given mode as defined by the seed, after LoadMeta.
	ModeSnaps(mode string) ([]*Snap, error)
}

// Open returns a Seed implementation for the seed at seedDir.
// label if not empty is used to identify a Core 20 recovery system seed.
func Open(seedDir, label string) (Seed, error) {
	if label != "" {
		if err := internal.ValidateUC20SeedSystemLabel(label); err != nil {
			return nil, err
		}
		return &seed20{systemDir: filepath.Join(seedDir, "systems", label)}, nil
	}
	return &seed16{seedDir: seedDir}, nil
}

// ReadSystemEssential retrieves in one go information about the model
// and essential snaps of the given types for the Core 20 recovery
// system seed specified by seedDir and label (which cannot be empty).
func ReadSystemEssential(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*Snap, error) {
	if label == "" {
		return nil, nil, fmt.Errorf("system label cannot be empty")
	}
	seed20, err := Open(seedDir, label)
	if err != nil {
		return nil, nil, err
	}

	// load assertions into a temporary database
	if err := seed20.LoadAssertions(nil, nil); err != nil {
		return nil, nil, err
	}

	// load and verify info about essential snaps
	if err := seed20.LoadEssentialMeta(essentialTypes, tm); err != nil {
		return nil, nil, err
	}

	return seed20.Model(), seed20.EssentialSnaps(), nil
}

// ReadSystemEssentialAndBetterEarliestTime retrieves in one go
// information about the model and essential snaps of the given types
// for the Core 20 recovery system seed specified by seedDir and label
// (which cannot be empty).
// It can operate even if current system time is unreliable by taking
// a earliestTime lower bound for current time.
// It returns as well an improved lower bound by considering
// appropriate assertions in the seed.
func ReadSystemEssentialAndBetterEarliestTime(seedDir, label string, essentialTypes []snap.Type, earliestTime time.Time, tm timings.Measurer) (*asserts.Model, []*Snap, time.Time, error) {
	if label == "" {
		return nil, nil, time.Time{}, fmt.Errorf("system label cannot be empty")
	}
	seed20, err := Open(seedDir, label)
	if err != nil {
		return nil, nil, time.Time{}, err

	}

	improve := func(a asserts.Assertion) {
		// we consider only snap-revision and snap-declaration
		// assertions here as they must be store-signed, see
		// checkConsistency for each type
		// other assertions might be signed not by the store
		// nor the brand so they might be provided by an
		// attacker, signed using a registered key but
		// containing unreliable time
		var tstamp time.Time
		switch a.Type() {
		default:
			// not one of the store-signed assertion types
			return
		case asserts.SnapRevisionType:
			sr := a.(*asserts.SnapRevision)
			tstamp = sr.Timestamp()
		case asserts.SnapDeclarationType:
			sd := a.(*asserts.SnapDeclaration)
			tstamp = sd.Timestamp()
		}
		if tstamp.After(earliestTime) {
			earliestTime = tstamp
		}
	}

	// create a temporary database, commitTo will invoke improve
	db, commitTo, err := newMemAssertionsDB(improve)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	// set up the database to check for key expiry only assuming
	// earliestTime (if not zero)
	db.SetEarliestTime(earliestTime)

	// load assertions into the temporary database
	if err := seed20.LoadAssertions(db, commitTo); err != nil {
		return nil, nil, time.Time{}, err
	}

	// load and verify info about essential snaps
	if err := seed20.LoadEssentialMeta(essentialTypes, tm); err != nil {
		return nil, nil, time.Time{}, err
	}

	// consider the model's timestamp as well - it must be signed
	// by the brand so is safe from the attack detailed above
	mod := seed20.Model()
	if mod.Timestamp().After(earliestTime) {
		earliestTime = mod.Timestamp()
	}

	return mod, seed20.EssentialSnaps(), earliestTime, nil
}
