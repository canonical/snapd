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

// Package seed implements loading and validating of seed data.
package seed

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
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

	// Model returns the seed provided model assertion. It is an
	// error to call Model before LoadAssertions.
	Model() (*asserts.Model, error)

	// Brand returns the brand information of the seed. It is an
	// error to call Brand before LoadAssertions.
	Brand() (*asserts.Account, error)

	// LoadMeta loads the seed and seed's snaps metadata while
	// verifying the underlying snaps against assertions. It can
	// return ErrNoMeta if there is no metadata nor snaps in the
	// seed, this is legitimate only on classic. It is an error to
	// call LoadMeta before LoadAssertions.
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

// EssentialMetaLoaderSeed is a Seed that can be asked to load and verify
// only a subset of the essential model snaps via LoadEssentialMeta.
type EssentialMetaLoaderSeed interface {
	Seed

	// LoadEssentialMeta loads the seed's snaps metadata for the
	// essential snaps with types in the essentialTypes set while
	// verifying them against assertions. It can return ErrNoMeta
	// if there is no metadata nor snaps in the seed, this is
	// legitimate only on classic. It is an error to call LoadMeta
	// before LoadAssertions or to mix it with LoadMeta.
	LoadEssentialMeta(essentialTypes []snap.Type, tm timings.Measurer) error
}

// Open returns a Seed implementation for the seed at seedDir.
// label if not empty is used to identify a Core 20 recovery system seed.
func Open(seedDir, label string) (Seed, error) {
	if label != "" {
		if err := validateUC20SeedSystemLabel(label); err != nil {
			return nil, err
		}
		return &seed20{systemDir: filepath.Join(seedDir, "systems", label)}, nil
	}
	// TODO: consider if systems is present to open the Core 20
	// system if there is only one, or the lexicographically
	// highest label one?
	return &seed16{seedDir: seedDir}, nil
}

// ReadSystemEssential retrieves in one go information about the model
// and essential snaps of the given types for the Core 20 recovery
// system seed specified by seedDir and label (which cannot be empty).
func ReadSystemEssential(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*Snap, error) {
	if label == "" {
		return nil, nil, fmt.Errorf("system label cannot be empty")
	}
	seed, err := Open(seedDir, label)
	if err != nil {
		return nil, nil, err
	}

	seed20, ok := seed.(EssentialMetaLoaderSeed)
	if !ok {
		return nil, nil, fmt.Errorf("internal error: UC20 seed must implement EssentialMetaLoaderSeed")
	}

	// load assertions into a temporary database
	if err := seed20.LoadAssertions(nil, nil); err != nil {
		return nil, nil, err
	}

	// Model always succeeds after LoadAssertions
	mod, _ := seed20.Model()

	// load and verify info about essential snaps
	if err := seed20.LoadEssentialMeta(essentialTypes, tm); err != nil {
		return nil, nil, err
	}

	return mod, seed20.EssentialSnaps(), nil
}
