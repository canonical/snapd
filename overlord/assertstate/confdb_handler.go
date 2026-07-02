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

package assertstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/overlord/confdbstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
)

// valsetsConfdbHandler implements confdbstate.SystemConfdbCustodian
// for the "validation-sets" system confdb-schema. On commit it translates the
// altered storage paths back into (accountID, setName) pairs, reads the data
// from the committed transaction via the admin view, and updates (or forgets)
// the corresponding ValidationSetTracking in the snapd state.
type valsetsConfdbHandler struct{}

// NewValsetsConfdbHandler returns the system confdb handler for validation-sets.
func NewValsetsConfdbHandler() confdbstate.SystemConfdbHandler {
	return &valsetsConfdbHandler{}
}

func (c *valsetsConfdbHandler) SchemaName() string {
	return "validation-sets"
}

func (c *valsetsConfdbHandler) Commit(st *state.State, tx *confdbstate.Transaction) ([]*state.TaskSet, error) {
	view, err := confdbstate.GetView(st, "system", "validation-sets", "admin")
	if err != nil {
		return nil, fmt.Errorf("internal error: unexpected confdb-schema in validation-sets handler: %v", err)
	}

	type vsKey struct{ account, name string }
	valsets := make(map[vsKey][][]confdb.Accessor)
	for _, path := range tx.AlteredPaths() {
		if len(path) < 3 {
			return nil, fmt.Errorf("internal error: unexpected storage path: %v", confdb.JoinAccessors(path))
		}

		// if we ever change the storage schema, we need to adjust this code so fail hard here
		if path[0].Name() != "v1" {
			return nil, fmt.Errorf("internal error: cannot write to system/validation-sets: unsupported storage version %q", path[0].Name())
		}

		k := vsKey{account: path[1].Name(), name: path[2].Name()}
		valsets[k] = append(valsets[k], path)
	}

	for k := range valsets {
		request := k.account + "." + k.name
		result, err := confdbstate.GetViaView(tx, view, []string{request}, nil, confdb.AdminAccess)
		if err != nil {
			if errors.Is(err, &confdb.NoDataError{}) {
				if err := ForgetValidationSet(st, k.account, k.name, ForgetValidationSetOpts{}); err != nil {
					return nil, fmt.Errorf("cannot forget validation set %s/%s: %v", k.account, k.name, err)
				}
				continue
			}
			return nil, fmt.Errorf("cannot read validation set %s/%s from confdb: %v", k.account, k.name, err)
		}

		// TODO: GetViaView returns value wrapped in map keyed by request. Maybe
		// just use the confdb method directly? We don't really need this here
		resultMap, ok := result.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("internal error: unexpected result type %T for validation set %s/%s", result, k.account, k.name)
		}
		val := resultMap[request]

		tr := &ValidationSetTracking{}
		err = GetValidationSet(st, k.account, k.name, tr)
		if err != nil && !errors.Is(err, state.ErrNoState) {
			return nil, err
		}
		tr.AccountID = k.account
		tr.Name = k.name

		err = updateValSetTracking(k.account, k.name, tr, val)
		if err != nil {
			return nil, err
		}

		UpdateValidationSet(st, tr)
	}

	return nil, nil
}

// updateValSetTracking uses the values set through confdb to update the
// ValidationSetTracking.
func updateValSetTracking(accountID, name string, tr *ValidationSetTracking, val any) error {
	valset, ok := val.(map[string]any)
	if !ok {
		return fmt.Errorf("internal error: unexpected type %T for validation set %s/%s", val, accountID, name)
	}

	if rawMode, ok := valset["mode"]; ok {
		mode, ok := rawMode.(string)
		if !ok {
			return fmt.Errorf(`validation set %s/%s "mode" should be a string: %v`, accountID, name, rawMode)
		}

		// per the storage schema these are the only choices and it can't be unset
		switch mode {
		case "monitor":
			tr.Mode = Monitor
		case "enforce":
			tr.Mode = Enforce
		}
	}

	if rawSeq, ok := valset["pinned-sequence"]; ok {
		v, ok := rawSeq.(float64)
		if !ok {
			return fmt.Errorf(`validation set %s/%s "pinned-sequence" should be an int: %v`, accountID, name, rawSeq)
		}

		tr.PinnedAt = int(v)
	} else {
		tr.PinnedAt = 0
	}

	return nil
}

// Databag reads all validation set tracking from the state and returns a
// confdb.JSONDatabag with data in the nested storage structure expected by the
// system/validation-sets confdb-schema (v1.<account>.<set-name>).
func (c *valsetsConfdbHandler) Databag(st *state.State) (confdb.JSONDatabag, error) {
	sets, err := ValidationSets(st)
	if err != nil {
		return nil, err
	}

	bag := confdb.NewJSONDatabag()
	if len(sets) == 0 {
		return bag, nil
	}

	db := DB(st)
	accounts := make(map[string]json.RawMessage)
	for _, tr := range sets {
		entry := map[string]any{
			"sequence": tr.Current,
		}
		switch tr.Mode {
		case Monitor:
			entry["mode"] = "monitor"
		case Enforce:
			entry["mode"] = "enforce"
		}

		if tr.PinnedAt != 0 {
			entry["pinned-sequence"] = tr.PinnedAt
		}

		// fetch the validation-set assertion to get snap constraints (for reading
		// purposes only)
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
		entry["revision"] = vs.Revision()
		snaps := buildSnapsEntry(vs.Snaps())
		if len(snaps) > 0 {
			entry["snaps"] = snaps
		}
		// TODO: add test coverage for this

		entryJSON, err := json.Marshal(entry)
		if err != nil {
			return nil, fmt.Errorf("internal error: cannot marshal validation set %s/%s: %v", tr.AccountID, tr.Name, err)
		}

		// get or create the account-level map
		var accountMap map[string]json.RawMessage
		if raw, ok := accounts[tr.AccountID]; ok {
			if err := json.Unmarshal(raw, &accountMap); err != nil {
				return nil, fmt.Errorf("internal error: cannot unmarshal account map: %v", err)
			}
		} else {
			accountMap = make(map[string]json.RawMessage)
		}
		accountMap[tr.Name] = json.RawMessage(entryJSON)

		accountJSON, err := json.Marshal(accountMap)
		if err != nil {
			return nil, fmt.Errorf("internal error: cannot marshal account map: %v", err)
		}
		accounts[tr.AccountID] = json.RawMessage(accountJSON)
	}

	v1JSON, err := json.Marshal(accounts)
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot marshal v1 map: %v", err)
	}

	bag["v1"] = json.RawMessage(v1JSON)
	return bag, nil
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
