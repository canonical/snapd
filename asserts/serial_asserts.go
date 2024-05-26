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
	"errors"
	"fmt"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/strutil"
)

// Serial holds a serial assertion, which is a statement binding a
// device identity with the device public key.
type Serial struct {
	assertionBase
	timestamp time.Time
	pubKey    PublicKey
}

// BrandID returns the brand identifier of the device.
func (ser *Serial) BrandID() string {
	return ser.HeaderString("brand-id")
}

// Model returns the model name identifier of the device.
func (ser *Serial) Model() string {
	return ser.HeaderString("model")
}

// Serial returns the serial identifier of the device, together with
// brand id and model they form the unique identifier of the device.
func (ser *Serial) Serial() string {
	return ser.HeaderString("serial")
}

// DeviceKey returns the public key of the device.
func (ser *Serial) DeviceKey() PublicKey {
	return ser.pubKey
}

// Timestamp returns the time when the serial assertion was issued.
func (ser *Serial) Timestamp() time.Time {
	return ser.timestamp
}

func (ser *Serial) checkConsistency(db RODatabase, acck *AccountKey) error {
	if ser.AuthorityID() != ser.BrandID() {
		// serial authority and brand do not match, check the model
		a := mylog.Check2(db.Find(ModelType, map[string]string{
			"series":   release.Series,
			"brand-id": ser.BrandID(),
			"model":    ser.Model(),
		}))
		if err != nil && !errors.Is(err, &NotFoundError{}) {
			return err
		}
		if errors.Is(err, &NotFoundError{}) || !strutil.ListContains(a.(*Model).SerialAuthority(), ser.AuthorityID()) {
			return fmt.Errorf("serial with authority %q different from brand %q without model assertion with serial-authority set to to allow for them", ser.AuthorityID(), ser.BrandID())
		}
	}
	return nil
}

func assembleSerial(assert assertionBase) (Assertion, error) {
	// brand-id and authority-id can diverge if the model allows
	// for it via serial-authority, check for brand-id well-formedness
	_ := mylog.Check2(checkStringMatches(assert.headers, "brand-id", validAccountID))

	_ = mylog.Check2(checkModel(assert.headers))

	encodedKey := mylog.Check2(checkNotEmptyString(assert.headers, "device-key"))

	pubKey := mylog.Check2(DecodePublicKey([]byte(encodedKey)))

	keyID := mylog.Check2(checkNotEmptyString(assert.headers, "device-key-sha3-384"))

	if keyID != pubKey.ID() {
		return nil, fmt.Errorf("device key does not match provided key id")
	}

	timestamp := mylog.Check2(checkRFC3339Date(assert.headers, "timestamp"))

	// ignore extra headers and non-empty body for future compatibility
	return &Serial{
		assertionBase: assert,
		timestamp:     timestamp,
		pubKey:        pubKey,
	}, nil
}

// SerialRequest holds a serial-request assertion, which is a self-signed request to obtain a full device identity bound to the device public key.
type SerialRequest struct {
	assertionBase
	pubKey PublicKey
}

// BrandID returns the brand identifier of the device making the request.
func (sreq *SerialRequest) BrandID() string {
	return sreq.HeaderString("brand-id")
}

// Model returns the model name identifier of the device making the request.
func (sreq *SerialRequest) Model() string {
	return sreq.HeaderString("model")
}

// Serial returns the optional proposed serial identifier for the device, the service taking the request might use it or ignore it.
func (sreq *SerialRequest) Serial() string {
	return sreq.HeaderString("serial")
}

// RequestID returns the id for the request, obtained from and to be presented to the serial signing service.
func (sreq *SerialRequest) RequestID() string {
	return sreq.HeaderString("request-id")
}

// DeviceKey returns the public key of the device making the request.
func (sreq *SerialRequest) DeviceKey() PublicKey {
	return sreq.pubKey
}

func assembleSerialRequest(assert assertionBase) (Assertion, error) {
	_ := mylog.Check2(checkNotEmptyString(assert.headers, "brand-id"))

	_ = mylog.Check2(checkModel(assert.headers))

	_ = mylog.Check2(checkNotEmptyString(assert.headers, "request-id"))

	_ = mylog.Check2(checkOptionalString(assert.headers, "serial"))

	encodedKey := mylog.Check2(checkNotEmptyString(assert.headers, "device-key"))

	pubKey := mylog.Check2(DecodePublicKey([]byte(encodedKey)))

	if pubKey.ID() != assert.SignKeyID() {
		return nil, fmt.Errorf("device key does not match included signing key id")
	}

	// ignore extra headers and non-empty body for future compatibility
	return &SerialRequest{
		assertionBase: assert,
		pubKey:        pubKey,
	}, nil
}

// DeviceSessionRequest holds a device-session-request assertion, which is a request wrapping a store-provided nonce to start a session by a device signed with its key.
type DeviceSessionRequest struct {
	assertionBase
	timestamp time.Time
}

// BrandID returns the brand identifier of the device making the request.
func (req *DeviceSessionRequest) BrandID() string {
	return req.HeaderString("brand-id")
}

// Model returns the model name identifier of the device making the request.
func (req *DeviceSessionRequest) Model() string {
	return req.HeaderString("model")
}

// Serial returns the serial identifier of the device making the request,
// together with brand id and model it forms the unique identifier of
// the device.
func (req *DeviceSessionRequest) Serial() string {
	return req.HeaderString("serial")
}

// Nonce returns the nonce obtained from store and to be presented when requesting a device session.
func (req *DeviceSessionRequest) Nonce() string {
	return req.HeaderString("nonce")
}

// Timestamp returns the time when the device-session-request was created.
func (req *DeviceSessionRequest) Timestamp() time.Time {
	return req.timestamp
}

func assembleDeviceSessionRequest(assert assertionBase) (Assertion, error) {
	_ := mylog.Check2(checkModel(assert.headers))

	_ = mylog.Check2(checkNotEmptyString(assert.headers, "nonce"))

	timestamp := mylog.Check2(checkRFC3339Date(assert.headers, "timestamp"))

	// ignore extra headers and non-empty body for future compatibility
	return &DeviceSessionRequest{
		assertionBase: assert,
		timestamp:     timestamp,
	}, nil
}
