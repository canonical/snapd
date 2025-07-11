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
)

var (
	validMessageID   = regexp.MustCompile(`^([a-zA-Z0-9]{4,16})(?:-(\d+))?$`)
	validMessageKind = regexp.MustCompile(`^[a-z]+(?:-[a-z]+)*$`)
)

// DeviceID represents a unique device identifier composed of <brand-id>.<model>.<serial>.
type DeviceID struct {
	BrandID string
	Model   string
	Serial  string
}

// RequestMessage represents a request message assertion used to trigger actions on snapd.
type RequestMessage struct {
	assertionBase

	id     string
	seqNum *int

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

// SeqNum returns the message's sequence number within a sequence, or nil if not sequenced.
func (req *RequestMessage) SeqNum() *int {
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

	rawID, err := checkStringMatches(assert.headers, "message-id", validMessageID)
	if err != nil {
		return nil, err
	}

	id := rawID
	var seqNum *int
	dashIdx := strings.LastIndex(rawID, "-")
	if dashIdx != -1 {
		id = rawID[:dashIdx]
		seq, _ := strconv.Atoi(rawID[dashIdx+1:])
		seqNum = &seq
	}

	_, err = checkStringMatches(assert.headers, "message-kind", validMessageKind)
	if err != nil {
		return nil, err
	}

	validDeviceID := regexp.MustCompile(`^[^.]+\.[^.]+\.[^.]+$`)
	devices, err := checkStringListMatches(assert.headers, "devices", validDeviceID)
	if err != nil {
		return nil, err
	}
	if len(devices) == 0 {
		return nil, errors.New(`"devices" header must not be empty`)
	}

	var deviceIDs []DeviceID
	for i, device := range devices {
		errPrefix := fmt.Sprintf("cannot parse device at position %d", i+1)
		parts := strings.Split(device, ".")

		if !validAccountID.MatchString(parts[0]) {
			return nil, fmt.Errorf("%s: invalid brand-id: %s", errPrefix, parts[0])
		}

		if !validModel.MatchString(parts[1]) {
			return nil, fmt.Errorf("%s: invalid model: %s", errPrefix, parts[1])
		}

		deviceID := DeviceID{BrandID: parts[0], Model: parts[1], Serial: parts[2]}
		deviceIDs = append(deviceIDs, deviceID)
	}

	// TODO: Update after refactoring overlord.snapstate.checkAssumes for reuse
	// Currently matches snapd<version> or some[-feature]*
	var validAssumesHeuristic = regexp.MustCompile(`^(?:snapd[1-9]\d*(?:\.\d+)*|[a-z]+(?:-[a-z]+)*)$`)
	assumes, err := checkStringListMatches(assert.headers, "assumes", validAssumesHeuristic)
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
