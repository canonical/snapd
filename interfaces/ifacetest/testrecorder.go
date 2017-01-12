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

package ifacetest

import (
	"github.com/snapcore/snapd/interfaces"
)

// TestRecorder is a recorder intended for testing.
type TestRecorder struct {
	Snippets []string
}

// AddSnippet appends a snippet to a list stored in the recorder.
func (rec *TestRecorder) AddSnippet(snippet string) {
	rec.Snippets = append(rec.Snippets, snippet)
}

// Implementation of methods required by interfaces.Recorder

// RecordConnectedPlug records test side-effects of having a connected plug.
func (rec *TestRecorder) RecordConnectedPlug(iface interfaces.Interface, plug *interfaces.Plug, slot *interfaces.Slot) error {
	if iface, ok := iface.(testAware); ok {
		return iface.RecordTestConnectedPlug(rec, plug, slot)
	}
	return nil
}

// RecordConnectedSlot records test side-effects of having a connected slot.
func (rec *TestRecorder) RecordConnectedSlot(iface interfaces.Interface, plug *interfaces.Plug, slot *interfaces.Slot) error {
	if iface, ok := iface.(testAware); ok {
		return iface.RecordTestConnectedSlot(rec, plug, slot)
	}
	return nil
}

// RecordPermanentPlug records test side-effects of having a plug.
func (rec *TestRecorder) RecordPermanentPlug(iface interfaces.Interface, plug *interfaces.Plug) error {
	if iface, ok := iface.(testAware); ok {
		return iface.RecordTestPermanentPlug(rec, plug)
	}
	return nil
}

// RecordPermanentSlot records test side-effects of having a slot.
func (rec *TestRecorder) RecordPermanentSlot(iface interfaces.Interface, slot *interfaces.Slot) error {
	if iface, ok := iface.(testAware); ok {
		return iface.RecordTestPermanentSlot(rec, slot)
	}
	return nil
}

// testAware describes an Interface that can to interact with the test backend.
type testAware interface {
	RecordTestConnectedPlug(rec *TestRecorder, plug *interfaces.Plug, slot *interfaces.Slot) error
	RecordTestConnectedSlot(rec *TestRecorder, plug *interfaces.Plug, slot *interfaces.Slot) error
	RecordTestPermanentPlug(rec *TestRecorder, plug *interfaces.Plug) error
	RecordTestPermanentSlot(rec *TestRecorder, slot *interfaces.Slot) error
}
