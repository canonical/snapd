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

package ifacestate_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/state"
)

func TestConnectionsToMapEmpty(t *testing.T) {
	result := ifacestate.ConnectionsToMap(nil, "")
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	conns := obj["connections"].([]any)
	if len(conns) != 0 {
		t.Fatalf("expected no connections, got %d", len(conns))
	}
}

func TestConnectionsToMapAllFields(t *testing.T) {
	static := map[string]any{"read-paths": []string{"/etc"}}
	dynamic := map[string]any{"connect-plug-uid": "1000"}
	info := []ifacestate.MCPConnectionResult{{
		Ref: &interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "network"}, SlotRef: interfaces.SlotRef{Snap: "core", Name: "network"}},
		State: ifacestate.ConnectionState{
			Interface:        "network",
			StaticPlugAttrs:  static,
			StaticSlotAttrs:  static,
			DynamicPlugAttrs: dynamic,
			DynamicSlotAttrs: dynamic,
		},
	}}
	result := ifacestate.ConnectionsToMap(info, "")
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	conns := obj["connections"].([]any)
	if len(conns) != 1 {
		t.Fatalf("expected one connection, got %d", len(conns))
	}
	conn := conns[0].(map[string]any)
	if conn["plug_snap"] != "consumer" || conn["slot_snap"] != "core" {
		t.Fatalf("unexpected connection endpoints: %#v", conn)
	}
	if conn["manual"] != true {
		t.Fatalf("expected manual=true, got %#v", conn["manual"])
	}
}

func TestListConnectionsToolCallSuccess(t *testing.T) {
	st := state.New(nil)
	result, err := (ifacestate.ListConnectionsTool{}).Call(context.Background(), st, map[string]any{"snap": "app"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	conns := obj["connections"].([]any)
	if len(conns) != 0 {
		t.Fatalf("expected no connections, got %d", len(conns))
	}
}
