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

package apparmor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

// Specification assists in collecting apparmor entries associated with an interface.
type Specification struct {
	// context for various Add{...}Snippet functions
	securityTags []string
	snapName     string

	// snippets are indexed by security tag and describe parts of apparmor policy
	// for snap application and hook processes. The security tag encodes the identify
	// of the application or hook.
	snippets map[string][]string
	// sunSnippets are indexed by snap name and describe parts of apparmor policy
	// for snap-update-ns executing on behalf of a given snap.
	sunSnippets map[string][]string
}

// setScope sets the scope of subsequent AddSnippet family functions.
// The returned function resets the scope to an empty scope.
func (spec *Specification) setScope(securityTags []string, snapName string) (restore func()) {
	spec.securityTags = securityTags
	spec.snapName = snapName
	return func() {
		spec.securityTags = nil
		spec.snapName = ""
	}
}

// AddSnippet adds a new apparmor snippet to all applications using the interface.
func (spec *Specification) AddSnippet(snippet string) {
	if len(spec.securityTags) == 0 {
		return
	}
	if spec.snippets == nil {
		spec.snippets = make(map[string][]string)
	}
	for _, tag := range spec.securityTags {
		spec.snippets[tag] = append(spec.snippets[tag], snippet)
		sort.Strings(spec.snippets[tag])
	}
}

// AddSunSnippet adds a new apparmor snippet for the snap-update-ns program.
func (spec *Specification) AddSunSnippet(snippet string) {
	if spec.snapName == "" {
		return
	}
	if spec.sunSnippets == nil {
		spec.sunSnippets = make(map[string][]string)
	}
	spec.sunSnippets[spec.snapName] = append(spec.sunSnippets[spec.snapName], snippet)
}

// AddSnapLayout adds apparmor snippets based on the layout of the snap.
func (spec *Specification) AddSnapLayout(si *snap.Info) {
	if len(si.Layout) == 0 {
		return
	}

	// walk the layout elements in deterministic order, by mount point name
	paths := make([]string, 0, len(si.Layout))
	for path := range si.Layout {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	// get tags describing all apps and hooks
	tags := make([]string, 0, len(si.Apps)+len(si.Hooks))
	for _, app := range si.Apps {
		tags = append(tags, app.SecurityTag())
	}
	for _, hook := range si.Hooks {
		tags = append(tags, hook.SecurityTag())
	}

	// append layout snippets to all tags; the layout applies equally to the
	// entire snap as the entire snap uses one mount namespace.
	if spec.snippets == nil {
		spec.snippets = make(map[string][]string)
	}
	for _, tag := range tags {
		for _, path := range paths {
			snippet := snippetFromLayout(si.Layout[path])
			spec.snippets[tag] = append(spec.snippets[tag], snippet)
		}
		sort.Strings(spec.snippets[tag])
	}
}

func snippetFromLayout(layout *snap.Layout) string {
	mountPoint := layout.Snap.ExpandSnapVariables(layout.Path)
	return fmt.Sprintf("# Layout path: %[1]s\n%[1]s{,/**} mrwklix,", mountPoint)
}

func copySnippets(m map[string][]string) map[string][]string {
	result := make(map[string][]string, len(m))
	for k, v := range m {
		result[k] = append([]string(nil), v...)
	}
	return result
}

// Snippets returns a deep copy of all the added application snippets.
func (spec *Specification) Snippets() map[string][]string {
	return copySnippets(spec.snippets)
}

// SnippetForTag returns a combined snippet for given security tag with individual snippets
// joined with newline character. Empty string is returned for non-existing security tag.
func (spec *Specification) SnippetForTag(tag string) string {
	return strings.Join(spec.snippets[tag], "\n")
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

// SunSnippets returns a deep copy of all the added snap-update-ns snippets.
func (spec *Specification) SunSnippets() map[string][]string {
	return copySnippets(spec.sunSnippets)
}

// Implementation of methods required by interfaces.Specification

// AddConnectedPlug records apparmor-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		AppArmorConnectedPlug(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		restore := spec.setScope(plug.SecurityTags(), plug.Snap().Name())
		defer restore()
		return iface.AppArmorConnectedPlug(spec, plug, slot)
	}
	return nil
}

// AddConnectedSlot records mount-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		AppArmorConnectedSlot(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		restore := spec.setScope(slot.SecurityTags(), slot.Snap().Name())
		defer restore()
		return iface.AppArmorConnectedSlot(spec, plug, slot)
	}
	return nil
}

// AddPermanentPlug records mount-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *snap.PlugInfo) error {
	type definer interface {
		AppArmorPermanentPlug(spec *Specification, plug *snap.PlugInfo) error
	}
	if iface, ok := iface.(definer); ok {
		restore := spec.setScope(plug.SecurityTags(), plug.Snap.Name())
		defer restore()
		return iface.AppArmorPermanentPlug(spec, plug)
	}
	return nil
}

// AddPermanentSlot records mount-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *snap.SlotInfo) error {
	type definer interface {
		AppArmorPermanentSlot(spec *Specification, slot *snap.SlotInfo) error
	}
	if iface, ok := iface.(definer); ok {
		restore := spec.setScope(slot.SecurityTags(), slot.Snap.Name())
		defer restore()
		return iface.AppArmorPermanentSlot(spec, slot)
	}
	return nil
}
