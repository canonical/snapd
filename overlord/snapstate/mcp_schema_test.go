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

package snapstate_test

import (
	"reflect"
	"testing"

	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/snapstate"
)

func TestMCPSchemaHelpers(t *testing.T) {
	tests := []struct {
		name          string
		schema        map[string]any
		expected      map[string]any
		requiredField string
	}{
		{name: "snapSummary", schema: mcp.OutputSchemaFromType(snapstate.GetSnapResult{}), expected: expectedSnapSummarySchema(), requiredField: "name"},
		{name: "listSnaps", schema: mcp.OutputSchemaFromType(snapstate.ListSnapsResult{}), expected: expectedListSnapsOutputSchema(), requiredField: "snaps"},
		{name: "storeSnapSummary", schema: mcp.OutputSchemaFromType(snapstate.StoreSnapSummary{}), expected: expectedStoreSnapSummarySchema(), requiredField: "name"},
		{name: "searchStoreSnaps", schema: mcp.OutputSchemaFromType(snapstate.SearchStoreSnapsResult{}), expected: expectedSearchStoreSnapsOutputSchema(), requiredField: "store_snaps"},
		{name: "storeSnapDetails", schema: mcp.OutputSchemaFromType(snapstate.GetStoreSnapResult{}), expected: expectedStoreSnapDetailsSchema(), requiredField: "channels"},
		{name: "listChanges", schema: mcp.OutputSchemaFromType(snapstate.ListChangesResult{}), expected: expectedListChangesOutputSchema(), requiredField: "changes"},
		{name: "listChangeTasks", schema: mcp.OutputSchemaFromType(snapstate.ListChangeTasksResult{}), expected: expectedListChangeTasksOutputSchema(), requiredField: "tasks"},
		{name: "listServices", schema: mcp.OutputSchemaFromType(snapstate.ListServicesResult{}), expected: expectedListServicesOutputSchema(), requiredField: "services"},
		{name: "serviceLogs", schema: mcp.OutputSchemaFromType(snapstate.GetServiceLogsResult{}), expected: expectedServiceLogsOutputSchema(), requiredField: "logs"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !reflect.DeepEqual(test.schema, test.expected) {
				t.Fatalf("schema %s does not match expected literal schema", test.name)
			}
			if test.schema["type"] != "object" {
				t.Fatalf("schema %s must have object root, got %#v", test.name, test.schema["type"])
			}
			props, ok := test.schema["properties"].(map[string]any)
			if !ok {
				t.Fatalf("schema %s must expose properties", test.name)
			}
			if _, ok := props[test.requiredField]; !ok {
				t.Fatalf("schema %s missing expected property %q", test.name, test.requiredField)
			}
		})
	}

	channels := expectedStoreSnapDetailsSchema()["properties"].(map[string]any)["channels"].(map[string]any)
	if channels["type"] != "object" {
		t.Fatalf("store snap details channels must be object, got %#v", channels["type"])
	}
}

func TestMCPToolDescriptorsUseCustomOutputSchemas(t *testing.T) {
	tests := []struct {
		name       string
		descriptor mcp.ToolDescriptor
		expected   map[string]any
	}{
		{name: "listSnaps", descriptor: snapstate.ListSnapsToolDescriptor, expected: expectedListSnapsOutputSchema()},
		{name: "getSnap", descriptor: snapstate.GetSnapToolDescriptor, expected: expectedSnapSummarySchema()},
		{name: "searchStoreSnaps", descriptor: snapstate.SearchStoreSnapsToolDescriptor, expected: expectedSearchStoreSnapsOutputSchema()},
		{name: "getStoreSnap", descriptor: snapstate.GetStoreSnapToolDescriptor, expected: expectedStoreSnapDetailsSchema()},
		{name: "listChanges", descriptor: snapstate.ListChangesToolDescriptor, expected: expectedListChangesOutputSchema()},
		{name: "listChangeTasks", descriptor: snapstate.ListChangeTasksToolDescriptor, expected: expectedListChangeTasksOutputSchema()},
		{name: "listServices", descriptor: snapstate.ListServicesToolDescriptor, expected: expectedListServicesOutputSchema()},
		{name: "getServiceLogs", descriptor: snapstate.GetServiceLogsToolDescriptor, expected: expectedServiceLogsOutputSchema()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !reflect.DeepEqual(test.descriptor.OutputSchema, test.expected) {
				t.Fatalf("descriptor %s advertises unexpected output schema", test.name)
			}
		})
	}
}

func expectedSnapSummarySchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":      map[string]any{"type": "string"},
			"version":   map[string]any{"type": "string"},
			"revision":  map[string]any{"type": "integer"},
			"channel":   map[string]any{"type": "string"},
			"installed": map[string]any{"type": "boolean"},
			"developer": map[string]any{"type": "string"},
			"status":    map[string]any{"type": "string"},
			"title":     map[string]any{"type": "string"},
			"summary":   map[string]any{"type": "string"},
		},
		"required":             []string{"name", "version", "revision", "channel", "installed", "developer", "status", "title", "summary"},
		"additionalProperties": false,
	}
}

func expectedListSnapsOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"snaps": map[string]any{
				"type":  "array",
				"items": expectedSnapSummarySchema(),
			},
		},
		"required":             []string{"snaps"},
		"additionalProperties": false,
	}
}

func expectedStoreSnapSummarySchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":      map[string]any{"type": "string"},
			"version":   map[string]any{"type": "string"},
			"revision":  map[string]any{"type": "integer"},
			"developer": map[string]any{"type": "string"},
			"title":     map[string]any{"type": "string"},
			"summary":   map[string]any{"type": "string"},
			"snap_id":   map[string]any{"type": "string"},
			"store_url": map[string]any{"type": "string"},
		},
		"required":             []string{"name", "version", "revision", "developer", "title", "summary", "snap_id", "store_url"},
		"additionalProperties": false,
	}
}

func expectedSearchStoreSnapsOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"store_snaps": map[string]any{
				"type":  "array",
				"items": expectedStoreSnapSummarySchema(),
			},
		},
		"required":             []string{"store_snaps"},
		"additionalProperties": false,
	}
}

func expectedStoreSnapDetailsSchema() map[string]any {
	base := expectedStoreSnapSummarySchema()
	properties := base["properties"].(map[string]any)
	properties["description"] = map[string]any{"type": "string"}
	properties["type"] = map[string]any{"type": "string"}
	properties["confinement"] = map[string]any{"type": "string"}
	properties["license"] = map[string]any{"type": "string"}
	properties["base"] = map[string]any{"type": "string"}
	properties["architectures"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	properties["tracks"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	properties["publisher"] = map[string]any{"type": "string"}
	properties["website"] = map[string]any{"type": "string"}
	properties["contact"] = map[string]any{"type": "string"}
	properties["channels"] = map[string]any{
		"type": "object",
		"additionalProperties": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel":     map[string]any{"type": "string"},
				"version":     map[string]any{"type": "string"},
				"revision":    map[string]any{"type": "integer"},
				"confinement": map[string]any{"type": "string"},
				"size":        map[string]any{"type": "integer"},
				"released_at": map[string]any{"type": "string", "format": "date-time"},
			},
			"required":             []string{"channel", "version", "revision", "confinement", "size", "released_at"},
			"additionalProperties": false,
		},
	}
	base["required"] = []string{"name", "version", "revision", "developer", "title", "summary", "snap_id", "store_url", "description", "type", "confinement", "license", "base", "architectures", "tracks", "publisher", "website", "contact", "channels"}
	return base
}

func expectedListChangesOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"changes": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":         map[string]any{"type": "string"},
						"kind":       map[string]any{"type": "string"},
						"summary":    map[string]any{"type": "string"},
						"status":     map[string]any{"type": "string"},
						"ready":      map[string]any{"type": "boolean"},
						"spawn_time": map[string]any{"type": "string", "format": "date-time"},
						"ready_time": map[string]any{"type": "string", "format": "date-time"},
						"snap_names": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"err":        map[string]any{"type": "string"},
					},
					"required":             []string{"id", "kind", "summary", "status", "ready", "spawn_time"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"changes"},
		"additionalProperties": false,
	}
}

func expectedListChangeTasksOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"change_id": map[string]any{"type": "string"},
			"tasks": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":      map[string]any{"type": "string"},
						"kind":    map[string]any{"type": "string"},
						"summary": map[string]any{"type": "string"},
						"status":  map[string]any{"type": "string"},
						"progress": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"label": map[string]any{"type": "string"},
								"done":  map[string]any{"type": "integer"},
								"total": map[string]any{"type": "integer"},
							},
							"required":             []string{"label", "done", "total"},
							"additionalProperties": false,
						},
						"log": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					},
					"required":             []string{"id", "kind", "summary", "status", "progress"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"change_id", "tasks"},
		"additionalProperties": false,
	}
}

func expectedListServicesOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"services": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"service_name": map[string]any{"type": "string"},
						"snap_name":    map[string]any{"type": "string"},
						"app_name":     map[string]any{"type": "string"},
						"daemon":       map[string]any{"type": "string"},
						"daemon_scope": map[string]any{"type": "string"},
						"service_unit": map[string]any{"type": "string"},
					},
					"required":             []string{"service_name", "snap_name", "app_name", "daemon", "daemon_scope", "service_unit"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"services"},
		"additionalProperties": false,
	}
}

func expectedServiceLogsOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"service_name": map[string]any{"type": "string"},
			"logs": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"timestamp": map[string]any{"type": "string", "format": "date-time"},
						"message":   map[string]any{"type": "string"},
						"sid":       map[string]any{"type": "string"},
						"pid":       map[string]any{"type": "string"},
						"priority":  map[string]any{"type": "integer"},
					},
					"required":             []string{"timestamp", "message", "sid", "pid"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"service_name", "logs"},
		"additionalProperties": false,
	}
}
