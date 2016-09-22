// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"time"
)

// SystemUser holds a system-user assertion which allows creating local
// system users.
type SystemUser struct {
	assertionBase
	series    []string
	models    []string
	sshKeys   []string
	timestamp time.Time
	until     time.Time
}

func (su *SystemUser) BrandID() string {
	return su.HeaderString("brand-id")
}

func (su *SystemUser) EMail() string {
	return su.HeaderString("email")
}

func (su *SystemUser) Series() []string {
	return su.series
}

func (su *SystemUser) Models() []string {
	return su.models
}

func (su *SystemUser) Name() string {
	return su.HeaderString("name")
}

func (su *SystemUser) Username() string {
	return su.HeaderString("username")
}

func (su *SystemUser) Password() string {
	return su.HeaderString("password")
}

func (su *SystemUser) SSHKeys() []string {
	return su.sshKeys
}

func (su *SystemUser) Timestamp() time.Time {
	return su.timestamp
}

func (su *SystemUser) Until() time.Time {
	return su.until
}

// Implement further consistency checks.
func (su *SystemUser) checkConsistency(db RODatabase, acck *AccountKey) error {
	if !db.IsTrustedAccount(acck.AuthorityID()) {
		return fmt.Errorf("system-user assertion for %q is not signed by a directly trusted authority: %s", su.EMail(), acck.AuthorityID())
	}
	return nil
}

// sanity
var _ consistencyChecker = (*SystemUser)(nil)

func assembleSystemUser(assert assertionBase) (Assertion, error) {
	_, err := checkNotEmptyString(assert.headers, "brand-id")
	if err != nil {
		return nil, err
	}
	_, err = checkNotEmptyString(assert.headers, "email")
	if err != nil {
		return nil, err
	}
	series, err := checkStringList(assert.headers, "series")
	if err != nil {
		return nil, err
	}
	models, err := checkStringList(assert.headers, "models")
	if err != nil {
		return nil, err
	}
	sshKeys, err := checkStringList(assert.headers, "ssh-keys")
	if err != nil {
		return nil, err
	}
	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}
	until, err := checkRFC3339Date(assert.headers, "until")
	if err != nil {
		return nil, err
	}

	return &SystemUser{
		assertionBase: assert,
		series:        series,
		models:        models,
		sshKeys:       sshKeys,
		timestamp:     timestamp,
		until:         until,
	}, nil
}
