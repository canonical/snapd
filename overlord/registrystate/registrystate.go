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
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/i18n"
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
	view, err := GetView(st, account, registryName, viewName)
	if err != nil {
		return err
	}

	tx, err := NewTransaction(st, account, registryName)
	if err != nil {
		return err
	}

	if err := SetViaView(tx, view, requests); err != nil {
		return err
	}

	return tx.Commit(st, view.Registry().Schema)
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

// GetView returns the view identified by the account, registry and view name.
func GetView(st *state.State, account, registryName, viewName string) (*registry.View, error) {
	registryAssert, err := assertstateRegistry(st, account, registryName)
	if err != nil {
		if errors.Is(err, &asserts.NotFoundError{}) {
			// replace the not found error so the output matches the usual registry ID layout
			return nil, registry.NewNotFoundError(i18n.G("cannot find registry %s/%s: assertion not found"), account, registryName)

		}
		return nil, fmt.Errorf(i18n.G("cannot find registry assertion %s/%s: %v"), account, registryName, err)
	}
	reg := registryAssert.Registry()

	view := reg.View(viewName)
	if view == nil {
		return nil, registry.NewNotFoundError(i18n.G("cannot find view %q in registry %s/%s"), viewName, account, registryName)
	}

	return view, nil
}

// Get finds the view identified by the account, registry and view names and
// uses it to get the values for the specified fields. The results are returned
// in a map of fields to their values, unless there are no fields in which case
// case all views are returned.
func Get(st *state.State, account, registryName, viewName string, fields []string) (interface{}, error) {
	view, err := GetView(st, account, registryName, viewName)
	if err != nil {
		return nil, err
	}

	bag, err := readDatabag(st, account, registryName)
	if err != nil {
		return nil, err
	}

	return GetViaView(bag, view, fields)
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
		var reqStr string
		switch len(fields) {
		case 0:
			// leave empty, so the message reflects the request gets the whole view
		case 1:
			// we should error out of the check in the loop before we hit this, but
			// let's be robust in case we do
			reqStr = fmt.Sprintf(i18n.G(" %q through"), fields[0])
		default:
			reqStr = fmt.Sprintf(i18n.G(" %s through"), strutil.Quoted(fields))
		}

		return nil, registry.NewNotFoundError(i18n.G("cannot get%s %s/%s/%s: no view data"), reqStr, view.Registry().Account, view.Registry().Name, view.Name)
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

// GetTransactionToModify retrieves or creates a transaction to change the view's
// registry. The state must be locked by the caller. Takes a context which should
// contain a hookstate.Context in it, if it came from a hook context. Calling
// ctx.Done will trigger the creation of a modify-registry change and wait for
// its completion (unless one already existed, in which case it just persists
// changes to the existing transaction).
func GetTransactionToModify(ctx *Context, st *state.State, view *registry.View) (*Transaction, error) {
	account, registryName := view.Registry().Account, view.Registry().Name

	// check if we're already running in the context of a committing transaction
	hookCtx := ctx.hookCtx
	if IsRegistryHook(hookCtx) {
		// running in the context of a transaction, so if the referenced registry
		// doesn't match that tx, we only allow the caller to read the other registry
		t, _ := hookCtx.Task()
		tx, saveTxChanges, err := GetStoredTransaction(t)
		if err != nil {
			return nil, fmt.Errorf("cannot access registry view %s/%s/%s: cannot get transaction: %v", account, registryName, view.Name, err)
		}

		if tx.RegistryAccount != account || tx.RegistryName != registryName {
			return nil, fmt.Errorf("cannot access registry %s/%s: ongoing transaction for %s/%s", account, registryName, tx.RegistryAccount, tx.RegistryName)
		}

		// update the commit task to save transaction changes made by the hook
		ctx.onDone(func() error {
			saveTxChanges()
			return nil
		})

		return tx, nil
	}
	// TODO: add concurrency checks

	// not running in an existing registry hook context, so create a transaction
	// and a change to verify its changes and commit
	tx, err := NewTransaction(st, account, registryName)
	if err != nil {
		return nil, fmt.Errorf("cannot modify registry view %s/%s/%s: cannot create transaction: %v", account, registryName, view.Name, err)
	}

	ctx.onDone(func() error {
		var chg *state.Change
		if hookCtx == nil || hookCtx.IsEphemeral() {
			chg = st.NewChange("modify-registry", fmt.Sprintf("Modify registry \"%s/%s\"", account, registryName))
		} else {
			// we're running in the context of a non-registry hook, add the tasks to that change
			task, _ := hookCtx.Task()
			chg = task.Change()
		}

		var callingSnap string
		if hookCtx != nil {
			callingSnap = hookCtx.InstanceName()
		}

		ts, err := createChangeRegistryTasks(st, tx, view, callingSnap)
		if err != nil {
			return err
		}
		chg.AddAll(ts)

		commitTask, err := ts.Edge(commitEdge)
		if err != nil {
			return err
		}

		clearTxTask, err := ts.Edge(clearTxEdge)
		if err != nil {
			return err
		}

		err = setOngoingTransaction(st, account, registryName, commitTask.ID())
		if err != nil {
			return err
		}

		taskReady := make(chan struct{})
		var done bool
		id := st.AddTaskStatusChangedHandler(func(t *state.Task, old, new state.Status) {
			if !done && t.ID() == clearTxTask.ID() && new.Ready() {
				done = true
				taskReady <- struct{}{}
				return
			}
		})

		ensureNow(st)
		st.Unlock()
		<-taskReady
		st.Lock()
		st.RemoveTaskStatusChangedHandler(id)
		return nil
	})

	return tx, nil
}

var ensureNow = func(st *state.State) {
	st.EnsureBefore(0)
}

const (
	commitEdge  = state.TaskSetEdge("commit-edge")
	clearTxEdge = state.TaskSetEdge("clear-tx-edge")
)

func createChangeRegistryTasks(st *state.State, tx *Transaction, view *registry.View, callingSnap string) (*state.TaskSet, error) {
	custodianPlugs, err := getCustodianPlugsForView(st, view)
	if err != nil {
		return nil, err
	}

	if len(custodianPlugs) == 0 {
		return nil, fmt.Errorf("cannot commit changes to registry %s/%s: no custodian snap installed", view.Registry().Account, view.Registry().Name)
	}

	custodianNames := make([]string, 0, len(custodianPlugs))
	for name := range custodianPlugs {
		custodianNames = append(custodianNames, name)
	}

	// process the change/save hooks in a deterministic order (useful for testing
	// and potentially for the snaps themselves)
	sort.Strings(custodianNames)

	ts := state.NewTaskSet()
	linkTask := func(t *state.Task) {
		tasks := ts.Tasks()
		if len(tasks) > 0 {
			t.WaitFor(tasks[len(tasks)-1])
		}
		ts.AddTask(t)
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
		return nil, err
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
	for _, t := range ts.Tasks() {
		t.Set("commit-task", commitTask.ID())
	}
	linkTask(commitTask)
	ts.MarkEdge(commitTask, commitEdge)

	// clear the ongoing tx from the state and unblock other writers waiting for it
	clearTxTask := st.NewTask("clear-registry-tx", "Clears the ongoing registry transaction from state")
	linkTask(clearTxTask)
	clearTxTask.Set("commit-task", commitTask.ID())
	ts.MarkEdge(clearTxTask, clearTxEdge)

	return ts, nil
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

// GetStoredTransaction returns the transaction associated with the task
// (even if indirectly) and a callback to persist changes made to it.
func GetStoredTransaction(t *state.Task) (tx *Transaction, saveTxChanges func(), err error) {
	err = t.Get("registry-transaction", &tx)
	if err == nil {
		saveTxChanges = func() {
			t.Set("registry-transaction", tx)
		}

		return tx, saveTxChanges, nil
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

	saveTxChanges = func() {
		ct.Set("registry-transaction", tx)
	}
	return tx, saveTxChanges, nil
}

// IsRegistryHook returns whether the hook context belongs to a registry hook.
func IsRegistryHook(ctx *hookstate.Context) bool {
	return ctx != nil && !ctx.IsEphemeral() &&
		(strings.HasPrefix(ctx.HookName(), "change-view-") ||
			strings.HasPrefix(ctx.HookName(), "save-view-") ||
			strings.HasSuffix(ctx.HookName(), "-view-changed"))
}

// Context is used by GetTransaction to defer actions until a point after the
// call (e.g., blocking on a registry transaction only when the caller wants to).
// The context also carries a hookstate.Context that is used to determine whether
// the registry is being accessed from a snap hook.
type Context struct {
	hookCtx *hookstate.Context

	onDoneCallbacks []func() error
}

func NewContext(ctx *hookstate.Context) *Context {
	regCtx := &Context{
		hookCtx: ctx,
	}

	if ctx != nil {
		ctx.OnDone(func() error {
			return regCtx.Done()
		})
	}

	return regCtx
}

func (c *Context) onDone(f func() error) {
	c.onDoneCallbacks = append(c.onDoneCallbacks, f)
}

func (c *Context) Done() error {
	var firstErr error
	for _, f := range c.onDoneCallbacks {
		if err := f(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}
