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

	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/state"
)

func TestListPlugsToolFilters(t *testing.T) {
	st := state.New(nil)
	repo := setupInterfaceRepo(t, st)

	result, err := (ifacestate.ListPlugsTool{}).Call(context.Background(), st, map[string]any{"snap_name": "consumer", "interface": "network"})
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

	plugs := obj["plugs"].([]any)
	if len(plugs) != 1 {
		t.Fatalf("expected one plug, got %d", len(plugs))
	}
	plug := plugs[0].(map[string]any)
	if plug["snap_name"] != "consumer" || plug["name"] != "network-plug" {
		t.Fatalf("unexpected plug payload: %#v", plug)
	}
	if plug["interface"] != "network" {
		t.Fatalf("unexpected interface in payload: %#v", plug)
	}
	if _, ok := plug["attrs"]; ok {
		t.Fatalf("did not expect attrs in concise output: %#v", plug)
	}

	// quick check: setup produced multiple plugs before filtering
	if total := len(repo.AllPlugs("")); total < 2 {
		t.Fatalf("expected at least 2 plugs in test setup, got %d", total)
	}
}

func TestListPlugsToolIncludesDetailsWhenRequested(t *testing.T) {
	st := state.New(nil)
	_ = setupInterfaceRepo(t, st)

	result, err := (ifacestate.ListPlugsTool{}).Call(context.Background(), st, map[string]any{
		"snap_name":       "consumer",
		"interface":       "network",
		"include_details": true,
	})
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

	plugs := obj["plugs"].([]any)
	if len(plugs) != 1 {
		t.Fatalf("expected one plug, got %d", len(plugs))
	}
	plug := plugs[0].(map[string]any)
	if apps, ok := plug["apps"].([]any); !ok || len(apps) == 0 {
		t.Fatalf("expected apps in detailed output, got %#v", plug)
	}
	if _, ok := plug["attrs"]; ok {
		if attrs, ok := plug["attrs"].(map[string]any); !ok {
			t.Fatalf("expected attrs to be map-like if present, got %#v", plug["attrs"])
		} else if len(attrs) == 0 {
			// attrs may be empty/omitted from JSON map; that is acceptable
		}
	}
}
