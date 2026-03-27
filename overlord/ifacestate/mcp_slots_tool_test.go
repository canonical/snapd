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

func TestListSlotsToolFilters(t *testing.T) {
	st := state.New(nil)
	_ = setupInterfaceRepo(t, st)

	result, err := (ifacestate.ListSlotsTool{}).Call(context.Background(), st, map[string]any{"snap_name": "provider", "interface": "network"})
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

	slots := obj["slots"].([]any)
	if len(slots) != 1 {
		t.Fatalf("expected one slot, got %d", len(slots))
	}
	slot := slots[0].(map[string]any)
	if slot["snap_name"] != "provider" || slot["name"] != "network-slot" {
		t.Fatalf("unexpected slot payload: %#v", slot)
	}
	if slot["interface"] != "network" {
		t.Fatalf("unexpected interface in payload: %#v", slot)
	}
	if _, ok := slot["attrs"]; ok {
		t.Fatalf("did not expect attrs in concise output: %#v", slot)
	}
}

func TestListSlotsToolIncludesDetailsWhenRequested(t *testing.T) {
	st := state.New(nil)
	_ = setupInterfaceRepo(t, st)

	result, err := (ifacestate.ListSlotsTool{}).Call(context.Background(), st, map[string]any{
		"snap_name":       "provider",
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

	slots := obj["slots"].([]any)
	if len(slots) != 1 {
		t.Fatalf("expected one slot, got %d", len(slots))
	}
	slot := slots[0].(map[string]any)
	if apps, ok := slot["apps"].([]any); !ok || len(apps) == 0 {
		t.Fatalf("expected apps in detailed output, got %#v", slot)
	}
	if _, ok := slot["attrs"]; ok {
		if attrs, ok := slot["attrs"].(map[string]any); !ok {
			t.Fatalf("expected attrs to be map-like if present, got %#v", slot["attrs"])
		} else if len(attrs) == 0 {
			// empty attrs is acceptable
		}
	}
}
