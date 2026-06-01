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

package ifacestate

import (
	"context"
	"reflect"
	"testing"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func TestMCPOutputSchemas(t *testing.T) {
	tests := []struct {
		name          string
		schema        map[string]any
		expected      map[string]any
		requiredField string
	}{
		{name: "connections", schema: mcp.OutputSchemaFromType(listConnectionsResult{}), expected: expectedListConnectionsOutputSchema(), requiredField: "connections"},
		{name: "plugs", schema: mcp.OutputSchemaFromType(listPlugsResult{}), expected: expectedListPlugsOutputSchema(), requiredField: "plugs"},
		{name: "slots", schema: mcp.OutputSchemaFromType(listSlotsResult{}), expected: expectedListSlotsOutputSchema(), requiredField: "slots"},
		{name: "interfaceTypes", schema: mcp.OutputSchemaFromType(listInterfaceTypesResult{}), expected: expectedListInterfaceTypesOutputSchema(), requiredField: "interface_types"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !reflect.DeepEqual(test.schema, test.expected) {
				t.Fatalf("schema %s does not match expected literal schema", test.name)
			}
			if test.schema["type"] != "object" {
				t.Fatalf("schema %s must have object root, got %#v", test.name, test.schema["type"])
			}
			props := test.schema["properties"].(map[string]any)
			if _, ok := props[test.requiredField]; !ok {
				t.Fatalf("schema %s missing required property %q", test.name, test.requiredField)
			}
		})
	}
}

func TestMCPToolDescriptorsUseCustomOutputSchemas(t *testing.T) {
	tests := []struct {
		name       string
		descriptor mcp.ToolDescriptor
		expected   map[string]any
	}{
		{name: "connections", descriptor: listConnectionsToolDescriptor, expected: expectedListConnectionsOutputSchema()},
		{name: "plugs", descriptor: listPlugsToolDescriptor, expected: expectedListPlugsOutputSchema()},
		{name: "slots", descriptor: listSlotsToolDescriptor, expected: expectedListSlotsOutputSchema()},
		{name: "interfaceTypes", descriptor: listInterfaceTypesToolDescriptor, expected: expectedListInterfaceTypesOutputSchema()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !reflect.DeepEqual(test.descriptor.OutputSchema, test.expected) {
				t.Fatalf("descriptor %s advertises unexpected output schema", test.name)
			}
		})
	}
}

func expectedListConnectionsOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"connections": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"plug_snap": map[string]any{"type": "string"},
						"plug_name": map[string]any{"type": "string"},
						"slot_snap": map[string]any{"type": "string"},
						"slot_name": map[string]any{"type": "string"},
						"interface": map[string]any{"type": "string"},
						"static_attrs": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"plug": map[string]any{"type": "object"},
								"slot": map[string]any{"type": "object"},
							},
							"required":             []string{"plug", "slot"},
							"additionalProperties": false,
						},
						"dynamic_attrs": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"plug": map[string]any{"type": "object"},
								"slot": map[string]any{"type": "object"},
							},
							"required":             []string{"plug", "slot"},
							"additionalProperties": false,
						},
						"manual": map[string]any{"type": "boolean"},
					},
					"required":             []string{"plug_snap", "plug_name", "slot_snap", "slot_name", "interface", "static_attrs", "dynamic_attrs", "manual"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"connections"},
		"additionalProperties": false,
	}
}

func expectedListPlugsOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"plugs": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"snap_name": map[string]any{"type": "string"},
						"name":      map[string]any{"type": "string"},
						"interface": map[string]any{"type": "string"},
						"label":     map[string]any{"type": "string"},
						"attrs":     map[string]any{"type": "object"},
						"apps": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
						"unscoped":    map[string]any{"type": "boolean"},
						"hotplug_key": map[string]any{"type": "string"},
					},
					"required":             []string{"snap_name", "name", "interface"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"plugs"},
		"additionalProperties": false,
	}
}

func expectedListSlotsOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"slots": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"snap_name": map[string]any{"type": "string"},
						"name":      map[string]any{"type": "string"},
						"interface": map[string]any{"type": "string"},
						"label":     map[string]any{"type": "string"},
						"attrs":     map[string]any{"type": "object"},
						"apps": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
						"unscoped":    map[string]any{"type": "boolean"},
						"hotplug_key": map[string]any{"type": "string"},
					},
					"required":             []string{"snap_name", "name", "interface"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"slots"},
		"additionalProperties": false,
	}
}

func expectedListInterfaceTypesOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"interface_types": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":    map[string]any{"type": "string"},
						"summary": map[string]any{"type": "string"},
						"base_policy": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"plugs": map[string]any{"type": "string"},
								"slots": map[string]any{"type": "string"},
							},
							"required":             []string{"plugs", "slots"},
							"additionalProperties": false,
						},
						"implemented_backends": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
					},
					"required":             []string{"name"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"interface_types"},
		"additionalProperties": false,
	}
}

func TestMCPValidationAndMappingHelpers(t *testing.T) {
	if err := validateOptionalString(map[string]any{"snap": true}, "snap"); err == nil {
		t.Fatal("validateOptionalString should reject non-string value")
	}
	if err := validateOptionalString(map[string]any{"snap": "ok"}, "snap"); err != nil {
		t.Fatalf("validateOptionalString returned error: %v", err)
	}
	if err := validateOptionalBool(map[string]any{"include_details": "yes"}, "include_details"); err == nil {
		t.Fatal("validateOptionalBool should reject non-bool value")
	}
	if err := validateOptionalBool(map[string]any{"include_details": true}, "include_details"); err != nil {
		t.Fatalf("validateOptionalBool returned error: %v", err)
	}

	plugSnap := &snap.Info{SuggestedName: "consumer", SideInfo: snap.SideInfo{RealName: "consumer"}}
	plug := &snap.PlugInfo{
		Snap:      plugSnap,
		Name:      "network",
		Interface: "network",
		Label:     "Network",
		Attrs:     map[string]any{"foo": "bar"},
		Apps: map[string]*snap.AppInfo{
			"z": {Snap: plugSnap, Name: "z"},
			"a": {Snap: plugSnap, Name: "a"},
		},
		Unscoped: true,
	}
	plugMap := plugToMap(plug, true)
	if plugMap["snap_name"] != "consumer" || plugMap["interface"] != "network" {
		t.Fatalf("unexpected plug map: %#v", plugMap)
	}
	if plugMap["unscoped"] != true {
		t.Fatalf("expected unscoped=true in plug map, got %#v", plugMap)
	}
	if _, ok := plugMap["is_unscoped"]; ok {
		t.Fatalf("plug map must not expose legacy is_unscoped key: %#v", plugMap)
	}
	apps := plugMap["apps"].([]string)
	if len(apps) != 2 || apps[0] != "a" || apps[1] != "z" {
		t.Fatalf("apps were not sorted: %#v", apps)
	}
	if concise := plugToMap(plug, false); len(concise) != 3 {
		t.Fatalf("concise plug map should contain only base fields: %#v", concise)
	}

	slotSnap := &snap.Info{SuggestedName: "provider", SideInfo: snap.SideInfo{RealName: "provider"}}
	slot := &snap.SlotInfo{
		Snap:       slotSnap,
		Name:       "network",
		Interface:  "network",
		Label:      "Network slot",
		Attrs:      map[string]any{"foo": "bar"},
		Apps:       map[string]*snap.AppInfo{"svc": {Snap: slotSnap, Name: "svc"}},
		Unscoped:   true,
		HotplugKey: snap.HotplugKey("hotplug"),
	}
	slotMap := slotToMap(slot, true)
	if slotMap["hotplug_key"] != "hotplug" {
		t.Fatalf("unexpected slot map: %#v", slotMap)
	}
	if slotMap["unscoped"] != true {
		t.Fatalf("expected unscoped=true in slot map, got %#v", slotMap)
	}
	if _, ok := slotMap["is_unscoped"]; ok {
		t.Fatalf("slot map must not expose legacy is_unscoped key: %#v", slotMap)
	}
	if names := sortedAppNames(slot.Apps); len(names) != 1 || names[0] != "svc" {
		t.Fatalf("unexpected sorted app names: %#v", names)
	}
}

func TestMCPToolTypedHelpers(t *testing.T) {
	st := state.New(nil)
	repo := interfaces.NewRepository()
	st.Lock()
	ifacerepo.Replace(st, repo)
	st.Unlock()

	if _, ok := (listConnectionsTool{}).ArgsType().(*listConnectionsArgs); !ok {
		t.Fatal("listConnectionsTool ArgsType returned unexpected type")
	}
	if _, ok := (listConnectionsTool{}).ResultType().(*listConnectionsResult); !ok {
		t.Fatal("listConnectionsTool ResultType returned unexpected type")
	}
	if err := (listConnectionsTool{}).ValidateArgs(struct{}{}); err == nil {
		t.Fatal("listConnectionsTool ValidateArgs should reject wrong type")
	}
	if _, err := (listConnectionsTool{}).CallWithArgs(context.Background(), st, struct{}{}); err == nil {
		t.Fatal("listConnectionsTool CallWithArgs should reject wrong type")
	}

	if err := (listPlugsTool{}).ValidateArgs(struct{}{}); err == nil {
		t.Fatal("listPlugsTool ValidateArgs should reject wrong type")
	}
	if _, err := (listPlugsTool{}).CallWithArgs(context.Background(), st, struct{}{}); err == nil {
		t.Fatal("listPlugsTool CallWithArgs should reject wrong type")
	}

	if err := (listSlotsTool{}).ValidateArgs(struct{}{}); err == nil {
		t.Fatal("listSlotsTool ValidateArgs should reject wrong type")
	}
	if _, err := (listSlotsTool{}).CallWithArgs(context.Background(), st, struct{}{}); err == nil {
		t.Fatal("listSlotsTool CallWithArgs should reject wrong type")
	}

	if err := (listInterfaceTypesTool{}).ValidateArgs(struct{}{}); err == nil {
		t.Fatal("listInterfaceTypesTool ValidateArgs should reject wrong type")
	}
	if _, err := (listInterfaceTypesTool{}).CallWithArgs(context.Background(), st, struct{}{}); err == nil {
		t.Fatal("listInterfaceTypesTool CallWithArgs should reject wrong type")
	}
}

func TestRepoFromStateRecover(t *testing.T) {
	if _, err := repoFromState(nil); err == nil {
		t.Fatal("repoFromState should recover from nil state access")
	}
}

func TestImplementedBackends(t *testing.T) {
	iface := &ifacetest.TestInterface{InterfaceName: "network"}
	backends := implementedBackends(iface)
	if len(backends) == 0 {
		t.Fatal("implementedBackends should detect supported backends")
	}
}
