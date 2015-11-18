// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"golang.org/x/crypto/openpgp/packet"
)

// AccountKey holds an account-key assertion, asserting a public key
// belonging to the account.
type AccountKey struct {
	AssertionBase
	since     time.Time
	until     time.Time
	publicKey *packet.PublicKey
}

// AccountID returns the account-id of this account-key.
func (ak *AccountKey) AccountID() string {
	return ak.Header("account-id")
}

// Since returns the time when the account key starts being valid.
func (ak *AccountKey) Since() time.Time {
	return ak.since
}

// Until returns the time when the account key stops being valid.
func (ak *AccountKey) Until() time.Time {
	return ak.until
}

// TODO: move check* helpers to separate file if they get reused

func checkRFC3339Date(ab *AssertionBase, name string) (time.Time, error) {
	dateStr := ab.Header(name)
	if dateStr == "" {
		return time.Time{}, fmt.Errorf("%v header is mandatory", name)
	}
	date, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("%v header is not a RFC3339 date: %v", name, err)
	}
	return date, nil
}

func splitFormatAndDecode(formatAndBase64 []byte) (string, []byte, error) {
	parts := bytes.SplitN(formatAndBase64, []byte(" "), 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("expected format and base64 data separated by space")
	}
	buf := make([]byte, base64.StdEncoding.DecodedLen(len(parts[1])))
	n, err := base64.StdEncoding.Decode(buf, parts[1])
	if err != nil {
		return "", nil, fmt.Errorf("could not decode base64 data: %v", err)
	}
	return string(parts[0]), buf[:n], nil
}

func checkPublicKey(ab *AssertionBase, fingerprintName string) (*packet.PublicKey, error) {
	pubKeyBody := ab.Body()
	if len(pubKeyBody) == 0 {
		return nil, fmt.Errorf("expected public key, not empty body")
	}
	format, key, err := splitFormatAndDecode(pubKeyBody)
	if err != nil {
		return nil, fmt.Errorf("public key: %v", err)
	}
	if format != "openpgp" {
		return nil, fmt.Errorf("unsupported public key format: %q", format)
	}
	pkt, err := packet.Read(bytes.NewBuffer(key))
	if err != nil {
		return nil, fmt.Errorf("could not parse public key data: %v", err)
	}
	pubk, ok := pkt.(*packet.PublicKey)
	if !ok {
		return nil, fmt.Errorf("expected public key, got instead: %T", pkt)
	}
	fp, err := hex.DecodeString(ab.Header(fingerprintName))
	if err != nil {
		return nil, fmt.Errorf("could not parse %v header: %v", fingerprintName, err)
	}
	if len(fp) == 0 {
		return nil, fmt.Errorf("missing %v header", fingerprintName)
	}
	if bytes.Compare(fp, pubk.Fingerprint[:]) != 0 {
		return nil, fmt.Errorf("public key does not match provided fingerprint")
	}
	return pubk, nil
}

func buildAccountKey(assert AssertionBase) (Assertion, error) {
	if assert.Header("account-id") == "" {
		return nil, fmt.Errorf("account-id header is mandatory")
	}
	since, err := checkRFC3339Date(&assert, "since")
	if err != nil {
		return nil, err
	}
	until, err := checkRFC3339Date(&assert, "until")
	if err != nil {
		return nil, err
	}
	if !until.After(since) {
		return nil, fmt.Errorf("invalid 'since' and 'until' times (no gap after 'since' till 'until')")
	}
	pubk, err := checkPublicKey(&assert, "fingerprint")
	if err != nil {
		return nil, err
	}
	// ignore extra headers for future compatibility
	return &AccountKey{
		AssertionBase: assert,
		since:         since,
		until:         until,
		publicKey:     pubk,
	}, nil
}

func init() {
	typeRegistry[AccountKeyType] = &assertionTypeRegistration{
		builder:    buildAccountKey,
		primaryKey: []string{"account-id", "fingerprint"},
	}
}
