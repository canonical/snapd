// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2018 Canonical Ltd
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

package udev

import (
	"fmt"
	"sort"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

type entry struct {
	snippet string
	iface   string
	tag     string
}

// Specification assists in collecting udev snippets associated with an interface.
type Specification struct {
	// Snippets are stored in a map for de-duplication
	snippets map[string]bool
	entries  []entry
	iface    string

	securityTags             []string
	udevadmSubsystemTriggers []string
	controlsDeviceCgroup     bool
}

// SetControlsDeviceCgroup marks a specification as needing to control
// its own device cgroup which prevents generation of any udev tagging rules
// for this snap name
// TODO: this setting should also imply setting Delegates=true in the
// ServicePermanentPlug somehow, perhaps just for the commonInterface
func (spec *Specification) SetControlsDeviceCgroup() {
	spec.controlsDeviceCgroup = true
}

// ControlsDeviceCgroup returns whether a specification was marked as needing to
// control its own device cgroup which prevents generation of any udev tagging
// rules for this snap name.
func (spec *Specification) ControlsDeviceCgroup() bool {
	return spec.controlsDeviceCgroup
}

func (spec *Specification) addEntry(snippet, tag string) {
	if spec.snippets == nil {
		spec.snippets = make(map[string]bool)
	}
	if !spec.snippets[snippet] {
		spec.snippets[snippet] = true
		e := entry{
			snippet: snippet,
			iface:   spec.iface,
			tag:     tag,
		}
		spec.entries = append(spec.entries, e)
	}
}

// AddSnippet adds a new udev snippet.
func (spec *Specification) AddSnippet(snippet string) {
	spec.addEntry(snippet, "")
}

func udevTag(securityTag string) string {
	return strings.Replace(securityTag, ".", "_", -1)
}

// TagDevice adds an app/hook specific udev tag to devices described by the
// snippet and adds an app/hook-specific RUN rule for hotplugging.
func (spec *Specification) TagDevice(snippet string) {
	for _, securityTag := range spec.securityTags {
		tag := udevTag(securityTag)
		spec.addEntry(fmt.Sprintf("# %s\n%s, TAG+=\"%s\"", spec.iface, snippet, tag), tag)
		// SUBSYSTEM=="module" is for kernel modules not devices.
		// SUBSYSTEM=="subsystem" is for subsystems (the top directories in /sys/class). Not for devices.
		// When loaded, they send an ADD event
		// snap-device-helper expects devices only, not modules nor subsystems
		spec.addEntry(fmt.Sprintf("TAG==\"%s\", SUBSYSTEM!=\"module\", SUBSYSTEM!=\"subsystem\", RUN+=\"%s/snap-device-helper %s\"",
			tag, dirs.DistroLibExecDir, tag), tag)
	}
}

type byTagAndSnippet []entry

func (c byTagAndSnippet) Len() int      { return len(c) }
func (c byTagAndSnippet) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c byTagAndSnippet) Less(i, j int) bool {
	if c[i].tag != c[j].tag {
		return c[i].tag < c[j].tag
	}
	return c[i].snippet < c[j].snippet
}

// Snippets returns a copy of all the snippets added so far.
func (spec *Specification) Snippets() (result []string) {
	// If one of the interfaces controls it's own device cgroup, then
	// we don't want to enforce a device cgroup, which is only turned on if
	// there are udev rules, and as such we don't want to generate any udev
	// rules

	if spec.ControlsDeviceCgroup() {
		return nil
	}
	entries := make([]entry, len(spec.entries))
	copy(entries, spec.entries)
	sort.Sort(byTagAndSnippet(entries))

	result = make([]string, 0, len(spec.entries))
	for _, entry := range entries {
		result = append(result, entry.snippet)
	}
	return result
}

// Implementation of methods required by interfaces.Specification

// AddConnectedPlug records udev-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		UDevConnectedPlug(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	ifname := iface.Name()
	if iface, ok := iface.(definer); ok {
		spec.securityTags = plug.SecurityTags()
		spec.iface = ifname
		defer func() { spec.securityTags = nil; spec.iface = "" }()
		return iface.UDevConnectedPlug(spec, plug, slot)
	}
	return nil
}

// AddConnectedSlot records mount-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		UDevConnectedSlot(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	ifname := iface.Name()
	if iface, ok := iface.(definer); ok {
		spec.securityTags = slot.SecurityTags()
		spec.iface = ifname
		defer func() { spec.securityTags = nil; spec.iface = "" }()
		return iface.UDevConnectedSlot(spec, plug, slot)
	}
	return nil
}

// AddPermanentPlug records mount-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *snap.PlugInfo) error {
	type definer interface {
		UDevPermanentPlug(spec *Specification, plug *snap.PlugInfo) error
	}
	ifname := iface.Name()
	if iface, ok := iface.(definer); ok {
		spec.securityTags = plug.SecurityTags()
		spec.iface = ifname
		defer func() { spec.securityTags = nil; spec.iface = "" }()
		return iface.UDevPermanentPlug(spec, plug)
	}
	return nil
}

// AddPermanentSlot records mount-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *snap.SlotInfo) error {
	type definer interface {
		UDevPermanentSlot(spec *Specification, slot *snap.SlotInfo) error
	}
	ifname := iface.Name()
	if iface, ok := iface.(definer); ok {
		spec.securityTags = slot.SecurityTags()
		spec.iface = ifname
		defer func() { spec.securityTags = nil; spec.iface = "" }()
		return iface.UDevPermanentSlot(spec, slot)
	}
	return nil
}

// TriggerSubsystem informs ReloadRules() to also do
// 'udevadm trigger <subsystem specific>'.
// IMPORTANT: because there is currently no way to call TriggerSubsystem during
// interface disconnect, TriggerSubsystem() should typically only by used in
// UDevPermanentSlot since the rules are permanent until the snap is removed.
func (spec *Specification) TriggerSubsystem(subsystem string) {
	if subsystem == "" {
		return
	}

	if strutil.ListContains(spec.udevadmSubsystemTriggers, subsystem) {
		return
	}
	spec.udevadmSubsystemTriggers = append(spec.udevadmSubsystemTriggers, subsystem)
}

func (spec *Specification) TriggeredSubsystems() []string {
	if len(spec.udevadmSubsystemTriggers) == 0 {
		return nil
	}
	c := make([]string, len(spec.udevadmSubsystemTriggers))
	copy(c, spec.udevadmSubsystemTriggers)
	return c
}
