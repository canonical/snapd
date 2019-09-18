// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2019 Canonical Ltd
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
	"strings"
	"time"

	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/strutil"
)

// TODO: for ModelSnap
//  * consider moving snap.Type out of snap and using it in ModelSnap
//    but remember assertions use "core" (never "os") for TypeOS
//  * consider having a first-class Presence type

// ModelSnap holds the details about a snap specified by a model assertion.
type ModelSnap struct {
	Name   string
	SnapID string
	// SnapType is one of: app|base|gadget|kernel|core, default is app
	SnapType string
	// Modes in which the snap must be made available
	Modes []string
	// DefaultChannel is the initial tracking channel, default is stable
	DefaultChannel string
	// Track is a locked track for the snap, if set DefaultChannel
	// cannot be set at the same time
	Track string
	// Presence is one of: required|optional
	Presence string
}

// SnapName implements naming.SnapRef.
func (s *ModelSnap) SnapName() string {
	return s.Name
}

// ID implements naming.SnapRef.
func (s *ModelSnap) ID() string {
	return s.SnapID
}

type modelSnaps struct {
	base             *ModelSnap
	gadget           *ModelSnap
	kernel           *ModelSnap
	snapsNoEssential []*ModelSnap
}

func (ms *modelSnaps) list() (allSnaps []*ModelSnap, requiredWithEssentialSnaps []naming.SnapRef, numEssentialSnaps int) {
	addSnap := func(snap *ModelSnap, essentialSnap int) {
		if snap == nil {
			return
		}
		numEssentialSnaps += essentialSnap
		allSnaps = append(allSnaps, snap)
		if snap.Presence == "required" {
			requiredWithEssentialSnaps = append(requiredWithEssentialSnaps, snap)
		}
	}

	addSnap(ms.base, 1)
	addSnap(ms.gadget, 1)
	addSnap(ms.kernel, 1)
	for _, snap := range ms.snapsNoEssential {
		addSnap(snap, 0)
	}
	return allSnaps, requiredWithEssentialSnaps, numEssentialSnaps
}

var (
	essentialSnapModes = []string{"run", "ephemeral"}
	defaultModes       = []string{"run"}
)

func checkExtendedSnaps(extendedSnaps interface{}, base string) (*modelSnaps, error) {
	const wrongHeaderType = `"snaps" header must be a list of maps`

	entries, ok := extendedSnaps.([]interface{})
	if !ok {
		return nil, fmt.Errorf(wrongHeaderType)
	}

	var modelSnaps modelSnaps
	seen := make(map[string]bool, len(entries))
	seenIDs := make(map[string]string, len(entries))

	for _, entry := range entries {
		snap, ok := entry.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf(wrongHeaderType)
		}
		modelSnap, err := checkModelSnap(snap)
		if err != nil {
			return nil, err
		}

		if seen[modelSnap.Name] {
			return nil, fmt.Errorf("cannot list the same snap %q multiple times", modelSnap.Name)
		}
		// at this time we do not support parallel installing
		// from model/seed
		if underName := seenIDs[modelSnap.SnapID]; underName != "" {
			return nil, fmt.Errorf("cannot specify the same snap id %q multiple times, specified for snaps %q and %q", modelSnap.SnapID, underName, modelSnap.Name)
		}
		seen[modelSnap.Name] = true
		seenIDs[modelSnap.SnapID] = modelSnap.Name

		essential := false
		switch {
		case modelSnap.SnapType == "kernel":
			essential = true
			if modelSnaps.kernel != nil {
				return nil, fmt.Errorf("cannot specify multiple kernel snaps: %q and %q", modelSnaps.kernel.Name, modelSnap.Name)
			}
			modelSnaps.kernel = modelSnap
		case modelSnap.SnapType == "gadget":
			essential = true
			if modelSnaps.gadget != nil {
				return nil, fmt.Errorf("cannot specify multiple gadget snaps: %q and %q", modelSnaps.gadget.Name, modelSnap.Name)
			}
			modelSnaps.gadget = modelSnap
		case modelSnap.Name == base:
			essential = true
			if modelSnap.SnapType != "base" {
				return nil, fmt.Errorf(`boot base %q must specify type "base", not %q`, base, modelSnap.SnapType)
			}
			modelSnaps.base = modelSnap
		}

		if essential {
			if len(modelSnap.Modes) != 0 || modelSnap.Presence != "" {
				return nil, fmt.Errorf("essential snaps are always available, cannot specify modes or presence for snap %q", modelSnap.Name)
			}
			modelSnap.Modes = essentialSnapModes
		}

		if len(modelSnap.Modes) == 0 {
			modelSnap.Modes = defaultModes
		}
		if modelSnap.Presence == "" {
			modelSnap.Presence = "required"
		}

		if !essential {
			modelSnaps.snapsNoEssential = append(modelSnaps.snapsNoEssential, modelSnap)
		}
	}

	return &modelSnaps, nil
}

var (
	validSnapTypes     = []string{"app", "base", "gadget", "kernel", "core"}
	validSnapMode      = regexp.MustCompile("^[a-z][-a-z]+$")
	validSnapPresences = []string{"required", "optional"}
)

func checkModelSnap(snap map[string]interface{}) (*ModelSnap, error) {
	name, err := checkNotEmptyStringWhat(snap, "name", "of snap")
	if err != nil {
		return nil, err
	}
	if err := naming.ValidateSnap(name); err != nil {
		return nil, fmt.Errorf("invalid snap name %q", name)
	}

	what := fmt.Sprintf("of snap %q", name)

	snapID, err := checkStringMatchesWhat(snap, "id", what, validSnapID)
	if err != nil {
		return nil, err
	}

	typ, err := checkOptionalStringWhat(snap, "type", what)
	if err != nil {
		return nil, err
	}
	if typ == "" {
		typ = "app"
	}
	if !strutil.ListContains(validSnapTypes, typ) {
		return nil, fmt.Errorf("type of snap %q must be one of app|base|gadget|kernel|core", name)
	}

	modes, err := checkStringListInMap(snap, "modes", fmt.Sprintf("%q %s", "modes", what), validSnapMode)
	if err != nil {
		return nil, err
	}

	defaultChannel, err := checkOptionalStringWhat(snap, "default-channel", what)
	if err != nil {
		return nil, err
	}
	// TODO: final name of this
	track, err := checkOptionalStringWhat(snap, "track", what)
	if err != nil {
		return nil, err
	}

	if defaultChannel != "" && track != "" {
		return nil, fmt.Errorf("snap %q cannot specify both default channel and locked track", name)
	}
	if track == "" && defaultChannel == "" {
		defaultChannel = "stable"
	}

	if defaultChannel != "" {
		_, err := channel.Parse(defaultChannel, "-")
		if err != nil {
			return nil, fmt.Errorf("invalid default channel for snap %q: %v", name, err)
		}
	} else {
		trackCh, err := channel.ParseVerbatim(track, "-")
		if err != nil || !trackCh.VerbatimTrackOnly() {
			return nil, fmt.Errorf("invalid locked track for snap %q: %s", name, track)
		}
	}

	presence, err := checkOptionalStringWhat(snap, "presence", what)
	if err != nil {
		return nil, err
	}
	if presence != "" && !strutil.ListContains(validSnapPresences, presence) {
		return nil, fmt.Errorf("presence of snap %q must be one of required|optional", name)
	}

	return &ModelSnap{
		Name:           name,
		SnapID:         snapID,
		SnapType:       typ,
		Modes:          modes, // can be empty
		DefaultChannel: defaultChannel,
		Track:          track,
		Presence:       presence, // can be empty
	}, nil
}

// unextended case support

func checkSnapWithTrack(headers map[string]interface{}, which string) (*ModelSnap, error) {
	_, ok := headers[which]
	if !ok {
		return nil, nil
	}
	value, ok := headers[which].(string)
	if !ok {
		return nil, fmt.Errorf(`%q header must be a string`, which)
	}
	l := strings.SplitN(value, "=", 2)

	name := l[0]
	track := ""
	if err := validateSnapName(name, which); err != nil {
		return nil, err
	}
	if len(l) > 1 {
		track = l[1]
		if strings.Count(track, "/") != 0 {
			return nil, fmt.Errorf(`%q channel selector must be a track name only`, which)
		}
		channelRisks := []string{"stable", "candidate", "beta", "edge"}
		if strutil.ListContains(channelRisks, track) {
			return nil, fmt.Errorf(`%q channel selector must be a track name`, which)
		}
	}

	defaultChannel := ""
	if track == "" {
		defaultChannel = "stable"
	}

	return &ModelSnap{
		Name:           name,
		SnapType:       which,
		Modes:          defaultModes,
		DefaultChannel: defaultChannel,
		Track:          track,
		Presence:       "required",
	}, nil
}

func validateSnapName(name string, headerName string) error {
	if err := naming.ValidateSnap(name); err != nil {
		return fmt.Errorf("invalid snap name in %q header: %s", headerName, name)
	}
	return nil
}

func checkRequiredSnap(name string, headerName string, snapType string) (*ModelSnap, error) {
	if err := validateSnapName(name, headerName); err != nil {
		return nil, err
	}

	return &ModelSnap{
		Name:           name,
		SnapType:       snapType,
		Modes:          defaultModes,
		DefaultChannel: "stable",
		Presence:       "required",
	}, nil
}

// Model holds a model assertion, which is a statement by a brand
// about the properties of a device model.
type Model struct {
	assertionBase
	classic bool

	baseSnap   *ModelSnap
	gadgetSnap *ModelSnap
	kernelSnap *ModelSnap

	allSnaps []*ModelSnap
	// consumers of this info should care only about snap identity =>
	// snapRef
	requiredWithEssentialSnaps []naming.SnapRef
	numEssentialSnaps          int

	sysUserAuthority []string
	timestamp        time.Time
}

// BrandID returns the brand identifier. Same as the authority id.
func (mod *Model) BrandID() string {
	return mod.HeaderString("brand-id")
}

// Model returns the model name identifier.
func (mod *Model) Model() string {
	return mod.HeaderString("model")
}

// DisplayName returns the human-friendly name of the model or
// falls back to Model if this was not set.
func (mod *Model) DisplayName() string {
	display := mod.HeaderString("display-name")
	if display == "" {
		return mod.Model()
	}
	return display
}

// Series returns the series of the core software the model uses.
func (mod *Model) Series() string {
	return mod.HeaderString("series")
}

// Classic returns whether the model is a classic system.
func (mod *Model) Classic() bool {
	return mod.classic
}

// Architecture returns the archicteture the model is based on.
func (mod *Model) Architecture() string {
	return mod.HeaderString("architecture")
}

// GadgetSnap returns the details of the gadget snap the model uses.
func (mod *Model) GadgetSnap() *ModelSnap {
	return mod.gadgetSnap
}

// Gadget returns the gadget snap the model uses.
func (mod *Model) Gadget() string {
	if mod.gadgetSnap == nil {
		return ""
	}
	return mod.gadgetSnap.Name
}

// GadgetTrack returns the gadget track the model uses.
// XXX this should go away
func (mod *Model) GadgetTrack() string {
	if mod.gadgetSnap == nil {
		return ""
	}
	return mod.gadgetSnap.Track
}

// KernelSnap returns the details of the kernel snap the model uses.
func (mod *Model) KernelSnap() *ModelSnap {
	return mod.kernelSnap
}

// Kernel returns the kernel snap the model uses.
// XXX this should go away
func (mod *Model) Kernel() string {
	if mod.kernelSnap == nil {
		return ""
	}
	return mod.kernelSnap.Name
}

// KernelTrack returns the kernel track the model uses.
// XXX this should go away
func (mod *Model) KernelTrack() string {
	if mod.kernelSnap == nil {
		return ""
	}
	return mod.kernelSnap.Track
}

// Base returns the base snap the model uses.
func (mod *Model) Base() string {
	return mod.HeaderString("base")
}

// BaseSnap returns the details of the base snap the model uses.
func (mod *Model) BaseSnap() *ModelSnap {
	return mod.baseSnap
}

// Store returns the snap store the model uses.
func (mod *Model) Store() string {
	return mod.HeaderString("store")
}

// RequiredNoEssentialSnaps returns the snaps that must be installed at all times and cannot be removed for this model, excluding the essential snaps (gadget, kernel, boot base).
func (mod *Model) RequiredNoEssentialSnaps() []naming.SnapRef {
	return mod.requiredWithEssentialSnaps[mod.numEssentialSnaps:]
}

// RequiredWithEssentialSnaps returns the snaps that must be installed at all times and cannot be removed for this model, including the essential snaps (gadget, kernel, boot base).
func (mod *Model) RequiredWithEssentialSnaps() []naming.SnapRef {
	return mod.requiredWithEssentialSnaps
}

// AllSnaps returns all the snap listed by the model.
func (mod *Model) AllSnaps() []*ModelSnap {
	return mod.allSnaps
}

// SystemUserAuthority returns the authority ids that are accepted as signers of system-user assertions for this model. Empty list means any.
func (mod *Model) SystemUserAuthority() []string {
	return mod.sysUserAuthority
}

// Timestamp returns the time when the model assertion was issued.
func (mod *Model) Timestamp() time.Time {
	return mod.timestamp
}

// Implement further consistency checks.
func (mod *Model) checkConsistency(db RODatabase, acck *AccountKey) error {
	// TODO: double check trust level of authority depending on class and possibly allowed-modes
	return nil
}

// sanity
var _ consistencyChecker = (*Model)(nil)

// limit model to only lowercase for now
var validModel = regexp.MustCompile("^[a-zA-Z0-9](?:-?[a-zA-Z0-9])*$")

func checkModel(headers map[string]interface{}) (string, error) {
	s, err := checkStringMatches(headers, "model", validModel)
	if err != nil {
		return "", err
	}

	// TODO: support the concept of case insensitive/preserving string headers
	if strings.ToLower(s) != s {
		return "", fmt.Errorf(`"model" header cannot contain uppercase letters`)
	}
	return s, nil
}

func checkAuthorityMatchesBrand(a Assertion) error {
	typeName := a.Type().Name
	authorityID := a.AuthorityID()
	brand := a.HeaderString("brand-id")
	if brand != authorityID {
		return fmt.Errorf("authority-id and brand-id must match, %s assertions are expected to be signed by the brand: %q != %q", typeName, authorityID, brand)
	}
	return nil
}

func checkOptionalSystemUserAuthority(headers map[string]interface{}, brandID string) ([]string, error) {
	const name = "system-user-authority"
	v, ok := headers[name]
	if !ok {
		return []string{brandID}, nil
	}
	switch x := v.(type) {
	case string:
		if x == "*" {
			return nil, nil
		}
	case []interface{}:
		lst, err := checkStringListMatches(headers, name, validAccountID)
		if err == nil {
			return lst, nil
		}
	}
	return nil, fmt.Errorf("%q header must be '*' or a list of account ids", name)
}

var (
	modelMandatory           = []string{"architecture", "gadget", "kernel"}
	extendedCoreMandatory    = []string{"architecture", "base"}
	extendedSnapsConflicting = []string{"gadget", "kernel", "required-snaps"}
	classicModelOptional     = []string{"architecture", "gadget"}
)

func assembleModel(assert assertionBase) (Assertion, error) {
	err := checkAuthorityMatchesBrand(&assert)
	if err != nil {
		return nil, err
	}

	_, err = checkModel(assert.headers)
	if err != nil {
		return nil, err
	}

	classic, err := checkOptionalBool(assert.headers, "classic")
	if err != nil {
		return nil, err
	}

	// Core 20 extended snaps header
	extendedSnaps, extended := assert.headers["snaps"]
	if extended {
		if classic {
			return nil, fmt.Errorf("cannot use extended snaps header for a classic model (yet)")
		}

		for _, conflicting := range extendedSnapsConflicting {
			if _, ok := assert.headers[conflicting]; ok {
				return nil, fmt.Errorf("cannot specify separate %q header once using the extended snaps header", conflicting)
			}
		}

	} else if classic {
		if _, ok := assert.headers["kernel"]; ok {
			return nil, fmt.Errorf("cannot specify a kernel with a classic model")
		}
		if _, ok := assert.headers["base"]; ok {
			return nil, fmt.Errorf("cannot specify a base with a classic model")
		}
	}

	checker := checkNotEmptyString
	toCheck := modelMandatory
	if extended {
		toCheck = extendedCoreMandatory
	} else if classic {
		checker = checkOptionalString
		toCheck = classicModelOptional
	}

	for _, h := range toCheck {
		if _, err := checker(assert.headers, h); err != nil {
			return nil, err
		}
	}

	// base, if provided, must be a valid snap name too
	var baseSnap *ModelSnap
	base, err := checkOptionalString(assert.headers, "base")
	if err != nil {
		return nil, err
	}
	if base != "" {
		baseSnap, err = checkRequiredSnap(base, "base", "base")
		if err != nil {
			return nil, err
		}
	}

	// store is optional but must be a string, defaults to the ubuntu store
	if _, err = checkOptionalString(assert.headers, "store"); err != nil {
		return nil, err
	}

	// display-name is optional but must be a string
	if _, err = checkOptionalString(assert.headers, "display-name"); err != nil {
		return nil, err
	}

	var modSnaps *modelSnaps
	if extended {
		// TODO: support and consider grade!
		modSnaps, err = checkExtendedSnaps(extendedSnaps, base)
		if err != nil {
			return nil, err
		}
		if modSnaps.gadget == nil {
			return nil, fmt.Errorf(`one "snaps" header entry must specify the model gadget`)
		}
		if modSnaps.kernel == nil {
			return nil, fmt.Errorf(`one "snaps" header entry must specify the model kernel`)
		}

		if modSnaps.base == nil {
			// complete with defaults,
			// the assumption is that base names are very stable
			// essentially fixed
			modSnaps.base = baseSnap
			modSnaps.base.Modes = essentialSnapModes
		}
	} else {
		modSnaps = &modelSnaps{
			base: baseSnap,
		}
		// kernel/gadget must be valid snap names and can have (optional) tracks
		// - validate those
		modSnaps.kernel, err = checkSnapWithTrack(assert.headers, "kernel")
		if err != nil {
			return nil, err
		}
		modSnaps.gadget, err = checkSnapWithTrack(assert.headers, "gadget")
		if err != nil {
			return nil, err
		}

		// required snap must be valid snap names
		reqSnaps, err := checkStringList(assert.headers, "required-snaps")
		if err != nil {
			return nil, err
		}
		for _, name := range reqSnaps {
			reqSnap, err := checkRequiredSnap(name, "required-snaps", "")
			if err != nil {
				return nil, err
			}
			modSnaps.snapsNoEssential = append(modSnaps.snapsNoEssential, reqSnap)
		}
	}

	sysUserAuthority, err := checkOptionalSystemUserAuthority(assert.headers, assert.HeaderString("brand-id"))
	if err != nil {
		return nil, err
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	allSnaps, requiredWithEssentialSnaps, numEssentialSnaps := modSnaps.list()

	// NB:
	// * core is not supported at this time, it defaults to ubuntu-core
	// in prepare-image until rename and/or introduction of the header.
	// * some form of allowed-modes, class are postponed,
	//
	// prepare-image takes care of not allowing them for now

	// ignore extra headers and non-empty body for future compatibility
	return &Model{
		assertionBase:              assert,
		classic:                    classic,
		baseSnap:                   modSnaps.base,
		gadgetSnap:                 modSnaps.gadget,
		kernelSnap:                 modSnaps.kernel,
		allSnaps:                   allSnaps,
		requiredWithEssentialSnaps: requiredWithEssentialSnaps,
		numEssentialSnaps:          numEssentialSnaps,
		sysUserAuthority:           sysUserAuthority,
		timestamp:                  timestamp,
	}, nil
}
