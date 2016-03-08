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
	return mod.Header("brand-id")
}

// Model returns the model name identifier.
func (mod *Model) Model() string {
	return mod.Header("model")
}

// Series returns the series of the core software the model uses.
func (mod *Model) Series() string {
	return mod.Header("series")
}

// OS returns the OS variant the model uses.
func (mod *Model) OS() string {
	return mod.Header("os")
}

// Architecture returns the archicteture the model is based on.
func (mod *Model) Architecture() string {
	return mod.Header("architecture")
}

// Gadget returns the gadget snap the model uses.
func (mod *Model) Gadget() string {
	return mod.Header("gadget")
}

// Kernel returns the kernel snap the model uses.
func (mod *Model) Kernel() string {
	return mod.Header("kernel")
}

// Store returns the snap store the model uses.
func (mod *Model) Store() string {
	return mod.Header("store")
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
	return mod.Header("class")
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

var modelMandatory = []string{"os", "architecture", "gadget", "kernel", "store", "class"}

func assembleModel(assert assertionBase) (Assertion, error) {
	if assert.headers["brand-id"] != assert.headers["authority-id"] {
		return nil, fmt.Errorf("authority-id and brand-id must match, model assertions are expected to be signed by the brand: %q != %q", assert.headers["authority-id"], assert.headers["brand-id"])
	}

	for _, mandatory := range modelMandatory {
		if _, err := checkMandatory(assert.headers, mandatory); err != nil {
			return nil, err
		}
	}

	// TODO: check 'class' value already here? fundamental policy derives from it

	allowedModes, err := checkCommaSepList(assert.headers, "allowed-modes")
	if err != nil {
		return nil, err
	}

	requiredSnaps, err := checkCommaSepList(assert.headers, "required-snaps")
	if err != nil {
		return nil, err
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	// ignore extra headers and non-empty body for future compatibility
	return &Model{
		assertionBase: assert,
		allowedModes:  allowedModes,
		requiredSnaps: requiredSnaps,
		timestamp:     timestamp,
	}, nil
}

// DeviceSerial holds a device-serial assertion, which is a statement binding a
// device identity with the device public key.
type DeviceSerial struct {
	assertionBase
	timestamp time.Time
	pubKey    PublicKey
}

// BrandID returns the brand identifier of the device.
func (ds *DeviceSerial) BrandID() string {
	return ds.Header("brand-id")
}

// Model returns the model name identifier of the device.
func (ds *DeviceSerial) Model() string {
	return ds.Header("model")
}

// Serial returns the serial of the device, together with brand id and model
// they form the unique identifier of the device.
func (ds *DeviceSerial) Serial() string {
	return ds.Header("serial")
}

// DeviceKey returns the public key of the device.
func (ds *DeviceSerial) DeviceKey() PublicKey {
	return ds.pubKey
}

// Timestamp returns the time when the device-serial assertion was issued.
func (ds *DeviceSerial) Timestamp() time.Time {
	return ds.timestamp
}

// TODO: implement further consistency checks for DeviceSerial but first review approach

func assembleDeviceSerial(assert assertionBase) (Assertion, error) {
	// TODO: authority-id can only == canonical or brand-id

	encodedKey, err := checkMandatory(assert.headers, "device-key")
	if err != nil {
		return nil, err
	}
	pubKey, err := decodePublicKey([]byte(encodedKey))
	if err != nil {
		return nil, err
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	// ignore extra headers and non-empty body for future compatibility
	return &DeviceSerial{
		assertionBase: assert,
		timestamp:     timestamp,
		pubKey:        pubKey,
	}, nil
}
