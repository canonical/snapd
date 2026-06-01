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
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/state"
)

func TestListInterfaceTypesToolIncludesStaticInfoAndBackends(t *testing.T) {
	st := state.New(nil)
	repo := interfaces.NewRepository()
	st.Lock()
	ifacerepo.Replace(st, repo)
	st.Unlock()

	fullIface := &ifacetest.TestInterface{InterfaceName: "full-iface", InterfaceStaticInfo: interfaces.StaticInfo{
		Summary:              "full test interface",
		BaseDeclarationPlugs: "allow-installation: true",
		BaseDeclarationSlots: "allow-installation: true",
	}}
	if err := repo.AddInterface(fullIface); err != nil {
		t.Fatalf("cannot add full-iface: %v", err)
	}

	minimalIface := minimalTestInterface{
		name: "minimal-iface",
		info: interfaces.StaticInfo{
			Summary:              "minimal interface",
			BaseDeclarationPlugs: "deny-auto-connection: true",
		},
	}
	if err := repo.AddInterface(minimalIface); err != nil {
		t.Fatalf("cannot add minimal-iface: %v", err)
	}

	result, err := (ifacestate.ListInterfaceTypesTool{}).Call(context.Background(), st, map[string]any{"name": "iface", "include_details": true})
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

	types := obj["interface_types"].([]any)
	if len(types) != 2 {
		t.Fatalf("expected two interface types, got %d", len(types))
	}

	byName := make(map[string]map[string]any, len(types))
	for _, it := range types {
		info := it.(map[string]any)
		byName[info["name"].(string)] = info
	}

	full := byName["full-iface"]
	if full == nil {
		t.Fatalf("missing full-iface in response: %#v", byName)
	}
	if full["summary"] != "full test interface" {
		t.Fatalf("unexpected summary for full-iface: %#v", full["summary"])
	}
	fullBasePolicy := full["base_policy"].(map[string]any)
	if fullBasePolicy["plugs"] != "allow-installation: true" {
		t.Fatalf("unexpected plugs base policy: %#v", fullBasePolicy)
	}
	if fullBasePolicy["slots"] != "allow-installation: true" {
		t.Fatalf("unexpected slots base policy: %#v", fullBasePolicy)
	}
	fullBackendsRaw := full["implemented_backends"].([]any)
	for _, backend := range []string{"apparmor", "seccomp", "dbus", "udev", "kmod", "systemd", "polkit", "ldconfig", "configfiles", "symlinks"} {
		found := false
		for _, b := range fullBackendsRaw {
			if s, ok := b.(string); ok && s == backend {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected backend %q in %#v", backend, fullBackendsRaw)
		}
	}

	minimal := byName["minimal-iface"]
	if minimal == nil {
		t.Fatalf("missing minimal-iface in response: %#v", byName)
	}
	minimalBackendsRawIface := minimal["implemented_backends"]
	minimalBackendsRaw, _ := minimalBackendsRawIface.([]any)
	if len(minimalBackendsRaw) != 0 {
		t.Fatalf("expected no implemented backends for minimal-iface, got %#v", minimalBackendsRawIface)
	}
}

func TestListInterfaceTypesToolOmitsDetailsByDefault(t *testing.T) {
	st := state.New(nil)
	repo := interfaces.NewRepository()
	st.Lock()
	ifacerepo.Replace(st, repo)
	st.Unlock()

	iface := minimalTestInterface{name: "content-like", info: interfaces.StaticInfo{Summary: "summary"}}
	if err := repo.AddInterface(iface); err != nil {
		t.Fatalf("cannot add test interface: %v", err)
	}

	result, err := (ifacestate.ListInterfaceTypesTool{}).Call(context.Background(), st, map[string]any{"name": "content"})
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

	types := obj["interface_types"].([]any)
	if len(types) != 1 {
		t.Fatalf("expected one interface type, got %d", len(types))
	}
	item := types[0].(map[string]any)
	if item["name"] != "content-like" {
		t.Fatalf("unexpected interface type payload: %#v", item)
	}
	if summary, ok := item["summary"]; ok && summary != "" {
		t.Fatalf("expected summary to be empty in concise output, got %#v", summary)
	}
	if _, ok := item["base_policy"]; ok {
		t.Fatalf("did not expect base_policy in concise output: %#v", item)
	}
	if _, ok := item["implemented_backends"]; ok {
		t.Fatalf("did not expect implemented_backends in concise output: %#v", item)
	}
}
