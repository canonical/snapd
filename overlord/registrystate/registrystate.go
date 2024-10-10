// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2023-2024 Canonical Ltd
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

package registrystate

import (
	"errors"
	"fmt"
	"sort"

	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/registry"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

var assertstateRegistry = assertstate.Registry

// Set finds the view identified by the account, registry and view names and
// sets the request fields to their respective values.
func Set(st *state.State, account, registryName, viewName string, requests map[string]interface{}) error {
	registryAssert, err := assertstateRegistry(st, account, registryName)
	if err != nil {
		return err
	}
	reg := registryAssert.Registry()

	view := reg.View(viewName)
	if view == nil {
		var keys []string
		if len(requests) > 0 {
			keys = make([]string, 0, len(requests))
			for k := range requests {
				keys = append(keys, k)
			}
		}

		return &registry.NotFoundError{
			Account:      account,
			RegistryName: registryName,
			View:         viewName,
			Operation:    "set",
			Requests:     keys,
			Cause:        "not found",
		}
	}

	tx, err := NewTransaction(st, account, registryName)
	if err != nil {
		return err
	}

	if err := SetViaView(tx, view, requests); err != nil {
		return err
	}

	return tx.Commit(st, reg.Schema)
}

// SetViaView uses the view to set the requests in the transaction's databag.
func SetViaView(bag registry.DataBag, view *registry.View, requests map[string]interface{}) error {
	for field, value := range requests {
		var err error
		if value == nil {
			err = view.Unset(bag, field)
		} else {
			err = view.Set(bag, field, value)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

// Get finds the view identified by the account, registry and view names and
// uses it to get the values for the specified fields. The results are returned
// in a map of fields to their values, unless there are no fields in which case
// case all views are returned.
func Get(st *state.State, account, registryName, viewName string, fields []string) (interface{}, error) {
	registryAssert, err := assertstateRegistry(st, account, registryName)
	if err != nil {
		return nil, err
	}
	reg := registryAssert.Registry()

	view := reg.View(viewName)
	if view == nil {
		return nil, &registry.NotFoundError{
			Account:      account,
			RegistryName: registryName,
			View:         viewName,
			Operation:    "get",
			Requests:     fields,
			Cause:        "not found",
		}
	}

	tx, err := NewTransaction(st, account, registryName)
	if err != nil {
		return nil, err
	}

	return GetViaView(tx, view, fields)
}

// GetViaView uses the view to get values for the fields from the databag in
// the transaction.
func GetViaView(bag registry.DataBag, view *registry.View, fields []string) (interface{}, error) {
	if len(fields) == 0 {
		val, err := view.Get(bag, "")
		if err != nil {
			return nil, err
		}

		return val, nil
	}

	results := make(map[string]interface{}, len(fields))
	for _, field := range fields {
		value, err := view.Get(bag, field)
		if err != nil {
			if errors.Is(err, &registry.NotFoundError{}) && len(fields) > 1 {
				// keep looking; return partial result if only some fields are found
				continue
			}

			return nil, err
		}

		results[field] = value
	}

	if len(results) == 0 {
		return nil, &registry.NotFoundError{
			Account:      view.Registry().Account,
			RegistryName: view.Registry().Name,
			View:         view.Name,
			Operation:    "get",
			Requests:     fields,
			Cause:        "matching rules don't map to any values",
		}
	}

	return results, nil
}

var readDatabag = func(st *state.State, account, registryName string) (registry.JSONDataBag, error) {
	var databags map[string]map[string]registry.JSONDataBag
	if err := st.Get("registry-databags", &databags); err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			return registry.NewJSONDataBag(), nil
		}
		return nil, err
	}

	if databags[account] == nil || databags[account][registryName] == nil {
		return registry.NewJSONDataBag(), nil
	}

	return databags[account][registryName], nil
}

var writeDatabag = func(st *state.State, databag registry.JSONDataBag, account, registryName string) error {
	var databags map[string]map[string]registry.JSONDataBag
	err := st.Get("registry-databags", &databags)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	} else if errors.Is(err, &state.NoStateError{}) || databags[account] == nil || databags[account][registryName] == nil {
		databags = map[string]map[string]registry.JSONDataBag{
			account: {registryName: registry.NewJSONDataBag()},
		}
	}

	databags[account][registryName] = databag
	st.Set("registry-databags", databags)
	return nil
}

type cachedRegistryTx struct {
	account  string
	registry string
}

// RegistryTransaction returns the registry.Transaction cached in the context
// or creates one and caches it, if none existed. The context must be locked by
// the caller.
func RegistryTransaction(ctx *hookstate.Context, reg *registry.Registry) (*Transaction, error) {
	key := cachedRegistryTx{
		account:  reg.Account,
		registry: reg.Name,
	}
	tx, ok := ctx.Cached(key).(*Transaction)
	if ok {
		return tx, nil
	}

	tx, err := NewTransaction(ctx.State(), reg.Account, reg.Name)
	if err != nil {
		return nil, err
	}

	ctx.OnDone(func() error {
		return tx.Commit(ctx.State(), reg.Schema)
	})

	ctx.Cache(key, tx)
	return tx, nil
}

func createChangeRegistryTasks(st *state.State, chg *state.Change, tx *Transaction, view *registry.View, callingSnap string) error {
	custodianPlugs, err := getCustodianPlugsForView(st, view)
	if err != nil {
		return err
	}

	if len(custodianPlugs) == 0 {
		return fmt.Errorf("cannot commit changes to registry %s/%s: no custodian snap installed", view.Registry().Account, view.Registry().Name)
	}

	custodianNames := make([]string, 0, len(custodianPlugs))
	for name := range custodianPlugs {
		custodianNames = append(custodianNames, name)
	}

	// process the change/save hooks in a deterministic order (useful for testing
	// and potentially for the snaps themselves)
	sort.Strings(custodianNames)

	var tasks []*state.Task
	linkTask := func(t *state.Task) {
		if len(tasks) > 0 {
			t.WaitFor(tasks[len(tasks)-1])
		}
		tasks = append(tasks, t)
		chg.AddTask(t)
	}

	// if the transaction errors, clear the tx from the state
	clearTxOnErrTask := st.NewTask("clear-registry-tx-on-error", "Clears the ongoing registry transaction from state (on error)")
	linkTask(clearTxOnErrTask)

	// look for plugs that reference the relevant view and create run-hooks for
	// them, if the snap has those hooks
	for _, name := range custodianNames {
		plug := custodianPlugs[name]
		custodian := plug.Snap
		if _, ok := custodian.Hooks["change-view-"+plug.Name]; !ok {
			continue
		}

		const ignoreError = false
		chgViewTask := setupRegistryHook(st, name, "change-view-"+plug.Name, ignoreError)
		// run change-view-<plug> hooks in a sequential, deterministic order
		linkTask(chgViewTask)
	}

	for _, name := range custodianNames {
		plug := custodianPlugs[name]
		custodian := plug.Snap
		if _, ok := custodian.Hooks["save-view-"+plug.Name]; !ok {
			continue
		}

		const ignoreError = false
		saveViewTask := setupRegistryHook(st, name, "save-view-"+plug.Name, ignoreError)
		// also run save-view hooks sequentially so, if one fails, we can determine
		// which tasks need to be rolled back
		linkTask(saveViewTask)
	}

	// run view-changed hooks for any plug that references a view that could have
	// changed with this data modification
	paths := tx.AlteredPaths()
	affectedPlugs, err := getPlugsAffectedByPaths(st, view.Registry(), paths)
	if err != nil {
		return err
	}

	viewChangedSnaps := make([]string, 0, len(affectedPlugs))
	for name := range affectedPlugs {
		viewChangedSnaps = append(viewChangedSnaps, name)
	}
	sort.Strings(viewChangedSnaps)

	for _, snapName := range viewChangedSnaps {
		if snapName == callingSnap {
			// the snap making the changes doesn't need to be notified
			continue
		}

		for _, plug := range affectedPlugs[snapName] {
			// TODO: run these concurrently or keep sequential for predictability?
			const ignoreError = true
			task := setupRegistryHook(st, snapName, plug.Name+"-view-changed", ignoreError)
			linkTask(task)
		}
	}

	// commit after custodians save ephemeral data
	commitTask := st.NewTask("commit-registry-tx", fmt.Sprintf("Commit changes to registry \"%s/%s\"", view.Registry().Account, view.Registry().Name))
	commitTask.Set("registry-transaction", tx)
	// link all previous tasks to the commit task that carries the transaction
	for _, t := range tasks {
		t.Set("commit-task", commitTask.ID())
	}
	linkTask(commitTask)

	// clear the ongoing tx from the state and unblock other writers waiting for it
	clearTxTask := st.NewTask("clear-registry-tx", "Clears the ongoing registry transaction from state")
	linkTask(clearTxTask)
	clearTxTask.Set("commit-task", commitTask.ID())

	return nil
}

func getCustodianPlugsForView(st *state.State, view *registry.View) (map[string]*snap.PlugInfo, error) {
	repo := ifacerepo.Get(st)
	plugs := repo.AllPlugs("registry")

	custodians := make(map[string]*snap.PlugInfo)
	for _, plug := range plugs {
		conns, err := repo.Connected(plug.Snap.InstanceName(), plug.Name)
		if err != nil {
			return nil, err
		}
		if len(conns) == 0 {
			continue
		}

		if role, ok := plug.Attrs["role"]; !ok || role != "custodian" {
			continue
		}

		account, registryName, viewName, err := snap.RegistryPlugAttrs(plug)
		if err != nil {
			return nil, err
		}

		if view.Registry().Account != account || view.Registry().Name != registryName ||
			view.Name != viewName {
			continue
		}

		// TODO: if a snap has more than one plug providing access to a view, then
		// which plug we're getting here becomes unpredictable. We should check
		// for this at some point (interface connection?)
		custodians[plug.Snap.SnapName()] = plug
	}

	return custodians, nil
}

func getPlugsAffectedByPaths(st *state.State, registry *registry.Registry, storagePaths []string) (map[string][]*snap.PlugInfo, error) {
	var viewNames []string
	for _, path := range storagePaths {
		views := registry.GetViewsAffectedByPath(path)
		for _, view := range views {
			viewNames = append(viewNames, view.Name)
		}
	}

	repo := ifacerepo.Get(st)
	plugs := repo.AllPlugs("registry")

	affectedPlugs := make(map[string][]*snap.PlugInfo)
	for _, plug := range plugs {
		conns, err := repo.Connected(plug.Snap.InstanceName(), plug.Name)
		if err != nil {
			return nil, err
		}

		if len(conns) == 0 {
			continue
		}

		account, registryName, viewName, err := snap.RegistryPlugAttrs(plug)
		if err != nil {
			return nil, err
		}

		if account != registry.Account || registryName != registry.Name || !strutil.ListContains(viewNames, viewName) {
			continue
		}

		snapPlugs := affectedPlugs[plug.Snap.InstanceName()]
		affectedPlugs[plug.Snap.InstanceName()] = append(snapPlugs, plug)
	}

	return affectedPlugs, nil
}

// GetStoredTransaction returns the registry transaction associate with the
// task (even if indirectly) and the task in which it was stored.
func GetStoredTransaction(t *state.Task) (*Transaction, *state.Task, error) {
	var tx *Transaction
	err := t.Get("registry-transaction", &tx)
	if err == nil {
		return tx, t, nil
	} else if !errors.Is(err, &state.NoStateError{}) {
		return nil, nil, err
	}

	var id string
	err = t.Get("commit-task", &id)
	if err != nil {
		return nil, nil, err
	}

	ct := t.State().Task(id)
	if ct == nil {
		return nil, nil, fmt.Errorf("cannot find task %s", id)
	}

	if err := ct.Get("registry-transaction", &tx); err != nil {
		return nil, nil, err
	}

	return tx, ct, nil
}

func setTransaction(t *state.Task, tx *Transaction) {
	t.Set("registry-transaction", tx)
}
