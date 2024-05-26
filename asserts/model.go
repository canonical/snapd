// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2023 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/strutil"
)

// ModelComponent holds details for components specified by a model assertion.
type ModelComponent struct {
	// Presence can be optional or required
	Presence string
	// Modes is an optional list of modes, which must be a subset
	// of the ones for the snap
	Modes []string
}

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
	// DefaultChannel is the initial tracking channel,
	// default is latest/stable in an extended model
	DefaultChannel string
	// PinnedTrack is a pinned track for the snap, if set DefaultChannel
	// cannot be set at the same time (Core 18 models feature)
	PinnedTrack string
	// Presence is one of: required|optional
	Presence string
	// Classic indicates that this classic snap is intentionally
	// included in a classic model
	Classic bool
	// Components is a map of component names to ModelComponent
	Components map[string]ModelComponent
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
	snapd            *ModelSnap
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

	addSnap(ms.snapd, 1)
	addSnap(ms.kernel, 1)
	addSnap(ms.base, 1)
	addSnap(ms.gadget, 1)
	for _, snap := range ms.snapsNoEssential {
		addSnap(snap, 0)
	}
	return allSnaps, requiredWithEssentialSnaps, numEssentialSnaps
}

var (
	essentialSnapModes = []string{"run", "ephemeral"}
	defaultModes       = []string{"run"}
)

func checkExtendedSnaps(extendedSnaps interface{}, base string, grade ModelGrade, modelIsClassic bool) (*modelSnaps, error) {
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
		modelSnap := mylog.Check2(checkModelSnap(snap, base, grade, modelIsClassic))

		if seen[modelSnap.Name] {
			return nil, fmt.Errorf("cannot list the same snap %q multiple times", modelSnap.Name)
		}
		seen[modelSnap.Name] = true
		// at this time we do not support parallel installing
		// from model/seed
		if snapID := modelSnap.SnapID; snapID != "" {
			if underName := seenIDs[snapID]; underName != "" {
				return nil, fmt.Errorf("cannot specify the same snap id %q multiple times, specified for snaps %q and %q", snapID, underName, modelSnap.Name)
			}
			seenIDs[snapID] = modelSnap.Name
		}

		switch {
		case modelSnap.SnapType == "snapd":
			// TODO: allow to be explicit only in grade: dangerous?
			if modelSnaps.snapd != nil {
				return nil, fmt.Errorf("cannot specify multiple snapd snaps: %q and %q", modelSnaps.snapd.Name, modelSnap.Name)
			}
			modelSnaps.snapd = modelSnap
		case modelSnap.SnapType == "kernel":
			if modelSnaps.kernel != nil {
				return nil, fmt.Errorf("cannot specify multiple kernel snaps: %q and %q", modelSnaps.kernel.Name, modelSnap.Name)
			}
			modelSnaps.kernel = modelSnap
		case modelSnap.SnapType == "gadget":
			if modelSnaps.gadget != nil {
				return nil, fmt.Errorf("cannot specify multiple gadget snaps: %q and %q", modelSnaps.gadget.Name, modelSnap.Name)
			}
			modelSnaps.gadget = modelSnap
		case modelSnap.Name == base:
			if modelSnap.SnapType != "base" {
				return nil, fmt.Errorf(`boot base %q must specify type "base", not %q`, base, modelSnap.SnapType)
			}
			modelSnaps.base = modelSnap
		}

		if !isEssentialSnap(modelSnap.Name, modelSnap.SnapType, base) {
			modelSnaps.snapsNoEssential = append(modelSnaps.snapsNoEssential, modelSnap)
		}
	}

	return &modelSnaps, nil
}

var (
	validSnapTypes     = []string{"app", "base", "gadget", "kernel", "core", "snapd"}
	validSnapMode      = regexp.MustCompile("^[a-z][-a-z]+$")
	validSnapPresences = []string{"required", "optional"}
)

func isEssentialSnap(snapName, snapType, modelBase string) bool {
	switch snapType {
	case "snapd", "kernel", "gadget":
		return true
	}
	if snapName == modelBase {
		return true
	}
	return false
}

func checkModesForSnap(snap map[string]interface{}, isEssential bool, what string) ([]string, error) {
	modes := mylog.Check2(checkStringListInMap(snap, "modes", fmt.Sprintf("%q %s", "modes", what),
		validSnapMode))

	if isEssential {
		if len(modes) != 0 {
			return nil, fmt.Errorf("essential snaps are always available, cannot specify modes %s", what)
		}
		modes = essentialSnapModes
	}

	if len(modes) == 0 {
		modes = defaultModes
	}

	return modes, nil
}

func checkModelSnap(snap map[string]interface{}, modelBase string, grade ModelGrade, modelIsClassic bool) (*ModelSnap, error) {
	name := mylog.Check2(checkNotEmptyStringWhat(snap, "name", "of snap"))
	mylog.Check(naming.ValidateSnap(name))

	what := fmt.Sprintf("of snap %q", name)

	var snapID string
	_, ok := snap["id"]
	if ok {
		snapID = mylog.Check2(checkStringMatchesWhat(snap, "id", what, naming.ValidSnapID))
	} else {
		// snap ids are optional with grade dangerous to allow working
		// with local/not pushed yet to the store snaps
		if grade != ModelDangerous {
			return nil, fmt.Errorf(`"id" %s is mandatory for %s grade model`, what, grade)
		}
	}

	typ := mylog.Check2(checkOptionalStringWhat(snap, "type", what))

	if typ == "" {
		typ = "app"
	}
	if !strutil.ListContains(validSnapTypes, typ) {
		return nil, fmt.Errorf("type of snap %q must be one of %s", name, strings.Join(validSnapTypes, "|"))
	}

	presence := mylog.Check2(checkOptionalStringWhat(snap, "presence", what))

	if presence != "" && !strutil.ListContains(validSnapPresences, presence) {
		return nil, fmt.Errorf("presence of snap %q must be one of required|optional", name)
	}
	essential := isEssentialSnap(name, typ, modelBase)
	if essential && presence != "" {
		return nil, fmt.Errorf("essential snaps are always available, cannot specify presence for snap %q", name)
	}
	if presence == "" {
		presence = "required"
	}

	modes := mylog.Check2(checkModesForSnap(snap, essential, what))

	defaultChannel := mylog.Check2(checkOptionalStringWhat(snap, "default-channel", what))

	if defaultChannel == "" {
		defaultChannel = "latest/stable"
	}
	defCh := mylog.Check2(channel.ParseVerbatim(defaultChannel, "-"))

	if defCh.Track == "" {
		return nil, fmt.Errorf("default channel for snap %q must specify a track", name)
	}

	isClassic := mylog.Check2(checkOptionalBoolWhat(snap, "classic", what))

	if isClassic && !modelIsClassic {
		return nil, fmt.Errorf("snap %q cannot be classic in non-classic model", name)
	}
	if isClassic && typ != "app" {
		return nil, fmt.Errorf("snap %q cannot be classic with type %q instead of app", name, typ)
	}
	if isClassic && (len(modes) != 1 || modes[0] != "run") {
		return nil, fmt.Errorf("classic snap %q not allowed outside of run mode: %v",
			name, modes)
	}

	components := mylog.Check2(checkComponentsForMaps(snap, modes, what))

	return &ModelSnap{
		Name:           name,
		SnapID:         snapID,
		SnapType:       typ,
		Modes:          modes, // can be empty
		DefaultChannel: defaultChannel,
		Presence:       presence, // can be empty
		Classic:        isClassic,
		Components:     components, // can be empty
	}, nil
}

// This is what we expect for components:
/**
snaps:
 - name: <snap-name>
   ...
   presence: "optional"|"required" # optional, defaults to "required"
   modes:    [<mode-specifier>]    # list of modes
   components:             	     # optional
      <component-name-1>:
         presence: "optional"|"required"
         modes:    [<mode-specifier>] # list of modes, optional
                                      # must be a subset of snap modes
                                      # defaults to the same modes
                                      # as the snap
      <component-name-2>: "required"|"optional" # presence, shortcut syntax
**/
func checkComponentsForMaps(m map[string]interface{}, validModes []string, what string) (map[string]ModelComponent, error) {
	const compsField = "components"
	value, ok := m[compsField]
	if !ok {
		return nil, nil
	}
	comps, ok := value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%q %s must be a map from strings to components",
			compsField, what)
	}

	res := make(map[string]ModelComponent, len(comps))
	for name, comp := range comps {
		mylog.Check(
			// Name of component follows the same rules as snap components
			naming.ValidateSnap(name))

		// "comp: required|optional" case
		compWhat := fmt.Sprintf("of component %q %s", name, what)
		presence, ok := comp.(string)
		if ok {
			if !strutil.ListContains(validSnapPresences, presence) {
				return nil, fmt.Errorf("presence %s must be one of required|optional", compWhat)
			}
			res[name] = ModelComponent{
				Presence: presence,
				Modes:    append([]string(nil), validModes...),
			}
			continue
		}

		// try map otherwise
		compFields, ok := comp.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("%s must be a map of strings to components or one of required|optional",
				compWhat)
		}
		// Error out if unexpected entry
		for key := range compFields {
			if !strutil.ListContains([]string{"presence", "modes"}, key) {
				return nil, fmt.Errorf("entry %q %s is unknown", key, compWhat)
			}
		}
		presence := mylog.Check2(checkNotEmptyStringWhat(compFields, "presence", compWhat))

		if !strutil.ListContains(validSnapPresences, presence) {
			return nil, fmt.Errorf("presence %s must be one of required|optional", compWhat)
		}
		modes := mylog.Check2(checkStringListInMap(compFields, "modes",
			fmt.Sprintf("modes %s", compWhat), validSnapMode))

		if len(modes) == 0 {
			modes = append([]string(nil), validModes...)
		} else {
			for _, m := range modes {
				if !strutil.ListContains(validModes, m) {
					return nil, fmt.Errorf("mode %q %s is incompatible with the snap modes", m, compWhat)
				}
			}
		}
		res[name] = ModelComponent{Presence: presence, Modes: modes}
	}

	return res, nil
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
	mylog.Check(validateSnapName(name, which))

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

	return &ModelSnap{
		Name:        name,
		SnapType:    which,
		Modes:       defaultModes,
		PinnedTrack: track,
		Presence:    "required",
	}, nil
}

func validateSnapName(name string, headerName string) error {
	mylog.Check(naming.ValidateSnap(name))

	return nil
}

func checkRequiredSnap(name string, headerName string, snapType string) (*ModelSnap, error) {
	mylog.Check(validateSnapName(name, headerName))

	return &ModelSnap{
		Name:     name,
		SnapType: snapType,
		Modes:    defaultModes,
		Presence: "required",
	}, nil
}

// ModelGrade characterizes the security of the model which then
// controls related policy.
type ModelGrade string

const (
	ModelGradeUnset ModelGrade = "unset"
	// ModelSecured implies mandatory full disk encryption and secure boot.
	ModelSecured ModelGrade = "secured"
	// ModelSigned implies all seed snaps are signed and mentioned
	// in the model, i.e. no unasserted or extra snaps.
	ModelSigned ModelGrade = "signed"
	// ModelDangerous allows unasserted snaps and extra snaps.
	ModelDangerous ModelGrade = "dangerous"
)

// StorageSafety characterizes the requested storage safety of
// the model which then controls what encryption is used
type StorageSafety string

const (
	StorageSafetyUnset StorageSafety = "unset"
	// StorageSafetyEncrypted implies mandatory full disk encryption.
	StorageSafetyEncrypted StorageSafety = "encrypted"
	// StorageSafetyPreferEncrypted implies full disk
	// encryption when the system supports it.
	StorageSafetyPreferEncrypted StorageSafety = "prefer-encrypted"
	// StorageSafetyPreferUnencrypted implies no full disk
	// encryption by default even if the system supports
	// encryption.
	StorageSafetyPreferUnencrypted StorageSafety = "prefer-unencrypted"
)

var validStorageSafeties = []string{string(StorageSafetyEncrypted), string(StorageSafetyPreferEncrypted), string(StorageSafetyPreferUnencrypted)}

var validModelGrades = []string{string(ModelSecured), string(ModelSigned), string(ModelDangerous)}

// gradeToCode encodes grades into 32 bits, trying to be slightly future-proof:
//   - lower 16 bits are reserved
//   - in the higher bits use the sequence 1, 8, 16 to have some space
//     to possibly add new grades in between
var gradeToCode = map[ModelGrade]uint32{
	ModelGradeUnset: 0,
	ModelDangerous:  0x10000,
	ModelSigned:     0x80000,
	ModelSecured:    0x100000,
	// reserved by secboot to measure classic models
	// "ClassicModelGradeMask": 0x80000000
}

// Code returns a bit representation of the grade, for example for
// measuring it in a full disk encryption implementation.
func (mg ModelGrade) Code() uint32 {
	code, ok := gradeToCode[mg]
	if !ok {
		panic(fmt.Sprintf("unknown model grade: %s", mg))
	}
	return code
}

type ModelValidationSetMode string

const (
	ModelValidationSetModePreferEnforced ModelValidationSetMode = "prefer-enforce"
	ModelValidationSetModeEnforced       ModelValidationSetMode = "enforce"
)

var validModelValidationSetModes = []string{
	string(ModelValidationSetModePreferEnforced),
	string(ModelValidationSetModeEnforced),
}

// ModelValidationSet represents a reference to a validation set assertion.
// The structure also describes how the validation set will be applied
// to the device, and whether the validation set should be pinned to
// a specific sequence.
type ModelValidationSet struct {
	// AccountID is the account ID the validation set originates from.
	// If this was not explicitly set in the stanza, this will instead
	// be set to the brand ID.
	AccountID string
	// Name is the name of the validation set from the account ID.
	Name string
	// Sequence, if non-zero, specifies that the validation set should be
	// pinned at this sequence number.
	Sequence int
	// Mode is the enforcement mode the validation set should be applied with.
	Mode ModelValidationSetMode
}

// SequenceKey returns the sequence key for this validation set.
func (mvs *ModelValidationSet) SequenceKey() string {
	return vsSequenceKey(release.Series, mvs.AccountID, mvs.Name)
}

func (mvs *ModelValidationSet) AtSequence() *AtSequence {
	return &AtSequence{
		Type:        ValidationSetType,
		SequenceKey: []string{release.Series, mvs.AccountID, mvs.Name},
		Sequence:    mvs.Sequence,
		Pinned:      mvs.Sequence > 0,
		Revision:    RevisionNotKnown,
	}
}

// Model holds a model assertion, which is a statement by a brand
// about the properties of a device model.
type Model struct {
	assertionBase
	classic bool

	baseSnap   *ModelSnap
	gadgetSnap *ModelSnap
	kernelSnap *ModelSnap

	grade ModelGrade

	storageSafety StorageSafety

	allSnaps []*ModelSnap
	// consumers of this info should care only about snap identity =>
	// snapRef
	requiredWithEssentialSnaps []naming.SnapRef
	numEssentialSnaps          int

	validationSets []*ModelValidationSet

	serialAuthority  []string
	sysUserAuthority []string
	preseedAuthority []string
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

// Distribution returns the linux distro specified in the model.
func (mod *Model) Distribution() string {
	return mod.HeaderString("distribution")
}

// Architecture returns the architecture the model is based on.
func (mod *Model) Architecture() string {
	return mod.HeaderString("architecture")
}

// Grade returns the stability grade of the model. Will be ModelGradeUnset
// for Core 16/18 models.
func (mod *Model) Grade() ModelGrade {
	return mod.grade
}

// StorageSafety returns the storage safety for the model. Will be
// StorageSafetyUnset for Core 16/18 models.
func (mod *Model) StorageSafety() StorageSafety {
	return mod.storageSafety
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
	return mod.gadgetSnap.PinnedTrack
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
	return mod.kernelSnap.PinnedTrack
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

// RequiredNoEssentialSnaps returns the snaps that must be installed at all times and cannot be removed for this model, excluding the essential snaps (gadget, kernel, boot base, snapd).
func (mod *Model) RequiredNoEssentialSnaps() []naming.SnapRef {
	return mod.requiredWithEssentialSnaps[mod.numEssentialSnaps:]
}

// RequiredWithEssentialSnaps returns the snaps that must be installed at all times and cannot be removed for this model, including any essential snaps (gadget, kernel, boot base, snapd).
func (mod *Model) RequiredWithEssentialSnaps() []naming.SnapRef {
	return mod.requiredWithEssentialSnaps
}

// EssentialSnaps returns all essential snaps explicitly mentioned by
// the model.
// They are always returned according to this order with some skipped
// if not mentioned: snapd, kernel, boot base, gadget.
func (mod *Model) EssentialSnaps() []*ModelSnap {
	return mod.allSnaps[:mod.numEssentialSnaps]
}

// SnapsWithoutEssential returns all the snaps listed by the model
// without any of the essential snaps (as returned by EssentialSnaps).
// They are returned in the order of mention by the model.
func (mod *Model) SnapsWithoutEssential() []*ModelSnap {
	return mod.allSnaps[mod.numEssentialSnaps:]
}

// AllSnaps returns all the snaps listed by the model, across all modes.
// Essential snaps are at the front of the slice, followed by the non-essential
// snaps. The essential snaps follow the same order as returned by
// EssentialSnaps. The non-essential snaps are returned in the order they are
// mentioned in the model.
func (mod *Model) AllSnaps() []*ModelSnap {
	return mod.allSnaps
}

// ValidationSets returns all the validation-sets listed by the model.
func (mod *Model) ValidationSets() []*ModelValidationSet {
	return mod.validationSets
}

// SerialAuthority returns the authority ids that are accepted as
// signers for serial assertions for this model. It always includes the
// brand of the model.
func (mod *Model) SerialAuthority() []string {
	return mod.serialAuthority
}

// SystemUserAuthority returns the authority ids that are accepted as
// signers of system-user assertions for this model. Empty list means
// any, otherwise it always includes the brand of the model.
func (mod *Model) SystemUserAuthority() []string {
	return mod.sysUserAuthority
}

// PreseedAuthority returns the authority ids that are accepted as
// signers of the preseed binary blob for this model. It always includes the
// brand of the model.
func (mod *Model) PreseedAuthority() []string {
	return mod.preseedAuthority
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

// expected interface is implemented
var _ consistencyChecker = (*Model)(nil)

// limit model to only lowercase for now
var validModel = regexp.MustCompile("^[a-zA-Z0-9](?:-?[a-zA-Z0-9])*$")

func checkModel(headers map[string]interface{}) (string, error) {
	s := mylog.Check2(checkStringMatches(headers, "model", validModel))

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

func checkOptionalAuthority(headers map[string]interface{}, name string, brandID string, acceptsWildcard bool) ([]string, error) {
	ids := []string{brandID}
	v, ok := headers[name]
	if !ok {
		return ids, nil
	}
	switch x := v.(type) {
	case string:
		if acceptsWildcard && x == "*" {
			return nil, nil
		}
	case []interface{}:
		lst := mylog.Check2(checkStringListMatches(headers, name, validAccountID))
		if err == nil {
			if !strutil.ListContains(lst, brandID) {
				lst = append(ids, lst...)
			}
			return lst, nil
		}
	}

	if acceptsWildcard {
		return nil, fmt.Errorf("%q header must be '*' or a list of account ids", name)
	} else {
		return nil, fmt.Errorf("%q header must be a list of account ids", name)
	}
}

func checkOptionalSerialAuthority(headers map[string]interface{}, brandID string) ([]string, error) {
	const acceptsWildcard = false
	return checkOptionalAuthority(headers, "serial-authority", brandID, acceptsWildcard)
}

func checkOptionalSystemUserAuthority(headers map[string]interface{}, brandID string) ([]string, error) {
	const acceptsWildcard = true
	return checkOptionalAuthority(headers, "system-user-authority", brandID, acceptsWildcard)
}

func checkOptionalPreseedAuthority(headers map[string]interface{}, brandID string) ([]string, error) {
	const acceptsWildcard = false
	return checkOptionalAuthority(headers, "preseed-authority", brandID, acceptsWildcard)
}

func checkModelValidationSetAccountID(headers map[string]interface{}, what, brandID string) (string, error) {
	accountID := mylog.Check2(checkOptionalStringWhat(headers, "account-id", what))

	// default to brand ID if account ID is not provided
	if accountID == "" {
		return brandID, nil
	}
	return accountID, nil
}

// checkOptionalModelValidationSetSequence reads the optional 'sequence' member, if
// not set, returns 0 as this means unpinned. Unfortunately we are not able
// to reuse `checkSequence` as it operates inside different parameters.
func checkOptionalModelValidationSetSequence(headers map[string]interface{}, what string) (int, error) {
	// Default to 0 when the sequence header is not present
	if _, ok := headers["sequence"]; !ok {
		return 0, nil
	}

	seq := mylog.Check2(checkIntWhat(headers, "sequence", what))

	// If sequence is provided, only accept positive values above 0
	if seq <= 0 {
		return 0, fmt.Errorf("\"sequence\" %s must be larger than 0 or left unspecified (meaning tracking latest)", what)
	}
	return seq, nil
}

func checkModelValidationSetMode(headers map[string]interface{}, what string) (ModelValidationSetMode, error) {
	modeStr := mylog.Check2(checkNotEmptyStringWhat(headers, "mode", what))

	if modeStr != "" && !strutil.ListContains(validModelValidationSetModes, modeStr) {
		return "", fmt.Errorf("\"mode\" %s must be %s, not %q", what, strings.Join(validModelValidationSetModes, "|"), modeStr)
	}
	return ModelValidationSetMode(modeStr), nil
}

func checkModelValidationSet(headers map[string]interface{}, brandID string) (*ModelValidationSet, error) {
	name := mylog.Check2(checkStringMatchesWhat(headers, "name", "of validation-set", validValidationSetName))

	what := fmt.Sprintf("of validation-set %q", name)
	accountID := mylog.Check2(checkModelValidationSetAccountID(headers, what, brandID))

	what = fmt.Sprintf("of validation-set \"%s/%s\"", accountID, name)
	seq := mylog.Check2(checkOptionalModelValidationSetSequence(headers, what))

	mode := mylog.Check2(checkModelValidationSetMode(headers, what))

	return &ModelValidationSet{
		AccountID: accountID,
		Name:      name,
		Sequence:  seq,
		Mode:      mode,
	}, nil
}

func checkOptionalModelValidationSets(headers map[string]interface{}, brandID string) ([]*ModelValidationSet, error) {
	valSets, ok := headers["validation-sets"]
	if !ok {
		return nil, nil
	}

	entries, ok := valSets.([]interface{})
	if !ok {
		return nil, fmt.Errorf(`"validation-sets" must be a list of validation sets`)
	}

	vss := make([]*ModelValidationSet, len(entries))
	seen := make(map[string]bool, len(entries))
	for i, entry := range entries {
		data, ok := entry.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf(`entry in "validation-sets" is not a valid validation-set`)
		}

		vs := mylog.Check2(checkModelValidationSet(data, brandID))

		vsKey := fmt.Sprintf("%s/%s", vs.AccountID, vs.Name)
		if seen[vsKey] {
			return nil, fmt.Errorf("cannot add validation set %q twice", vsKey)
		}

		vss[i] = vs
		seen[vsKey] = true
	}
	return vss, nil
}

var (
	modelMandatory           = []string{"architecture", "gadget", "kernel"}
	extendedMandatory        = []string{"architecture", "base"}
	extendedSnapsConflicting = []string{"gadget", "kernel", "required-snaps"}
	classicModelOptional     = []string{"architecture", "gadget"}

	// The distribution header must be a valid ID according to
	// https://www.freedesktop.org/software/systemd/man/os-release.html#ID=
	validDistribution = regexp.MustCompile(`^[a-z0-9._-]*$`)
)

func assembleModel(assert assertionBase) (Assertion, error) {
	mylog.Check(checkAuthorityMatchesBrand(&assert))

	_ = mylog.Check2(checkModel(assert.headers))

	classic := mylog.Check2(checkOptionalBool(assert.headers, "classic"))

	// Core 20 extended snaps header
	extendedSnaps, extended := assert.headers["snaps"]
	if extended {
		for _, conflicting := range extendedSnapsConflicting {
			if _, ok := assert.headers[conflicting]; ok {
				return nil, fmt.Errorf("cannot specify separate %q header once using the extended snaps header", conflicting)
			}
		}
	} else {
		if _, ok := assert.headers["grade"]; ok {
			return nil, fmt.Errorf("cannot specify a grade for model without the extended snaps header")
		}
		if _, ok := assert.headers["storage-safety"]; ok {
			return nil, fmt.Errorf("cannot specify storage-safety for model without the extended snaps header")
		}
	}

	if classic && !extended {
		if _, ok := assert.headers["kernel"]; ok {
			return nil, fmt.Errorf("cannot specify a kernel with a non-extended classic model")
		}
		if _, ok := assert.headers["base"]; ok {
			return nil, fmt.Errorf("cannot specify a base with a non-extended classic model")
		}
	}

	// distribution mandatory for classic with extended snaps, not
	// allowed otherwise.
	if classic && extended {
		_ := mylog.Check2(checkStringMatches(assert.headers, "distribution", validDistribution))
	} else if _, ok := assert.headers["distribution"]; ok {
		return nil, fmt.Errorf("cannot specify distribution for model unless it is classic and has an extended snaps header")
	}

	checker := checkNotEmptyString
	toCheck := modelMandatory
	if extended {
		toCheck = extendedMandatory
	} else if classic {
		checker = checkOptionalString
		toCheck = classicModelOptional
	}

	for _, h := range toCheck {
		mylog.Check2(checker(assert.headers, h))
	}

	// base, if provided, must be a valid snap name too
	var baseSnap *ModelSnap
	base := mylog.Check2(checkOptionalString(assert.headers, "base"))

	if base != "" {
		baseSnap = mylog.Check2(checkRequiredSnap(base, "base", "base"))
	}
	mylog.Check2(

		// store is optional but must be a string, defaults to the ubuntu store
		checkOptionalString(assert.headers, "store"))
	mylog.Check2(

		// display-name is optional but must be a string
		checkOptionalString(assert.headers, "display-name"))

	var modSnaps *modelSnaps
	grade := ModelGradeUnset
	storageSafety := StorageSafetyUnset
	if extended {
		gradeStr := mylog.Check2(checkOptionalString(assert.headers, "grade"))

		if gradeStr != "" && !strutil.ListContains(validModelGrades, gradeStr) {
			return nil, fmt.Errorf("grade for model must be %s, not %q", strings.Join(validModelGrades, "|"), gradeStr)
		}
		grade = ModelSigned
		if gradeStr != "" {
			grade = ModelGrade(gradeStr)
		}

		storageSafetyStr := mylog.Check2(checkOptionalString(assert.headers, "storage-safety"))

		if storageSafetyStr != "" && !strutil.ListContains(validStorageSafeties, storageSafetyStr) {
			return nil, fmt.Errorf("storage-safety for model must be %s, not %q", strings.Join(validStorageSafeties, "|"), storageSafetyStr)
		}
		if storageSafetyStr != "" {
			storageSafety = StorageSafety(storageSafetyStr)
		} else {
			if grade == ModelSecured {
				storageSafety = StorageSafetyEncrypted
			} else {
				storageSafety = StorageSafetyPreferEncrypted
			}
		}

		if grade == ModelSecured && storageSafety != StorageSafetyEncrypted {
			return nil, fmt.Errorf(`secured grade model must not have storage-safety overridden, only "encrypted" is valid`)
		}

		modSnaps = mylog.Check2(checkExtendedSnaps(extendedSnaps, base, grade, classic))

		hasKernel := modSnaps.kernel != nil
		hasGadget := modSnaps.gadget != nil
		if !classic {
			if !hasGadget {
				return nil, fmt.Errorf(`one "snaps" header entry must specify the model gadget`)
			}
			if !hasKernel {
				return nil, fmt.Errorf(`one "snaps" header entry must specify the model kernel`)
			}
		} else {
			if hasKernel && !hasGadget {
				return nil, fmt.Errorf("cannot specify a kernel in an extended classic model without a model gadget")
			}
		}

		if modSnaps.base == nil {
			// complete with defaults,
			// the assumption is that base names are very stable
			// essentially fixed
			modSnaps.base = baseSnap
			snapID := naming.WellKnownSnapID(modSnaps.base.Name)
			if snapID == "" && grade != ModelDangerous {
				return nil, fmt.Errorf(`cannot specify not well-known base %q without a corresponding "snaps" header entry`, modSnaps.base.Name)
			}
			modSnaps.base.SnapID = snapID
			modSnaps.base.Modes = essentialSnapModes
			modSnaps.base.DefaultChannel = "latest/stable"
		}
	} else {
		modSnaps = &modelSnaps{
			base: baseSnap,
		}
		// kernel/gadget must be valid snap names and can have (optional) tracks
		// - validate those
		modSnaps.kernel = mylog.Check2(checkSnapWithTrack(assert.headers, "kernel"))

		modSnaps.gadget = mylog.Check2(checkSnapWithTrack(assert.headers, "gadget"))

		// required snap must be valid snap names
		reqSnaps := mylog.Check2(checkStringList(assert.headers, "required-snaps"))

		for _, name := range reqSnaps {
			reqSnap := mylog.Check2(checkRequiredSnap(name, "required-snaps", ""))

			modSnaps.snapsNoEssential = append(modSnaps.snapsNoEssential, reqSnap)
		}
	}

	brandID := assert.HeaderString("brand-id")

	serialAuthority := mylog.Check2(checkOptionalSerialAuthority(assert.headers, brandID))

	sysUserAuthority := mylog.Check2(checkOptionalSystemUserAuthority(assert.headers, brandID))

	preseedAuthority := mylog.Check2(checkOptionalPreseedAuthority(assert.headers, brandID))

	timestamp := mylog.Check2(checkRFC3339Date(assert.headers, "timestamp"))

	allSnaps, requiredWithEssentialSnaps, numEssentialSnaps := modSnaps.list()

	valSets := mylog.Check2(checkOptionalModelValidationSets(assert.headers, brandID))

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
		grade:                      grade,
		storageSafety:              storageSafety,
		allSnaps:                   allSnaps,
		requiredWithEssentialSnaps: requiredWithEssentialSnaps,
		numEssentialSnaps:          numEssentialSnaps,
		validationSets:             valSets,
		serialAuthority:            serialAuthority,
		sysUserAuthority:           sysUserAuthority,
		preseedAuthority:           preseedAuthority,
		timestamp:                  timestamp,
	}, nil
}
