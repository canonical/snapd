// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package prompting

import (
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"time"
)

type OutcomeType string

const (
	OutcomeUnset OutcomeType = ""
	OutcomeAllow OutcomeType = "allow"
	OutcomeDeny  OutcomeType = "deny"
)

// IsAllow returns true if the outcome is OutcomeAllow, false if the outcome is
// OutcomeDeny, or an error if it cannot be parsed.
func (outcome OutcomeType) IsAllow() (bool, error) {
	switch outcome {
	case OutcomeAllow:
		return true, nil
	case OutcomeDeny:
		return false, nil
	default:
		return false, fmt.Errorf(`outcome must be %q or %q: %q`, OutcomeAllow, OutcomeDeny, outcome)
	}
}

// ValidateOutcome returns nil if the given outcome is valid, otherwise an error.
func ValidateOutcome(outcome OutcomeType) error {
	switch outcome {
	case OutcomeAllow, OutcomeDeny:
		return nil
	default:
		return fmt.Errorf(`outcome must be %q or %q: %q`, OutcomeAllow, OutcomeDeny, outcome)
	}
}

type LifespanType string

const (
	LifespanUnset    LifespanType = ""
	LifespanForever  LifespanType = "forever"
	LifespanSession  LifespanType = "session"
	LifespanSingle   LifespanType = "single"
	LifespanTimespan LifespanType = "timespan"
)

// ValidateLifespanExpiration checks that the given lifespan is valid and that
// the given expiration is valid for that lifespan.
//
// If the lifespan is LifespanTimespan LifespanTimespan, then expiration must
// be non-zero and be after the given currTime. Otherwise, it must be zero.
// Returns an error if any of the above are invalid.
func ValidateLifespanExpiration(lifespan LifespanType, expiration time.Time, currTime time.Time) error {
	switch lifespan {
	case LifespanForever, LifespanSession, LifespanSingle:
		if !expiration.IsZero() {
			return fmt.Errorf(`expiration must be omitted when lifespan is %q, but received non-zero expiration: %q`, lifespan, expiration)
		}
	case LifespanTimespan:
		if expiration.IsZero() {
			return fmt.Errorf(`expiration must be non-zero when lifespan is %q, but received empty expiration`, lifespan)
		}
		if currTime.After(expiration) {
			return fmt.Errorf("expiration time has already passed: %q", expiration)
		}
	default:
		return fmt.Errorf(`invalid lifespan: %q`, lifespan)
	}
	return nil
}

// ValidateLifespanParseDuration checks that the given lifespan is valid and
// that the given duration is valid for that lifespan.
//
// If the lifespan is LifespanTimespan, then duration must be a string parsable
// by time.ParseDuration(), representing the duration of time for which the rule
// should be valid. Otherwise, it must be empty. Returns an error if any of the
// above are invalid, otherwise computes the expiration time of the rule based
// on the current time and the given duration and returns it.
func ValidateLifespanParseDuration(lifespan LifespanType, duration string) (time.Time, error) {
	var expiration time.Time
	switch lifespan {
	case LifespanForever, LifespanSession, LifespanSingle:
		if duration != "" {
			return expiration, fmt.Errorf(`duration must be empty when lifespan is %q, but received non-empty duration: %q`, lifespan, duration)
		}
	case LifespanTimespan:
		if duration == "" {
			return expiration, fmt.Errorf(`duration must be non-empty when lifespan is %q, but received empty expiration`, lifespan)
		}
		parsedDuration, err := time.ParseDuration(duration)
		if err != nil {
			return expiration, fmt.Errorf(`error parsing duration string: %q`, duration)
		}
		if parsedDuration <= 0 {
			return expiration, fmt.Errorf(`duration must be greater than zero: %q`, duration)
		}
		expiration = time.Now().Add(parsedDuration)
	default:
		return expiration, fmt.Errorf(`invalid lifespan: %q`, lifespan)
	}
	return expiration, nil
}

// NewIDAndTimestamp returns a new unique ID and corresponding timestamp.
//
// The ID is the current unix time in nanoseconds encoded as a string in base32.
// The timestamp is the same time, encoded as a string in time.RFC3339Nano.
func NewIDAndTimestamp() (id string, timestamp time.Time) {
	now := time.Now()
	nowUnix := uint64(now.UnixNano())
	nowBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(nowBytes, nowUnix)
	id = base32.StdEncoding.EncodeToString(nowBytes)
	return id, now
}
