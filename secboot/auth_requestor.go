// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

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

package secboot

import (
	"errors"
	"fmt"
	"io"
	"strings"

	sb "github.com/snapcore/secboot"
)


type authRequesterStringer struct {
}

func (s *authRequesterStringer) RequestUserCredentialString(name, path string, authType sb.UserAuthType) (string, error) {
	var fmtMsg string
	switch authType {
	case sb.UserAuthTypePassphrase:
		fmtMsg = "Enter passphrase for %[1]s (%[2]s):"
	case sb.UserAuthTypePIN:
		fmtMsg = "Enter PIN for %[1]s (%[2]s):"
	case sb.UserAuthTypeRecoveryKey:
		fmtMsg = "Enter recovery key for %[1]s (%[2]s):"
	case sb.UserAuthTypePassphrase | sb.UserAuthTypePIN:
		fmtMsg = "Enter passphrase or PIN for %[1]s (%[2]s):"
	case sb.UserAuthTypePassphrase | sb.UserAuthTypeRecoveryKey:
		fmtMsg = "Enter passphrase or recovery key for %[1]s (%[2]s):"
	case sb.UserAuthTypePIN | sb.UserAuthTypeRecoveryKey:
		fmtMsg = "Enter PIN or recovery key for %[1]s (%[2]s):"
	case sb.UserAuthTypePassphrase | sb.UserAuthTypePIN | sb.UserAuthTypeRecoveryKey:
		fmtMsg = "Enter passphrase, PIN or recovery key for %[1]s (%[2]s):"
	default:
		return "", errors.New("unexpected UserAuthType")
	}
	return fmt.Sprintf(fmtMsg, name, path), nil
}

func (s *authRequesterStringer) NotifyUserAuthResultString(name, path string, result sb.UserAuthResult, authTypes, unavailableAuthTypes sb.UserAuthType) (string, error) {
	switch result {
	case sb.UserAuthResultSuccess:
		var fmtString string
		switch authTypes {
		case sb.UserAuthTypePassphrase:
			fmtString = "Unlocked %s (%s) successfully with passphrase"
		case sb.UserAuthTypePIN:
			fmtString = "Unlocked %s (%s) successfully with PIN"
		case sb.UserAuthTypeRecoveryKey:
			fmtString = "Unlocked %s (%s) successfully with recovery key"
		default:
			return "", errors.New("unexpected UserAuthType")
		}
		return fmt.Sprintf(fmtString, name, path), nil
	case sb.UserAuthResultFailed:
		var b strings.Builder

		switch authTypes {
		case sb.UserAuthTypePassphrase:
			io.WriteString(&b, "Incorrect passphrase")
		case sb.UserAuthTypePIN:
			io.WriteString(&b, "Incorrect PIN")
		case sb.UserAuthTypeRecoveryKey:
			io.WriteString(&b, "Incorrect recovery key")
		case sb.UserAuthTypePassphrase | sb.UserAuthTypePIN:
			io.WriteString(&b, "Incorrect passphrase or PIN")
		case sb.UserAuthTypePassphrase | sb.UserAuthTypeRecoveryKey:
			io.WriteString(&b, "Incorrect passphrase or recovery key")
		case sb.UserAuthTypePIN | sb.UserAuthTypeRecoveryKey:
			io.WriteString(&b, "Incorrect PIN or recovery key")
		case sb.UserAuthTypePassphrase | sb.UserAuthTypePIN | sb.UserAuthTypeRecoveryKey:
			io.WriteString(&b, "Incorrect passphrase, PIN or recovery key")
		default:
			return "", errors.New("unexpected UserAuthType")
		}

		switch unavailableAuthTypes {
		case sb.UserAuthType(0):
		case sb.UserAuthTypePassphrase:
			io.WriteString(&b, ". No more passphrase tries remaining")
		case sb.UserAuthTypePIN:
			io.WriteString(&b, ". No more PIN tries remaining")
		case sb.UserAuthTypeRecoveryKey:
			io.WriteString(&b, ". No more recovery key tries remaining")
		case sb.UserAuthTypePassphrase | sb.UserAuthTypePIN:
			io.WriteString(&b, ". No more passphrase or PIN tries remaining")
		case sb.UserAuthTypePassphrase | sb.UserAuthTypeRecoveryKey:
			io.WriteString(&b, ". No more passphrase or recovery key tries remaining")
		case sb.UserAuthTypePIN | sb.UserAuthTypeRecoveryKey:
			io.WriteString(&b, ". No more PIN or recovery key tries remaining")
		case sb.UserAuthTypePassphrase | sb.UserAuthTypePIN | sb.UserAuthTypeRecoveryKey:
			io.WriteString(&b, ". No more passphrase, PIN or recovery key tries remaining")
		default:
			return "", errors.New("unexpected UserAuthType")
		}

		return b.String(), nil
	case sb.UserAuthResultInvalidFormat:
		switch authTypes {
		case sb.UserAuthTypePIN:
			return "Invalid PIN", nil
		case sb.UserAuthTypeRecoveryKey:
			return "Invalid recovery key", nil
		case sb.UserAuthTypePIN | sb.UserAuthTypeRecoveryKey:
			return "Invalid PIN or recovery key", nil
		default:
			return "", errors.New("unexpected UserAuthType")
		}
	default:
		return "", errors.New("unexpected UserAuthResult")
	}
}
