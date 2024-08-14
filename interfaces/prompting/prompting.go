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

// Package prompting provides common types and functions related to AppArmor
// prompting.
package prompting

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

var (
	// ErrExpirationInThePast may be wrapped with the invalid expiration.
	ErrExpirationInThePast = fmt.Errorf("cannot have expiration time in the past")
)

// Metadata stores information about the origin or applicability of a prompt or
// rule.
type Metadata struct {
	// User is the UID of the subject (user) triggering the applicable requests.
	User uint32
	// Snap is the instance name of the snap for which the prompt or rule applies.
	Snap string
	// Interface is the interface for which the prompt or rule applies.
	Interface string
}

type IDType uint64

func (i IDType) String() string {
	return fmt.Sprintf("%016X", uint64(i))
}

func (i *IDType) MarshalJSON() ([]byte, error) {
	return json.Marshal(i.String())
}

func (i *IDType) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("cannot read ID into string: %w", err)
	}
	value, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return fmt.Errorf("cannot parse ID as uint64: %w", err)
	}
	*i = IDType(value)
	return nil
}

// OutcomeType describes the outcome associated with a reply or rule.
type OutcomeType string

const (
	// OutcomeUnset indicates that no outcome was specified, and should only
	// be used while unmarshalling outcome fields marked as omitempty.
	OutcomeUnset OutcomeType = ""
	// OutcomeAllow indicates that a corresponding request should be allowed.
	OutcomeAllow OutcomeType = "allow"
	// OutcomeDeny indicates that a corresponding request should be denied.
	OutcomeDeny OutcomeType = "deny"
)

func (outcome *OutcomeType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	value := OutcomeType(s)
	switch value {
	case OutcomeAllow, OutcomeDeny:
		*outcome = value
	default:
		return fmt.Errorf(`cannot have outcome other than %q or %q: %q`, OutcomeAllow, OutcomeDeny, value)
	}
	return nil
}

// AsBool returns true if the outcome is OutcomeAllow, false if the outcome is
// OutcomeDeny, or an error if it cannot be parsed.
func (outcome OutcomeType) AsBool() (bool, error) {
	switch outcome {
	case OutcomeAllow:
		return true, nil
	case OutcomeDeny:
		return false, nil
	default:
		return false, fmt.Errorf(`internal error: invalid outcome: %q`, outcome)
	}
}

// LifespanType describes the temporal scope for which a reply or rule applies.
type LifespanType string

const (
	// LifespanUnset indicates that no lifespan was specified, and should only
	// be used while unmarshalling lifespan fields marked as omitempty.
	LifespanUnset LifespanType = ""
	// LifespanForever indicates that the reply/rule should never expire.
	LifespanForever LifespanType = "forever"
	// LifespanSingle indicates that a reply should only apply once, and should
	// not be used to create a rule.
	LifespanSingle LifespanType = "single"
	// LifespanTimespan indicates that a reply/rule should apply for a given
	// duration or until a given expiration timestamp.
	LifespanTimespan LifespanType = "timespan"
	// TODO: add LifespanSession which expires after the user logs out
	// LifespanSession  LifespanType = "session"
)

func (lifespan *LifespanType) UnmarshalJSON(data []byte) error {
	var lifespanStr string
	if err := json.Unmarshal(data, &lifespanStr); err != nil {
		return err
	}
	value := LifespanType(lifespanStr)
	switch value {
	case LifespanForever, LifespanSingle, LifespanTimespan:
		*lifespan = value
	default:
		return fmt.Errorf(`cannot have lifespan other than %q, %q, or %q: %q`, LifespanForever, LifespanSingle, LifespanTimespan, value)
	}
	return nil
}

// ValidateExpiration checks that the given expiration is valid for the
// receiver lifespan.
//
// If the lifespan is LifespanTimespan, then expiration must be non-zero and be
// after the given currTime. Otherwise, it must be zero. Returns an error if
// any of the above are invalid.
func (lifespan LifespanType) ValidateExpiration(expiration time.Time, currTime time.Time) error {
	switch lifespan {
	case LifespanForever, LifespanSingle:
		if !expiration.IsZero() {
			return fmt.Errorf(`cannot have specified expiration when lifespan is %q: %q`, lifespan, expiration)
		}
	case LifespanTimespan:
		if expiration.IsZero() {
			return fmt.Errorf(`cannot have unspecified expiration when lifespan is %q`, lifespan)
		}
		if currTime.After(expiration) {
			return fmt.Errorf("%w: %q", ErrExpirationInThePast, expiration)
		}
	default:
		// Should not occur, since lifespan is validated when unmarshalled
		return fmt.Errorf(`internal error: invalid lifespan: %q`, lifespan)
	}
	return nil
}

// ParseDuration checks that the given duration is valid for the receiver
// lifespan and parses it into an expiration timestamp.
//
// If the lifespan is LifespanTimespan, then duration must be a string parsable
// by time.ParseDuration(), representing the duration of time for which the rule
// should be valid. Otherwise, it must be empty. Returns an error if any of the
// above are invalid, otherwise computes the expiration time of the rule based
// on the given currTime and the given duration and returns it.
func (lifespan LifespanType) ParseDuration(duration string, currTime time.Time) (time.Time, error) {
	var expiration time.Time
	switch lifespan {
	case LifespanForever, LifespanSingle:
		if duration != "" {
			return expiration, fmt.Errorf(`cannot have specified duration when lifespan is %q: %q`, lifespan, duration)
		}
	case LifespanTimespan:
		if duration == "" {
			return expiration, fmt.Errorf(`cannot have unspecified duration when lifespan is %q`, lifespan)
		}
		parsedDuration, err := time.ParseDuration(duration)
		if err != nil {
			return expiration, fmt.Errorf(`cannot parse duration: %w`, err)
		}
		if parsedDuration <= 0 {
			return expiration, fmt.Errorf(`cannot have zero or negative duration: %q`, duration)
		}
		expiration = currTime.Add(parsedDuration)
	default:
		// Should not occur, since lifespan is validated when unmarshalled
		return expiration, fmt.Errorf(`internal error: invalid lifespan: %q`, lifespan)
	}
	return expiration, nil
}
