// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package seccomp

import (
	"bytes"
	"sort"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

// Specification keeps all the seccomp snippets.
type Specification struct {
	appSet *interfaces.SnapAppSet
	// Snippets are indexed by security tag.
	snippets     map[string][]string
	securityTags []string

	// suppressSuidCreationDeny tracks the security tags for which the
	// default setuid/setgid file creation deny rules should be omitted.
	suppressSuidCreationDeny map[string]bool
}

func NewSpecification(appSet *interfaces.SnapAppSet) *Specification {
	return &Specification{
		appSet: appSet,
	}
}

func (spec *Specification) SnapAppSet() *interfaces.SnapAppSet {
	return spec.appSet
}

// SetSuppressSuidCreationDeny requests that the default seccomp hardening
// rules denying creation of files with setuid/setgid bits set are omitted
// from the generated profiles of the apps and hooks bound to the active
// security tags (ie those that have the plug of the calling interface).
// Super-privileged interfaces such as dracut-support use this to allow
// staging files that preserve setuid/setgid modes (eg with cp -a).
func (spec *Specification) SetSuppressSuidCreationDeny() {
	if len(spec.securityTags) == 0 {
		return
	}
	if spec.suppressSuidCreationDeny == nil {
		spec.suppressSuidCreationDeny = make(map[string]bool)
	}
	for _, tag := range spec.securityTags {
		spec.suppressSuidCreationDeny[tag] = true
	}
}

// SuppressSuidCreationDeny returns true when the default setuid/setgid file
// creation deny rules should be omitted from the generated profile of the
// given security tag.
func (spec *Specification) SuppressSuidCreationDeny(tag string) bool {
	return spec.suppressSuidCreationDeny[tag]
}

// AddSnippet adds a new seccomp snippet.
func (spec *Specification) AddSnippet(snippet string) {
	if len(spec.securityTags) == 0 {
		return
	}
	if spec.snippets == nil {
		spec.snippets = make(map[string][]string)
	}
	for _, tag := range spec.securityTags {
		spec.snippets[tag] = append(spec.snippets[tag], snippet)
	}
}

// Snippets returns a deep copy of all the added snippets.
func (spec *Specification) Snippets() map[string][]string {
	result := make(map[string][]string, len(spec.snippets))
	for k, v := range spec.snippets {
		vCopy := make([]string, 0, len(v))
		vCopy = append(vCopy, v...)
		result[k] = vCopy
	}
	return result
}

// SnippetForTag returns a combined snippet for given security tag with individual snippets
// joined with newline character. Empty string is returned for non-existing security tag.
func (spec *Specification) SnippetForTag(tag string) string {
	var buffer bytes.Buffer
	sort.Strings(spec.snippets[tag])
	for _, snippet := range spec.snippets[tag] {
		buffer.WriteString(snippet)
		buffer.WriteRune('\n')
	}
	return buffer.String()
}

// SecurityTags returns a list of security tags which have a snippet.
func (spec *Specification) SecurityTags() []string {
	var tags []string
	for t := range spec.snippets {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

// Implementation of methods required by interfaces.Specification

// AddConnectedPlug records seccomp-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		SecCompConnectedPlug(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		tags, err := spec.appSet.SecurityTagsForConnectedPlug(plug)
		if err != nil {
			return err
		}

		spec.securityTags = tags
		defer func() { spec.securityTags = nil }()
		return iface.SecCompConnectedPlug(spec, plug, slot)
	}
	return nil
}

// AddConnectedSlot records seccomp-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		SecCompConnectedSlot(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		tags, err := spec.appSet.SecurityTagsForConnectedSlot(slot)
		if err != nil {
			return err
		}

		spec.securityTags = tags
		defer func() { spec.securityTags = nil }()
		return iface.SecCompConnectedSlot(spec, plug, slot)
	}
	return nil
}

// AddPermanentPlug records seccomp-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *snap.PlugInfo) error {
	type definer interface {
		SecCompPermanentPlug(spec *Specification, plug *snap.PlugInfo) error
	}
	if iface, ok := iface.(definer); ok {
		tags, err := spec.appSet.SecurityTagsForPlug(plug)
		if err != nil {
			return err
		}

		spec.securityTags = tags
		defer func() { spec.securityTags = nil }()
		return iface.SecCompPermanentPlug(spec, plug)
	}
	return nil
}

// AddPermanentSlot records seccomp-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *snap.SlotInfo) error {
	type definer interface {
		SecCompPermanentSlot(spec *Specification, slot *snap.SlotInfo) error
	}
	if iface, ok := iface.(definer); ok {
		tags, err := spec.appSet.SecurityTagsForSlot(slot)
		if err != nil {
			return err
		}

		spec.securityTags = tags
		defer func() { spec.securityTags = nil }()
		return iface.SecCompPermanentSlot(spec, slot)
	}
	return nil
}
