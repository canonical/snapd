// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package servicestate

import (
	"fmt"

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/servicestate/internal"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/systemd"
)

var (
	systemdVersionError error
)

func checkSystemdVersion() {
	systemdVersionError = systemd.EnsureAtLeast(230)
}

func init() {
	checkSystemdVersion()
}

func quotaGroupsAvailable(st *state.State) error {
	// check if the systemd version is too old
	if systemdVersionError != nil {
		return fmt.Errorf("cannot use quotas with incompatible systemd: %v", systemdVersionError)
	}

	tr := config.NewTransaction(st)
	enableQuotaGroups, err := features.Flag(tr, features.QuotaGroups)
	if err != nil && !config.IsNoOption(err) {
		return err
	}
	if !enableQuotaGroups {
		return fmt.Errorf("experimental feature disabled - test it by setting 'experimental.quota-groups' to true")
	}

	return nil
}

// CreateQuota attempts to create the specified quota group with the specified
// snaps in it.
// TODO: should this use something like QuotaGroupUpdate with fewer fields?
func CreateQuota(st *state.State, name string, parentName string, snaps []string, memoryLimit quantity.Size) (*state.TaskSet, error) {
	if err := quotaGroupsAvailable(st); err != nil {
		return nil, err
	}

	allGrps, err := AllQuotas(st)
	if err != nil {
		return nil, err
	}

	// make sure the group does not exist yet
	if _, ok := allGrps[name]; ok {
		return nil, fmt.Errorf("group %q already exists", name)
	}

	if memoryLimit == 0 {
		return nil, fmt.Errorf("cannot create quota group with no memory limit set")
	}

	// make sure the memory limit is at least 4K, that is the minimum size
	// to allow nesting, otherwise groups with less than 4K will trigger the
	// oom killer to be invoked when a new group is added as a sub-group to the
	// larger group.
	if memoryLimit <= 4*quantity.SizeKiB {
		return nil, fmt.Errorf("memory limit for group %q is too small: size must be larger than 4KB", name)
	}

	// make sure the specified snaps exist and aren't currently in another group
	if err := validateSnapForAddingToGroup(st, snaps, name, allGrps); err != nil {
		return nil, err
	}

	if err := CheckQuotaChangeConflictMany(st, []string{name}); err != nil {
		return nil, err
	}
	if err := snapstate.CheckChangeConflictMany(st, snaps, ""); err != nil {
		return nil, err
	}

	// create the task with the action in it
	qc := QuotaControlAction{
		Action:      "create",
		QuotaName:   name,
		MemoryLimit: memoryLimit,
		AddSnaps:    snaps,
		ParentName:  parentName,
	}

	ts := state.NewTaskSet()

	summary := fmt.Sprintf("Create quota group %q", name)
	task := st.NewTask("quota-control", summary)
	task.Set("quota-control-actions", []QuotaControlAction{qc})
	ts.AddTask(task)

	return ts, nil
}

// RemoveQuota deletes the specific quota group. Any snaps currently in the
// quota will no longer be in any quota group, even if the quota group being
// removed is a sub-group.
// TODO: currently this only supports removing leaf sub-group groups, it doesn't
// support removing parent quotas, but probably it makes sense to allow that too
func RemoveQuota(st *state.State, name string) (*state.TaskSet, error) {
	if snapdenv.Preseeding() {
		return nil, fmt.Errorf("removing quota groups not supported while preseeding")
	}

	allGrps, err := AllQuotas(st)
	if err != nil {
		return nil, err
	}

	// make sure the group exists
	grp, ok := allGrps[name]
	if !ok {
		return nil, fmt.Errorf("cannot remove non-existent quota group %q", name)
	}

	// XXX: remove this limitation eventually
	if len(grp.SubGroups) != 0 {
		return nil, fmt.Errorf("cannot remove quota group %q with sub-groups, remove the sub-groups first", name)
	}

	if err := CheckQuotaChangeConflictMany(st, []string{name}); err != nil {
		return nil, err
	}
	if err := snapstate.CheckChangeConflictMany(st, grp.Snaps, ""); err != nil {
		return nil, err
	}

	qc := QuotaControlAction{
		Action:    "remove",
		QuotaName: name,
	}

	ts := state.NewTaskSet()

	summary := fmt.Sprintf("Remove quota group %q", name)
	task := st.NewTask("quota-control", summary)
	task.Set("quota-control-actions", []QuotaControlAction{qc})
	ts.AddTask(task)

	return ts, nil
}

// QuotaGroupUpdate reflects all of the modifications that can be performed on
// a quota group in one operation.
type QuotaGroupUpdate struct {
	// AddSnaps is the set of snaps to add to the quota group. These are
	// instance names of snaps, and are appended to the existing snaps in
	// the quota group
	AddSnaps []string

	// NewMemoryLimit is the new memory limit to be used for the quota group. If
	// zero, then the quota group's memory limit is not changed.
	NewMemoryLimit quantity.Size
}

// UpdateQuota updates the quota as per the options.
// TODO: this should support more kinds of updates such as moving groups between
// parents, removing sub-groups from their parents, and removing snaps from
// the group.
func UpdateQuota(st *state.State, name string, updateOpts QuotaGroupUpdate) (*state.TaskSet, error) {
	if err := quotaGroupsAvailable(st); err != nil {
		return nil, err
	}

	allGrps, err := AllQuotas(st)
	if err != nil {
		return nil, err
	}

	grp, ok := allGrps[name]
	if !ok {
		return nil, fmt.Errorf("group %q does not exist", name)
	}

	// check that the memory limit is not being decreased
	if updateOpts.NewMemoryLimit != 0 {
		// we disallow decreasing the memory limit because it is difficult to do
		// so correctly with the current state of our code in
		// EnsureSnapServices, see comment in ensureSnapServicesForGroup for
		// full details
		if updateOpts.NewMemoryLimit < grp.MemoryLimit {
			return nil, fmt.Errorf("cannot decrease memory limit of existing quota-group, remove and re-create it to decrease the limit")
		}
	}

	// now ensure that all of the snaps mentioned in AddSnaps exist as snaps and
	// that they aren't already in an existing quota group
	if err := validateSnapForAddingToGroup(st, updateOpts.AddSnaps, name, allGrps); err != nil {
		return nil, err
	}

	if err := CheckQuotaChangeConflictMany(st, []string{name}); err != nil {
		return nil, err
	}
	if err := snapstate.CheckChangeConflictMany(st, updateOpts.AddSnaps, ""); err != nil {
		return nil, err
	}

	// create the action and the correspoding task set
	qc := QuotaControlAction{
		Action:      "update",
		QuotaName:   name,
		MemoryLimit: updateOpts.NewMemoryLimit,
		AddSnaps:    updateOpts.AddSnaps,
	}

	ts := state.NewTaskSet()

	summary := fmt.Sprintf("Update quota group %q", name)
	task := st.NewTask("quota-control", summary)
	task.Set("quota-control-actions", []QuotaControlAction{qc})
	ts.AddTask(task)

	return ts, nil
}

// EnsureSnapAbsentFromQuota ensures that the specified snap is not present
// in any quota group, usually in preparation for removing that snap from the
// system to keep the quota group itself consistent.
// This function is idempotent, since if it was interrupted after unlocking the
// state inside ensureSnapServicesForGroup it will not re-execute since the
// specified snap will not be present inside the group reference in the state.
func EnsureSnapAbsentFromQuota(st *state.State, snap string) error {
	allGrps, err := AllQuotas(st)
	if err != nil {
		return err
	}

	// try to find the snap in any group
	for _, grp := range allGrps {
		for idx, sn := range grp.Snaps {
			if sn == snap {
				// drop this snap from the list of Snaps by swapping it with the
				// last snap in the list, and then dropping the last snap from
				// the list
				grp.Snaps[idx] = grp.Snaps[len(grp.Snaps)-1]
				grp.Snaps = grp.Snaps[:len(grp.Snaps)-1]

				// update the quota group state
				allGrps, err = internal.PatchQuotas(st, grp)
				if err != nil {
					return err
				}

				// ensure service states are updated - note we have to add the
				// snap as an extra snap to ensure since it was removed from the
				// group and thus won't be considered just by looking at the
				// group pointer directly
				opts := &ensureSnapServicesForGroupOptions{
					allGrps:    allGrps,
					extraSnaps: []string{snap},
				}
				// TODO: we could pass timing and progress here from the task we
				// are executing as eventually
				return ensureSnapServicesStateForGroup(st, grp, opts)
			}
		}
	}

	// the snap wasn't in any group, nothing to do
	return nil
}

// QuotaChangeConflictError represents an error because of quota group conflicts between changes.
type QuotaChangeConflictError struct {
	Quota      string
	ChangeKind string
	// a Message is optional, otherwise one is composed from the other information
	Message string
}

func (e *QuotaChangeConflictError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.ChangeKind != "" {
		return fmt.Sprintf("quota group %q has %q change in progress", e.Quota, e.ChangeKind)
	}
	return fmt.Sprintf("quota group %q has changes in progress", e.Quota)
}

// CheckQuotaChangeConflictMany ensures that for the given quota groups no other
// changes that alters them (like create, update, remove) are in
// progress. If a conflict is detected an error is returned.
func CheckQuotaChangeConflictMany(st *state.State, quotaNames []string) error {
	quotaMap := make(map[string]bool, len(quotaNames))
	for _, k := range quotaNames {
		quotaMap[k] = true
	}

	for _, task := range st.Tasks() {
		chg := task.Change()
		if chg == nil || chg.IsReady() {
			continue
		}

		quotas, err := affectedQuotas(task)
		if err != nil {
			return err
		}

		for _, quota := range quotas {
			if quotaMap[quota] {
				return &QuotaChangeConflictError{Quota: quota, ChangeKind: chg.Kind()}
			}
		}
	}

	return nil
}

func affectedQuotas(task *state.Task) ([]string, error) {
	// so far only quota-control is relevant
	if task.Kind() != "quota-control" {
		return nil, nil
	}

	qcs := []QuotaControlAction{}
	if err := task.Get("quota-control-actions", &qcs); err != nil {
		return nil, fmt.Errorf("internal error: cannot get quota-control-actions: %v", err)
	}
	quotas := make([]string, 0, len(qcs))
	for _, qc := range qcs {
		// TODO: the affected quotas will expand beyond this
		// if we support reparenting or orphaning
		quotas = append(quotas, qc.QuotaName)
	}
	return quotas, nil
}
