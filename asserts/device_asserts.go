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

// TODO: model assertion still needs final design review!

// Model holds a model assertion, which is a statement by a brand
// about the properties of a device model.
type Model struct {
	assertionBase
	allowedModes  []string
	requiredSnaps []string
	timestamp     time.Time
}

// BrandID returns the brand identifier. Same as the authority id.
func (mod *Model) BrandID() string {
	return mod.HeaderString("brand-id")
}

// Model returns the model name identifier.
func (mod *Model) Model() string {
	return mod.HeaderString("model")
}

// Series returns the series of the core software the model uses.
func (mod *Model) Series() string {
	return mod.HeaderString("series")
}

// Core returns the core snap the model uses.
func (mod *Model) Core() string {
	return mod.HeaderString("core")
}

// Architecture returns the archicteture the model is based on.
func (mod *Model) Architecture() string {
	return mod.HeaderString("architecture")
}

// Gadget returns the gadget snap the model uses.
func (mod *Model) Gadget() string {
	return mod.HeaderString("gadget")
}

// Kernel returns the kernel snap the model uses.
func (mod *Model) Kernel() string {
	return mod.HeaderString("kernel")
}

// Store returns the snap store the model uses.
func (mod *Model) Store() string {
	return mod.HeaderString("store")
}

// AllowedModes returns which ones of the "classic" and "developer" modes are allowed for the model.
func (mod *Model) AllowedModes() []string {
	return mod.allowedModes
}

// RequiredSnaps returns the snaps that must be installed at all times and cannot be removed for this model.
func (mod *Model) RequiredSnaps() []string {
	return mod.requiredSnaps
}

// Class returns which class the model belongs to defining policies for
// additional software installation.
func (mod *Model) Class() string {
	return mod.HeaderString("class")
}

// Timestamp returns the time when the model assertion was issued.
func (mod *Model) Timestamp() time.Time {
	return mod.timestamp
}

// Implement further consistency checks.
func (mod *Model) checkConsistency(db RODatabase, acck *AccountKey) error {
	// TODO: double check trust level of authority depending on class and possibly allowed-modes
	return nil
}

// sanity
var _ consistencyChecker = (*Model)(nil)

var modelMandatory = []string{"core", "architecture", "gadget", "kernel", "store", "class"}

func assembleModel(assert assertionBase) (Assertion, error) {
	if assert.headers["brand-id"] != assert.headers["authority-id"] {
		return nil, fmt.Errorf("authority-id and brand-id must match, model assertions are expected to be signed by the brand: %q != %q", assert.headers["authority-id"], assert.headers["brand-id"])
	}

	for _, mandatory := range modelMandatory {
		if _, err := checkNotEmptyString(assert.headers, mandatory); err != nil {
			return nil, err
		}
	}

	// TODO: check 'class' value already here? fundamental policy derives from it

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	// ignore extra headers and non-empty body for future compatibility
	return &Model{
		assertionBase: assert,
		allowedModes:  nil, // XXX: empty for now
		requiredSnaps: nil, // XXX: empty for now
		timestamp:     timestamp,
	}, nil
}

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

// TODO: implement further consistency checks for Serial but first review approach

func assembleSerial(assert assertionBase) (Assertion, error) {
	// TODO: authority-id can only == canonical or brand-id

	encodedKey, err := checkNotEmptyString(assert.headers, "device-key")
	if err != nil {
		return nil, err
	}
	pubKey, err := DecodePublicKey([]byte(encodedKey))
	if err != nil {
		return nil, err
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	// ignore extra headers and non-empty body for future compatibility
	return &Serial{
		assertionBase: assert,
		timestamp:     timestamp,
		pubKey:        pubKey,
	}, nil
}

// SerialProof holds a serial-proof assertion, which is a self-signed request to prove device owns device key.
type SerialProof struct {
	assertionBase
}

// Nonce returns the nonce obtained from store and to be presented when requesting a device session.
func (sproof *SerialProof) Nonce() string {
	return sproof.HeaderString("nonce")
}

func assembleSerialProof(assert assertionBase) (Assertion, error) {
	_, err := checkNotEmptyString(assert.headers, "nonce")
	if err != nil {
		return nil, err
	}

	return &SerialProof{
		assertionBase: assert,
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

// RequestID returns the id for the request, obtained from and to be presented to the serial signing service.
func (sreq *SerialRequest) RequestID() string {
	return sreq.HeaderString("request-id")
}

// DeviceKey returns the public key of the device making the request.
func (sreq *SerialRequest) DeviceKey() PublicKey {
	return sreq.pubKey
}

func assembleSerialRequest(assert assertionBase) (Assertion, error) {
	_, err := checkNotEmptyString(assert.headers, "brand-id")
	if err != nil {
		return nil, err
	}

	_, err = checkNotEmptyString(assert.headers, "model")
	if err != nil {
		return nil, err
	}

	_, err = checkNotEmptyString(assert.headers, "request-id")
	if err != nil {
		return nil, err
	}

	encodedKey, err := checkNotEmptyString(assert.headers, "device-key")
	if err != nil {
		return nil, err
	}
	pubKey, err := DecodePublicKey([]byte(encodedKey))
	if err != nil {
		return nil, err
	}

	if pubKey.ID() != assert.SignKeyID() {
		return nil, fmt.Errorf("device key does not match included signing key id")
	}

	// ignore extra headers and non-empty body for future compatibility
	return &SerialRequest{
		assertionBase: assert,
		pubKey:        pubKey,
	}, nil
}
