// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2022 Canonical Ltd
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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
)

var (
	ErrNoAssertions       = errors.New("no seed assertions")
	ErrNoPreseedAssertion = errors.New("no seed preseed assertion")
	ErrNoMeta             = errors.New("no seed metadata")

	open = Open
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

// AllModes can be passed to Seed.LoadMeta to load metadata for snaps
// for all modes.
const AllModes = ""

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

	// SetParallelism suggests that n parallel jobs should be used
	// to load and verify snap metadata by Load*Meta operations.
	// The default is one single job.
	SetParallelism(n int)

	// LoadEssentialMeta loads the seed's snaps metadata for the
	// essential snaps with types in the essentialTypes set while
	// verifying them against assertions. It can return ErrNoMeta
	// if there is no metadata nor snaps in the seed, this is
	// legitimate only on classic. It can be called multiple times
	// if needed before invoking LoadMeta.
	// It will panic if called before LoadAssertions.
	LoadEssentialMeta(essentialTypes []snap.Type, tm timings.Measurer) error

	// LoadEssentialMetaWithSnapHandler loads the seed's snaps metadata
	// for the essential snaps with types in the essentialTypes
	// set while verifying them against assertions. It can return
	// ErrNoMeta if there is no metadata nor snaps in the seed,
	// this is legitimate only on classic. It can be called
	// multiple times if needed before invoking LoadMeta.
	// It will panic if called before LoadAssertions.
	// A SnapHandler can be passed to perform dedicated seed snap
	// handling at the same time as digest computation.
	// No caching of essential snaps across Load*Meta* methods is
	// performed if a handler is provided.
	LoadEssentialMetaWithSnapHandler(essentialTypes []snap.Type, handler SnapHandler, tm timings.Measurer) error

	// LoadMeta loads the seed and seed's snaps metadata while
	// verifying the underlying snaps against assertions. It can
	// return ErrNoMeta if there is no metadata nor snaps in the
	// seed, this is legitimate only on classic.
	// It will panic if called before LoadAssertions.
	// If a precise mode is passed and not AllModes it will
	// load the metadata only for the snaps of that mode.
	// At which point ModeSnaps will only accept that mode
	// and Iter and NumSnaps only consider the snaps for that mode.
	// An optional SnapHandler can be passed to perform dedicated
	// seed snap handling at the same time as digest computation.
	// No caching of essential snaps across Load*Meta* methods is
	// performed if a handler is provided.
	LoadMeta(mode string, handler SnapHandler, tm timings.Measurer) error

	// UsesSnapdSnap returns whether the system as defined by the
	// seed will use the snapd snap, after LoadMeta.
	UsesSnapdSnap() bool

	// EssentialSnaps returns the essential snaps as defined by
	// the seed, after LoadMeta.
	EssentialSnaps() []*Snap

	// ModeSnaps returns the snaps that should be available
	// in the given mode as defined by the seed, after LoadMeta.
	// If LoadMeta was passed a precise mode, passing a different
	// mode here will result in error.
	ModeSnaps(mode string) ([]*Snap, error)

	// NumSnaps returns the total number of snaps for which
	// LoadMeta loaded their metadata.
	NumSnaps() int

	// Iter provides a way to iterately perform a function on
	// each of the snaps for which LoadMeta loaded their metadata.
	Iter(f func(sn *Snap) error) error
}

// A SnapHandler can be used to perform any dedicated handling of seed
// snaps and their digest computation while seed snap metadata loading
// and verification is being performed.
type SnapHandler interface {
	// HandleAndDigestAssertedSnap should compute the digest of
	// the given snap and perform any dedicated
	// handling. essentialType is provided only for essential
	// snaps.
	// snapRev is provided by UC20+ seeds.
	// deriveRev is provided by UC16/18 seeds, it can be used
	// to get early access to the snap revision based on the digest.
	// A different path can be returned if the snap has been copied
	// elsewhere.
	HandleAndDigestAssertedSnap(name, path string, essentialType snap.Type, snapRev *asserts.SnapRevision, deriveRev func(snapSHA3_384 string, snapSize uint64) (snap.Revision, error), tm timings.Measurer) (newPath, snapSHA3_384 string, snapSize uint64, err error)

	// HandleUnassertedSnap should perfrom any dedicated handling
	// for the given unasserted snap.
	// A different path can be returned if the snap has been copied
	// elsewhere.
	HandleUnassertedSnap(name, path string, tm timings.Measurer) (newPath string, err error)
}

// A AutoImportAssertionsLoaderSeed can be used to import all auto import assertions
// via LoadAutoImportAssertions.
type AutoImportAssertionsLoaderSeed interface {
	// LoadAutoImportAssertions attempts to loads all auto import assertions
	// from the root of the seed.
	LoadAutoImportAssertions(commitTo func(*asserts.Batch) error) error
}

// PreseedCapable seeds can support preseeding data in them.
type PreseedCapable interface {
	Seed
	// HasArtifact returns whether the given artifact file is present in the seed.
	HasArtifact(relName string) bool
	// ArtifactPath returns the path of an artifact file in the seed.
	ArtifactPath(relName string) string
	// LoadPreesdAssertion tries to load the preseed assertion from the seed
	// if any. It returns ErrNoPressedAssertion if there is none.
	// It will panic if called before LoadAssertions.
	// Any assertion will be committed using the commitTo provided
	// to LoadAssertions.
	LoadPreseedAssertion() (*asserts.Preseed, error)
}

// Copier can be implemented by a seed that supports copying itself to a given
// destination.
type Copier interface {
	Seed
	// Copy copies the seed to the given seedDir with the label provided. If
	// label is empty, then the label of the seed that implements Copier is
	// used. This interface only makes sense to implement for UC20+ seeds. Copy
	// requires you to call the LoadAssertions method first. Note that LoadMeta
	// for all modes will be called by Copy. If LoadMeta was called previously
	// on this Seed with a different mode, then that metadata will be
	// overwritten by the metadata for all modes.
	Copy(seedDir string, label string, tm timings.Measurer) error
}

// Open returns a Seed implementation for the seed at seedDir.
// label if not empty is used to identify a Core 20 recovery system seed.
func Open(seedDir, label string) (Seed, error) {
	if label != "" {
		if err := asserts.IsValidSystemLabel(label); err != nil {
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

// ReadSeedAndBetterEarliestTime retrieves in one go the seed and
// assertions for the Core 20 recovery system seed specified by
// seedDir and label (which cannot be empty). numJobs specifies the
// suggested number of jobs to run in parallel (0 disables
// parallelism).  It can operate even if current system time is
// unreliable by taking a earliestTime lower bound for current time.
// It returns as well an improved lower bound by considering
// appropriate assertions in the seed.
func ReadSeedAndBetterEarliestTime(seedDir, label string, earliestTime time.Time, numJobs int, tm timings.Measurer) (Seed, time.Time, error) {
	if label == "" {
		return nil, time.Time{}, fmt.Errorf("system label cannot be empty")
	}
	seed20, err := open(seedDir, label)
	if err != nil {
		return nil, time.Time{}, err

	}

	if numJobs > 0 {
		seed20.SetParallelism(numJobs)
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
		return nil, time.Time{}, err
	}
	// set up the database to check for key expiry only assuming
	// earliestTime (if not zero)
	db.SetEarliestTime(earliestTime)

	// load assertions into the temporary database
	if err := seed20.LoadAssertions(db, commitTo); err != nil {
		return nil, time.Time{}, err
	}

	// consider the model's timestamp as well - it must be signed
	// by the brand so is safe from the attack detailed above
	mod := seed20.Model()
	if mod.Timestamp().After(earliestTime) {
		earliestTime = mod.Timestamp()
	}

	return seed20, earliestTime, nil
}
