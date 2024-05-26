// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2022 Canonical Ltd
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

// Package asserts implements snappy assertions and a database
// abstraction for managing and holding them.
package asserts

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/ddkwork/golibrary/mylog"
)

// NotFoundError is returned when an assertion can not be found.
type NotFoundError struct {
	Type    *AssertionType
	Headers map[string]string
}

func (e *NotFoundError) Error() string {
	pk := mylog.Check2(PrimaryKeyFromHeaders(e.Type, e.Headers))
	if err != nil || len(e.Headers) != len(pk) {
		// TODO: worth conveying more information?
		return fmt.Sprintf("%s assertion not found", e.Type.Name)
	}

	return fmt.Sprintf("%v not found", &Ref{Type: e.Type, PrimaryKey: pk})
}

func (e *NotFoundError) Is(err error) bool {
	_, ok := err.(*NotFoundError)
	return ok
}

// A Backstore stores assertions. It can store and retrieve assertions
// by type under unique primary key headers (whose names are available
// from assertType.PrimaryKey). Plus it supports searching by headers.
// Lookups can be limited to a maximum allowed format.
type Backstore interface {
	// Put stores an assertion.
	// It is responsible for checking that assert is newer than a
	// previously stored revision with the same primary key headers.
	Put(assertType *AssertionType, assert Assertion) error
	// Get returns the assertion with the given unique key for its
	// primary key headers.
	// A suffix of optional primary keys can be left out from key
	// in which case their default values are implied.
	// If the assertion is not present it returns a
	// NotFoundError, usually with omitted Headers.
	Get(assertType *AssertionType, key []string, maxFormat int) (Assertion, error)
	// Search returns assertions matching the given headers.
	// It invokes foundCb for each found assertion.
	Search(assertType *AssertionType, headers map[string]string, foundCb func(Assertion), maxFormat int) error
	// SequenceMemberAfter returns for a sequence-forming assertType the
	// first assertion in the sequence under the given sequenceKey
	// with sequence number larger than after. If after==-1 it
	// returns the assertion with largest sequence number. If none
	// exists it returns a NotFoundError, usually with omitted
	// Headers. If assertType is not sequence-forming it can
	// panic.
	SequenceMemberAfter(assertType *AssertionType, sequenceKey []string, after, maxFormat int) (SequenceMember, error)
}

type nullBackstore struct{}

func (nbs nullBackstore) Put(t *AssertionType, a Assertion) error {
	return fmt.Errorf("cannot store assertions without setting a proper assertion backstore implementation")
}

func (nbs nullBackstore) Get(t *AssertionType, k []string, maxFormat int) (Assertion, error) {
	return nil, &NotFoundError{Type: t}
}

func (nbs nullBackstore) Search(t *AssertionType, h map[string]string, f func(Assertion), maxFormat int) error {
	return nil
}

func (nbs nullBackstore) SequenceMemberAfter(t *AssertionType, kp []string, after, maxFormat int) (SequenceMember, error) {
	return nil, &NotFoundError{Type: t}
}

// keyNotFoundError is returned when the key with a given ID cannot be found.
type keyNotFoundError struct {
	msg string
}

func (e *keyNotFoundError) Error() string { return e.msg }

func (e *keyNotFoundError) Is(target error) bool {
	_, ok := target.(*keyNotFoundError)
	return ok
}

// IsKeyNotFound returns true when the error indicates that a given key was not
// found.
func IsKeyNotFound(err error) bool {
	return errors.Is(err, &keyNotFoundError{})
}

// A KeypairManager is a manager and backstore for private/public key pairs.
type KeypairManager interface {
	// Put stores the given private/public key pair,
	// making sure it can be later retrieved by its unique key id with Get.
	// Trying to store a key with an already present key id should
	// result in an error.
	Put(privKey PrivateKey) error
	// Get returns the private/public key pair with the given key id. The
	// error can be tested with IsKeyNotFound to check whether the given key
	// was not found, or other error occurred.
	Get(keyID string) (PrivateKey, error)
	// Delete deletes the private/public key pair with the given key id.
	Delete(keyID string) error
}

// DatabaseConfig for an assertion database.
type DatabaseConfig struct {
	// trusted set of assertions (account and account-key supported),
	// used to establish root keys and trusted authorities
	Trusted []Assertion
	// predefined assertions but that do not establish foundational trust
	OtherPredefined []Assertion
	// backstore for assertions, left unset storing assertions will error
	Backstore Backstore
	// manager/backstore for keypairs, defaults to in-memory implementation
	KeypairManager KeypairManager
	// assertion checkers used by Database.Check, left unset DefaultCheckers will be used which is recommended
	Checkers []Checker
}

// RevisionError indicates a revision improperly used for an operation.
type RevisionError struct {
	Used, Current int
}

func (e *RevisionError) Error() string {
	if e.Used < 0 || e.Current < 0 {
		// TODO: message may need tweaking once there's a use.
		return "assertion revision is unknown"
	}
	if e.Used == e.Current {
		return fmt.Sprintf("revision %d is already the current revision", e.Used)
	}
	if e.Used < e.Current {
		return fmt.Sprintf("revision %d is older than current revision %d", e.Used, e.Current)
	}
	return fmt.Sprintf("revision %d is more recent than current revision %d", e.Used, e.Current)
}

// UnsupportedFormatError indicates an assertion with a format iteration not yet supported by the present version of asserts.
type UnsupportedFormatError struct {
	Ref    *Ref
	Format int
	// Update marks there was already a current revision of the assertion and it has been kept.
	Update bool
}

func (e *UnsupportedFormatError) Error() string {
	postfx := ""
	if e.Update {
		postfx = " (current not updated)"
	}
	return fmt.Sprintf("proposed %q assertion has format %d but %d is latest supported%s", e.Ref.Type.Name, e.Format, e.Ref.Type.MaxSupportedFormat(), postfx)
}

// IsUnaccceptedUpdate returns whether the error indicates that an
// assertion revision was already present and has been kept because
// the update was not accepted.
func IsUnaccceptedUpdate(err error) bool {
	switch x := err.(type) {
	case *UnsupportedFormatError:
		return x.Update
	case *RevisionError:
		return x.Used <= x.Current
	}
	return false
}

// A RODatabase exposes read-only access to an assertion database.
type RODatabase interface {
	// IsTrustedAccount returns whether the account is part of the trusted set.
	IsTrustedAccount(accountID string) bool
	// Find an assertion based on arbitrary headers.
	// Provided headers must contain the primary key for the assertion type.
	// Optional primary key headers can be omitted in which case
	// their default values will be used.
	// It returns a NotFoundError if the assertion cannot be found.
	Find(assertionType *AssertionType, headers map[string]string) (Assertion, error)
	// FindPredefined finds an assertion in the predefined sets
	// (trusted or not) based on arbitrary headers.  Provided
	// headers must contain the primary key for the assertion
	// type.
	// Optional primary key headers can be omitted in which case
	// their default values will be used.
	// It returns a NotFoundError if the assertion cannot
	// be found.
	FindPredefined(assertionType *AssertionType, headers map[string]string) (Assertion, error)
	// FindTrusted finds an assertion in the trusted set based on
	// arbitrary headers.  Provided headers must contain the
	// primary key for the assertion type.
	// Optional primary key headers can be omitted in which case
	// their default values will be used.
	// It returns a NotFoundError if the assertion cannot be
	// found.
	FindTrusted(assertionType *AssertionType, headers map[string]string) (Assertion, error)
	// FindMany finds assertions based on arbitrary headers.
	// It returns a NotFoundError if no assertion can be found.
	FindMany(assertionType *AssertionType, headers map[string]string) ([]Assertion, error)
	// FindManyPredefined finds assertions in the predefined sets
	// (trusted or not) based on arbitrary headers.  It returns a
	// NotFoundError if no assertion can be found.
	FindManyPredefined(assertionType *AssertionType, headers map[string]string) ([]Assertion, error)
	// FindSequence finds an assertion for the given headers and after for
	// a sequence-forming type.
	// The provided headers must contain a sequence key, i.e. a prefix of
	// the primary key for the assertion type except for the sequence
	// number header.
	// The assertion is the first in the sequence under the sequence key
	// with sequence number > after.
	// If after is -1 it returns instead the assertion with the largest
	// sequence number.
	// It will constraint itself to assertions with format <= maxFormat
	// unless maxFormat is -1.
	// It returns a NotFoundError if the assertion cannot be found.
	FindSequence(assertType *AssertionType, sequenceHeaders map[string]string, after, maxFormat int) (SequenceMember, error)
	// Check tests whether the assertion is properly signed and consistent with all the stored knowledge.
	Check(assert Assertion) error
}

// A Checker defines a check on an assertion considering aspects such as
// the signing key, and consistency with other
// assertions in the database.
type Checker func(assert Assertion, signingKey *AccountKey, roDB RODatabase, checkTimeEarliest, checkTimeLatest time.Time) error

// Database holds assertions and can be used to sign or check
// further assertions.
type Database struct {
	bs         Backstore
	keypairMgr KeypairManager

	trusted    Backstore
	predefined Backstore
	// all backstores to consider for find
	backstores []Backstore
	// backstores of dbs this was built on by stacking
	stackedOn []Backstore

	checkers     []Checker
	earliestTime time.Time
}

// OpenDatabase opens the assertion database based on the configuration.
func OpenDatabase(cfg *DatabaseConfig) (*Database, error) {
	bs := cfg.Backstore
	keypairMgr := cfg.KeypairManager

	if bs == nil {
		bs = nullBackstore{}
	}
	if keypairMgr == nil {
		keypairMgr = NewMemoryKeypairManager()
	}

	trustedBackstore := NewMemoryBackstore()

	for _, a := range cfg.Trusted {
		switch accepted := a.(type) {
		case *AccountKey:
			accKey := accepted
			mylog.Check(trustedBackstore.Put(AccountKeyType, accKey))

		case *Account:
			acct := accepted
			mylog.Check(trustedBackstore.Put(AccountType, acct))

		default:
			return nil, fmt.Errorf("cannot predefine trusted assertions that are not account-key or account: %s", a.Type().Name)
		}
	}

	otherPredefinedBackstore := NewMemoryBackstore()

	for _, a := range cfg.OtherPredefined {
		mylog.Check(otherPredefinedBackstore.Put(a.Type(), a))
	}

	checkers := cfg.Checkers
	if len(checkers) == 0 {
		checkers = DefaultCheckers
	}
	dbCheckers := make([]Checker, len(checkers))
	copy(dbCheckers, checkers)

	return &Database{
		bs:         bs,
		keypairMgr: keypairMgr,
		trusted:    trustedBackstore,
		predefined: otherPredefinedBackstore,
		// order here is relevant, Find* precedence and
		// findAccountKey depend on it, trusted should win over the
		// general backstore!
		backstores: []Backstore{trustedBackstore, otherPredefinedBackstore, bs},
		checkers:   dbCheckers,
	}, nil
}

// WithStackedBackstore returns a new database that adds to the given backstore
// only but finds in backstore and the base database backstores and
// cross-checks against all of them.
// This is useful to cross-check a set of assertions without adding
// them to the database.
func (db *Database) WithStackedBackstore(backstore Backstore) *Database {
	// original bs goes in front of stacked-on ones
	stackedOn := []Backstore{db.bs}
	stackedOn = append(stackedOn, db.stackedOn...)
	// find order: trusted, predefined, new backstore, stacked-on ones
	backstores := []Backstore{db.trusted, db.predefined}
	backstores = append(backstores, backstore)
	backstores = append(backstores, stackedOn...)
	return &Database{
		bs:         backstore,
		keypairMgr: db.keypairMgr,
		trusted:    db.trusted,
		predefined: db.predefined,
		backstores: backstores,
		stackedOn:  stackedOn,
		checkers:   db.checkers,
	}
}

// ImportKey stores the given private/public key pair.
func (db *Database) ImportKey(privKey PrivateKey) error {
	return db.keypairMgr.Put(privKey)
}

// for validity checking of base64 hash strings
var base64HashLike = regexp.MustCompile("^[[:alnum:]_-]*$")

func (db *Database) safeGetPrivateKey(keyID string) (PrivateKey, error) {
	if keyID == "" {
		return nil, fmt.Errorf("key id is empty")
	}
	if !base64HashLike.MatchString(keyID) {
		return nil, fmt.Errorf("key id contains unexpected chars: %q", keyID)
	}
	return db.keypairMgr.Get(keyID)
}

// PublicKey returns the public key part of the key pair that has the given key id.
func (db *Database) PublicKey(keyID string) (PublicKey, error) {
	privKey := mylog.Check2(db.safeGetPrivateKey(keyID))

	return privKey.PublicKey(), nil
}

// Sign assembles an assertion with the provided information and signs it
// with the private key from `headers["authority-id"]` that has the provided key id.
func (db *Database) Sign(assertType *AssertionType, headers map[string]interface{}, body []byte, keyID string) (Assertion, error) {
	privKey := mylog.Check2(db.safeGetPrivateKey(keyID))

	return assembleAndSign(assertType, headers, body, privKey)
}

// findAccountKey finds an AccountKey exactly with account id and key id.
func (db *Database) findAccountKey(authorityID, keyID string) (*AccountKey, error) {
	key := []string{keyID}
	// consider trusted account keys then disk stored account keys
	for _, bs := range db.backstores {
		a := mylog.Check2(bs.Get(AccountKeyType, key, AccountKeyType.MaxSupportedFormat()))
		if err == nil {
			hit := a.(*AccountKey)
			if hit.AccountID() != authorityID {
				return nil, fmt.Errorf("found public key %q from %q but expected it from: %s", keyID, hit.AccountID(), authorityID)
			}
			return hit, nil
		}
		if !errors.Is(err, &NotFoundError{}) {
			return nil, err
		}
	}
	return nil, &NotFoundError{Type: AccountKeyType}
}

// IsTrustedAccount returns whether the account is part of the trusted set.
func (db *Database) IsTrustedAccount(accountID string) bool {
	if accountID == "" {
		return false
	}
	_ := mylog.Check2(db.trusted.Get(AccountType, []string{accountID}, AccountType.MaxSupportedFormat()))
	return err == nil
}

var timeNow = time.Now

// SetEarliestTime affects how key expiration is checked.
// Instead of considering current system time, only assume that current time
// is >= earliest. If earliest is zero reset to considering current system time.
func (db *Database) SetEarliestTime(earliest time.Time) {
	db.earliestTime = earliest
}

// Check tests whether the assertion is properly signed and consistent with all the stored knowledge.
func (db *Database) Check(assert Assertion) error {
	if !assert.SupportedFormat() {
		return &UnsupportedFormatError{Ref: assert.Ref(), Format: assert.Format()}
	}

	typ := assert.Type()
	// assume current time is >= earliestTime and <= latestTime
	earliestTime := db.earliestTime
	var latestTime time.Time
	if earliestTime.IsZero() {
		// use the current system time by setting both to it
		earliestTime = timeNow()
		latestTime = earliestTime
	}

	var accKey *AccountKey

	if typ.flags&noAuthority == 0 {
		// TODO: later may need to consider type of assert to find candidate keys
		accKey = mylog.Check2(db.findAccountKey(assert.AuthorityID(), assert.SignKeyID()))
		if errors.Is(err, &NotFoundError{}) {
			return fmt.Errorf("no matching public key %q for signature by %q", assert.SignKeyID(), assert.AuthorityID())
		}

	} else {
		if assert.AuthorityID() != "" {
			return fmt.Errorf("internal error: %q assertion cannot have authority-id set", typ.Name)
		}
	}

	for _, checker := range db.checkers {
		mylog.Check(checker(assert, accKey, db, earliestTime, latestTime))
	}

	return nil
}

// Add persists the assertion after ensuring it is properly signed and consistent with all the stored knowledge.
// It will return an error when trying to add an older revision of the assertion than the one currently stored.
func (db *Database) Add(assert Assertion) error {
	ref := assert.Ref()

	if len(ref.PrimaryKey) == 0 {
		return fmt.Errorf("internal error: assertion type %q has no primary key", ref.Type.Name)
	}
	mylog.Check(db.Check(assert))

	for i, keyVal := range ref.PrimaryKey {
		if keyVal == "" {
			return fmt.Errorf("missing or non-string primary key header: %v", ref.Type.PrimaryKey[i])
		}
	}

	// assuming trusted account keys/assertions will be managed
	// through the os snap this seems the safest policy until we
	// know more/better
	_ = mylog.Check2(db.trusted.Get(ref.Type, ref.PrimaryKey, ref.Type.MaxSupportedFormat()))
	if !errors.Is(err, &NotFoundError{}) {
		return fmt.Errorf("cannot add %q assertion with primary key clashing with a trusted assertion: %v", ref.Type.Name, ref.PrimaryKey)
	}

	_ = mylog.Check2(db.predefined.Get(ref.Type, ref.PrimaryKey, ref.Type.MaxSupportedFormat()))
	if !errors.Is(err, &NotFoundError{}) {
		return fmt.Errorf("cannot add %q assertion with primary key clashing with a predefined assertion: %v", ref.Type.Name, ref.PrimaryKey)
	}

	// this is non empty only in the stacked case
	if len(db.stackedOn) != 0 {
		headers := mylog.Check2(HeadersFromPrimaryKey(ref.Type, ref.PrimaryKey))

		cur := mylog.Check2(find(db.stackedOn, ref.Type, headers, -1))
		if err == nil {
			curRev := cur.Revision()
			rev := assert.Revision()
			if curRev >= rev {
				return &RevisionError{Current: curRev, Used: rev}
			}
		} else if !errors.Is(err, &NotFoundError{}) {
			return err
		}
	}

	return db.bs.Put(ref.Type, assert)
}

func searchMatch(assert Assertion, expectedHeaders map[string]string) bool {
	// check non-primary-key headers as well
	for expectedKey, expectedValue := range expectedHeaders {
		if assert.Header(expectedKey) != expectedValue {
			return false
		}
	}
	return true
}

func find(backstores []Backstore, assertionType *AssertionType, headers map[string]string, maxFormat int) (Assertion, error) {
	mylog.Check(checkAssertType(assertionType))

	maxSupp := assertionType.MaxSupportedFormat()
	if maxFormat == -1 {
		maxFormat = maxSupp
	} else {
		if maxFormat > maxSupp {
			return nil, fmt.Errorf("cannot find %q assertions for format %d higher than supported format %d", assertionType.Name, maxFormat, maxSupp)
		}
	}

	keyValues := mylog.Check2(PrimaryKeyFromHeaders(assertionType, headers))

	var assert Assertion
	for _, bs := range backstores {
		a := mylog.Check2(bs.Get(assertionType, keyValues, maxFormat))
		if err == nil {
			assert = a
			break
		}
		if !errors.Is(err, &NotFoundError{}) {
			return nil, err
		}
	}

	if assert == nil || !searchMatch(assert, headers) {
		return nil, &NotFoundError{Type: assertionType, Headers: headers}
	}

	return assert, nil
}

// Find an assertion based on arbitrary headers.
// Provided headers must contain the primary key for the assertion type.
// Optional primary key headers can be omitted in which case
// their default values will be used.
// It returns a NotFoundError if the assertion cannot be found.
func (db *Database) Find(assertionType *AssertionType, headers map[string]string) (Assertion, error) {
	return find(db.backstores, assertionType, headers, -1)
}

// FindMaxFormat finds an assertion like Find but such that its
// format is <= maxFormat by passing maxFormat along to the backend.
// It returns a NotFoundError if such an assertion cannot be found.
func (db *Database) FindMaxFormat(assertionType *AssertionType, headers map[string]string, maxFormat int) (Assertion, error) {
	return find(db.backstores, assertionType, headers, maxFormat)
}

// FindPredefined finds an assertion in the predefined sets (trusted
// or not) based on arbitrary headers.  Provided headers must contain
// the primary key for the assertion type.
// Optional primary key headers can be omitted in which case
// their default values will be used.
// It returns a NotFoundError if the assertion cannot be found.
func (db *Database) FindPredefined(assertionType *AssertionType, headers map[string]string) (Assertion, error) {
	return find([]Backstore{db.trusted, db.predefined}, assertionType, headers, -1)
}

// FindTrusted finds an assertion in the trusted set based on arbitrary headers.
// Provided headers must contain the primary key for the assertion type.
// Optional primary key headers can be omitted in which case
// their default values will be used.
// It returns a NotFoundError if the assertion cannot be found.
func (db *Database) FindTrusted(assertionType *AssertionType, headers map[string]string) (Assertion, error) {
	return find([]Backstore{db.trusted}, assertionType, headers, -1)
}

func (db *Database) findMany(backstores []Backstore, assertionType *AssertionType, headers map[string]string) ([]Assertion, error) {
	mylog.Check(checkAssertType(assertionType))

	res := []Assertion{}

	foundCb := func(assert Assertion) {
		res = append(res, assert)
	}

	// TODO: Find variant taking this
	maxFormat := assertionType.MaxSupportedFormat()
	for _, bs := range backstores {
		mylog.Check(bs.Search(assertionType, headers, foundCb, maxFormat))
	}

	if len(res) == 0 {
		return nil, &NotFoundError{Type: assertionType, Headers: headers}
	}
	return res, nil
}

// FindMany finds assertions based on arbitrary headers.
// It returns a NotFoundError if no assertion can be found.
func (db *Database) FindMany(assertionType *AssertionType, headers map[string]string) ([]Assertion, error) {
	return db.findMany(db.backstores, assertionType, headers)
}

// FindManyPrefined finds assertions in the predefined sets (trusted
// or not) based on arbitrary headers.  It returns a NotFoundError if
// no assertion can be found.
func (db *Database) FindManyPredefined(assertionType *AssertionType, headers map[string]string) ([]Assertion, error) {
	return db.findMany([]Backstore{db.trusted, db.predefined}, assertionType, headers)
}

// FindSequence finds an assertion for the given headers and after for
// a sequence-forming type.
// The provided headers must contain a sequence key, i.e. a prefix of
// the primary key for the assertion type except for the sequence
// number header.
// The assertion is the first in the sequence under the sequence key
// with sequence number > after.
// If after is -1 it returns instead the assertion with the largest
// sequence number.
// It will constraint itself to assertions with format <= maxFormat
// unless maxFormat is -1.
// It returns a NotFoundError if the assertion cannot be found.
func (db *Database) FindSequence(assertType *AssertionType, sequenceHeaders map[string]string, after, maxFormat int) (SequenceMember, error) {
	mylog.Check(checkAssertType(assertType))

	if !assertType.SequenceForming() {
		return nil, fmt.Errorf("cannot use FindSequence with non sequence-forming assertion type %q", assertType.Name)
	}
	maxSupp := assertType.MaxSupportedFormat()
	if maxFormat == -1 {
		maxFormat = maxSupp
	} else {
		if maxFormat > maxSupp {
			return nil, fmt.Errorf("cannot find %q assertions for format %d higher than supported format %d", assertType.Name, maxFormat, maxSupp)
		}
	}

	// form the sequence key using all keys but the last one which
	// is the sequence number
	seqKey := mylog.Check2(keysFromHeaders(assertType.PrimaryKey[:len(assertType.PrimaryKey)-1], sequenceHeaders, nil))

	// find the better result across backstores' results
	better := func(cur, a SequenceMember) SequenceMember {
		if cur == nil {
			return a
		}
		curSeq := cur.Sequence()
		aSeq := a.Sequence()
		if after == -1 {
			if aSeq > curSeq {
				return a
			}
		} else {
			if aSeq < curSeq {
				return a
			}
		}
		return cur
	}

	var assert SequenceMember
	for _, bs := range db.backstores {
		a := mylog.Check2(bs.SequenceMemberAfter(assertType, seqKey, after, maxFormat))
		if err == nil {
			assert = better(assert, a)
			continue
		}
		if !errors.Is(err, &NotFoundError{}) {
			return nil, err
		}
	}

	if assert != nil {
		return assert, nil
	}

	return nil, &NotFoundError{Type: assertType, Headers: sequenceHeaders}
}

// assertion checkers

// CheckSigningKeyIsNotExpired checks that the signing key is not expired.
func CheckSigningKeyIsNotExpired(assert Assertion, signingKey *AccountKey, roDB RODatabase, checkTimeEarliest, checkTimeLatest time.Time) error {
	if signingKey == nil {
		// assert isn't signed with an account-key key, CheckSignature
		// will fail anyway unless we teach it more stuff,
		// Also this check isn't so relevant for self-signed asserts
		// (e.g. account-key-request)
		return nil
	}
	if !signingKey.isValidAssumingCurTimeWithin(checkTimeEarliest, checkTimeLatest) {
		mismatchReason := timeMismatchMsg(checkTimeEarliest, checkTimeLatest, signingKey.since, signingKey.until)
		return fmt.Errorf("assertion is signed with expired public key %q from %q: %s", assert.SignKeyID(), assert.AuthorityID(), mismatchReason)
	}
	return nil
}

func timeMismatchMsg(earliest, latest, keySince, keyUntil time.Time) string {
	var msg string

	validFrom := earliest.Format(time.RFC3339)
	if !latest.IsZero() && !latest.Equal(earliest) {
		validTo := latest.Format(time.RFC3339)
		msg = fmt.Sprintf("current time range is [%s, %s]", validFrom, validTo)
	} else {
		msg = fmt.Sprintf("current time is %s", validFrom)
	}

	keyFrom := keySince.Format(time.RFC3339)
	if !keyUntil.IsZero() {
		keyTo := keyUntil.Format(time.RFC3339)
		return msg + fmt.Sprintf(" but key is valid during [%s, %s)", keyFrom, keyTo)
	}

	return msg + fmt.Sprintf(" but key is valid from %s", keyFrom)
}

// CheckSignature checks that the signature is valid.
func CheckSignature(assert Assertion, signingKey *AccountKey, roDB RODatabase, checkTimeEarliest, checkTimeLatest time.Time) (err error) {
	var pubKey PublicKey
	if signingKey != nil {
		pubKey = signingKey.publicKey()
		if assert.AuthorityID() != signingKey.AccountID() {
			return fmt.Errorf("assertion authority %q does not match public key from %q", assert.AuthorityID(), signingKey.AccountID())
		}
		if !signingKey.canSign(assert) {
			return fmt.Errorf("assertion does not match signing constraints for public key %q from %q", assert.SignKeyID(), assert.AuthorityID())
		}
	} else {
		custom, ok := assert.(customSigner)
		if !ok {
			return fmt.Errorf("cannot check no-authority assertion type %q", assert.Type().Name)
		}
		pubKey = custom.signKey()
	}
	content, encSig := assert.Signature()
	signature := mylog.Check2(decodeSignature(encSig))
	mylog.Check(pubKey.verify(content, signature))

	return nil
}

type timestamped interface {
	Timestamp() time.Time
}

// CheckTimestampVsSigningKeyValidity verifies that the timestamp of
// the assertion is within the signing key validity.
func CheckTimestampVsSigningKeyValidity(assert Assertion, signingKey *AccountKey, roDB RODatabase, checkTimeEarliest, checkTimeLatest time.Time) error {
	if signingKey == nil {
		// assert isn't signed with an account-key key, CheckSignature
		// will fail anyway unless we teach it more stuff.
		// Also this check isn't so relevant for self-signed asserts
		// (e.g. account-key-request)
		return nil
	}
	if tstamped, ok := assert.(timestamped); ok {
		checkTime := tstamped.Timestamp()
		if !signingKey.isValidAt(checkTime) {
			until := ""
			if !signingKey.Until().IsZero() {
				until = fmt.Sprintf(" until %q", signingKey.Until())
			}
			return fmt.Errorf("%s assertion timestamp %q outside of signing key validity (key valid since %q%s)",
				assert.Type().Name, checkTime, signingKey.Since(), until)
		}
	}
	return nil
}

// A consistencyChecker performs further checks based on the full
// assertion database knowledge and its own signing key.
type consistencyChecker interface {
	checkConsistency(roDB RODatabase, signingKey *AccountKey) error
}

// CheckCrossConsistency verifies that the assertion is consistent with the other statements in the database.
func CheckCrossConsistency(assert Assertion, signingKey *AccountKey, roDB RODatabase, checkTimeEarliest, checkTimeLatest time.Time) error {
	// see if the assertion requires further checks
	if checker, ok := assert.(consistencyChecker); ok {
		return checker.checkConsistency(roDB, signingKey)
	}
	return nil
}

// DefaultCheckers lists the default and recommended assertion
// checkers used by Database if none are specified in the
// DatabaseConfig.Checkers.
var DefaultCheckers = []Checker{
	CheckSigningKeyIsNotExpired,
	CheckSignature,
	CheckTimestampVsSigningKeyValidity,
	CheckCrossConsistency,
}
