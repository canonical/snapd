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

package confdb

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/confdbstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
)

var validationSetsConfdbSchemaHeaders = []byte(`type: confdb-schema
account-id: system
authority-id: canonical
name: validation-sets
summary: Manage the validation sets installed on a device
views:
  state:
    summary: Read the validation sets state
    rules:
      -
        request: {account}.{validation-set}
        storage: v1.{account}.{validation-set}
        access: read
        content:
          -
            storage: snaps[{n}]
            content:
              -
                storage: name
              -
                storage: id
              -
                storage: revision
              -
                storage: presence
              -
                storage: components
                content:
                  -
                    storage: {component}
                    content:
                      -
                        storage: presence
                      -
                        storage: revision
          -
            storage: mode
          -
            storage: status
          -
            storage: sequence
          -
            storage: revision
          -
            storage: pinned-sequence
  admin:
    summary: Control validation sets
    rules:
      -
        request: {account}.{set-name}
        storage: v1.{account}.{set-name}
        access: write
        content:
          -
            storage: pinned-sequence
          -
            storage: mode
      -
        request: {account}.{set-name}
        storage: v1.{account}.{set-name}
        access: read
        content:
          -
            storage: snaps[{n}]
            content:
              -
                storage: name
              -
                storage: id
              -
                storage: revision
              -
                storage: presence
          -
            storage: status
          -
            storage: sequence
          -
            storage: revision
          -
            storage: pinned-sequence
          -
            storage: mode
  pinning-admin:
    summary: Control pinning of validation sets
    rules:
      -
        request: {account}.{set-name}.pinned-sequence
        storage: v1.{account}.{set-name}.pinned-sequence
        access: read-write
`)

// NOTE: JSON needs to be sorted, otherwise decoding validation would fail.
var validationSetsConfdbSchemaBody = []byte(`{
  "storage": {
    "aliases": {
      "account-id": {
        "pattern": "^(?:[a-z0-9A-Z]{32}|[-a-z0-9]{2,28})$",
        "type": "string"
      },
      "natural-number": {
        "min": 1,
        "type": "int"
      },
      "presence": {
        "choices": [
          "required",
          "optional",
          "invalid"
        ],
        "type": "string"
      },
      "set-name": {
        "pattern": "^[a-z0-9](?:-?[a-z0-9])*$",
        "type": "string"
      }
    },
    "schema": {
      "v1": {
        "keys": "${account-id}",
        "values": {
          "keys": "${set-name}",
          "values": {
            "required": [
              "mode"
            ],
            "schema": {
              "mode": {
                "choices": [
                  "monitor",
                  "enforce"
                ],
                "type": "string"
              },
              "pinned-sequence": "${natural-number}",
              "revision": "${natural-number}",
              "sequence": "${natural-number}",
              "snaps": {
                "type": "array",
                "unique": true,
                "values": {
                  "schema": {
                    "components": {
                      "values": {
                        "schema": {
                          "presence": "${presence}",
                          "revision": "${natural-number}"
                        }
                      }
                    },
                    "id": "string",
                    "name": "string",
                    "presence": "${presence}",
                    "revision": "${natural-number}"
                  }
                }
              },
              "status": {
                "choices": [
                  "valid",
                  "invalid"
                ],
                "type": "string"
              }
            }
          }
        }
      }
    }
  }
}`)

func init() {
	if err := asserts.RegisterBuiltinConfdbSchema(validationSetsConfdbSchemaHeaders, validationSetsConfdbSchemaBody); err != nil {
		panic(fmt.Sprintf("cannot create builtin validation-sets confdb-schema: %v", err))
	}

	confdbstate.RegisterConfdbHandler(&ValsetsConfdbHandler{})
}

// ValsetsConfdbHandler implements confdbstate.SystemConfdbHandler. Translates
// confdb changes to system/validation-sets into val set state and vice-versa.
type ValsetsConfdbHandler struct{}

func (c *ValsetsConfdbHandler) SchemaName() string {
	return "validation-sets"
}

// Databag reads all validation set tracking from the state and returns a
// confdb.JSONDatabag structured as described in the system/validation-sets
// confdb-schema. State must be locked by caller.
func (c *ValsetsConfdbHandler) Databag(st *state.State) (confdb.JSONDatabag, error) {
	sets, err := assertstate.ValidationSets(st)
	if err != nil {
		return nil, err
	}

	if len(sets) == 0 {
		return confdb.NewJSONDatabag(), nil
	}

	db := assertstate.DB(st)
	installedSnaps, _, err := snapstate.InstalledSnaps(st)
	if err != nil {
		return nil, err
	}

	accounts := make(map[string]map[string]any)
	for _, tr := range sets {
		valset := map[string]any{
			"sequence": tr.Current,
		}
		switch tr.Mode {
		case assertstate.Monitor:
			valset["mode"] = "monitor"
		case assertstate.Enforce:
			valset["mode"] = "enforce"
		}

		if tr.PinnedAt != 0 {
			valset["pinned-sequence"] = tr.PinnedAt
		}

		// fetch the validation-set assertion to get snap constraints (read only)
		a, err := db.Find(asserts.ValidationSetType, map[string]string{
			"series":     release.Series,
			"account-id": tr.AccountID,
			"name":       tr.Name,
			"sequence":   strconv.Itoa(tr.Current),
		})
		if err != nil {
			return nil, fmt.Errorf("cannot find validation-set %s/%s: %v", tr.AccountID, tr.Name, err)
		}

		vs := a.(*asserts.ValidationSet)
		valset["revision"] = vs.Revision()
		vsets := snapasserts.NewValidationSets()
		if err := vsets.Add(vs); err != nil {
			return nil, err
		}
		status := "valid"
		if err := vsets.CheckInstalledSnaps(installedSnaps, nil); err != nil {
			status = "invalid"
		}
		valset["status"] = status

		snaps := buildSnapsEntry(vs.Snaps())
		if len(snaps) > 0 {
			valset["snaps"] = snaps
		}

		if accounts[tr.AccountID] == nil {
			accounts[tr.AccountID] = make(map[string]any)
		}
		accounts[tr.AccountID][tr.Name] = valset
	}

	raw, err := json.Marshal(accounts)
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot marshal v1 map: %v", err)
	}

	return map[string]json.RawMessage{"v1": raw}, nil
}

// buildSnapsEntry converts validation-set snap constraints into the storage
// format expected by the confdb schema: an array of maps with name, id,
// presence, revision, and components.
func buildSnapsEntry(snaps []*asserts.ValidationSetSnap) []map[string]any {
	result := make([]map[string]any, 0, len(snaps))
	for _, sn := range snaps {
		snapEntry := map[string]any{
			"name": sn.Name,
			"id":   sn.SnapID,
		}

		if sn.Presence != "" {
			snapEntry["presence"] = string(sn.Presence)
		}
		if sn.Revision > 0 {
			snapEntry["revision"] = sn.Revision
		}

		if len(sn.Components) > 0 {
			components := make(map[string]any, len(sn.Components))
			for compName, comp := range sn.Components {
				compEntry := make(map[string]any)
				if comp.Presence != "" {
					compEntry["presence"] = string(comp.Presence)
				}
				if comp.Revision > 0 {
					compEntry["revision"] = comp.Revision
				}
				components[compName] = compEntry
			}
			snapEntry["components"] = components
		}
		result = append(result, snapEntry)
	}
	return result
}

// Commit translates the changes in the Transaction into validation-set state.
// State must be locked by caller.
func (c *ValsetsConfdbHandler) Commit(st *state.State, tx *confdbstate.Transaction) ([]*state.TaskSet, error) {
	view, err := confdbstate.GetView(st, "system", "validation-sets", "admin")
	if err != nil {
		return nil, fmt.Errorf("internal error: unexpected confdb-schema in validation-sets handler: %v", err)
	}

	type vsKey struct{ account, name string }
	valsets := make(map[vsKey][][]confdb.Accessor)
	for _, path := range tx.AlteredPaths() {
		if len(path) < 3 {
			// shouldn't be possible as confdb-schema doesn't allow it
			return nil, fmt.Errorf("internal error: unexpected storage path: %v", confdb.JoinAccessors(path))
		}

		// Databag() will need changes if we add v2 paths in the confdb-schema, so
		// fail here to flag the issue
		if path[0].Name() != "v1" {
			return nil, fmt.Errorf("internal error: cannot write to system/validation-sets: unsupported storage version %q", path[0].Name())
		}

		k := vsKey{account: path[1].Name(), name: path[2].Name()}
		valsets[k] = append(valsets[k], path)
	}

	for k := range valsets {
		request := k.account + "." + k.name
		result, err := view.Get(tx, request, nil, confdb.AdminAccess)
		if err != nil {
			if errors.Is(err, &confdb.NoDataError{}) {
				if err := assertstate.ForgetValidationSet(st, k.account, k.name, assertstate.ForgetValidationSetOpts{}); err != nil {
					return nil, fmt.Errorf("cannot forget validation set %s/%s: %v", k.account, k.name, err)
				}
				continue
			}
			return nil, fmt.Errorf("cannot read validation set %s/%s from confdb: %v", k.account, k.name, err)
		}

		val, ok := result.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("internal error: unexpected result type %T for validation set %s/%s", result, k.account, k.name)
		}

		tr := &assertstate.ValidationSetTracking{}
		err = assertstate.GetValidationSet(st, k.account, k.name, tr)
		if err != nil && !errors.Is(err, state.ErrNoState) {
			return nil, fmt.Errorf("cannot read validation-set %s/%s for commit: %v", k.account, k.name, err)
		}
		tr.AccountID = k.account
		tr.Name = k.name

		err = applyChanges(k.account, k.name, tr, val)
		if err != nil {
			return nil, err
		}

		assertstate.UpdateValidationSet(st, tr)
	}

	return nil, nil
}

// applyChanges applies values set through confdb to the ValidationSetTracking.
func applyChanges(accountID, name string, tr *assertstate.ValidationSetTracking, val any) error {
	valset, ok := val.(map[string]any)
	if !ok {
		return fmt.Errorf("internal error: unexpected type %T for validation set %s/%s", val, accountID, name)
	}

	if rawMode, ok := valset["mode"]; ok {
		mode, ok := rawMode.(string)
		if !ok {
			// writes are validated against the storage schema so shouldn't be possible
			return fmt.Errorf(`internal error: "mode" should be a string, got %[1]T: %[1]v`, rawMode)
		}

		// per the storage schema these are the only choices and it can't be unset
		switch mode {
		case "monitor":
			tr.Mode = assertstate.Monitor
		case "enforce":
			tr.Mode = assertstate.Enforce
		}
	}

	if rawSeq, ok := valset["pinned-sequence"]; ok {
		v, ok := rawSeq.(float64)
		if !ok {
			// writes are validated against the storage schema so shouldn't be possible
			return fmt.Errorf(`internal error: "pinned-sequence" should be an int, got %[1]T: %[1]v`, rawSeq)
		}

		tr.PinnedAt = int(v)
	} else {
		tr.PinnedAt = 0
	}

	return nil
}
