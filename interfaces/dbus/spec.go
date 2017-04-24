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
	"fmt"
	"sort"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

// Specification keeps all the dbus snippets.
type Specification struct {
	// Snippets are indexed by security tag.
	snippets     map[string][]string
	securityTags []string

	// sessionServices map a security tag to the dbus-serivce file content
	sessionServices map[string]string
}

// AddService adds a new dbus service
func (spec *Specification) AddService(bus, name string, appInfo *snap.AppInfo) {
	if bus == "session" {
		if spec.sessionServices == nil {
			spec.sessionServices = make(map[string]string)
		}
		// We set only 'Name' and 'Exec' for now. We may add
		// 'User' for 'system' services when we support
		// per-snap users. Don't specify 'SystemdService' and
		// just let dbus-daemon launch the service since
		// 'SystemdService' is only used by dbus-daemon to
		// tell systemd to launch the service and systemd user
		// sessions aren't available everywhere yet.
		spec.sessionServices[appInfo.SecurityTag()] = fmt.Sprintf(`[D-BUS Service]
Name=%s
Exec=%s
`, name, appInfo.LauncherCommand())
	}
}

// SessionServices returns a deep copy of all services
func (spec *Specification) SessionServices() map[string]string {
	result := make(map[string]string, len(spec.sessionServices))
	for k, v := range spec.sessionServices {
		result[k] = v
	}
	return result
}

// AddSnippet adds a new dbus snippet.
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
		result[k] = append([]string(nil), v...)
	}
	return result
}

// SnippetForTag returns a combined snippet for given security tag with individual snippets
// joined with newline character. Empty string is returned for non-existing security tag.
func (spec *Specification) SnippetForTag(tag string) string {
	var buffer bytes.Buffer
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

// AddConnectedPlug records dbus-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.Plug, slot *interfaces.Slot) error {
	type definer interface {
		DBusConnectedPlug(spec *Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	}
	if iface, ok := iface.(definer); ok {
		spec.securityTags = plug.SecurityTags()
		defer func() { spec.securityTags = nil }()
		return iface.DBusConnectedPlug(spec, plug, slot)
	}
	return nil
}

// AddConnectedSlot records dbus-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.Plug, slot *interfaces.Slot) error {
	type definer interface {
		DBusConnectedSlot(spec *Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	}
	if iface, ok := iface.(definer); ok {
		spec.securityTags = slot.SecurityTags()
		defer func() { spec.securityTags = nil }()
		return iface.DBusConnectedSlot(spec, plug, slot)
	}
	return nil
}

// AddPermanentPlug records dbus-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *interfaces.Plug) error {
	type definer interface {
		DBusPermanentPlug(spec *Specification, plug *interfaces.Plug) error
	}
	if iface, ok := iface.(definer); ok {
		spec.securityTags = plug.SecurityTags()
		defer func() { spec.securityTags = nil }()
		return iface.DBusPermanentPlug(spec, plug)
	}
	return nil
}

// AddPermanentSlot records dbus-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *interfaces.Slot) error {
	type definer interface {
		DBusPermanentSlot(spec *Specification, slot *interfaces.Slot) error
	}
	if iface, ok := iface.(definer); ok {
		spec.securityTags = slot.SecurityTags()
		defer func() { spec.securityTags = nil }()
		return iface.DBusPermanentSlot(spec, slot)
	}
	return nil
}
