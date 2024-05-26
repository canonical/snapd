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

	"github.com/ddkwork/golibrary/mylog"
)

// validSystemUserUsernames matches the regex we allow by osutil/user.go:IsValidUsername
var validSystemUserUsernames = regexp.MustCompile(`^[a-z0-9][-a-z0-9._]*$`)

// SystemUser holds a system-user assertion which allows creating local
// system users.
type SystemUser struct {
	assertionBase
	series     []string
	models     []string
	serials    []string
	sshKeys    []string
	since      time.Time
	until      time.Time
	expiration string

	forcePasswordChange bool
}

// BrandID returns the brand identifier that signed this assertion.
func (su *SystemUser) BrandID() string {
	return su.HeaderString("brand-id")
}

// Email returns the email address that this assertion is valid for.
func (su *SystemUser) Email() string {
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

// Serials returns the serials that this assertion is valid for.
func (su *SystemUser) Serials() []string {
	return su.serials
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

// ForcePasswordChange returns true if the user needs to change the password
// after the first login.
func (su *SystemUser) ForcePasswordChange() bool {
	return su.forcePasswordChange
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

// UserExpiration returns the expiration or validity duration of the user created.
//
// If no expiration was specified, this will return an zero time.Time structure.
//
// If expiration was set to 'until-expiration' then the .Until() time will be
// returned.
func (su *SystemUser) UserExpiration() time.Time {
	if su.expiration == "until-expiration" {
		return su.until
	}
	return time.Time{}
}

// ValidAt returns whether the system-user is valid at 'when' time.
func (su *SystemUser) ValidAt(when time.Time) bool {
	valid := when.After(su.since) || when.Equal(su.since)
	if valid {
		valid = when.Before(su.until)
	}
	return valid
}

// Implement further consistency checks.
func (su *SystemUser) checkConsistency(db RODatabase, acck *AccountKey) error {
	// Do the cross-checks when this assertion is actually used,
	// i.e. in the create-user code. See also Model.checkConsitency

	return nil
}

// expected interface is implemented
var _ consistencyChecker = (*SystemUser)(nil)

type shadow struct {
	ID     string
	Rounds string
	Salt   string
	Hash   string
}

// crypt(3) compatible hashes have the forms:
// - $id$salt$hash
// - $id$rounds=N$salt$hash
func parseShadowLine(line string) (*shadow, error) {
	l := strings.SplitN(line, "$", 5)
	if len(l) != 4 && len(l) != 5 {
		return nil, fmt.Errorf(`hashed password must be of the form "$integer-id$salt$hash", see crypt(3)`)
	}

	// if rounds is the second field, the line must consist of 4
	if strings.HasPrefix(l[2], "rounds=") && len(l) == 4 {
		return nil, fmt.Errorf(`missing hash field`)
	}

	// shadow line without $rounds=N$
	if len(l) == 4 {
		return &shadow{
			ID:   l[1],
			Salt: l[2],
			Hash: l[3],
		}, nil
	}
	// shadow line with rounds
	return &shadow{
		ID:     l[1],
		Rounds: l[2],
		Salt:   l[3],
		Hash:   l[4],
	}, nil
}

// see crypt(3) for the legal chars
var isValidSaltAndHash = regexp.MustCompile(`^[a-zA-Z0-9./]+$`).MatchString

func checkHashedPassword(headers map[string]interface{}, name string) (string, error) {
	pw := mylog.Check2(checkOptionalString(headers, name))

	// the pw string is optional, so just return if its empty
	if pw == "" {
		return "", nil
	}

	// parse the shadow line
	shd := mylog.Check2(parseShadowLine(pw))

	// and verify it

	// see crypt(3), ID 6 means SHA-512 (since glibc 2.7)
	ID := mylog.Check2(strconv.Atoi(shd.ID))

	// double check that we only allow modern hashes
	if ID < 6 {
		return "", fmt.Errorf("%q header only supports $id$ values of 6 (sha512crypt) or higher", name)
	}

	// the $rounds=N$ part is optional
	if strings.HasPrefix(shd.Rounds, "rounds=") {
		rounds := mylog.Check2(strconv.Atoi(strings.SplitN(shd.Rounds, "=", 2)[1]))

		if rounds < 5000 || rounds > 999999999 {
			return "", fmt.Errorf("%q header rounds parameter out of bounds: %d", name, rounds)
		}
	}

	if !isValidSaltAndHash(shd.Salt) {
		return "", fmt.Errorf("%q header has invalid chars in salt %q", name, shd.Salt)
	}
	if !isValidSaltAndHash(shd.Hash) {
		return "", fmt.Errorf("%q header has invalid chars in hash %q", name, shd.Hash)
	}

	return pw, nil
}

func checkSystemUserPresence(assert assertionBase) (string, error) {
	str := mylog.Check2(checkOptionalString(assert.headers, "user-presence"))
	if err != nil || str == "" {
		return "", err
	}
	if assert.Format() < 2 {
		return "", fmt.Errorf(`the "user-presence" header is only supported for format 2 or greater`)
	}

	if str != "until-expiration" {
		return "", fmt.Errorf(`invalid "user-presence" header, only explicit valid value is "until-expiration": %q`, str)
	}
	return str, nil
}

func assembleSystemUser(assert assertionBase) (Assertion, error) {
	// brand-id here can be different from authority-id,
	// the code using the assertion must use the policy set
	// by the model assertion system-user-authority header
	email := mylog.Check2(checkNotEmptyString(assert.headers, "email"))
	mylog.Check2(mail.ParseAddress(email))

	series := mylog.Check2(checkStringList(assert.headers, "series"))

	models := mylog.Check2(checkStringList(assert.headers, "models"))

	serials := mylog.Check2(checkStringList(assert.headers, "serials"))

	if len(serials) > 0 && assert.Format() < 1 {
		return nil, fmt.Errorf(`the "serials" header is only supported for format 1 or greater`)
	}
	if len(serials) > 0 && len(models) != 1 {
		return nil, fmt.Errorf(`in the presence of the "serials" header "models" must specify exactly one model`)
	}
	mylog.Check2(checkOptionalString(assert.headers, "name"))
	mylog.Check2(checkStringMatches(assert.headers, "username", validSystemUserUsernames))

	password := mylog.Check2(checkHashedPassword(assert.headers, "password"))

	forcePasswordChange := mylog.Check2(checkOptionalBool(assert.headers, "force-password-change"))

	if forcePasswordChange && password == "" {
		return nil, fmt.Errorf(`cannot use "force-password-change" with an empty "password"`)
	}

	sshKeys := mylog.Check2(checkStringList(assert.headers, "ssh-keys"))

	since := mylog.Check2(checkRFC3339Date(assert.headers, "since"))

	until := mylog.Check2(checkRFC3339Date(assert.headers, "until"))

	if until.Before(since) {
		return nil, fmt.Errorf("'until' time cannot be before 'since' time")
	}
	expiration := mylog.Check2(checkSystemUserPresence(assert))

	// "global" system-user assertion can only be valid for 1y
	if len(models) == 0 && until.After(since.AddDate(1, 0, 0)) {
		return nil, fmt.Errorf("'until' time cannot be more than 365 days in the future when no models are specified")
	}

	return &SystemUser{
		assertionBase:       assert,
		series:              series,
		models:              models,
		serials:             serials,
		sshKeys:             sshKeys,
		since:               since,
		until:               until,
		expiration:          expiration,
		forcePasswordChange: forcePasswordChange,
	}, nil
}

func systemUserFormatAnalyze(headers map[string]interface{}, body []byte) (formatnum int, err error) {
	formatnum = 0

	serials := mylog.Check2(checkStringList(headers, "serials"))

	if len(serials) > 0 {
		formatnum = 1
	}

	presence := mylog.Check2(checkOptionalString(headers, "user-presence"))

	if presence != "" {
		formatnum = 2
	}

	return formatnum, nil
}
