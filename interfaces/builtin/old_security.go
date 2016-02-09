// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package builtin

import (
	"fmt"

	"github.com/ubuntu-core/snappy/interfaces"
)

// SecurityOverrideDefinition is used to override apparmor or seccomp security.
type SecurityOverrideDefinition struct {
	ReadPaths    []string `yaml:"read-paths,omitempty" json:"read-paths,omitempty"`
	WritePaths   []string `yaml:"write-paths,omitempty" json:"write-paths,omitempty"`
	Abstractions []string `yaml:"abstractions,omitempty" json:"abstractions,omitempty"`
	Syscalls     []string `yaml:"syscalls,omitempty" json:"syscalls,omitempty"`

	// deprecated keys, we warn when we see those
	DeprecatedAppArmor interface{} `yaml:"apparmor,omitempty" json:"apparmor,omitempty"`
	DeprecatedSeccomp  interface{} `yaml:"seccomp,omitempty" json:"seccomp,omitempty"`
}

// SecurityPolicyDefinition is used to provide hand-crafted policy.
type SecurityPolicyDefinition struct {
	AppArmor string `yaml:"apparmor" json:"apparmor"`
	Seccomp  string `yaml:"seccomp" json:"seccomp"`
}

// SecurityDefinitions contains the common apparmor/seccomp definitions.
type SecurityDefinitions struct {
	// SecurityTemplate is a template name like "default".
	SecurityTemplate string `yaml:"security-template,omitempty" json:"security-template,omitempty"`
	// SecurityOverride is a override for the high level security json.
	SecurityOverride *SecurityOverrideDefinition `yaml:"security-override,omitempty" json:"security-override,omitempty"`
	// SecurityPolicy is a hand-crafted low-level policy.
	SecurityPolicy *SecurityPolicyDefinition `yaml:"security-policy,omitempty" json:"security-policy,omitempty"`
	// SecurityCaps is are the apparmor/seccomp capabilities for an app.
	SecurityCaps []string `yaml:"caps,omitempty" json:"caps,omitempty"`
}

// OldSecurityInterface allows to use 15.04 security features.
type OldSecurityInterface struct{}

// String returns the same value as Name().
func (iface *OldSecurityInterface) String() string {
	return iface.Name()
}

// Name returns the name of the old-security type.
func (iface *OldSecurityInterface) Name() string {
	return "old-security"
}

// SanitizePlug checks and possibly modifies a plug.
func (iface *OldSecurityInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("interface is not of type %q", iface))
	}
	// NOTE: there's nothing to do on the plug-side.
	return nil
}

// SanitizeSlot checks and possibly modifies a slot.
func (iface *OldSecurityInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of type %q", iface))
	}
	// TODO: sanitize SecurityDefinitions encoded as attributes.
	return nil
}

// PlugSecuritySnippet returns the configuration snippet required to provide a old-security.
func (iface *OldSecurityInterface) PlugSecuritySnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return nil, nil
	case interfaces.SecuritySecComp:
		return nil, nil
	case interfaces.SecurityDBus:
		return nil, nil
	case interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// SlotSecuritySnippet returns the configuration snippet required to use a old-security.
func (iface *OldSecurityInterface) SlotSecuritySnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return nil, nil
	case interfaces.SecuritySecComp:
		return nil, nil
	case interfaces.SecurityDBus:
		return nil, nil
	case interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}
