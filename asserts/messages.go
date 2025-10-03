// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"strconv"
	"strings"
	"time"

	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/strutil"
)

var (
	validMessageID   = regexp.MustCompile(`^([a-zA-Z0-9]{4,16})(?:-(\d+))?$`)
	validMessageKind = regexp.MustCompile(`^[a-z]+(?:-[a-z]+)*$`)
)

// DeviceID represents a unique device identifier composed of <brand-id>.<model>.<serial>.
type DeviceID struct {
	Serial  string
	Model   string
	BrandID string
}

func newDeviceIDFromString(rawID string) (*DeviceID, error) {
	parts := strutil.SplitRightN(rawID, ".", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid device id: %s", rawID)
	}

	if !validModel.MatchString(parts[1]) {
		return nil, fmt.Errorf("invalid model: %s", parts[1])
	}

	if !validAccountID.MatchString(parts[2]) {
		return nil, fmt.Errorf("invalid brand-id: %s", parts[0])
	}

	return &DeviceID{Serial: parts[0], Model: parts[1], BrandID: parts[2]}, nil
}

// RequestMessage represents a request message assertion used to trigger actions on snapd.
type RequestMessage struct {
	assertionBase

	id     string
	seqNum int

	devices []DeviceID

	assumes []string

	sinceUntil
	timestamp time.Time
}

// AccountID returns the account identifier that sent this request message.
func (req *RequestMessage) AccountID() string {
	return req.HeaderString("account-id")
}

// ID returns the message identifier without any sequence number suffix.
func (req *RequestMessage) ID() string {
	return req.id
}

// SeqNum returns the message's sequence number within a sequence, or zero if not sequenced.
func (req *RequestMessage) SeqNum() int {
	return req.seqNum
}

// Kind returns the message kind that determines which subsystem handles the message.
func (req *RequestMessage) Kind() string {
	return req.HeaderString("message-kind")
}

// Devices returns the list of device IDs that this message targets.
func (req *RequestMessage) Devices() []DeviceID {
	return req.devices
}

// Assumes returns the list of device properties required for this message to be applied.
func (req *RequestMessage) Assumes() []string {
	return req.assumes
}

func assembleRequestMessage(assert assertionBase) (Assertion, error) {
	accountID := assert.HeaderString("account-id")
	if !validAccountID.MatchString(accountID) {
		return nil, fmt.Errorf("invalid account id: %s", accountID)
	}

	id, seqNum, err := parseMessageID(assert.HeaderString("message-id"))
	if err != nil {
		return nil, err
	}

	_, err = checkStringMatches(assert.headers, "message-kind", validMessageKind)
	if err != nil {
		return nil, err
	}

	deviceIDs, err := parseDevices(assert.headers)
	if err != nil {
		return nil, err
	}

	assumes, err := checkAssumes(assert.headers)
	if err != nil {
		return nil, err
	}

	sinceUntil, err := checkValidSinceUntilWhat(assert.headers, "header")
	if err != nil {
		return nil, err
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	if len(assert.body) == 0 {
		return nil, errors.New("body must not be empty")
	}

	return &RequestMessage{
		assertionBase: assert,
		id:            id,
		seqNum:        seqNum,
		devices:       deviceIDs,
		assumes:       assumes,
		sinceUntil:    *sinceUntil,
		timestamp:     timestamp,
	}, nil
}

func parseMessageID(rawID string) (string, int, error) {
	if !validMessageID.MatchString(rawID) {
		return "", 0, fmt.Errorf("invalid message-id: %s", rawID)
	}

	parts := strings.SplitN(rawID, "-", 2)
	if len(parts) == 1 {
		return parts[0], 0, nil
	}

	seqNum, _ := strconv.Atoi(parts[1])
	if seqNum <= 0 {
		return "", 0, fmt.Errorf("invalid message-id: sequence number must be greater than 0")
	}

	return parts[0], seqNum, nil
}

func parseDevices(headers map[string]any) ([]DeviceID, error) {
	devices, err := checkStringList(headers, "devices")
	if err != nil {
		return nil, err
	}
	if len(devices) == 0 {
		return nil, errors.New(`"devices" header must not be empty`)
	}

	var deviceIDs []DeviceID
	for i, rawDeviceId := range devices {
		deviceID, err := newDeviceIDFromString(rawDeviceId)
		if err != nil {
			return nil, fmt.Errorf("cannot parse device at position %d: %w", i+1, err)
		}

		deviceIDs = append(deviceIDs, *deviceID)
	}

	return deviceIDs, nil
}

func checkAssumes(headers map[string]any) ([]string, error) {
	assumes, err := checkStringList(headers, "assumes")
	if err != nil {
		return nil, err
	}

	err = naming.ValidateAssumes(assumes)
	if err != nil {
		return nil, fmt.Errorf("invalid assumes: %w", err)
	}

	return assumes, nil
}

func checkValidSinceUntilWhat(m map[string]any, what string) (*sinceUntil, error) {
	since, err := checkRFC3339DateWhat(m, "valid-since", what)
	if err != nil {
		return nil, err
	}

	until, err := checkRFC3339DateWhat(m, "valid-until", what)
	if err != nil {
		return nil, err
	}

	if until.Before(since) {
		return nil, fmt.Errorf("'valid-until' time cannot be before 'valid-since' time")
	}

	return &sinceUntil{since: since, until: until}, nil
}
