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

// SetViaView finds the view identified by the account, registry and view names
// and sets the request fields to their respective values.
func SetViaView(st *state.State, account, registryName, viewName string, requests map[string]interface{}) error {
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

	readOnly := false
	tx, err := NewTransaction(st, readOnly, account, registryName)
	if err != nil {
		return err
	}

	if err = SetViaViewInTx(tx, view, requests); err != nil {
		return err
	}

	// TODO: this also needs to spawn a commit hooks/tasks (or just remove this
	// and use Get/SetViaTx directly)
	return tx.Commit(st, reg.Schema)
}

// SetViaViewInTx uses the view to set the requests in the transaction's databag.
func SetViaViewInTx(tx *Transaction, view *registry.View, requests map[string]interface{}) error {
	for field, value := range requests {
		var err error
		if value == nil {
			err = view.Unset(tx, field)
		} else {
			err = view.Set(tx, field, value)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

// GetViaView finds the view identified by the account, registry and view names
// and uses it to get the values for the specified fields. The results are
// returned in a map of fields to their values, unless there are no fields in
// which case all views are returned.
func GetViaView(st *state.State, account, registryName, viewName string, fields []string) (interface{}, error) {
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

	readOnly := false
	tx, err := NewTransaction(st, readOnly, account, registryName)
	if err != nil {
		return nil, err
	}

	return GetViaViewInTx(tx, view, fields)
}

// GetViaViewInTx uses the view to get values for the fields from the databag
// in the transaction.
func GetViaViewInTx(tx *Transaction, view *registry.View, fields []string) (interface{}, error) {
	if len(fields) == 0 {
		val, err := view.Get(tx, "")
		if err != nil {
			return nil, err
		}

		return val, nil
	}

	results := make(map[string]interface{}, len(fields))
	for _, field := range fields {
		value, err := view.Get(tx, field)
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
			Account:      tx.RegistryAccount,
			RegistryName: tx.RegistryName,
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

// GetTransaction retrieves or creates a transaction to access the view's registry,
// ctx can be nil if not called from a hook context.
func GetTransaction(ctx *hookstate.Context, st *state.State, view *registry.View) (tx *Transaction, blockingCommit string, err error) {
	account, registryName := view.Registry().Account, view.Registry().Name

	// check if we're already running in the context of a committing transaction
	if ctx != nil && !ctx.IsEphemeral() && IsRegistryHook(ctx) {
		// running in the context of a transaction, so if the referenced registry
		// doesn't match that tx, we only allow the caller to read the other registry
		t, _ := ctx.Task()
		var err error
		tx, commitTask, err := GetStoredTransaction(t)
		if err != nil {
			return nil, "", fmt.Errorf("cannot modify registry view %s/%s/%s: cannot get transaction: %v", account, registryName, view.Name, err)
		}

		if tx.RegistryAccount != account || tx.RegistryName != registryName {
			// accessing a different registry than the one with an ongoing transaction,
			// allow only reading
			readOnly := true
			tx, err := NewTransaction(st, readOnly, account, registryName)
			if err != nil {
				return nil, "", err
			}

			return tx, "", err
		}

		tx.OnDone(func() error {
			setTransaction(commitTask, tx)
			return nil
		})

		return tx, "", nil
	}

	ongoingTxCommit, err := GetTransactionCommit(st, account, registryName)
	if err != nil {
		return nil, "", err
	}

	if ongoingTxCommit != "" {
		// there's an ongoing transaction for this registry, wait for it to complete
		return nil, ongoingTxCommit, nil
	}

	// not running in an existing registry hook context, so create a transaction
	// and a change to verify its changes and commit
	readOnly := false
	tx, err = NewTransaction(st, readOnly, account, registryName)
	if err != nil {
		return nil, "", fmt.Errorf("cannot modify registry view %s/%s/%s: cannot create transaction: %v", account, registryName, view.Name, err)
	}

	tx.OnDone(func() error {
		var chg *state.Change
		if ctx == nil || ctx.IsEphemeral() {
			chg = st.NewChange("modify-registry", fmt.Sprintf("Modify registry \"%s/%s\"", account, registryName))
		} else {
			// we're running in the context of a non-registry hook, add the tasks to that change
			task, _ := ctx.Task()
			chg = task.Change()
		}

		var callingSnap string
		if ctx != nil {
			callingSnap = ctx.InstanceName()
		}

		err := CreateModifyRegistryChange(st, chg, tx, view, callingSnap)
		if err != nil {
			return err
		}

		_, commitTask, err := GetStoredTransaction(chg.Tasks()[0])
		if err != nil {
			return err
		}

		st.EnsureBefore(0)
		return SetTransactionCommit(st, account, registryName, commitTask.ID())
	})

	return tx, "", nil
}

func CreateModifyRegistryChange(st *state.State, chg *state.Change, tx *Transaction, view *registry.View, callingSnap string) error {
	// TODO: possible TOCTOU issue here? check again in handlers Precondition
	managerPlugs, err := getManagerPlugsForView(st, view)
	if err != nil {
		return err
	}

	if len(managerPlugs) == 0 {
		return fmt.Errorf("cannot commit changes to registry %s/%s: no manager snap installed", view.Registry().Account, view.Registry().Name)
	}

	managerNames := make([]string, 0, len(managerPlugs))
	for name := range managerPlugs {
		managerNames = append(managerNames, name)
	}

	// process the change/save hooks in a deterministic order (useful for testing
	// and potentially for the snaps)
	sort.Strings(managerNames)

	var tasks []*state.Task
	linkTask := func(t *state.Task) {
		if len(tasks) > 0 {
			t.WaitFor(tasks[len(tasks)-1])
		}
		tasks = append(tasks, t)
		chg.AddTask(t)
	}

	// if the transaction errors, clear the tx from the state
	clearTxTask := st.NewTask("clear-tx-on-error", "Clears the ongoing transaction from state (on error)")
	linkTask(clearTxTask)

	// look for plugs that reference the relevant view and create run-hooks for
	// them, if the snap has those hooks
	for _, name := range managerNames {
		plug := managerPlugs[name]
		manager := plug.Snap
		if _, ok := manager.Hooks["change-view-"+plug.Name]; !ok {
			continue
		}

		ignoreError := false
		chgViewTask := setupRegistryHook(st, name, "change-view-"+plug.Name, ignoreError)
		// run change-view-<plug> hooks in a sequential, deterministic order
		linkTask(chgViewTask)
	}

	for _, name := range managerNames {
		plug := managerPlugs[name]
		manager := plug.Snap
		if _, ok := manager.Hooks["save-view-"+plug.Name]; !ok {
			continue
		}

		ignoreError := false
		saveViewTask := setupRegistryHook(st, name, "save-view-"+plug.Name, ignoreError)
		// also run save-view hooks sequentially so, if one fails, we can determine
		// which tasks need to be rolled back
		linkTask(saveViewTask)
	}

	// commit after managers save ephemeral data
	commitTask := st.NewTask("commit-transaction", fmt.Sprintf("Commit changes to registry \"%s/%s\"", view.Registry().Account, view.Registry().Name))
	commitTask.Set("registry-transaction", tx)
	// link all previous tasks to the commit task that carries the transaction
	for _, t := range tasks {
		t.Set("commit-task", commitTask.ID())
	}
	linkTask(commitTask)

	// run view-changed hooks for any plug that references a view that could have
	// changed with this data modification
	paths := tx.AlteredPaths()
	affectedPlugs, err := getPlugsAffectedByPaths(st, view.Registry(), paths)
	if err != nil {
		return err
	}

	for snapName, plugs := range affectedPlugs {
		if snapName == callingSnap {
			// the snap making the changes doesn't need to be notified
			continue
		}

		for _, plug := range plugs {
			ignoreError := true
			task := setupRegistryHook(st, snapName, plug.Name+"-view-changed", ignoreError)
			chg.AddTask(task)
			// at this point, hook failure should not abort the change so run concurrently
			task.WaitFor(commitTask)
			task.Set("commit-task", commitTask.ID())
		}
	}

	return nil
}

func getManagerPlugsForView(st *state.State, view *registry.View) (map[string]*snap.PlugInfo, error) {
	repo := ifacerepo.Get(st)
	plugs := repo.AllPlugs("registry")

	managers := make(map[string]*snap.PlugInfo)
	for _, plug := range plugs {
		conns, err := repo.Connected(plug.Snap.InstanceName(), plug.Name)
		if err != nil {
			return nil, err
		}
		if len(conns) == 0 {
			continue
		}

		if role, ok := plug.Attrs["role"]; !ok || role != "manager" {
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

		// TODO: where should we check that there isn't more than one plug for a given view (per snap)
		managers[plug.Snap.SnapName()] = plug
	}

	return managers, nil
}

// GetTransction returns the registry transaction associate with the task (even
// if indirectly) and the task in which it was stored.
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

func GetTransactionCommit(st *state.State, account, registryName string) (string, error) {
	var txCommits map[string]string
	err := st.Get("registry-tx-commits", &txCommits)
	if err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			return "", nil
		}

		return "", err
	}

	registryRef := account + "/" + registryName
	return txCommits[registryRef], nil
}

func SetTransactionCommit(st *state.State, account, registryName string, commitTaskID string) error {
	var txCommits map[string]string
	err := st.Get("registry-tx-commits", &txCommits)
	if err != nil {
		if !errors.Is(err, &state.NoStateError{}) {
			return err
		}

		txCommits = make(map[string]string, 1)
	}

	registryRef := account + "/" + registryName
	txCommits[registryRef] = commitTaskID
	st.Set("registry-tx-commits", txCommits)
	return nil
}

func UnsetTransactionCommit(st *state.State, account, registryName string) error {
	var txCommits map[string]string
	err := st.Get("registry-tx-commits", &txCommits)
	if err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			// already unset, nothing to do
			return nil
		}
		return err
	}

	registryRef := account + "/" + registryName
	delete(txCommits, registryRef)

	if len(txCommits) == 0 {
		st.Set("registry-tx-commits", nil)
	} else {
		st.Set("registry-tx-commits", txCommits)
	}

	return nil
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
