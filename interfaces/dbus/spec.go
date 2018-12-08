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
	"text/template"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

// Specification keeps all the dbus snippets.
type Specification struct {
	// Snippets are indexed by security tag.
	snippets     map[string][]string
	securityTags []string

	sessionServices map[string]*Service
	systemServices  map[string]*Service
}

// Service describes an activatable D-Bus service
type Service struct {
	SecurityTag string
	BusName     string
	Content     []byte
}

// AddService adds a new D-Bus service
func (spec *Specification) AddService(bus, name string, appInfo *snap.AppInfo) error {
	serviceTemplate := `[D-BUS Service]
Name={{.BusName}}
Comment=Bus name for snap application {{.App.Snap.InstanceName}}.{{.App.Name}}
Exec={{.App.LauncherCommand}}
{{- if .IsSystem }}
User=root
{{- end}}
{{- if .SystemdService }}
SystemdService={{.SystemdService}}
{{- end}}
X-Snap={{.App.Snap.InstanceName}}
`
	t := template.Must(template.New("dbus-service").Parse(serviceTemplate))
	serviceData := struct {
		App            *snap.AppInfo
		BusName        string
		SystemdService string
		IsSystem       bool
	}{
		App:      appInfo,
		BusName:  name,
		IsSystem: bus == "system",
	}
	var services map[string]*Service
	switch bus {
	case "session":
		if spec.sessionServices == nil {
			spec.sessionServices = make(map[string]*Service)
		}
		services = spec.sessionServices
		// TODO: extract systemd service name for user service, once integrated
	case "system":
		if spec.systemServices == nil {
			spec.systemServices = make(map[string]*Service)
		}
		services = spec.systemServices
		if appInfo.IsService() {
			// TODO: return an error if this is not a system serice
			serviceData.SystemdService = appInfo.ServiceName()
		}
	default:
		panic("Unknown D-Bus bus")
	}

	if old, ok := services[name]; ok && old.SecurityTag != appInfo.SecurityTag() {
		return fmt.Errorf("multiple apps have claimed D-Bus name %v", name)
	}

	var templateOut bytes.Buffer
	if err := t.Execute(&templateOut, serviceData); err != nil {
		return err
	}
	services[name] = &Service{
		SecurityTag: appInfo.SecurityTag(),
		BusName:     name,
		Content:     templateOut.Bytes(),
	}
	return nil
}

// SessionServices returns a copy of all session services
func (spec *Specification) SessionServices() map[string]*Service {
	result := make(map[string]*Service, len(spec.sessionServices))
	for k, v := range spec.sessionServices {
		result[k] = v
	}
	return result
}

// SystemServices returns a copy of all session services
func (spec *Specification) SystemServices() map[string]*Service {
	result := make(map[string]*Service, len(spec.systemServices))
	for k, v := range spec.systemServices {
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
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		DBusConnectedPlug(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		spec.securityTags = plug.SecurityTags()
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
		spec.securityTags = slot.SecurityTags()
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
		spec.securityTags = plug.SecurityTags()
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
		spec.securityTags = slot.SecurityTags()
		defer func() { spec.securityTags = nil }()
		return iface.DBusPermanentSlot(spec, slot)
	}
	return nil
}
