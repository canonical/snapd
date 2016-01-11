// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package caps

const (
	msgEnumerateRequest   = "enumerate-request"
	msgEnumerateResponse  = "enumerate-response"
	msgGrantRequest       = "grant-request"
	msgGrantAccepted      = "grant-accepted"
	msgGrantRejected      = "grant-rejected"
	msgGrantNotification  = "grant-notification"
	msgRevokeRequest      = "revoke-request"
	msgRevokeNotification = "revoke-notification"
	msgOverrideRequest    = "override-request"
	msgOverrideResponse   = "override-response"
)

type msgBase struct {
	Message string `json:"message"`
}

// EnumerateRequest This message instructs the provider snap to enumerate
// capabilities.
//
// Receiver: snap providing capabilities
//
// The hook must respond with the “enumerate-response” message.
type EnumerateRequest struct {
	msgBase
}

func makeEnumerateRequest() *EnumerateRequest {
	return &EnumerateRequest{
		msgBase: msgBase{Message: msgEnumerateRequest},
	}
}

// CapabilityInfo describes a single capability.
// Unlike the Capability type, this structure can be copied and contains only
// simple strings and maps of strings.
type CapabilityInfo struct {
	Label      string            `json:"label"`
	Type       string            `json:"type"`
	Attributes map[string]string `json:"attrs,omitempty"`
}

// EnumerateResponse message contains a description of capabilities that the provider snap
// is offering.
//
// Receiver: snap providing capabilities
//
// This hook can be fired multiple times and provide different output each
// time. Snappy caches the output for some unspecified time for performance
// reasons.
type EnumerateResponse struct {
	msgBase
	Provides map[string]CapabilityInfo `json:"provides,omitempty"`
}

func makeCapAttributeCopy(cap *Capability) map[string]string {
	attrs := make(map[string]string, len(cap.Attrs))
	for attrName, attrValue := range cap.Attrs {
		attrs[attrName] = attrValue
	}
	return attrs
}

func makeEnumerateResponse(caps map[string]*Capability) *EnumerateResponse {
	provides := make(map[string]CapabilityInfo, len(caps))
	for name, cap := range caps {
		provides[name] = CapabilityInfo{
			Label:      cap.Label,
			Type:       cap.Type.Name,
			Attributes: makeCapAttributeCopy(cap),
		}
	}
	return &EnumerateResponse{
		msgBase:  msgBase{Message: msgEnumerateResponse},
		Provides: provides,
	}
}

// GrantRequest message contains a request to grant a concrete capability.
//
// Receiver: snap providing capabilities
//
// The capability-name must agree with the “enumerate-response” given earlier.
// The provider can accept or reject the request by responding with one of the
// two messages below. When multiple capabilities are granted to a snap they
// are granted in sequence, one after another.
type GrantRequest struct {
	msgBase
	Name string `json:"name"`
	Type string `json:"type"`
}

func makeGrantRequest(cap *Capability) *GrantRequest {
	return &GrantRequest{
		msgBase: msgBase{Message: msgGrantRequest},
		Name:    cap.Name,
		Type:    cap.Type.Name,
	}
}

// GrantAccepted message expresses successful attempt to grant a capability.
//
// Receiver: snap providing capabilities
//
// It contains the actual values of capability-name, capability-label,
// capability-type and all the attributes of the capability. The value of
// capability-name and capability-type must agree with the values in the
// “grant-request” message.
type GrantAccepted struct {
	msgBase
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Label      string            `json:"label"`
	Attributes map[string]string `json:"attrs,omitempty"`
}

func makeGrantAccepted(cap *Capability) *GrantAccepted {
	return &GrantAccepted{
		msgBase:    msgBase{Message: msgGrantAccepted},
		Name:       cap.Name,
		Type:       cap.Type.Name,
		Label:      cap.Label,
		Attributes: makeCapAttributeCopy(cap),
	}
}

// GrantRejected message expresses unsuccessful attempt to grant a capability.
//
// Receiver: snap providing capabilities
//
// If reason is “capability-depleted” then the request failed because the
// capability cannot be granted to any more snaps. If reason is
// “no-such-capability” then the request referred to a capability that either
// was not offered or is no longer offered. If reason is equal to “other” then
// error-message describes the reason in more detail.
type GrantRejected struct {
	msgBase
	Reason       string `json:"reason"`
	ErrorMessage string `json:"error_message,omitempty"`
}

func makeGrantRejected(reason, message string) *GrantRejected {
	return &GrantRejected{
		msgBase:      msgBase{Message: msgGrantRejected},
		Reason:       reason,
		ErrorMessage: message,
	}
}

// RevokeRequest message expresses request to revoke the last consumer of a capability.
//
// Receiver: snap providing capabilities
//
// This message is intended to let the provider snap perform any optional
// resource management when the final (or only) consumer of a capability is
// being removed, by revoking access to the capability. There is no response to
// this message.
type RevokeRequest struct {
	msgBase
	Name string `json:"name"`
}

func makeRevokeRequest(cap *Capability) *RevokeRequest {
	return &RevokeRequest{
		msgBase: msgBase{Message: msgRevokeRequest},
		Name:    cap.Name,
	}
}

// GrantNotification message expresses the notification about a grant of a
// capability to a consumer.
//
// Receiver: snap consuming capabilities
//
// This message is used when the capability is being revoked. The message is
// informational only, the capability is granted as soon as the hook exits. The
// slot-name is the value matching the value from the snap metadata yaml
// (consumes section).
type GrantNotification struct {
	msgBase
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Label      string            `json:"label"`
	Attributes map[string]string `json:"attrs,omitempty"`
	Slot       string            `json:"slot"`
}

func makeGrantNotification(cap *Capability, slot string) *GrantNotification {
	return &GrantNotification{
		msgBase:    msgBase{Message: msgGrantNotification},
		Name:       cap.Name,
		Type:       cap.Type.Name,
		Label:      cap.Label,
		Attributes: makeCapAttributeCopy(cap),
		Slot:       slot,
	}
}

// RevokeNotification message expresses the notification about the revocation
// of a capability from a consumer.
//
// Receiver: snap consuming capabilities.
//
// This message is used when the capability is being revoked. The message is
// informational only, the capability is revoked as soon as the hook exits.
// NOTE: for security reasons, background applications are restarted after a
// capability is revoked.
type RevokeNotification struct {
	msgBase
	Name string `json:"name"`
	Slot string `json:"slot"`
}

func makeRevokeNotification(cap *Capability, slot string) *RevokeNotification {
	return &RevokeNotification{
		msgBase: msgBase{Message: msgRevokeNotification},
		Name:    cap.Name,
		Slot:    slot,
	}
}

// OverrideRequest message instructs the gadget snap to enumerate capability overrides.
//
// Receiver: the gadget snap
type OverrideRequest struct {
	msgBase
}

func makeOverrideRequest() *OverrideRequest {
	return &OverrideRequest{
		msgBase: msgBase{Message: msgOverrideRequest},
	}
}

// CapabilityRenameInfo expresses the intent to rename one capability.
type CapabilityRenameInfo struct {
	NewName  string `json:"new-name"`
	NewLabel string `json:"new-label"`
}

// OverrideResponse message contains the declarative description of the
// overrides the gadget snap wishes to make.
//
// Receiver: the gadget snap
//
// The response contains two optional section: renames and suppresses.  Renames
// can alter the name and label of any of the capabilities being offered by
// other snaps. Suppresses can suppress any of the capabilities offered by
// other snaps. Suppresses uses shell-like glob pattern matching.
type OverrideResponse struct {
	msgBase
	Renames    map[string]CapabilityRenameInfo `json:"renames,omitempty"`
	Suppresses []string                        `json:"suppresses,omitempty"`
}

func makeOverrideResponse(renames map[string]CapabilityRenameInfo, suppresses []string) *OverrideResponse {
	return &OverrideResponse{
		msgBase:    msgBase{Message: msgOverrideResponse},
		Renames:    renames,
		Suppresses: suppresses,
	}
}
