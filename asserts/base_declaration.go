// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

	"github.com/snapcore/snapd/release"
)

// BaseDeclaration holds a base-declaration assertion, declaring the policies
// (to start with interface ones) applying to all snaps of a series.
type BaseDeclaration struct {
	assertionBase
	plugRules map[string]*PlugRule
	slotRules map[string]*SlotRule
	timestamp time.Time
}

// Series returns the series whose snaps are governed by the declaration.
func (basedcl *BaseDeclaration) Series() string {
	return basedcl.HeaderString("series")
}

// Timestamp returns the time when the base-declaration was issued.
func (basedcl *BaseDeclaration) Timestamp() time.Time {
	return basedcl.timestamp
}

// PlugRule returns the plug-side rule about the given interface if one was
// included in the plugs stanza of the declaration, otherwise it returns nil.
func (basedcl *BaseDeclaration) PlugRule(interfaceName string) *PlugRule {
	return basedcl.plugRules[interfaceName]
}

// SlotRule returns the slot-side rule about the given interface if one was
// included in the slots stanza of the declaration, otherwise it returns nil.
func (basedcl *BaseDeclaration) SlotRule(interfaceName string) *SlotRule {
	return basedcl.slotRules[interfaceName]
}

// Implement further consistency checks.
func (basedcl *BaseDeclaration) checkConsistency(db RODatabase, acck *AccountKey) error {
	if !db.IsTrustedAccount(basedcl.AuthorityID()) {
		return fmt.Errorf("base-declaration assertion for series %s is not signed by a directly trusted authority: %s", basedcl.Series(), basedcl.AuthorityID())
	}
	return nil
}

// expected interface is implemented
var _ consistencyChecker = (*BaseDeclaration)(nil)

func assembleBaseDeclaration(assert assertionBase) (Assertion, error) {
	var plugRules map[string]*PlugRule
	plugs, err := checkMap(assert.headers, "plugs")
	if err != nil {
		return nil, err
	}
	if plugs != nil {
		plugRules = make(map[string]*PlugRule, len(plugs))
		err := compilePlugRules(plugs, func(iface string, rule *PlugRule) {
			plugRules[iface] = rule
		})
		if err != nil {
			return nil, err
		}
	}

	var slotRules map[string]*SlotRule
	slots, err := checkMap(assert.headers, "slots")
	if err != nil {
		return nil, err
	}
	if slots != nil {
		slotRules = make(map[string]*SlotRule, len(slots))
		err := compileSlotRules(slots, func(iface string, rule *SlotRule) {
			slotRules[iface] = rule
		})
		if err != nil {
			return nil, err
		}
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	return &BaseDeclaration{
		assertionBase: assert,
		plugRules:     plugRules,
		slotRules:     slotRules,
		timestamp:     timestamp,
	}, nil
}

var (
	baseDeclarationCheckOrder      = []string{"type", "authority-id", "series"}
	baseDeclarationExpectedHeaders = map[string]any{
		"type":         "base-declaration",
		"authority-id": "canonical",
		"series":       release.Series,
	}
)

// InitBuiltinBaseDeclaration initializes the builtin base-declaration based on
// headers (or resets it if headers is nil).
func InitBuiltinBaseDeclaration(headers []byte) error {
	for i, as := range builtinAssertions {
		if _, ok := as.(*BaseDeclaration); ok {
			builtinAssertions = append(builtinAssertions[:i], builtinAssertions[i+1:]...)
			break
		}
	}

	if headers == nil {
		return nil
	}

	a, err := assembleBuiltinAssertion(BaseDeclarationType, headers, nil, builtinCheckParams{
		order:           baseDeclarationCheckOrder,
		expectedHeaders: baseDeclarationExpectedHeaders,
	})
	if err != nil {
		return err
	}

	builtinAssertions = append(builtinAssertions, a)
	return nil
}
