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
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/snapcore/snapd/interfaces/builtin"
	prompting_errors "github.com/snapcore/snapd/interfaces/prompting/errors"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/sandbox/cgroup"
)

var (
	cgroupProcessPathInTrackingCgroup = cgroup.ProcessPathInTrackingCgroup
)

// Request holds information about the content of a prompting request, as well
// as a means to send a reply to the originator of that request.
type Request struct {
	// Key is a namespaced ID which uniquely identifies this request. If a
	// request is re-received (indicated by a key matching one which was
	// previously received), then the content of the request must be identical
	// to that which was previously received.
	Key string
	// UID is the user ID of the subject which triggered the request.
	UID uint32
	// PID is the process ID of the process which triggered the request.
	PID int32
	// Cgroup is the cgroup path of the process which triggered the request.
	Cgroup string
	// AppArmorLabel is the AppArmor security label of the process which
	// triggered the request. E.g. snap.libreoffice.writer
	AppArmorLabel string
	// Interface is the snapd interface associated with this request.
	Interface string
	// Permissions is the abstract permissions being requested.
	Permissions []string
	// Path is the path associated with this request.
	// TODO: eventually decouple the concept of paths from Requests, Constraints,
	// and the rules backend, so interfaces without paths can be handled more
	// ergonomically.
	Path string
	// Reply causes a reply to be sent back to the originator of this request.
	Reply func(allowedPermissions []string) error
}

// NewListenerRequest parses the given [notify.MsgNotificationGeneric] into a
// [Request]. The request contains a reply closure which can be called by the
// manager or prompts backend. That reply closure, when called, converts its
// given abstract permissions to AppArmor permissions and uses the given
// `sendResponse` function to actually send the resulting response back to the
// kernel.
func NewListenerRequest(msg notify.MsgNotificationGeneric, sendResponse listener.SendResponseFunc) (*Request, error) {
	pid := msg.PID()
	cgroup, err := cgroupProcessPathInTrackingCgroup(int(pid))
	if err != nil {
		return nil, fmt.Errorf("cannot read cgroup path for request process with PID %d: %w", pid, err)
	}
	path := msg.Name()
	tagsets := msg.DeniedMetadataTagsets()
	iface, err := interfaceFromTagsets(tagsets)
	if err != nil {
		if !errors.Is(err, prompting_errors.ErrNoInterfaceTags) {
			// There was either more than one interface associated with tags, or
			// none which applied to all requested permissions.
			return nil, fmt.Errorf("cannot select interface from metadata tags: %w", err)
		}
		// There were no tags registered with a snapd interface, so we
		// look at the path to decide whether it's "home" or "camera".
		// XXX: this is a temporary workaround until metadata tags are
		// supported by the AppArmor parser and kernel.
		if builtin.DetectCameraFromPath(path) {
			iface = "camera"
		} else {
			iface = "home"
		}
	}
	id := msg.ID()
	key := buildListenerRequestKey(iface, id)
	aaAllowed, aaRequested, err := msg.AllowedDeniedPermissions()
	if err != nil {
		return nil, err
	}
	requestedPerms, err := AbstractPermissionsFromAppArmorPermissions(iface, aaRequested)
	if err != nil {
		return nil, err
	}
	reply := func(allowedPermissions []string) error {
		userAllowed, err := AbstractPermissionsToAppArmorPermissions(iface, allowedPermissions)
		if err != nil {
			return err
		}
		return sendResponse(id, aaAllowed, aaRequested, userAllowed)
	}
	return &Request{
		Key:           key,
		UID:           msg.SubjectUID(),
		PID:           pid,
		Cgroup:        cgroup,
		AppArmorLabel: msg.ProcessLabel(),
		Interface:     iface,
		Permissions:   requestedPerms,
		Path:          path,
		Reply:         reply,
	}, nil
}

func buildListenerRequestKey(iface string, id uint64) string {
	return fmt.Sprintf("kernel:%s:%016X", iface, id)
}

// Metadata stores information about the origin or applicability of a prompt or
// rule.
type Metadata struct {
	// User is the UID of the subject (user) triggering the applicable requests.
	User uint32
	// Snap is the instance name of the snap for which the prompt or rule applies.
	Snap string
	// PID is the PID of the process which triggered a request.
	// For rules, PID should be unset/ignored.
	PID int32
	// Cgroup is the cgroup path of the process which triggered a request.
	// For rules, Cgroup should be unset/ignored.
	Cgroup string
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

// MarshalText implements [encoding.TextMarshaler] for IDType. We need this so
// that IDType can be marshalled consistently when used as a map key, which is
// not addressible (so must have non-pointer receiver) and is converted to text
// so keys can be sorted before being marshalled as JSON.
//
// For more information, see [json.Marshal], in particular the discussion of
// marshalling map keys and values.
func (i IDType) MarshalText() ([]byte, error) {
	return []byte(i.String()), nil
}

// UnmarshalText implements [encoding.TextUnmarshaler] for IDType.
func (i *IDType) UnmarshalText(b []byte) error {
	id, err := IDFromString(string(b))
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

// At holds information about a particular point in time so it can be used to
// check whether rules or permission entries have expired.
type At struct {
	Time      time.Time
	SessionID IDType
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
	// LifespanSession indicates that a reply/rule should apply until the user
	// logs out.
	LifespanSession LifespanType = "session"
)

var (
	supportedLifespans = []string{string(LifespanForever), string(LifespanSession), string(LifespanSingle), string(LifespanTimespan)}
	// SupportedRuleLifespans defines the lifespans which are allowed for rules.
	// It is exported so it can be used outside this package when constructing
	// invalid lifespan errors.
	SupportedRuleLifespans = []string{string(LifespanForever), string(LifespanSession), string(LifespanTimespan)}
)

func (lifespan *LifespanType) UnmarshalJSON(data []byte) error {
	var lifespanStr string
	if err := json.Unmarshal(data, &lifespanStr); err != nil {
		return err
	}
	value := LifespanType(lifespanStr)
	switch value {
	case LifespanForever, LifespanSession, LifespanSingle, LifespanTimespan:
		*lifespan = value
	default:
		return prompting_errors.NewInvalidLifespanError(lifespanStr, supportedLifespans)
	}
	return nil
}

// ValidateExpiration checks that the given expiration and session ID are valid
// for the receiver lifespan.
//
// If the lifespan is LifespanTimespan, then expiration must be non-zero.
// Otherwise, it must be zero.
//
// If the lifespan is LifespanSession, then sessionID must be non-zero.
// Otherwise, it must be zero.
//
// Returns an error if any of the above are invalid.
func (lifespan LifespanType) ValidateExpiration(expiration time.Time, sessionID IDType) error {
	switch lifespan {
	case LifespanForever, LifespanSingle:
		if !expiration.IsZero() {
			return prompting_errors.NewInvalidExpirationError(expiration, fmt.Sprintf("cannot have specified expiration when lifespan is %q", lifespan))
		}
		if sessionID != 0 {
			return prompting_errors.NewInvalidExpirationError(expiration, fmt.Sprintf("cannot have specified session ID when lifespan is %q", lifespan))
		}
	case LifespanSession:
		if !expiration.IsZero() {
			return prompting_errors.NewInvalidExpirationError(expiration, fmt.Sprintf("cannot have specified expiration when lifespan is %q", lifespan))
		}
		if sessionID == 0 {
			return prompting_errors.NewInvalidExpirationError(expiration, fmt.Sprintf("cannot have unspecified session ID when lifespan is %q", lifespan))
		}
	case LifespanTimespan:
		if expiration.IsZero() {
			return prompting_errors.NewInvalidExpirationError(expiration, fmt.Sprintf("cannot have unspecified expiration when lifespan is %q", lifespan))
		}
		if sessionID != 0 {
			return prompting_errors.NewInvalidExpirationError(expiration, fmt.Sprintf("cannot have specified session ID when lifespan is %q", lifespan))
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
	case LifespanForever, LifespanSession, LifespanSingle:
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
