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

// Package dot builds graphviz dot representations for overlord changes.
package dot

import (
	"errors"
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/state"
	statedot "github.com/snapcore/snapd/overlord/state/dot"
	"github.com/snapcore/snapd/snap"
)

type hookSetup struct {
	Snap string `json:"snap"`
	Hook string `json:"hook"`
}

type snapSetup struct {
	SideInfo    *snap.SideInfo `json:"side-info,omitempty"`
	InstanceKey string         `json:"instance-key,omitempty"`
}

func (snapsup *snapSetup) InstanceName() string {
	if snapsup.SideInfo == nil || snapsup.SideInfo.RealName == "" {
		return ""
	}
	return snap.InstanceName(snapsup.SideInfo.RealName, snapsup.InstanceKey)
}

type componentSetup struct {
	CompSideInfo *snap.ComponentSideInfo `json:"comp-side-info,omitempty"`
}

// NewChangeGraph builds a new change graph using the default task labeler.
func NewChangeGraph(chg *state.Change, tag string) (*statedot.ChangeGraph, error) {
	return statedot.NewChangeGraph(chg, TaskLabel, tag)
}

// TaskLabel produces a unique label string for the given task.
func TaskLabel(t *state.Task) (string, error) {
	snapName, err := taskSnapName(t)
	if err != nil {
		return "", err
	}

	switch t.Kind() {
	case "run-hook":
		var hooksup hookSetup
		if err := t.Get("hook-setup", &hooksup); err != nil {
			return "", err
		}
		return fmt.Sprintf("[%s] %s:run-hook[%s]", t.ID(), hooksup.Snap, hooksup.Hook), nil
	}

	if snapName != "" {
		return fmt.Sprintf("[%s] %s:%s", t.ID(), snapName, t.Kind()), nil
	}

	label := fmt.Sprintf("[%s] %s", t.ID(), t.Kind())

	var plugRef interfaces.PlugRef
	if err := t.Get("plug", &plugRef); err != nil && !errors.Is(err, state.ErrNoState) {
		return "", err
	}

	var slotRef interfaces.SlotRef
	if err := t.Get("slot", &slotRef); err != nil && !errors.Is(err, state.ErrNoState) {
		return "", err
	}

	if plugRef.Snap != "" && slotRef.Snap != "" {
		label = fmt.Sprintf("[%s] %s[%s:%s %s:%s]", t.ID(), t.Kind(), plugRef.Snap, plugRef.Name, slotRef.Snap, slotRef.Name)
	}

	return label, nil
}

func taskSnapName(t *state.Task) (string, error) {
	snapsup, err := taskSnapSetup(t)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return "", err
	}
	if err == nil {
		return snapsup.InstanceName(), nil
	}

	compsup, err := taskComponentSetup(t)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return "", err
	}
	if err == nil && compsup.CompSideInfo != nil {
		return compsup.CompSideInfo.Component.SnapName, nil
	}

	return "", nil
}

func taskSnapSetup(t *state.Task) (*snapSetup, error) {
	var snapsup snapSetup

	err := t.Get("snap-setup", &snapsup)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if err == nil {
		return &snapsup, nil
	}

	var id string
	err = t.Get("snap-setup-task", &id)
	if err != nil {
		return nil, err
	}

	ts := t.State().Task(id)
	if ts == nil {
		return nil, fmt.Errorf("internal error: tasks are being pruned")
	}
	if err := ts.Get("snap-setup", &snapsup); err != nil {
		return nil, err
	}

	return &snapsup, nil
}

func taskComponentSetup(t *state.Task) (*componentSetup, error) {
	var compsup componentSetup

	err := t.Get("component-setup", &compsup)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if err == nil {
		return &compsup, nil
	}

	var id string
	err = t.Get("component-setup-task", &id)
	if err != nil {
		return nil, err
	}

	ts := t.State().Task(id)
	if ts == nil {
		return nil, fmt.Errorf("internal error: tasks are being pruned")
	}
	if err := ts.Get("component-setup", &compsup); err != nil {
		return nil, err
	}

	return &compsup, nil
}
