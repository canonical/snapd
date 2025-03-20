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

package confdbstate

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

var assertstateConfdbSchema = assertstate.ConfdbSchema

// SetViaView uses the view to set the requests in the transaction's databag.
func SetViaView(bag confdb.Databag, view *confdb.View, requests map[string]interface{}) error {
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

// GetView returns the view identified by the account, confdb schema and view name.
func GetView(st *state.State, account, dbSchemaName, viewName string) (*confdb.View, error) {
	confdbSchemaAs, err := assertstateConfdbSchema(st, account, dbSchemaName)
	if err != nil {
		if errors.Is(err, &asserts.NotFoundError{}) {
			// replace the not found error so the output matches the usual confdb ID layout
			return nil, confdb.NewNotFoundError(i18n.G("cannot find confdb schema %s/%s: assertion not found"), account, dbSchemaName)
		}

		return nil, fmt.Errorf(i18n.G("cannot find confdb-schema assertion %s/%s: %v"), account, dbSchemaName, err)
	}
	dbSchema := confdbSchemaAs.Schema()

	view := dbSchema.View(viewName)
	if view == nil {
		return nil, confdb.NewNotFoundError(i18n.G("cannot find view %q in confdb schema %s/%s"), viewName, account, dbSchemaName)
	}

	return view, nil
}

// GetViaView uses the view to get values for the fields from the databag in
// the transaction.
func GetViaView(bag confdb.Databag, view *confdb.View, fields []string) (interface{}, error) {
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
			if errors.Is(err, &confdb.NotFoundError{}) && len(fields) > 1 {
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

		return nil, confdb.NewNotFoundError(i18n.G("cannot get%s %s/%s/%s: no data"), reqStr, view.Schema().Account, view.Schema().Name, view.Name)
	}

	return results, nil
}

var readDatabag = func(st *state.State, account, dbSchemaName string) (confdb.JSONDatabag, error) {
	var databags map[string]map[string]confdb.JSONDatabag
	if err := st.Get("confdb-databags", &databags); err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			return confdb.NewJSONDatabag(), nil
		}
		return nil, err
	}

	if databags[account] == nil || databags[account][dbSchemaName] == nil {
		return confdb.NewJSONDatabag(), nil
	}

	return databags[account][dbSchemaName], nil
}

var writeDatabag = func(st *state.State, databag confdb.JSONDatabag, account, dbSchemaName string) error {
	var databags map[string]map[string]confdb.JSONDatabag
	err := st.Get("confdb-databags", &databags)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	} else if errors.Is(err, &state.NoStateError{}) || databags[account] == nil || databags[account][dbSchemaName] == nil {
		databags = map[string]map[string]confdb.JSONDatabag{
			account: {dbSchemaName: confdb.NewJSONDatabag()},
		}
	}

	databags[account][dbSchemaName] = databag
	st.Set("confdb-databags", databags)
	return nil
}

type CommitTxFunc func() (changeID string, waitChan <-chan struct{}, err error)

// GetTransactionToSet gets a transaction to change the confdb through the view.
// The state must be locked by the caller. Returns a transaction through which
// the confdb can be modified and a CommitTxFunc. The latter is called once the
// modifications are made to commit them. It will return a changeID and a channel,
// allowing the caller to block until commit. If a transaction was already ongoing,
// CommitTxFunc simply returns that without blocking (changes to it will be
// saved on ctx.Done()).
func GetTransactionToSet(ctx *hookstate.Context, st *state.State, view *confdb.View) (*Transaction, CommitTxFunc, error) {
	account, dbSchemaName := view.Schema().Account, view.Schema().Name

	// check if we're already running in the context of a committing transaction
	if IsConfdbHook(ctx) {
		// running in the context of a transaction, so if the referenced confdb schema
		// doesn't match that tx, we only allow the caller to read through the other confdb schema
		t, _ := ctx.Task()
		tx, _, saveTxChanges, err := GetStoredTransaction(t)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot access confdb through view %s/%s/%s: cannot get transaction: %v", account, dbSchemaName, view.Name, err)
		}

		if tx.ConfdbAccount != account || tx.ConfdbName != dbSchemaName {
			return nil, nil, fmt.Errorf("cannot access confdb through view %s/%s/%s: ongoing transaction for %s/%s", account, dbSchemaName, view.Name, tx.ConfdbAccount, tx.ConfdbName)
		}

		// update the commit task to save transaction changes made by the hook
		ctx.OnDone(func() error {
			saveTxChanges()
			return nil
		})

		return tx, nil, nil
	}
	// TODO: add early concurrency checks

	// not running in an existing confdb hook context, so create a transaction
	// and a change to verify its changes and commit
	tx, err := NewTransaction(st, account, dbSchemaName)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot modify confdb through view %s/%s/%s: cannot create transaction: %v", account, dbSchemaName, view.Name, err)
	}

	commitTx := func() (string, <-chan struct{}, error) {
		var chg *state.Change
		if ctx == nil || ctx.IsEphemeral() {
			chg = st.NewChange("set-confdb", fmt.Sprintf("Set confdb through \"%s/%s/%s\"", account, dbSchemaName, view.Name))
		} else {
			// we're running in the context of a non-confdb hook, add the tasks to that change
			task, _ := ctx.Task()
			chg = task.Change()
		}

		var callingSnap string
		if ctx != nil {
			callingSnap = ctx.InstanceName()
		}

		ts, err := createChangeConfdbTasks(st, tx, view, callingSnap)
		if err != nil {
			return "", nil, err
		}
		chg.AddAll(ts)

		commitTask, err := ts.Edge(commitEdge)
		if err != nil {
			return "", nil, err
		}

		clearTxTask, err := ts.Edge(clearTxEdge)
		if err != nil {
			return "", nil, err
		}

		err = setWriteTransaction(st, account, dbSchemaName, commitTask.ID())
		if err != nil {
			return "", nil, err
		}

		waitChan := make(chan struct{})
		st.AddTaskStatusChangedHandler(func(t *state.Task, old, new state.Status) (remove bool) {
			if t.ID() == clearTxTask.ID() && new.Ready() {
				close(waitChan)
				return true
			}
			return false
		})

		ensureNow(st)
		return chg.ID(), waitChan, nil
	}

	return tx, commitTx, nil
}

var (
	ensureNow = func(st *state.State) {
		st.EnsureBefore(0)
	}

	transactionTimeout = 2 * time.Minute
)

const (
	commitEdge  = state.TaskSetEdge("commit-edge")
	clearTxEdge = state.TaskSetEdge("clear-tx-edge")
)

func createChangeConfdbTasks(st *state.State, tx *Transaction, view *confdb.View, callingSnap string) (*state.TaskSet, error) {
	custodians, custodianPlugs, err := getCustodianPlugsForView(st, view)
	if err != nil {
		return nil, err
	}

	if len(custodianPlugs) == 0 {
		return nil, fmt.Errorf("cannot commit changes to confdb made through view %s/%s/%s: no custodian snap installed", view.Schema().Account, view.Schema().Name, view.Name)
	}

	ts := state.NewTaskSet()
	linkTask := func(t *state.Task) {
		tasks := ts.Tasks()
		if len(tasks) > 0 {
			t.WaitFor(tasks[len(tasks)-1])
		}
		ts.AddTask(t)
	}

	// if the transaction errors, clear the tx from the state
	clearTxOnErrTask := st.NewTask("clear-confdb-tx-on-error", "Clears the ongoing confdb transaction from state (on error)")
	linkTask(clearTxOnErrTask)

	// look for plugs that reference the relevant view and create run-hooks for
	// them, if the snap has those hooks
	for _, name := range custodians {
		plug := custodianPlugs[name]
		custodian := plug.Snap
		if _, ok := custodian.Hooks["change-view-"+plug.Name]; !ok {
			continue
		}

		const ignoreError = false
		chgViewTask := setupConfdbHook(st, name, "change-view-"+plug.Name, ignoreError)
		// run change-view-<plug> hooks in a sequential, deterministic order
		linkTask(chgViewTask)
	}

	for _, name := range custodians {
		plug := custodianPlugs[name]
		custodian := plug.Snap
		if _, ok := custodian.Hooks["save-view-"+plug.Name]; !ok {
			continue
		}

		const ignoreError = false
		saveViewTask := setupConfdbHook(st, name, "save-view-"+plug.Name, ignoreError)
		// also run save-view hooks sequentially so, if one fails, we can determine
		// which tasks need to be rolled back
		linkTask(saveViewTask)
	}

	// run observe-view hooks for any plug that references a view that could have
	// changed with this data modification
	paths := tx.AlteredPaths()
	affectedPlugs, err := getPlugsAffectedByPaths(st, view.Schema(), paths)
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
			task := setupConfdbHook(st, snapName, "observe-view-"+plug.Name, ignoreError)
			linkTask(task)
		}
	}

	// commit after custodians save ephemeral data
	commitTask := st.NewTask("commit-confdb-tx", fmt.Sprintf("Commit changes to confdb (%s/%s/%s)", view.Schema().Account, view.Schema().Name, view.Name))
	commitTask.Set("confdb-transaction", tx)
	// link all previous tasks to the commit task that carries the transaction
	for _, t := range ts.Tasks() {
		t.Set("tx-task", commitTask.ID())
	}
	linkTask(commitTask)
	ts.MarkEdge(commitTask, commitEdge)

	// clear the ongoing tx from the state and unblock other writers waiting for it
	clearTxTask := st.NewTask("clear-confdb-tx", "Clears the ongoing confdb transaction from state")
	linkTask(clearTxTask)
	clearTxTask.Set("tx-task", commitTask.ID())
	ts.MarkEdge(clearTxTask, clearTxEdge)

	return ts, nil
}

// getCustodianPlugsForView returns a list of snaps that have connected plugs
// declaring them as custodians of a confdb view. The list of custodians is
// sorted. It also returns a map of the snap names to plugs.
func getCustodianPlugsForView(st *state.State, view *confdb.View) ([]string, map[string]*snap.PlugInfo, error) {
	repo := ifacerepo.Get(st)
	plugs := repo.AllPlugs("confdb")

	var custodians []string
	custodianPlugs := make(map[string]*snap.PlugInfo)
	for _, plug := range plugs {
		conns, err := repo.Connected(plug.Snap.InstanceName(), plug.Name)
		if err != nil {
			return nil, nil, err
		}
		if len(conns) == 0 {
			continue
		}

		if role, ok := plug.Attrs["role"]; !ok || role != "custodian" {
			continue
		}

		account, dbSchemaName, viewName, err := snap.ConfdbPlugAttrs(plug)
		if err != nil {
			return nil, nil, err
		}

		if view.Schema().Account != account || view.Schema().Name != dbSchemaName ||
			view.Name != viewName {
			continue
		}

		// TODO: if a snap has more than one plug providing access to a view, then
		// which plug we're getting here becomes unpredictable. We should check
		// for this at some point (interface connection?)
		custodianPlugs[plug.Snap.SnapName()] = plug
		custodians = append(custodians, plug.Snap.InstanceName())
	}

	// we want to process these in a deterministic order (useful for testing
	// and potentially for the snaps themselves)
	sort.Strings(custodians)

	return custodians, custodianPlugs, nil
}

func getPlugsAffectedByPaths(st *state.State, dbSchema *confdb.Schema, storagePaths []string) (map[string][]*snap.PlugInfo, error) {
	var viewNames []string
	for _, path := range storagePaths {
		views := dbSchema.GetViewsAffectedByPath(path)
		for _, view := range views {
			viewNames = append(viewNames, view.Name)
		}
	}

	repo := ifacerepo.Get(st)
	plugs := repo.AllPlugs("confdb")

	affectedPlugs := make(map[string][]*snap.PlugInfo)
	for _, plug := range plugs {
		conns, err := repo.Connected(plug.Snap.InstanceName(), plug.Name)
		if err != nil {
			return nil, err
		}

		if len(conns) == 0 {
			continue
		}

		account, dbSchemaName, viewName, err := snap.ConfdbPlugAttrs(plug)
		if err != nil {
			return nil, err
		}

		if account != dbSchema.Account || dbSchemaName != dbSchema.Name || !strutil.ListContains(viewNames, viewName) {
			continue
		}

		snapPlugs := affectedPlugs[plug.Snap.InstanceName()]
		affectedPlugs[plug.Snap.InstanceName()] = append(snapPlugs, plug)
	}

	return affectedPlugs, nil
}

// GetStoredTransaction returns the transaction associated with the task
// (even if indirectly) and a callback to persist changes made to it.
func GetStoredTransaction(t *state.Task) (tx *Transaction, txTask *state.Task, saveTxChanges func(), err error) {
	err = t.Get("confdb-transaction", &tx)
	if err == nil {
		saveTxChanges = func() {
			t.Set("confdb-transaction", tx)
		}

		return tx, t, saveTxChanges, nil
	} else if !errors.Is(err, &state.NoStateError{}) {
		return nil, nil, nil, err
	}

	var id string
	err = t.Get("tx-task", &id)
	if err != nil {
		return nil, nil, nil, err
	}

	ct := t.State().Task(id)
	if ct == nil {
		return nil, nil, nil, fmt.Errorf("cannot find task %s", id)
	}

	if err := ct.Get("confdb-transaction", &tx); err != nil {
		return nil, nil, nil, err
	}

	saveTxChanges = func() {
		ct.Set("confdb-transaction", tx)
	}
	return tx, ct, saveTxChanges, nil
}

// IsConfdbHook returns whether the hook context belongs to a confdb hook.
func IsConfdbHook(ctx *hookstate.Context) bool {
	return ctx != nil && !ctx.IsEphemeral() &&
		(strings.HasPrefix(ctx.HookName(), "change-view-") ||
			strings.HasPrefix(ctx.HookName(), "save-view-") ||
			strings.HasPrefix(ctx.HookName(), "load-view-") ||
			strings.HasPrefix(ctx.HookName(), "query-view-") ||
			strings.HasPrefix(ctx.HookName(), "observe-view-"))
}

// IsConfdbHook returns whether the hook context belongs to a confdb hook.
func IsModifyConfdbHook(ctx *hookstate.Context) bool {
	return ctx != nil && !ctx.IsEphemeral() &&
		(strings.HasPrefix(ctx.HookName(), "change-view-") ||
			strings.HasPrefix(ctx.HookName(), "load-view-"))
}

// GetTransactionForSnapctlGet gets a transaction to read the view's confdb. It
// schedules tasks to load the confdb as needed, unless no custodian defined
// relevant hooks. Blocks until the confdb has been loaded into the Transaction.
// If no tasks need to run to load the confdb, returns without blocking.
func GetTransactionForSnapctlGet(ctx *hookstate.Context, view *confdb.View) (*Transaction, error) {
	st := ctx.State()
	account, schemaName := view.Schema().Account, view.Schema().Name
	if IsConfdbHook(ctx) {
		// running in the context of a transaction, so if the referenced confdb
		// doesn't match that tx, we only allow the caller to read the other confdb
		t, _ := ctx.Task()
		tx, _, _, err := GetStoredTransaction(t)
		if err != nil {
			return nil, fmt.Errorf("cannot load confdb view %s/%s/%s: cannot get transaction: %v", account, schemaName, view.Name, err)
		}

		if tx.ConfdbAccount != account || tx.ConfdbName != schemaName {
			// TODO: this should be enabled at some point
			return nil, fmt.Errorf("cannot load confdb %s/%s: ongoing transaction for %s/%s", account, schemaName, tx.ConfdbAccount, tx.ConfdbName)
		}

		// we're reading the tx that this hook is modifying, just return that
		return tx, nil
	}

	// not running in an existing confdb hook context, so create a transaction
	// and a change to load/modify data
	tx, err := NewTransaction(st, account, schemaName)
	if err != nil {
		return nil, fmt.Errorf("cannot load confdb view %s/%s/%s: cannot create transaction: %v", account, schemaName, view.Name, err)
	}

	ts, err := createLoadConfdbTasks(st, tx, view)
	if err != nil {
		return nil, err
	}

	if ts == nil {
		// no hooks or tasks to run, transaction can read databag directly
		return tx, nil
	}

	var chg *state.Change
	if ctx.IsEphemeral() {
		chg = st.NewChange("get-confdb", fmt.Sprintf("Get confdb through \"%s/%s/%s\"", account, schemaName, view.Name))
	} else {
		// we're running in the context of a non-confdb hook, add the tasks to that change
		task, _ := ctx.Task()
		chg = task.Change()
	}

	chg.AddAll(ts)

	clearTxTask, err := ts.Edge(clearTxEdge)
	if err != nil {
		return nil, err
	}

	waitChan := make(chan struct{})
	st.AddTaskStatusChangedHandler(func(t *state.Task, old, new state.Status) (remove bool) {
		if t.ID() == clearTxTask.ID() && new.Ready() {
			close(waitChan)
			return true
		}
		return false
	})

	err = addReadTransaction(st, account, schemaName, clearTxTask.ID())
	if err != nil {
		return nil, err
	}

	ensureNow(st)
	ctx.Unlock()

	select {
	case <-waitChan:
	case <-time.After(transactionTimeout):
		ctx.Lock()
		return nil, fmt.Errorf("cannot load confdb %s/%s in change %s: timed out after %s", account, schemaName, chg.ID(), transactionTimeout)
	}

	ctx.Lock()
	if err := clearTxTask.Get("confdb-transaction", &tx); err != nil {
		return nil, err
	}
	return tx, nil
}

// LoadConfdbAsync schedules a change to load a confdb, running any appropriate
// hooks and fulfilling the requests by reading the view and placing the resulting
// data in the change's data (so it can be read by the client).
func LoadConfdbAsync(st *state.State, view *confdb.View, requests []string) (changeID string, err error) {
	account, schemaName := view.Schema().Account, view.Schema().Name

	tx, err := NewTransaction(st, account, schemaName)
	if err != nil {
		return "", fmt.Errorf("cannot access confdb view %s/%s/%s: cannot create transaction: %v", account, schemaName, view.Name, err)
	}

	ts, err := createLoadConfdbTasks(st, tx, view)
	if err != nil {
		return "", err
	}

	chg := st.NewChange("get-confdb", fmt.Sprintf(`Get confdb through "%s/%s/%s"`, account, schemaName, view.Name))
	if ts != nil {
		// if there are hooks to run, link the read-confdb task to those tasks
		clearTxTask, err := ts.Edge(clearTxEdge)
		if err != nil {
			return "", err
		}

		// schedule a task to read the tx after the hook and add the data to the
		// change so it can be read by the client
		loadConfdbTask := st.NewTask("load-confdb-change", "Load confdb data into the change")
		loadConfdbTask.Set("requests", requests)
		loadConfdbTask.Set("view-name", view.Name)

		loadConfdbTask.Set("tx-task", clearTxTask.ID())
		loadConfdbTask.WaitFor(clearTxTask)
		chg.AddAll(ts)

		err = addReadTransaction(st, account, schemaName, clearTxTask.ID())
		if err != nil {
			return "", err
		}
		chg.AddTask(loadConfdbTask)
	} else {
		// no hooks to run so we can just load the values directly into the change
		// (we still need the change because the API is async)
		err := readViewIntoChange(chg, tx, view, requests)
		if err != nil {
			return "", err
		}
		chg.SetStatus(state.DoneStatus)
	}

	return chg.ID(), nil
}

// createLoadConfdbTasks returns a taskset with the hooks and tasks required to
// read a transaction through the given view. In case no custodian snap has any
// load-view or query-view hooks, nil is returned. If there are hooks to run,
// a clear-confdb-tx task is also scheduled to remove the ongoing transaction at the end.
func createLoadConfdbTasks(st *state.State, tx *Transaction, view *confdb.View) (*state.TaskSet, error) {
	custodians, custodianPlugs, err := getCustodianPlugsForView(st, view)
	if err != nil {
		return nil, err
	}

	if len(custodians) == 0 {
		return nil, fmt.Errorf("cannot load confdb through view %s/%s/%s: no custodian snap connected", view.Schema().Account, view.Schema().Name, view.Name)
	}

	ts := state.NewTaskSet()
	linkTask := func(t *state.Task) {
		tasks := ts.Tasks()
		if len(tasks) > 0 {
			t.WaitFor(tasks[len(tasks)-1])
		}
		ts.AddTask(t)
	}

	// check for load-view and query-view hooks on custodians
	for _, hookPrefix := range []string{"load-view-", "query-view-"} {
		for _, name := range custodians {
			plug := custodianPlugs[name]
			custodian := plug.Snap
			if _, ok := custodian.Hooks[hookPrefix+plug.Name]; !ok {
				continue
			}

			const ignoreError = false
			task := setupConfdbHook(st, name, hookPrefix+plug.Name, ignoreError)
			linkTask(task)
		}
	}

	if len(ts.Tasks()) == 0 {
		// no hooks to run and not running from API (don't need task to populate)
		// data in change so we can just read the databag synchronously
		return nil, nil
	}

	// clear the ongoing tx from the state and unblock other writers waiting for it
	clearTxTask := st.NewTask("clear-confdb-tx", "Clears the ongoing confdb transaction from state")
	clearTxTask.Set("confdb-transaction", tx)

	// link all previous tasks to the task that carries the transaction
	for _, t := range ts.Tasks() {
		t.Set("tx-task", clearTxTask.ID())
	}

	linkTask(clearTxTask)
	ts.MarkEdge(clearTxTask, clearTxEdge)

	return ts, nil
}
