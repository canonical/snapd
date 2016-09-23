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
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var validSystemUserUsernames = regexp.MustCompile(`^[a-z0-9][-a-z0-9+.-_]*$`)

// SystemUser holds a system-user assertion which allows creating local
// system users.
type SystemUser struct {
	assertionBase
	series  []string
	models  []string
	sshKeys []string
	since   time.Time
	until   time.Time
}

// BrandID returns the brand identifier that signed this assertion.
func (su *SystemUser) BrandID() string {
	return su.HeaderString("brand-id")
}

// EMail returns the email address that this assertion is valid for.
func (su *SystemUser) EMail() string {
	return su.HeaderString("email")
}

// Series returns the series that this assertion is valid for.
func (su *SystemUser) Series() []string {
	return su.series
}

// Models returns the models that this assertion is valid for.
func (su *SystemUser) Models() []string {
	return su.models
}

// Name returns the full name of the user (e.g. Random Guy).
func (su *SystemUser) Name() string {
	return su.HeaderString("name")
}

// Username returns the system user name that should be created (e.g. "foo").
func (su *SystemUser) Username() string {
	return su.HeaderString("username")
}

// Password returns the crypt(3) compatible password for the user.
// Note that only ID: $6$ or stronger is supported (sha512crypt).
func (su *SystemUser) Password() string {
	return su.HeaderString("password")
}

// SSHKeys returns the ssh keys for the user.
func (su *SystemUser) SSHKeys() []string {
	return su.sshKeys
}

// Since returns the time since the assertion is valid.
func (su *SystemUser) Since() time.Time {
	return su.since
}

// Until returns the time until the assertion is valid.
func (su *SystemUser) Until() time.Time {
	return su.until
}

// Implement further consistency checks.
func (su *SystemUser) checkConsistency(db RODatabase, acck *AccountKey) error {
	// Do the cross-checks when this assertion is actually used,
	// i.e. in the create-user code. See also Model.checkConsitency

	return nil
}

// sanity
var _ consistencyChecker = (*SystemUser)(nil)

func checkHashedPassword(headers map[string]interface{}, name string) (string, error) {
	pw, err := checkOptionalString(headers, name)
	if err != nil {
		return "", err
	}
	// crypt(3) compatible hashes have the form: $id$salt$hash
	l := strings.SplitN(pw, "$", 5)
	if len(l) != 4 && len(l) != 5 {
		return "", fmt.Errorf(`%q header must be a hashed password of the form "$integer-id$salt$hash", see crypt(3)`, name)
	}
	// see crypt(3), ID 6 means SHA-512 (since glibc 2.7)
	ID, err := strconv.Atoi(l[1])
	if err != nil {
		return "", fmt.Errorf(`%q header must start with "$integer-id$", got %q`, name, l[1])
	}
	// double check that we only allow modern hashes
	if ID < 6 {
		return "", fmt.Errorf("%q header only supports $id$ values of 6 (sha512crypt) or higher", name)
	}

	// the $rounds=N$ part is optional
	i := 2
	if strings.HasPrefix(l[i], "rounds=") {
		i++
	}

	// see crypt(3) for the legal chars
	validSaltAndHash := regexp.MustCompile(`^[a-zA-Z0-9./=]+$`)
	if !validSaltAndHash.MatchString(l[i]) {
		return "", fmt.Errorf("%q header has invalid chars in salt %q", name, l[2])
	}
	i++
	if i >= len(l) {
		return "", fmt.Errorf("%q header has missing hash field", name)
	}
	if !validSaltAndHash.MatchString(l[i]) {
		return "", fmt.Errorf("%q header has invalid chars in hash %q", name, l[3])
	}

	return pw, nil
}

func assembleSystemUser(assert assertionBase) (Assertion, error) {
	err := checkAuthorityMatchesBrand(&assert)
	if err != nil {
		return nil, err
	}
	email, err := checkNotEmptyString(assert.headers, "email")
	if err != nil {
		return nil, err
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return nil, fmt.Errorf(`"email" header must be a RFC 5322 compliant email address: %s`, err)
	}

	series, err := checkStringList(assert.headers, "series")
	if err != nil {
		return nil, err
	}
	models, err := checkStringList(assert.headers, "models")
	if err != nil {
		return nil, err
	}
	if _, err := checkOptionalString(assert.headers, "name"); err != nil {
		return nil, err
	}
	if _, err := checkStringMatches(assert.headers, "username", validSystemUserUsernames); err != nil {
		return nil, err
	}
	if _, err := checkHashedPassword(assert.headers, "password"); err != nil {
		return nil, err
	}

	sshKeys, err := checkStringList(assert.headers, "ssh-keys")
	if err != nil {
		return nil, err
	}
	since, err := checkRFC3339Date(assert.headers, "since")
	if err != nil {
		return nil, err
	}
	until, err := checkRFC3339Date(assert.headers, "until")
	if err != nil {
		return nil, err
	}
	if !until.IsZero() && until.Before(since) {
		return nil, fmt.Errorf("'until' time cannot be before 'since' time")
	}

	return &SystemUser{
		assertionBase: assert,
		series:        series,
		models:        models,
		sshKeys:       sshKeys,
		since:         since,
		until:         until,
	}, nil
}
