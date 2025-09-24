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
package fdestate

import (
	"fmt"
	"strings"
)

type KeyslotRefsNotFoundError struct {
	KeyslotRefs []KeyslotRef
}

func (e *KeyslotRefsNotFoundError) Error() string {
	switch len(e.KeyslotRefs) {
	case 0:
		// references not specified, keep error message generic
		return "key slot reference not found"
	case 1:
		return fmt.Sprintf("key slot reference %s not found", e.KeyslotRefs[0].String())
	default:
		var concatRefs strings.Builder
		concatRefs.WriteString(e.KeyslotRefs[0].String())
		for _, ref := range e.KeyslotRefs[1:] {
			concatRefs.WriteString(", ")
			concatRefs.WriteString(ref.String())
		}
		return fmt.Sprintf("key slot references [%s] not found", concatRefs.String())
	}
}

type keyslotsAlreadyExistsError struct {
	keyslots []Keyslot
}

func (e *keyslotsAlreadyExistsError) Error() string {
	if len(e.keyslots) == 1 {
		return fmt.Sprintf("key slot %s already exists", e.keyslots[0].Ref().String())
	} else {
		var concatRefs strings.Builder
		concatRefs.WriteString(e.keyslots[0].Ref().String())
		for _, ref := range e.keyslots[1:] {
			concatRefs.WriteString(", ")
			concatRefs.WriteString(ref.Ref().String())
		}
		return fmt.Sprintf("key slots [%s] already exist", concatRefs.String())
	}
}

type InvalidRecoveryKeyReason string

const (
	InvalidRecoveryKeyReasonExpired       InvalidRecoveryKeyReason = "expired"
	InvalidRecoveryKeyReasonNotFound      InvalidRecoveryKeyReason = "not-found"
	InvalidRecoveryKeyReasonInvalidFormat InvalidRecoveryKeyReason = "invalid-format"
	InvalidRecoveryKeyReasonNoMatchFound  InvalidRecoveryKeyReason = "no-match-found"
)

type InvalidRecoveryKeyError struct {
	Reason InvalidRecoveryKeyReason

	Message string
}

func (e *InvalidRecoveryKeyError) Error() string {
	if e.Message != "" {
		return e.Message
	}

	switch e.Reason {
	case InvalidRecoveryKeyReasonExpired:
		return "invalid recovery key: expired"
	case InvalidRecoveryKeyReasonNotFound:
		return "invalid recovery key: not found"
	case InvalidRecoveryKeyReasonInvalidFormat:
		return "invalid recovery key: bad format"
	case InvalidRecoveryKeyReasonNoMatchFound:
		return "invalid recovery key: no match found"
	default:
		return "internal error: unexpected recovery key error"
	}
}
