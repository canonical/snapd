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

	prompting_errors "github.com/snapcore/snapd/interfaces/prompting/errors"
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

func IDFromString(idStr string) (IDType, error) {
	value, err := strconv.ParseUint(idStr, 16, 64)
	if err != nil {
		return IDType(0), fmt.Errorf("%w: %v", prompting_errors.ErrInvalidID, err)
	}
	return IDType(value), nil
}

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
	id, err := IDFromString(s)
	if err != nil {
		return err
	}
	*i = id
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

var supportedOutcomes = []string{string(OutcomeAllow), string(OutcomeDeny)}

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
		return prompting_errors.NewInvalidOutcomeError(s, supportedOutcomes)
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
		return false, prompting_errors.NewInvalidOutcomeError(string(outcome), supportedOutcomes)
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

var (
	supportedLifespans = []string{string(LifespanForever), string(LifespanSingle), string(LifespanTimespan)}
	// SupportedRuleLifespans is exported so interfaces/promptin/requestrules
	// can use it when constructing a ErrRuleLifespanSingle
	SupportedRuleLifespans = []string{string(LifespanForever), string(LifespanTimespan)}
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
		return prompting_errors.NewInvalidLifespanError(lifespanStr, supportedLifespans)
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
			return prompting_errors.NewInvalidExpirationError(expiration, fmt.Sprintf("cannot have specified expiration when lifespan is %q", lifespan))
		}
	case LifespanTimespan:
		if expiration.IsZero() {
			return prompting_errors.NewInvalidExpirationError(expiration, fmt.Sprintf("cannot have unspecified expiration when lifespan is %q", lifespan))
		}
		if currTime.After(expiration) {
			return fmt.Errorf("%w: %q", prompting_errors.ErrRuleExpirationInThePast, expiration)
		}
	default:
		// Should not occur, since lifespan is validated when unmarshalled
		return prompting_errors.NewInvalidLifespanError(string(lifespan), supportedLifespans)
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
			return expiration, prompting_errors.NewInvalidDurationError(duration, fmt.Sprintf("cannot have specified duration when lifespan is %q", lifespan))
		}
	case LifespanTimespan:
		if duration == "" {
			return expiration, prompting_errors.NewInvalidDurationError(duration, fmt.Sprintf("cannot have unspecified duration when lifespan is %q", lifespan))
		}
		parsedDuration, err := time.ParseDuration(duration)
		if err != nil {
			return expiration, prompting_errors.NewInvalidDurationError(duration, fmt.Sprintf("cannot parse duration: %v", err))
		}
		if parsedDuration <= 0 {
			return expiration, prompting_errors.NewInvalidDurationError(duration, fmt.Sprintf("cannot have zero or negative duration: %q", duration))
		}
		expiration = currTime.Add(parsedDuration)
	default:
		// Should not occur, since lifespan is validated when unmarshalled
		return expiration, prompting_errors.NewInvalidLifespanError(string(lifespan), supportedLifespans)
	}
	return expiration, nil
}
