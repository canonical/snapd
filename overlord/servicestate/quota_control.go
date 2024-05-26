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
	"strings"

	"github.com/ddkwork/golibrary/mylog"
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

var systemdVersionError error

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
	return nil
}

func isExperimentalQuotasAvailable(st *state.State, quotaName string) error {
	tr := config.NewTransaction(st)
	status := mylog.Check2(features.Flag(tr, features.QuotaGroups))
	if err != nil && !config.IsNoOption(err) {
		return err
	}
	if !status {
		return fmt.Errorf("%s quota options are experimental - test it by setting 'experimental.quota-groups' to true", quotaName)
	}
	return nil
}

func verifyQuotaRequirements(st *state.State, resourceLimits quota.Resources) error {
	mylog.Check(
		// validate quotas in general are available
		quotaGroupsAvailable(st))

	// Upon initialization verification for systemd version 230 has already been done,
	// but for some of these quota types we need even higher version.
	// see: EnsureQuotaUsability

	// MemoryLimit requires systemd 211, so it's covered by the initial check
	// CPUQuota requires systemd 213, so no further checks need to be done
	// TasksMax requires systemd 228, so no further checks need to be done

	// AllowedCPUs requires systemd 243, so we need to verify the version here
	if resourceLimits.CPUSet != nil {
		mylog.Check(systemd.EnsureAtLeast(243))
	}

	// Journal quotas require systemd 245, so we need to verify the version here as well
	if resourceLimits.Journal != nil {
		mylog.Check(systemd.EnsureAtLeast(245))
		mylog.Check(

			// To use journal quotas, the quota-group experimental features must be enabled.
			isExperimentalQuotasAvailable(st, "journal"))

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

	// Services is the set of services to add to the quota group. These are
	// formatted as my-snap.my-service.
	Services []string

	// ResourceLimits is the resource limits to be used for the quota group.
	ResourceLimits quota.Resources
}

// CreateQuota attempts to create the specified quota group with the specified
// snaps in it.
func CreateQuota(st *state.State, name string, createOpts CreateQuotaOptions) (*state.TaskSet, error) {
	mylog.Check(verifyQuotaRequirements(st, createOpts.ResourceLimits))

	allGrps := mylog.Check2(AllQuotas(st))

	// make sure the group does not exist yet
	if _, ok := allGrps[name]; ok {
		return nil, fmt.Errorf("group %q already exists", name)
	}

	// verify we are not trying to add a mixture of services and snaps
	if len(createOpts.Snaps) > 0 && len(createOpts.Services) > 0 {
		return nil, fmt.Errorf("cannot mix services and snaps in the same quota group")
	}
	mylog.Check(

		// validate the resource limits for the group
		createOpts.ResourceLimits.Validate())
	mylog.Check(

		// validate that the system has the features needed for this resource
		resourcesCheckFeatureRequirements(&createOpts.ResourceLimits))

	// make sure the specified snaps exist and aren't currently in another group
	parentGrp := allGrps[createOpts.ParentName]
	mylog.Check(validateSnapForAddingToGroup(st, createOpts.Snaps, name, parentGrp, allGrps))
	mylog.Check(

		// if services are provided, the make sure they refer to correct snaps and valid
		// services.
		validateSnapServicesForAddingToGroup(st, createOpts.Services, name, parentGrp, allGrps))
	mylog.Check(CheckQuotaChangeConflictMany(st, []string{name}))
	mylog.Check(snapstate.CheckChangeConflictMany(st, createOpts.Snaps, ""))

	// create the task with the action in it
	qc := QuotaControlAction{
		Action:         "create",
		QuotaName:      name,
		ResourceLimits: createOpts.ResourceLimits,
		AddSnaps:       createOpts.Snaps,
		AddServices:    createOpts.Services,
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

	allGrps := mylog.Check2(AllQuotas(st))

	// make sure the group exists
	grp, ok := allGrps[name]
	if !ok {
		return nil, fmt.Errorf("cannot remove non-existent quota group %q", name)
	}

	// XXX: remove this limitation eventually
	if len(grp.SubGroups) != 0 {
		return nil, fmt.Errorf("cannot remove quota group %q with sub-groups, remove the sub-groups first", name)
	}
	mylog.Check(CheckQuotaChangeConflictMany(st, []string{name}))
	mylog.Check(snapstate.CheckChangeConflictMany(st, grp.Snaps, ""))

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

	// AddServices is the set of snap services to add to the quota group. These are
	// names of the format <snap.service>, and are appended to the existing services in
	// the quota group
	AddServices []string

	// NewResourceLimits is the new resource limits to be used for the quota group. A
	// limit is only changed if the corresponding limit is != nil.
	NewResourceLimits quota.Resources
}

// UpdateQuota updates the quota as per the options.
// TODO: this should support more kinds of updates such as moving groups between
// parents, removing sub-groups from their parents, and removing snaps from
// the group.
func UpdateQuota(st *state.State, name string, updateOpts UpdateQuotaOptions) (*state.TaskSet, error) {
	mylog.Check(verifyQuotaRequirements(st, updateOpts.NewResourceLimits))

	allGrps := mylog.Check2(AllQuotas(st))

	grp, ok := allGrps[name]
	if !ok {
		return nil, fmt.Errorf("group %q does not exist", name)
	}

	currentQuotas := grp.GetQuotaResources()
	mylog.Check(validateQuotaLimitsChange(grp, currentQuotas, updateOpts.NewResourceLimits))
	mylog.Check(

		// validate that the system has the features needed for this resource
		resourcesCheckFeatureRequirements(&updateOpts.NewResourceLimits))
	mylog.Check(

		// verify we are not trying to add a mixture of services and snaps
		groupEnsureOnlySnapsOrServices(updateOpts.AddSnaps, updateOpts.AddServices, grp))

	// now ensure that all of the snaps mentioned in AddSnaps exist as snaps and
	// that they aren't already in an existing quota group
	parentGrp := allGrps[grp.ParentGroup]
	mylog.Check(validateSnapForAddingToGroup(st, updateOpts.AddSnaps, name, parentGrp, allGrps))
	mylog.Check(

		// if services are provided, the make sure they refer to correct snaps and valid
		// services.
		validateSnapServicesForAddingToGroup(st, updateOpts.AddServices, name, parentGrp, allGrps))
	mylog.Check(CheckQuotaChangeConflictMany(st, []string{name}))
	mylog.Check(snapstate.CheckChangeConflictMany(st, updateOpts.AddSnaps, ""))

	// create the action and the correspoding task set
	qc := QuotaControlAction{
		Action:         "update",
		QuotaName:      name,
		ResourceLimits: updateOpts.NewResourceLimits,
		AddSnaps:       updateOpts.AddSnaps,
		AddServices:    updateOpts.AddServices,
	}

	ts := state.NewTaskSet()

	summary := fmt.Sprintf("Update quota group %q", name)
	task := st.NewTask("quota-control", summary)
	task.Set("quota-control-actions", []QuotaControlAction{qc})
	ts.AddTask(task)

	return ts, nil
}

// remove a string item at index i from the string slice,
// it maintains the ordering of the original slice.
func remove(slice []string, i int) []string {
	return append(slice[:i], slice[i+1:]...)
}

// removeServicesFromSubGroups removes all service references of a snap in
// sub-groups related to the group of the snap, and returns the groups that were modified.
func removeServicesFromSubGroups(grp *quota.Group, snap string, allGrps map[string]*quota.Group) ([]*quota.Group, error) {
	// If a snap has services in sub-groups, the services must be in the first level of sub-groups only,
	// that's why the code here does not check for nested sub-groups.
	var modifiedGrps []*quota.Group
	for _, name := range grp.SubGroups {
		subgrp, ok := allGrps[name]
		if !ok {
			return nil, fmt.Errorf("non-existent sub-group %q", name)
		}

		for idx, svc := range subgrp.Services {
			// the Services has names of format my-snap.my-service, so check
			// if the service starts with the snap name
			if strings.HasPrefix(svc, snap+".") {
				// found a service that matches the snap we are removing,
				// so remove that too
				subgrp.Services = remove(subgrp.Services, idx)
				modifiedGrps = append(modifiedGrps, subgrp)
			}
		}
	}
	return modifiedGrps, nil
}

// EnsureSnapAbsentFromQuota ensures that the specified snap is not present
// in any quota group, usually in preparation for removing that snap from the
// system to keep the quota group itself consistent.
// This function is idempotent, since if it was interrupted after unlocking the
// state inside ensureSnapServicesForGroup it will not re-execute since the
// specified snap will not be present inside the group reference in the state.
func EnsureSnapAbsentFromQuota(st *state.State, snap string) error {
	allGrps := mylog.Check2(AllQuotas(st))

	// try to find the snap in any group
	for _, grp := range allGrps {
		for idx, sn := range grp.Snaps {
			if sn == snap {
				// remove any snap reference from sub-groups, this returns
				// a list of modified sub-groups which we can then pass along
				// to PatchQuotas
				subGrps := mylog.Check2(removeServicesFromSubGroups(grp, snap, allGrps))

				grp.Snaps = remove(grp.Snaps, idx)

				// update the quota group state
				allGrps = mylog.Check2(internal.PatchQuotas(st, append(subGrps, grp)...))

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
	mylog.Check(CheckQuotaChangeConflictMany(st, []string{quotaGroup}))

	// This could result in doing 'setup-profiles' twice, but
	// unfortunately we can't execute this code earlier as the snap
	// needs to appear as installed first.
	quotaControlTask := st.NewTask("quota-add-snap", fmt.Sprintf(i18n.G("Add snap %q to quota group %q"),
		snapName, quotaGroup))
	quotaControlTask.Set("quota-name", quotaGroup)
	return quotaControlTask, nil
}
