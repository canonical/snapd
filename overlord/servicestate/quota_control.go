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
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/servicestate/internal"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap/quota"
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
	snapstate.AddSnapToQuotaGroup = AddSnapToQuotaGroup
	EnsureQuotaUsability()
}

// EnsureQuotaUsability is exported for unit tests from other packages to re-run
// the init() time checks for quota usability which set the errors which
// quotaGroupsAvailable() checks for.
// It saves the previous state of the usability errors to be restored via the
// provided restore function.
func EnsureQuotaUsability() (restore func()) {
	oldSystemdErr := systemdVersionError
	checkSystemdVersion()

	return func() {
		systemdVersionError = oldSystemdErr
	}
}

var resourcesCheckFeatureRequirements = func(r *quota.Resources) error {
	return r.CheckFeatureRequirements()
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

// CreateQuotaOptions reflects all of options available when creating new quota
// groups.
type CreateQuotaOptions struct {
	// ParentName is the name of the parent quota group, the group should be
	// placed under.
	ParentName string

	// Snaps is the set of snaps to add to the quota group. These are
	// instance names of snaps.
	Snaps []string

	// ResourceLimits is the resource limits to be used for the quota group.
	ResourceLimits quota.Resources
}

// CreateQuota attempts to create the specified quota group with the specified
// snaps in it.
func CreateQuota(st *state.State, name string, createOpts CreateQuotaOptions) (*state.TaskSet, error) {
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

	// validate the resource limits for the group
	if err := createOpts.ResourceLimits.Validate(); err != nil {
		return nil, fmt.Errorf("cannot create quota group %q: %v", name, err)
	}
	// validate that the system has the features needed for this resource
	if err := resourcesCheckFeatureRequirements(&createOpts.ResourceLimits); err != nil {
		return nil, fmt.Errorf("cannot create quota group %q: %v", name, err)
	}

	// make sure the specified snaps exist and aren't currently in another group
	if err := validateSnapForAddingToGroup(st, createOpts.Snaps, name, allGrps); err != nil {
		return nil, err
	}

	if err := CheckQuotaChangeConflictMany(st, []string{name}); err != nil {
		return nil, err
	}
	if err := snapstate.CheckChangeConflictMany(st, createOpts.Snaps, ""); err != nil {
		return nil, err
	}

	// create the task with the action in it
	qc := QuotaControlAction{
		Action:         "create",
		QuotaName:      name,
		ResourceLimits: createOpts.ResourceLimits,
		AddSnaps:       createOpts.Snaps,
		ParentName:     createOpts.ParentName,
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

// UpdateQuotaOptions reflects all of the modifications that can be performed on
// a quota group in one operation.
type UpdateQuotaOptions struct {
	// AddSnaps is the set of snaps to add to the quota group. These are
	// instance names of snaps, and are appended to the existing snaps in
	// the quota group
	AddSnaps []string

	// NewResourceLimits is the new resource limits to be used for the quota group. A
	// limit is only changed if the corresponding limit is != nil.
	NewResourceLimits quota.Resources
}

// UpdateQuota updates the quota as per the options.
// TODO: this should support more kinds of updates such as moving groups between
// parents, removing sub-groups from their parents, and removing snaps from
// the group.
func UpdateQuota(st *state.State, name string, updateOpts UpdateQuotaOptions) (*state.TaskSet, error) {
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

	currentQuotas := grp.GetQuotaResources()
	if err := currentQuotas.ValidateChange(updateOpts.NewResourceLimits); err != nil {
		return nil, fmt.Errorf("cannot update group %q: %v", name, err)
	}
	// validate that the system has the features needed for this resource
	if err := resourcesCheckFeatureRequirements(&updateOpts.NewResourceLimits); err != nil {
		return nil, fmt.Errorf("cannot update group %q: %v", name, err)
	}

	// ensure that the group we are modifying does not contain a mix of snaps and sub-groups
	// as we no longer support this, and existing quota groups might have this
	if err := ensureGroupIsNotMixed(name, allGrps); err != nil {
		return nil, err
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
		Action:         "update",
		QuotaName:      name,
		ResourceLimits: updateOpts.NewResourceLimits,
		AddSnaps:       updateOpts.AddSnaps,
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

// AddSnapToQuotaGroup returns a task for adding a snap to a quota group. It wraps the task creation
// with proper conflict detection for the affected quota-group. Conflict detection for the snap being
// added must be done by the larger context, as this function is intended to be used in the context
// of a more complex change.
func AddSnapToQuotaGroup(st *state.State, snapName string, quotaGroup string) (*state.Task, error) {
	if err := CheckQuotaChangeConflictMany(st, []string{quotaGroup}); err != nil {
		return nil, err
	}

	// This could result in doing 'setup-profiles' twice, but
	// unfortunately we can't execute this code earlier as the snap
	// needs to appear as installed first.
	quotaControlTask := st.NewTask("quota-add-snap", fmt.Sprintf(i18n.G("Add snap %q to quota group %q"),
		snapName, quotaGroup))
	quotaControlTask.Set("quota-name", quotaGroup)
	return quotaControlTask, nil
}
