// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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
	"bytes"
	"crypto"
	"fmt"
	"regexp"
	"time"

	_ "golang.org/x/crypto/sha3" // expected for digests

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
)

// SnapDeclaration holds a snap-declaration assertion, declaring a
// snap binding its identifying snap-id to a name, asserting its
// publisher and its other properties.
type SnapDeclaration struct {
	assertionBase
	refreshControl []string
	plugRules      map[string]*PlugRule
	slotRules      map[string]*SlotRule
	autoAliases    []string
	timestamp      time.Time
}

// Series returns the series for which the snap is being declared.
func (snapdcl *SnapDeclaration) Series() string {
	return snapdcl.HeaderString("series")
}

// SnapID returns the snap id of the declared snap.
func (snapdcl *SnapDeclaration) SnapID() string {
	return snapdcl.HeaderString("snap-id")
}

// SnapName returns the declared snap name.
func (snapdcl *SnapDeclaration) SnapName() string {
	return snapdcl.HeaderString("snap-name")
}

// PublisherID returns the identifier of the publisher of the declared snap.
func (snapdcl *SnapDeclaration) PublisherID() string {
	return snapdcl.HeaderString("publisher-id")
}

// Timestamp returns the time when the snap-declaration was issued.
func (snapdcl *SnapDeclaration) Timestamp() time.Time {
	return snapdcl.timestamp
}

// RefreshControl returns the ids of snaps whose updates are controlled by this declaration.
func (snapdcl *SnapDeclaration) RefreshControl() []string {
	return snapdcl.refreshControl
}

// PlugRule returns the plug-side rule about the given interface if one was included in the plugs stanza of the declaration, otherwise it returns nil.
func (snapdcl *SnapDeclaration) PlugRule(interfaceName string) *PlugRule {
	return snapdcl.plugRules[interfaceName]
}

// SlotRule returns the slot-side rule about the given interface if one was included in the slots stanza of the declaration, otherwise it returns nil.
func (snapdcl *SnapDeclaration) SlotRule(interfaceName string) *SlotRule {
	return snapdcl.slotRules[interfaceName]
}

// AutoAliases returns the optional auto-aliases granted to this snap.
func (snapdcl *SnapDeclaration) AutoAliases() []string {
	return snapdcl.autoAliases
}

// Implement further consistency checks.
func (snapdcl *SnapDeclaration) checkConsistency(db RODatabase, acck *AccountKey) error {
	if !db.IsTrustedAccount(snapdcl.AuthorityID()) {
		return fmt.Errorf("snap-declaration assertion for %q (id %q) is not signed by a directly trusted authority: %s", snapdcl.SnapName(), snapdcl.SnapID(), snapdcl.AuthorityID())
	}
	_, err := db.Find(AccountType, map[string]string{
		"account-id": snapdcl.PublisherID(),
	})
	if err == ErrNotFound {
		return fmt.Errorf("snap-declaration assertion for %q (id %q) does not have a matching account assertion for the publisher %q", snapdcl.SnapName(), snapdcl.SnapID(), snapdcl.PublisherID())
	}
	if err != nil {
		return err
	}

	return nil
}

// sanity
var _ consistencyChecker = (*SnapDeclaration)(nil)

// Prerequisites returns references to this snap-declaration's prerequisite assertions.
func (snapdcl *SnapDeclaration) Prerequisites() []*Ref {
	return []*Ref{
		{Type: AccountType, PrimaryKey: []string{snapdcl.PublisherID()}},
	}
}

func snapDeclarationFormatAnalyze(headers map[string]interface{}, body []byte) (formatnum int, err error) {
	_, plugsOk := headers["plugs"]
	_, slotsOk := headers["slots"]
	if plugsOk || slotsOk {
		return 1, nil
	}
	return 0, nil
}

var validAlias = regexp.MustCompile("^[a-zA-Z0-9][-_.a-zA-Z0-9]*$")

func assembleSnapDeclaration(assert assertionBase) (Assertion, error) {
	_, err := checkExistsString(assert.headers, "snap-name")
	if err != nil {
		return nil, err
	}

	_, err = checkNotEmptyString(assert.headers, "publisher-id")
	if err != nil {
		return nil, err
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	var refControl []string
	var plugRules map[string]*PlugRule
	var slotRules map[string]*SlotRule

	refControl, err = checkStringList(assert.headers, "refresh-control")
	if err != nil {
		return nil, err
	}

	plugs, err := checkMap(assert.headers, "plugs")
	if err != nil {
		return nil, err
	}
	if plugs != nil {
		plugRules = make(map[string]*PlugRule, len(plugs))
		for iface, rule := range plugs {
			plugRule, err := compilePlugRule(iface, rule)
			if err != nil {
				return nil, err
			}
			plugRules[iface] = plugRule
		}
	}

	slots, err := checkMap(assert.headers, "slots")
	if err != nil {
		return nil, err
	}
	if slots != nil {
		slotRules = make(map[string]*SlotRule, len(slots))
		for iface, rule := range slots {
			slotRule, err := compileSlotRule(iface, rule)
			if err != nil {
				return nil, err
			}
			slotRules[iface] = slotRule
		}
	}

	autoAliases, err := checkStringListMatches(assert.headers, "auto-aliases", validAlias)
	if err != nil {
		return nil, err
	}

	return &SnapDeclaration{
		assertionBase:  assert,
		refreshControl: refControl,
		plugRules:      plugRules,
		slotRules:      slotRules,
		autoAliases:    autoAliases,
		timestamp:      timestamp,
	}, nil
}

// SnapFileSHA3_384 computes the SHA3-384 digest of the given snap file.
// It also returns its size.
func SnapFileSHA3_384(snapPath string) (digest string, size uint64, err error) {
	sha3_384Dgst, size, err := osutil.FileDigest(snapPath, crypto.SHA3_384)
	if err != nil {
		return "", 0, fmt.Errorf("cannot compute snap %q digest: %v", snapPath, err)
	}

	sha3_384, err := EncodeDigest(crypto.SHA3_384, sha3_384Dgst)
	if err != nil {
		return "", 0, fmt.Errorf("cannot encode snap %q digest: %v", snapPath, err)
	}
	return sha3_384, size, nil
}

// SnapBuild holds a snap-build assertion, asserting the properties of a snap
// at the time it was built by the developer.
type SnapBuild struct {
	assertionBase
	size      uint64
	timestamp time.Time
}

// SnapSHA3_384 returns the SHA3-384 digest of the snap.
func (snapbld *SnapBuild) SnapSHA3_384() string {
	return snapbld.HeaderString("snap-sha3-384")
}

// SnapID returns the snap id of the snap.
func (snapbld *SnapBuild) SnapID() string {
	return snapbld.HeaderString("snap-id")
}

// SnapSize returns the size of the snap.
func (snapbld *SnapBuild) SnapSize() uint64 {
	return snapbld.size
}

// Grade returns the grade of the snap: devel|stable
func (snapbld *SnapBuild) Grade() string {
	return snapbld.HeaderString("grade")
}

// Timestamp returns the time when the snap-build assertion was created.
func (snapbld *SnapBuild) Timestamp() time.Time {
	return snapbld.timestamp
}

func assembleSnapBuild(assert assertionBase) (Assertion, error) {
	_, err := checkDigest(assert.headers, "snap-sha3-384", crypto.SHA3_384)
	if err != nil {
		return nil, err
	}

	_, err = checkNotEmptyString(assert.headers, "snap-id")
	if err != nil {
		return nil, err
	}

	_, err = checkNotEmptyString(assert.headers, "grade")
	if err != nil {
		return nil, err
	}

	size, err := checkUint(assert.headers, "snap-size", 64)
	if err != nil {
		return nil, err
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}
	// ignore extra headers and non-empty body for future compatibility
	return &SnapBuild{
		assertionBase: assert,
		size:          size,
		timestamp:     timestamp,
	}, nil
}

// SnapRevision holds a snap-revision assertion, which is a statement by the
// store acknowledging the receipt of a build of a snap and labeling it with a
// snap revision.
type SnapRevision struct {
	assertionBase
	snapSize     uint64
	snapRevision int
	timestamp    time.Time
}

// SnapSHA3_384 returns the SHA3-384 digest of the snap.
func (snaprev *SnapRevision) SnapSHA3_384() string {
	return snaprev.HeaderString("snap-sha3-384")
}

// SnapID returns the snap id of the snap.
func (snaprev *SnapRevision) SnapID() string {
	return snaprev.HeaderString("snap-id")
}

// SnapSize returns the size in bytes of the snap submitted to the store.
func (snaprev *SnapRevision) SnapSize() uint64 {
	return snaprev.snapSize
}

// SnapRevision returns the revision assigned to this build of the snap.
func (snaprev *SnapRevision) SnapRevision() int {
	return snaprev.snapRevision
}

// DeveloperID returns the id of the developer that submitted this build of the
// snap.
func (snaprev *SnapRevision) DeveloperID() string {
	return snaprev.HeaderString("developer-id")
}

// Timestamp returns the time when the snap-revision was issued.
func (snaprev *SnapRevision) Timestamp() time.Time {
	return snaprev.timestamp
}

// Implement further consistency checks.
func (snaprev *SnapRevision) checkConsistency(db RODatabase, acck *AccountKey) error {
	// TODO: expand this to consider other stores signing on their own
	if !db.IsTrustedAccount(snaprev.AuthorityID()) {
		return fmt.Errorf("snap-revision assertion for snap id %q is not signed by a store: %s", snaprev.SnapID(), snaprev.AuthorityID())
	}
	_, err := db.Find(AccountType, map[string]string{
		"account-id": snaprev.DeveloperID(),
	})
	if err == ErrNotFound {
		return fmt.Errorf("snap-revision assertion for snap id %q does not have a matching account assertion for the developer %q", snaprev.SnapID(), snaprev.DeveloperID())
	}
	if err != nil {
		return err
	}
	_, err = db.Find(SnapDeclarationType, map[string]string{
		// XXX: mediate getting current series through some context object? this gets the job done for now
		"series":  release.Series,
		"snap-id": snaprev.SnapID(),
	})
	if err == ErrNotFound {
		return fmt.Errorf("snap-revision assertion for snap id %q does not have a matching snap-declaration assertion", snaprev.SnapID())
	}
	if err != nil {
		return err
	}
	return nil
}

// sanity
var _ consistencyChecker = (*SnapRevision)(nil)

// Prerequisites returns references to this snap-revision's prerequisite assertions.
func (snaprev *SnapRevision) Prerequisites() []*Ref {
	return []*Ref{
		// XXX: mediate getting current series through some context object? this gets the job done for now
		{Type: SnapDeclarationType, PrimaryKey: []string{release.Series, snaprev.SnapID()}},
		{Type: AccountType, PrimaryKey: []string{snaprev.DeveloperID()}},
	}
}

func assembleSnapRevision(assert assertionBase) (Assertion, error) {
	_, err := checkDigest(assert.headers, "snap-sha3-384", crypto.SHA3_384)
	if err != nil {
		return nil, err
	}

	_, err = checkNotEmptyString(assert.headers, "snap-id")
	if err != nil {
		return nil, err
	}

	snapSize, err := checkUint(assert.headers, "snap-size", 64)
	if err != nil {
		return nil, err
	}

	snapRevision, err := checkInt(assert.headers, "snap-revision")
	if err != nil {
		return nil, err
	}
	if snapRevision < 1 {
		return nil, fmt.Errorf(`"snap-revision" header must be >=1: %d`, snapRevision)
	}

	_, err = checkNotEmptyString(assert.headers, "developer-id")
	if err != nil {
		return nil, err
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	return &SnapRevision{
		assertionBase: assert,
		snapSize:      snapSize,
		snapRevision:  snapRevision,
		timestamp:     timestamp,
	}, nil
}

// Validation holds a validation assertion, describing that a combination of
// (snap-id, approved-snap-id, approved-revision) has been validated for
// the series, meaning updating to that revision of approved-snap-id
// has been approved by the owner of the gating snap with snap-id.
type Validation struct {
	assertionBase
	revoked              bool
	timestamp            time.Time
	approvedSnapRevision int
}

// Series returns the series for which the validation holds.
func (validation *Validation) Series() string {
	return validation.HeaderString("series")
}

// SnapID returns the ID of the gating snap.
func (validation *Validation) SnapID() string {
	return validation.HeaderString("snap-id")
}

// ApprovedSnapID returns the ID of the gated snap.
func (validation *Validation) ApprovedSnapID() string {
	return validation.HeaderString("approved-snap-id")
}

// ApprovedSnapRevision returns the approved revision of the gated snap.
func (validation *Validation) ApprovedSnapRevision() int {
	return validation.approvedSnapRevision
}

// Revoked returns true if the validation has been revoked.
func (validation *Validation) Revoked() bool {
	return validation.revoked
}

// Timestamp returns the time when the validation was issued.
func (validation *Validation) Timestamp() time.Time {
	return validation.timestamp
}

// Implement further consistency checks.
func (validation *Validation) checkConsistency(db RODatabase, acck *AccountKey) error {
	_, err := db.Find(SnapDeclarationType, map[string]string{
		"series":  validation.Series(),
		"snap-id": validation.ApprovedSnapID(),
	})
	if err == ErrNotFound {
		return fmt.Errorf("validation assertion by snap-id %q does not have a matching snap-declaration assertion for approved-snap-id %q", validation.SnapID(), validation.ApprovedSnapID())
	}
	if err != nil {
		return err
	}
	a, err := db.Find(SnapDeclarationType, map[string]string{
		"series":  validation.Series(),
		"snap-id": validation.SnapID(),
	})
	if err == ErrNotFound {
		return fmt.Errorf("validation assertion by snap-id %q does not have a matching snap-declaration assertion", validation.SnapID())
	}
	if err != nil {
		return err
	}

	gatingDecl := a.(*SnapDeclaration)
	if gatingDecl.PublisherID() != validation.AuthorityID() {
		return fmt.Errorf("validation assertion by snap %q (id %q) not signed by its publisher", gatingDecl.SnapName(), validation.SnapID())
	}

	return nil
}

// sanity
var _ consistencyChecker = (*Validation)(nil)

// Prerequisites returns references to this validation's prerequisite assertions.
func (validation *Validation) Prerequisites() []*Ref {
	return []*Ref{
		{Type: SnapDeclarationType, PrimaryKey: []string{validation.Series(), validation.SnapID()}},
		{Type: SnapDeclarationType, PrimaryKey: []string{validation.Series(), validation.ApprovedSnapID()}},
	}
}

func assembleValidation(assert assertionBase) (Assertion, error) {
	approvedSnapRevision, err := checkInt(assert.headers, "approved-snap-revision")
	if err != nil {
		return nil, err
	}
	if approvedSnapRevision < 1 {
		return nil, fmt.Errorf(`"approved-snap-revision" header must be >=1: %d`, approvedSnapRevision)
	}

	revoked, err := checkOptionalBool(assert.headers, "revoked")
	if err != nil {
		return nil, err
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	return &Validation{
		assertionBase:        assert,
		revoked:              revoked,
		timestamp:            timestamp,
		approvedSnapRevision: approvedSnapRevision,
	}, nil
}

// BaseDeclaration holds a base-declaration assertion, declaring the
// policies (to start with interface ones) applying to all snaps of
// a series.
type BaseDeclaration struct {
	assertionBase
	plugRules map[string]*PlugRule
	slotRules map[string]*SlotRule
	timestamp time.Time
}

// Series returns the series whose snaps are governed by the declaration.
func (basedcl *BaseDeclaration) Series() string {
	return basedcl.HeaderString("series")
}

// Timestamp returns the time when the base-declaration was issued.
func (basedcl *BaseDeclaration) Timestamp() time.Time {
	return basedcl.timestamp
}

// PlugRule returns the plug-side rule about the given interface if one was included in the plugs stanza of the declaration, otherwise it returns nil.
func (basedcl *BaseDeclaration) PlugRule(interfaceName string) *PlugRule {
	return basedcl.plugRules[interfaceName]
}

// SlotRule returns the slot-side rule about the given interface if one was included in the slots stanza of the declaration, otherwise it returns nil.
func (basedcl *BaseDeclaration) SlotRule(interfaceName string) *SlotRule {
	return basedcl.slotRules[interfaceName]
}

// Implement further consistency checks.
func (basedcl *BaseDeclaration) checkConsistency(db RODatabase, acck *AccountKey) error {
	// XXX: not signed or stored yet in a db, but being ready for that
	if !db.IsTrustedAccount(basedcl.AuthorityID()) {
		return fmt.Errorf("base-declaration assertion for series %s is not signed by a directly trusted authority: %s", basedcl.Series(), basedcl.AuthorityID())
	}
	return nil
}

// sanity
var _ consistencyChecker = (*BaseDeclaration)(nil)

func assembleBaseDeclaration(assert assertionBase) (Assertion, error) {
	var plugRules map[string]*PlugRule
	plugs, err := checkMap(assert.headers, "plugs")
	if err != nil {
		return nil, err
	}
	if plugs != nil {
		plugRules = make(map[string]*PlugRule, len(plugs))
		for iface, rule := range plugs {
			plugRule, err := compilePlugRule(iface, rule)
			if err != nil {
				return nil, err
			}
			plugRules[iface] = plugRule
		}
	}

	var slotRules map[string]*SlotRule
	slots, err := checkMap(assert.headers, "slots")
	if err != nil {
		return nil, err
	}
	if slots != nil {
		slotRules = make(map[string]*SlotRule, len(slots))
		for iface, rule := range slots {
			slotRule, err := compileSlotRule(iface, rule)
			if err != nil {
				return nil, err
			}
			slotRules[iface] = slotRule
		}
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	return &BaseDeclaration{
		assertionBase: assert,
		plugRules:     plugRules,
		slotRules:     slotRules,
		timestamp:     timestamp,
	}, nil
}

var builtinBaseDeclaration *BaseDeclaration

// BuiltinBaseDeclaration exposes the initialized builtin base-declaration assertion. This is used by overlord/assertstate, other code should use assertstate.BaseDeclaration.
func BuiltinBaseDeclaration() *BaseDeclaration {
	return builtinBaseDeclaration
}

var (
	builtinBaseDeclarationCheckOrder      = []string{"type", "authority-id", "series"}
	builtinBaseDeclarationExpectedHeaders = map[string]interface{}{
		"type":         "base-declaration",
		"authority-id": "canonical",
		"series":       release.Series,
	}
)

// InitBuiltinBaseDeclaration initializes the builtin base-declaration based on headers (or resets it if headers is nil).
func InitBuiltinBaseDeclaration(headers []byte) error {
	if headers == nil {
		builtinBaseDeclaration = nil
		return nil
	}
	trimmed := bytes.TrimSpace(headers)
	h, err := parseHeaders(trimmed)
	if err != nil {
		return err
	}
	for _, name := range builtinBaseDeclarationCheckOrder {
		expected := builtinBaseDeclarationExpectedHeaders[name]
		if h[name] != expected {
			return fmt.Errorf("the builtin base-declaration %q header is not set to expected value %q", name, expected)
		}
	}
	revision, err := checkRevision(h)
	if err != nil {
		return fmt.Errorf("cannot assemble the builtin-base declaration: %v", err)
	}
	h["timestamp"] = time.Now().UTC().Format(time.RFC3339)
	a, err := assembleBaseDeclaration(assertionBase{
		headers:   h,
		body:      nil,
		revision:  revision,
		content:   trimmed,
		signature: []byte("$builtin"),
	})
	if err != nil {
		return fmt.Errorf("cannot assemble the builtin base-declaration: %v", err)
	}
	builtinBaseDeclaration = a.(*BaseDeclaration)
	return nil
}
