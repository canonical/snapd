// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2023 Canonical Ltd
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
	"errors"
	"fmt"
	"regexp"
	"time"
)

var validAccountKeyName = regexp.MustCompile(`^(?:[a-z0-9]+-?)*[a-z](?:-?[a-z0-9])*$`)

// AccountKey holds an account-key assertion, asserting a public key
// belonging to the account.
type AccountKey struct {
	assertionBase
	sinceUntil
	constraintMatchers []attrMatcher
	pubKey             PublicKey
}

type sinceUntil struct {
	since time.Time
	until time.Time
}

func checkSinceUntilWhat(m map[string]interface{}, what string) (*sinceUntil, error) {
	since, err := checkRFC3339DateWhat(m, "since", what)
	if err != nil {
		return nil, err
	}

	until, err := checkRFC3339DateWithDefaultWhat(m, "until", what, time.Time{})
	if err != nil {
		return nil, err
	}
	if !until.IsZero() && until.Before(since) {
		return nil, fmt.Errorf("'until' time cannot be before 'since' time")
	}

	return &sinceUntil{
		since: since,
		until: until,
	}, nil
}

// AccountID returns the account-id of this account-key.
func (ak *AccountKey) AccountID() string {
	return ak.HeaderString("account-id")
}

// Name returns the name of the account key.
func (ak *AccountKey) Name() string {
	return ak.HeaderString("name")
}

func IsValidAccountKeyName(name string) bool {
	return validAccountKeyName.MatchString(name)
}

// Since returns the time when the account key starts being valid.
func (ak *AccountKey) Since() time.Time {
	return ak.since
}

// Until returns the time when the account key stops being valid. A zero time means the key is valid forever.
func (ak *AccountKey) Until() time.Time {
	return ak.until
}

// PublicKeyID returns the key id used for lookup of the account key.
func (ak *AccountKey) PublicKeyID() string {
	return ak.pubKey.ID()
}

// isValidAt returns whether the since-until constraint is valid at 'when' time.
func (su *sinceUntil) isValidAt(when time.Time) bool {
	valid := when.After(su.since) || when.Equal(su.since)
	if valid && !su.until.IsZero() {
		valid = when.Before(su.until)
	}
	return valid
}

// isValidAssumingCurTimeWithin returns whether the since-until constraint  is
// possibly valid if the current time is known to be within [earliest,
// latest]. That means the intersection of possible current times and
// validity is not empty.
// If latest is zero, then current time is assumed to be >=earliest.
// If earliest == latest this is equivalent to isKeyValidAt().
func (su *sinceUntil) isValidAssumingCurTimeWithin(earliest, latest time.Time) bool {
	if !latest.IsZero() {
		// impossible input => false
		if latest.Before(earliest) {
			return false
		}
		if latest.Before(su.since) {
			return false
		}
	}
	if !su.until.IsZero() {
		if earliest.After(su.until) || earliest.Equal(su.until) {
			return false
		}
	}
	return true
}

// publicKey returns the underlying public key of the account key.
func (ak *AccountKey) publicKey() PublicKey {
	return ak.pubKey
}

// ConstraintsPrecheck checks whether the given type and headers match the signing constraints of the account key.
func (ak *AccountKey) ConstraintsPrecheck(assertType *AssertionType, headers map[string]interface{}) error {
	headersWithType := copyHeaders(headers)
	headersWithType["type"] = assertType.Name
	if !ak.matchAgainstConstraints(headersWithType) {
		return fmt.Errorf("headers do not match the account-key constraints")
	}
	return nil
}

func (ak *AccountKey) matchAgainstConstraints(headers map[string]interface{}) bool {
	matchers := ak.constraintMatchers
	// no constraints, everything is allowed
	if len(matchers) == 0 {
		return true
	}
	for _, m := range matchers {
		if m.match("", headers, &attrMatchingContext{
			attrWord: "header",
		}) == nil {
			return true
		}
	}
	return false
}

// canSign checks whether the given assertion matches the signing constraints of the account key.
func (ak *AccountKey) canSign(a Assertion) bool {
	return ak.matchAgainstConstraints(a.Headers())
}

func checkPublicKey(ab *assertionBase, keyIDName string) (PublicKey, error) {
	pubKey, err := DecodePublicKey(ab.Body())
	if err != nil {
		return nil, err
	}
	keyID, err := checkNotEmptyString(ab.headers, keyIDName)
	if err != nil {
		return nil, err
	}
	if keyID != pubKey.ID() {
		return nil, fmt.Errorf("public key does not match provided key id")
	}
	return pubKey, nil
}

// Implement further consistency checks.
func (ak *AccountKey) checkConsistency(db RODatabase, acck *AccountKey) error {
	if !db.IsTrustedAccount(ak.AuthorityID()) {
		return fmt.Errorf("account-key assertion for %q is not signed by a directly trusted authority: %s", ak.AccountID(), ak.AuthorityID())
	}
	_, err := db.Find(AccountType, map[string]string{
		"account-id": ak.AccountID(),
	})
	if errors.Is(err, &NotFoundError{}) {
		return fmt.Errorf("account-key assertion for %q does not have a matching account assertion", ak.AccountID())
	}
	if err != nil {
		return err
	}
	// XXX: Make this unconditional once account-key assertions are required to have a name.
	if ak.Name() != "" {
		// Check that we don't end up with multiple keys with
		// different IDs but the same account-id and name.
		// Note that this is a non-transactional check-then-add, so
		// is not a hard guarantee.  Backstores that can implement a
		// unique constraint should do so.
		assertions, err := db.FindMany(AccountKeyType, map[string]string{
			"account-id": ak.AccountID(),
			"name":       ak.Name(),
		})
		if err != nil && !errors.Is(err, &NotFoundError{}) {
			return err
		}
		for _, assertion := range assertions {
			existingAccKey := assertion.(*AccountKey)
			if ak.PublicKeyID() != existingAccKey.PublicKeyID() {
				return fmt.Errorf("account-key assertion for %q with ID %q has the same name %q as existing ID %q", ak.AccountID(), ak.PublicKeyID(), ak.Name(), existingAccKey.PublicKeyID())
			}
		}
	}
	return nil
}

// expected interface is implemented
var _ consistencyChecker = (*AccountKey)(nil)

// Prerequisites returns references to this account-key's prerequisite assertions.
func (ak *AccountKey) Prerequisites() []*Ref {
	return []*Ref{
		{Type: AccountType, PrimaryKey: []string{ak.AccountID()}},
	}
}

func assembleAccountKey(assert assertionBase) (Assertion, error) {
	_, err := checkNotEmptyString(assert.headers, "account-id")
	if err != nil {
		return nil, err
	}

	// XXX: We should require name to be present after backfilling existing assertions.
	_, ok := assert.headers["name"]
	if ok {
		_, err = checkStringMatches(assert.headers, "name", validAccountKeyName)
		if err != nil {
			return nil, err
		}
	}

	sinceUntil, err := checkSinceUntilWhat(assert.headers, "header")
	if err != nil {
		return nil, err
	}

	pubk, err := checkPublicKey(&assert, "public-key-sha3-384")
	if err != nil {
		return nil, err
	}

	var matchers []attrMatcher
	if cs, ok := assert.headers["constraints"]; ok {
		matchers, err = checkAKConstraints(cs)
		if err != nil {
			return nil, err
		}
	}

	// ignore extra headers for future compatibility
	return &AccountKey{
		assertionBase:      assert,
		sinceUntil:         *sinceUntil,
		constraintMatchers: matchers,
		pubKey:             pubk,
	}, nil
}

func checkAKConstraints(cs interface{}) ([]attrMatcher, error) {
	csmaps, ok := cs.([]interface{})
	if !ok {
		return nil, fmt.Errorf("assertions constraints must be a list of maps")
	}
	if len(csmaps) == 0 {
		// there is no syntax producing this scenario but be robust
		return nil, fmt.Errorf("assertions constraints cannot be empty")
	}
	matchers := make([]attrMatcher, 0, len(csmaps))
	for _, csmap := range csmaps {
		m, ok := csmap.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("assertions constraints must be a list of maps")
		}
		hm, err := checkMapWhat(m, "headers", "constraint")
		if err != nil {
			return nil, err
		}
		if hm == nil {
			return nil, fmt.Errorf(`"headers" constraint mandatory in asserions constraints`)
		}
		t, ok := hm["type"]
		if !ok {
			return nil, fmt.Errorf("type header constraint mandatory in asserions constraints")
		}
		tstr, ok := t.(string)
		if !ok {
			return nil, fmt.Errorf("type header constraint must be a string")
		}
		if tstr != regexp.QuoteMeta(tstr) {
			return nil, fmt.Errorf("type header constraint must be a precise string and not a regexp")
		}
		cc := compileContext{
			opts: &compileAttrMatcherOptions{},
		}
		matcher, err := compileAttrMatcher(cc, hm)
		if err != nil {
			return nil, fmt.Errorf("cannot compile headers constraint: %v", err)
		}
		matchers = append(matchers, matcher)
	}
	return matchers, nil
}

func accountKeyFormatAnalyze(headers map[string]interface{}, body []byte) (formatnum int, err error) {
	formatnum = 0
	if _, ok := headers["constraints"]; ok {
		formatnum = 1
	}
	return formatnum, nil
}

// AccountKeyRequest holds an account-key-request assertion, which is a self-signed request to prove that the requester holds the private key and wishes to create an account-key assertion for it.
type AccountKeyRequest struct {
	assertionBase
	sinceUntil
	pubKey PublicKey
}

// AccountID returns the account-id of this account-key-request.
func (akr *AccountKeyRequest) AccountID() string {
	return akr.HeaderString("account-id")
}

// Name returns the name of the account key.
func (akr *AccountKeyRequest) Name() string {
	return akr.HeaderString("name")
}

// Since returns the time when the requested account key starts being valid.
func (akr *AccountKeyRequest) Since() time.Time {
	return akr.since
}

// Until returns the time when the requested account key stops being valid. A zero time means the key is valid forever.
func (akr *AccountKeyRequest) Until() time.Time {
	return akr.until
}

// PublicKeyID returns the underlying public key ID of the requested account key.
func (akr *AccountKeyRequest) PublicKeyID() string {
	return akr.pubKey.ID()
}

// signKey returns the underlying public key of the requested account key.
func (akr *AccountKeyRequest) signKey() PublicKey {
	return akr.pubKey
}

// Implement further consistency checks.
func (akr *AccountKeyRequest) checkConsistency(db RODatabase, acck *AccountKey) error {
	_, err := db.Find(AccountType, map[string]string{
		"account-id": akr.AccountID(),
	})
	if errors.Is(err, &NotFoundError{}) {
		return fmt.Errorf("account-key-request assertion for %q does not have a matching account assertion", akr.AccountID())
	}
	if err != nil {
		return err
	}
	return nil
}

// expected interfaces are implemented
var (
	_ consistencyChecker = (*AccountKeyRequest)(nil)
	_ customSigner       = (*AccountKeyRequest)(nil)
)

// Prerequisites returns references to this account-key-request's prerequisite assertions.
func (akr *AccountKeyRequest) Prerequisites() []*Ref {
	return []*Ref{
		{Type: AccountType, PrimaryKey: []string{akr.AccountID()}},
	}
}

func assembleAccountKeyRequest(assert assertionBase) (Assertion, error) {
	_, err := checkNotEmptyString(assert.headers, "account-id")
	if err != nil {
		return nil, err
	}

	_, err = checkStringMatches(assert.headers, "name", validAccountKeyName)
	if err != nil {
		return nil, err
	}

	sinceUntil, err := checkSinceUntilWhat(assert.headers, "header")
	if err != nil {
		return nil, err
	}

	pubk, err := checkPublicKey(&assert, "public-key-sha3-384")
	if err != nil {
		return nil, err
	}

	// XXX TODO: support constraints also in account-key-request when
	// implementing more fully automated registration flows

	// ignore extra headers for future compatibility
	return &AccountKeyRequest{
		assertionBase: assert,
		sinceUntil:    *sinceUntil,
		pubKey:        pubk,
	}, nil
}
