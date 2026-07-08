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
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/overlord/confdbstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
)

// valsetsConfdbHandler implements confdbstate.SystemConfdbHandler. Translates
// confdb changes to system/validation-sets into val set state and vice-versa.
type valsetsConfdbHandler struct{}

func (c *valsetsConfdbHandler) SchemaName() string {
	return "validation-sets"
}

// Databag reads all validation set tracking from the state and returns a
// confdb.JSONDatabag structured as described in the system/validation-sets
// confdb-schema. State must be locked by caller.
func (c *valsetsConfdbHandler) Databag(st *state.State) (confdb.JSONDatabag, error) {
	sets, err := ValidationSets(st)
	if err != nil {
		return nil, err
	}

	if len(sets) == 0 {
		return confdb.NewJSONDatabag(), nil
	}

	db := DB(st)
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
		case Monitor:
			valset["mode"] = "monitor"
		case Enforce:
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

func (c *valsetsConfdbHandler) Commit(*state.State, *confdbstate.Transaction) ([]*state.TaskSet, error) {
	return nil, errors.New("not implemented yet")
}
