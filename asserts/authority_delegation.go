// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
)

// AuthorityDelegation holds an authority-delegation assertion, asserting
// that a specified delegated authority can sign a constrained set
// of assertions for a given account.
type AuthorityDelegation struct {
	assertionBase
	assertionConstraints []*AssertionConstraints
}

// AccountID returns the account-id of this authority-delegation.
func (ad *AuthorityDelegation) AccountID() string {
	return ad.HeaderString("account-id")
}

// DelegateID returns the delegated account-id for this authority-delegation.
func (ad *AuthorityDelegation) DelegateID() string {
	return ad.HeaderString("delegate-id")
}

// MatchingConstraints returns all the delegation constraints that match the given assertion.
func (ad *AuthorityDelegation) MatchingConstraints(a Assertion) []*AssertionConstraints {
	res := make([]*AssertionConstraints, 0, 1)
	for _, ac := range ad.assertionConstraints {
		if ac.Check(a) == nil {
			res = append(res, ac)
		}
	}
	if len(res) == 0 {
		return nil
	}
	return res
}

// Implement further consistency checks.
func (ad *AuthorityDelegation) checkConsistency(db RODatabase, acck *AccountKey) error {
	if !db.IsTrustedAccount(ad.AuthorityID()) {
		// XXX if this is relaxed then authority-id must otherwise
		// match account-id
		return fmt.Errorf("authority-delegation assertion for %q is not signed by a directly trusted authority: %s", ad.AccountID(), ad.AuthorityID())
	}
	_, err := db.Find(AccountType, map[string]string{
		"account-id": ad.AccountID(),
	})
	if IsNotFound(err) {
		return fmt.Errorf("authority-delegation assertion for %q does not have a matching account assertion", ad.AccountID())
	}
	if err != nil {
		return err
	}
	_, err = db.Find(AccountType, map[string]string{
		"account-id": ad.DelegateID(),
	})
	if IsNotFound(err) {
		return fmt.Errorf("authority-delegation assertion for %q does not have a matching account assertion for delegated %q", ad.AccountID(), ad.DelegateID())
	}
	if err != nil {
		return err
	}
	return nil
}

// sound
var _ consistencyChecker = (*AuthorityDelegation)(nil)

// Prerequisites returns references to this authority-delegation's prerequisite assertions.
func (ad *AuthorityDelegation) Prerequisites() []*Ref {
	return []*Ref{
		{Type: AccountType, PrimaryKey: []string{ad.AccountID()}},
		{Type: AccountType, PrimaryKey: []string{ad.DelegateID()}},
	}
}

func assembleAuthorityDelegation(assert assertionBase) (Assertion, error) {
	// account-id and delegate-id are checked by the general
	// primary key code

	cs, ok := assert.headers["assertions"]
	if !ok {
		return nil, fmt.Errorf("assertions constraints are mandatory")
	}
	csmaps, ok := cs.([]interface{})
	if !ok {
		return nil, fmt.Errorf("assertions constraints must be a list of maps")
	}
	if len(csmaps) == 0 {
		// there is no syntax producing this scenario but be robust
		return nil, fmt.Errorf("assertions constraints cannot be empty")
	}
	acs := make([]*AssertionConstraints, 0, len(csmaps))
	for _, csmap := range csmaps {
		m, ok := csmap.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("assertions constraints must be a list of maps")
		}
		typeName, err := checkNotEmptyStringWhat(m, "type", "constraint")
		if err != nil {
			return nil, err
		}
		t := Type(typeName)
		if t == nil {
			return nil, fmt.Errorf("%q is not a valid assertion type", typeName)
		}
		hm, err := checkMapWhat(m, "headers", "constraint")
		if err != nil {
			return nil, err
		}
		cc := compileContext{
			opts: &compileAttrMatcherOptions{},
		}
		matcher, err := compileAttrMatcher(cc, hm)
		if err != nil {
			return nil, fmt.Errorf("cannot compile headers constraint: %v", err)
		}
		sinceUntil, err := checkSinceUntilWhat(m, "constraint")
		if err != nil {
			return nil, err
		}
		_, onStore := m["on-store"]
		_, onBrand := m["on-brand"]
		_, onModel := m["on-model"]
		if onStore || onBrand || onModel {
			return nil, fmt.Errorf("device scope constraints not yet implemented")
		}
		acs = append(acs, &AssertionConstraints{
			assertType: t,
			matcher:    matcher,
			sinceUntil: *sinceUntil,
		})
	}

	// ignore extra headers for future compatibility
	return &AuthorityDelegation{
		assertionBase:        assert,
		assertionConstraints: acs,
	}, nil

}

// AssertionConstraints constraints a set of assertions of a given type.
type AssertionConstraints struct {
	assertType *AssertionType
	matcher    attrMatcher
	sinceUntil
	// XXX device scoping
}

// Check checks whether the assertion matches the constraints.
// It returns an error otherwise.
func (ac *AssertionConstraints) Check(a Assertion) error {
	if a.Type() != ac.assertType {
		return fmt.Errorf("assertion %q does not match constraint for assertion type %q", a.Type().Name, ac.assertType.Name)
	}
	return ac.matcher.match("", a.Headers(), &attrMatchingContext{
		attrWord: "header",
	})
}
