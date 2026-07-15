// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package dbus

import (
	"bytes"
	"sort"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

// snippetEntry holds a dbus snippet together with its priority.
// Snippets with lower priority values appear first in the combined output.
// When priorities are equal, snippets are ordered lexicographically by their
// content to guarantee a stable, deterministic result.
type snippetEntry struct {
	priority int
	snippet  string
}

// Specification keeps all the dbus snippets.
type Specification struct {
	// Snippets are indexed by security tag.
	appSet       *interfaces.SnapAppSet
	snippets     map[string][]snippetEntry
	securityTags []string
}

func NewSpecification(appSet *interfaces.SnapAppSet) *Specification {
	return &Specification{appSet: appSet}
}

func (spec *Specification) SnapAppSet() *interfaces.SnapAppSet {
	return spec.appSet
}

// AddSnippet adds a new dbus snippet with default priority 0.
func (spec *Specification) AddSnippet(snippet string) {
	spec.AddSnippetWithPriority(snippet, 0)
}

// AddSnippetWithPriority adds a new dbus snippet with the given priority.
// Snippets are combined in ascending priority order; snippets with equal
// priority are further sorted lexicographically by their content to ensure
// a deterministic result.
func (spec *Specification) AddSnippetWithPriority(snippet string, priority int) {
	if len(spec.securityTags) == 0 {
		return
	}
	if spec.snippets == nil {
		spec.snippets = make(map[string][]snippetEntry)
	}
	for _, tag := range spec.securityTags {
		spec.snippets[tag] = append(spec.snippets[tag], snippetEntry{priority: priority, snippet: snippet})
	}
}

// Snippets returns a deep copy of all the added snippets, with each tag's
// snippets sorted by priority then by content for deterministic output.
func (spec *Specification) Snippets() map[string][]string {
	result := make(map[string][]string, len(spec.snippets))
	for tag, entries := range spec.snippets {
		sorted := sortedSnippets(entries)
		result[tag] = sorted
	}
	return result
}

// sortedSnippets returns the snippet strings from entries sorted by priority
// (ascending) and then lexicographically by content as a tie-breaker.
func sortedSnippets(entries []snippetEntry) []string {
	cp := make([]snippetEntry, len(entries))
	copy(cp, entries)
	sort.SliceStable(cp, func(i, j int) bool {
		if cp[i].priority != cp[j].priority {
			return cp[i].priority < cp[j].priority
		}
		return cp[i].snippet < cp[j].snippet
	})
	out := make([]string, len(cp))
	for i, e := range cp {
		out[i] = e.snippet
	}
	return out
}

// SnippetForTag returns a combined snippet for given security tag with individual snippets
// joined with newline character. Empty string is returned for non-existing security tag.
// Snippets are ordered by priority (ascending) then lexicographically by content.
func (spec *Specification) SnippetForTag(tag string) string {
	entries, ok := spec.snippets[tag]
	if !ok {
		return ""
	}
	var buffer bytes.Buffer
	for _, s := range sortedSnippets(entries) {
		buffer.WriteString(s)
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

// AddConnectedPlug records dbus-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		DBusConnectedPlug(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		tags, err := spec.appSet.SecurityTagsForConnectedPlug(plug)
		if err != nil {
			return err
		}

		spec.securityTags = tags
		defer func() { spec.securityTags = nil }()
		return iface.DBusConnectedPlug(spec, plug, slot)
	}
	return nil
}

// AddConnectedSlot records dbus-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		DBusConnectedSlot(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		tags, err := spec.appSet.SecurityTagsForConnectedSlot(slot)
		if err != nil {
			return err
		}

		spec.securityTags = tags
		defer func() { spec.securityTags = nil }()
		return iface.DBusConnectedSlot(spec, plug, slot)
	}
	return nil
}

// AddPermanentPlug records dbus-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *snap.PlugInfo) error {
	type definer interface {
		DBusPermanentPlug(spec *Specification, plug *snap.PlugInfo) error
	}
	if iface, ok := iface.(definer); ok {
		tags, err := spec.appSet.SecurityTagsForPlug(plug)
		if err != nil {
			return err
		}

		spec.securityTags = tags
		defer func() { spec.securityTags = nil }()
		return iface.DBusPermanentPlug(spec, plug)
	}
	return nil
}

// AddPermanentSlot records dbus-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *snap.SlotInfo) error {
	type definer interface {
		DBusPermanentSlot(spec *Specification, slot *snap.SlotInfo) error
	}
	if iface, ok := iface.(definer); ok {
		tags, err := spec.appSet.SecurityTagsForSlot(slot)
		if err != nil {
			return err
		}

		spec.securityTags = tags
		defer func() { spec.securityTags = nil }()
		return iface.DBusPermanentSlot(spec, slot)
	}
	return nil
}
