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
// of assertion for a given account.
type AuthorityDelegation struct {
	assertionBase
	// assertionConstraints
}

// AccountID returns the account-id of this this authority-delegation.
func (ad *AuthorityDelegation) AccountID() string {
	return ad.HeaderString("account-id")
}

// DelegateID returns the delegated account-id for this authority-delegation.
func (ad *AuthorityDelegation) DelegateID() string {
	return ad.HeaderString("delegate-id")
}

// Implement further consistency checks.
func (ad *AuthorityDelegation) checkConsistency(db RODatabase, acck *AccountKey) error {
	// XXX test this
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
	// XXX test this
	return []*Ref{
		{Type: AccountType, PrimaryKey: []string{ad.AccountID()}},
		{Type: AccountType, PrimaryKey: []string{ad.DelegateID()}},
	}
}

func assembleAuthorityDelegation(assert assertionBase) (Assertion, error) {
	// XXX test errors
	_, err := checkNotEmptyString(assert.headers, "account-id")
	if err != nil {
		return nil, err
	}
	_, err = checkNotEmptyString(assert.headers, "delegate-id")
	if err != nil {
		return nil, err
	}

	// XXX parse assertion constraints

	// ignore extra headers for future compatibility
	return &AuthorityDelegation{
		assertionBase: assert,
	}, nil

}
