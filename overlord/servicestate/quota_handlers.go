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
	"errors"
	"fmt"
	"sort"
	"strings"

	tomb "gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/servicestate/internal"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timings"
	"github.com/snapcore/snapd/wrappers"
)

// QuotaControlAction is the serialized representation of a quota group
// modification that lives in a task.
type QuotaControlAction struct {
	// QuotaName is the name of the quota group being controlled.
	QuotaName string `json:"quota-name,omitempty"`

	// Action is the action being taken on the quota group. It can be either
	// "create", "update", or "remove".
	Action string `json:"action,omitempty"`

	// AddSnaps is the set of snaps to add to the quota group, valid for either
	// the "update" or the "create" actions.
	AddSnaps []string `json:"snaps,omitempty"`

	// AddServices is the set of services to add to the quota group, valid for either
	// the "update" or the "create" actions.
	AddServices []string `json:"services,omitempty"`

	// ResourceLimits is the set of resource limits to set on the quota group.
	// Either the initial limit the group is created with for the "create"
	// action, or if non-zero for the "update" the memory limit, then the new
	// value to be set.
	ResourceLimits quota.Resources `json:"resource-limits,omitempty"`

	// ParentName is the name of the parent for the quota group if it is being
	// created. Eventually this could be used with the "update" action to
	// support moving quota groups from one parent to another, but that is
	// currently not supported.
	ParentName string `json:"parent-name,omitempty"`
}

func (m *ServiceManager) doQuotaControl(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	qcs := []QuotaControlAction{}
	err := t.Get("quota-control-actions", &qcs)
	if err != nil {
		return fmt.Errorf("internal error: cannot get quota-control-actions: %v", err)
	}

	// TODO: support more than one action
	switch {
	case len(qcs) > 1:
		return fmt.Errorf("multiple quota group actions not supported yet")
	case len(qcs) == 0:
		return fmt.Errorf("internal error: no quota group actions for quota-control task")
	}

	qc := qcs[0]

	updated, servicesAffected, refreshProfiles, err := quotaStateAlreadyUpdated(t)
	if err != nil {
		return err
	}

	if !updated {
		allGrps, err := AllQuotas(st)
		if err != nil {
			return err
		}

		var grp *quota.Group
		switch qc.Action {
		case "create":
			grp, allGrps, refreshProfiles, err = quotaCreate(st, qc, allGrps)
		case "remove":
			grp, allGrps, refreshProfiles, err = quotaRemove(st, qc, allGrps)
		case "update":
			grp, allGrps, refreshProfiles, err = quotaUpdate(st, qc, allGrps)
		default:
			return fmt.Errorf("unknown action %q requested", qc.Action)
		}

		if err != nil {
			return err
		}

		// ensure service and slices on disk and their states are updated
		opts := &ensureSnapServicesForGroupOptions{
			allGrps: allGrps,
		}
		servicesAffected, err = ensureSnapServicesForGroup(st, t, grp, opts)
		if err != nil {
			return err
		}

		// All persistent modifications to disk are made and the
		// modifications to state will be committed by the
		// unlocking at the end of this task. If snapd gets
		// restarted before the end of this task, all the
		// modifications would be redone, and those
		// non-idempotent parts of the task would fail.
		// For this reason we record together with the changes
		// in state the fact that the changes were made,
		// to avoid repeating them.
		// What remains for this task handler is just to
		// refresh security profiles and restart services which
		// will happen regardless if we get rebooted after
		// unlocking the state - if we got rebooted before unlocking
		// the state, none of the changes we made to state would
		// be persisted and we would run through everything above
		// here again, but the second time around EnsureSnapServices
		// would end up doing nothing since it is idempotent.
		// So in the rare case that snapd gets restarted but is not a
		// reboot also record which services do need
		// restarting. There is a small chance that services
		// will be restarted again but is preferable to the
		// quota not applying to them.
		if err := rememberQuotaStateUpdated(t, servicesAffected, refreshProfiles); err != nil {
			return err
		}
	}

	if len(servicesAffected) > 0 {
		ts := state.NewTaskSet()
		var prevTask *state.Task
		queueTask := func(task *state.Task) {
			if prevTask != nil {
				task.WaitFor(prevTask)
			}
			ts.AddTask(task)
			prevTask = task
		}

		if refreshProfiles {
			addRefreshProfileTasks(st, queueTask, servicesAffected)
		}
		addRestartServicesTasks(st, queueTask, qc.QuotaName, servicesAffected)
		snapstate.InjectTasks(t, ts)
	}

	t.SetStatus(state.DoneStatus)
	return nil
}

func addRefreshProfileTasks(st *state.State, queueTask func(task *state.Task), servicesAffected map[*snap.Info][]*snap.AppInfo) {
	for info := range servicesAffected {
		setupProfilesTask := st.NewTask("setup-profiles", fmt.Sprintf(i18n.G("Update snap %q (%s) security profiles"), info.SnapName(), info.Revision))
		setupProfilesTask.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: info.SnapName(),
				Revision: info.Revision,
			},
		})
		queueTask(setupProfilesTask)
	}
}

func addRestartServicesTasks(st *state.State, queueTask func(task *state.Task), grpName string, servicesAffected map[*snap.Info][]*snap.AppInfo) {
	getServiceNames := func(services []*snap.AppInfo) []string {
		var names []string
		for _, svc := range services {
			names = append(names, svc.Name)
		}
		sort.Strings(names)
		return names
	}

	sortedInfos := make([]*snap.Info, 0, len(servicesAffected))
	for info := range servicesAffected {
		sortedInfos = append(sortedInfos, info)
	}
	sort.Slice(sortedInfos, func(i, j int) bool {
		return sortedInfos[i].InstanceName() < sortedInfos[j].InstanceName()
	})

	for _, info := range sortedInfos {
		restartTask := st.NewTask("service-control", fmt.Sprintf("Restarting services for snap %q", info.InstanceName()))
		restartTask.Set("service-action", ServiceAction{
			Action:                  "restart",
			SnapName:                info.InstanceName(),
			Services:                getServiceNames(servicesAffected[info]),
			RestartEnabledNonActive: false,
		})
		queueTask(restartTask)
	}
}

func (m *ServiceManager) doQuotaAddSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	snapsup, err := snapstate.TaskSnapSetup(t)
	if err != nil {
		return err
	}

	var quotaName string
	if err := t.Get("quota-name", &quotaName); err != nil {
		return fmt.Errorf("internal error: cannot get quota-name: %v", err)
	}

	allGrps, err := AllQuotas(st)
	if err != nil {
		return err
	}

	qc := QuotaControlAction{
		Action:    "update",
		QuotaName: quotaName,
		AddSnaps:  []string{snapsup.InstanceName()},
	}
	grp, allGrps, _, err := quotaUpdate(st, qc, allGrps)
	if err != nil {
		return err
	}

	// ensure service and slices on disk and their states are updated
	opts := &ensureSnapServicesForGroupOptions{
		allGrps: allGrps,
	}

	servicesAffected, err := ensureSnapServicesForGroup(st, t, grp, opts)
	if err != nil {
		return err
	}

	// ensure that if any services are affected, they get their profiles
	// refreshed immediately as a part of the install change.
	if len(servicesAffected) > 0 && grp.JournalLimit != nil {
		ts := state.NewTaskSet()
		addRefreshProfileTasks(st, func(task *state.Task) {
			ts.AddTask(task)
		}, servicesAffected)
		snapstate.InjectTasks(t, ts)
	}
	return nil
}

func (m *ServiceManager) undoQuotaAddSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	snapsup, err := snapstate.TaskSnapSetup(t)
	if err != nil {
		return err
	}

	if err := EnsureSnapAbsentFromQuota(st, snapsup.InstanceName()); err != nil {
		return err
	}
	return nil
}

var osutilBootID = osutil.BootID

type quotaStateUpdated struct {
	BootID              string              `json:"boot-id"`
	AppsToRestartBySnap map[string][]string `json:"apps-to-restart,omitempty"`
	RefreshProfiles     bool                `json:"refresh-profiles,omitempty"`
}

func rememberQuotaStateUpdated(t *state.Task, appsToRestartBySnap map[*snap.Info][]*snap.AppInfo, refreshProfiles bool) error {
	bootID, err := osutilBootID()
	if err != nil {
		return err
	}
	appNamesBySnapName := make(map[string][]string, len(appsToRestartBySnap))
	for info, apps := range appsToRestartBySnap {
		appNames := make([]string, len(apps))
		for i, app := range apps {
			appNames[i] = app.Name
		}
		appNamesBySnapName[info.InstanceName()] = appNames
	}
	t.Set("state-updated", quotaStateUpdated{
		BootID:              bootID,
		AppsToRestartBySnap: appNamesBySnapName,
		RefreshProfiles:     refreshProfiles,
	})
	return nil
}

func quotaStateAlreadyUpdated(t *state.Task) (ok bool, appsToRestartBySnap map[*snap.Info][]*snap.AppInfo, refreshProfiles bool, err error) {
	var updated quotaStateUpdated
	if err := t.Get("state-updated", &updated); err != nil {
		if errors.Is(err, state.ErrNoState) {
			return false, nil, false, nil
		}
		return false, nil, false, err
	}

	bootID, err := osutilBootID()
	if err != nil {
		return false, nil, false, err
	}
	if bootID != updated.BootID {
		// rebooted => nothing to restart
		return true, nil, false, nil
	}

	appsToRestartBySnap = make(map[*snap.Info][]*snap.AppInfo, len(updated.AppsToRestartBySnap))
	st := t.State()
	// best effort, ignore missing snaps and apps
	for instanceName, appNames := range updated.AppsToRestartBySnap {
		info, err := snapstate.CurrentInfo(st, instanceName)
		if err != nil {
			if _, ok := err.(*snap.NotInstalledError); ok {
				t.Logf("after snapd restart, snap %q went missing", instanceName)
				continue
			}
			return false, nil, false, err
		}
		apps := make([]*snap.AppInfo, 0, len(appNames))
		for _, appName := range appNames {
			app := info.Apps[appName]
			if app == nil || !app.IsService() {
				continue
			}
			apps = append(apps, app)
		}
		appsToRestartBySnap[info] = apps
	}
	return true, appsToRestartBySnap, updated.RefreshProfiles, nil
}

func quotaCreate(st *state.State, action QuotaControlAction, allGrps map[string]*quota.Group) (*quota.Group, map[string]*quota.Group, bool, error) {
	// make sure the group does not exist yet
	if _, ok := allGrps[action.QuotaName]; ok {
		return nil, nil, false, fmt.Errorf("group %q already exists", action.QuotaName)
	}

	// make sure that the parent group exists if we are creating a sub-group
	var parentGrp *quota.Group
	if action.ParentName != "" {
		var ok bool
		parentGrp, ok = allGrps[action.ParentName]
		if !ok {
			return nil, nil, false, fmt.Errorf("cannot create group under non-existent parent group %q", action.ParentName)
		}
	}

	// make sure the resource limits for the group are valid
	if err := action.ResourceLimits.Validate(); err != nil {
		return nil, nil, false, fmt.Errorf("cannot create quota group %q: %v", action.QuotaName, err)
	}

	// verify we are not trying to add a mixture of services and snaps
	if len(action.AddSnaps) > 0 && len(action.AddServices) > 0 {
		return nil, nil, false, fmt.Errorf("cannot mix services and snaps in the same quota group")
	}

	// make sure the specified snaps exist and aren't currently in another group
	if err := validateSnapForAddingToGroup(st, action.AddSnaps, action.QuotaName, parentGrp, allGrps); err != nil {
		return nil, nil, false, err
	}

	// if services are provided, the make sure they refer to correct snaps and valid
	// services.
	if err := validateSnapServicesForAddingToGroup(st, action.AddServices, action.QuotaName, parentGrp, allGrps); err != nil {
		return nil, nil, false, err
	}

	grp, allGrps, err := internal.CreateQuotaInState(st, action.QuotaName, parentGrp, action.AddSnaps, action.AddServices, action.ResourceLimits, allGrps)
	if err != nil {
		return nil, nil, false, err
	}
	refreshProfiles := grp.JournalLimit != nil
	return grp, allGrps, refreshProfiles, nil
}

func quotaRemove(st *state.State, action QuotaControlAction, allGrps map[string]*quota.Group) (*quota.Group, map[string]*quota.Group, bool, error) {
	// make sure the group exists
	grp, ok := allGrps[action.QuotaName]
	if !ok {
		return nil, nil, false, fmt.Errorf("cannot remove non-existent quota group %q", action.QuotaName)
	}

	// make sure some of the options are not set, it's an internal error if
	// anything other than the name and action are set for a removal
	if action.ParentName != "" {
		return nil, nil, false, fmt.Errorf("internal error, ParentName option cannot be used with remove action")
	}

	if len(action.AddSnaps) != 0 {
		return nil, nil, false, fmt.Errorf("internal error, AddSnaps option cannot be used with remove action")
	}

	if !action.ResourceLimits.Unset() {
		return nil, nil, false, fmt.Errorf("internal error, quota limit options cannot be used with remove action")
	}

	// XXX: remove this limitation eventually
	if len(grp.SubGroups) != 0 {
		return nil, nil, false, fmt.Errorf("cannot remove quota group with sub-groups, remove the sub-groups first")
	}

	// if this group has a parent, we need to remove the linkage to this
	// sub-group from the parent first
	if grp.ParentGroup != "" {
		// the parent here must exist otherwise AllQuotas would have failed
		// because state would have been inconsistent
		parent := allGrps[grp.ParentGroup]

		// ensure that the parent group of this group no longer mentions this
		// group as a sub-group - we know that it must since AllQuotas validated
		// the state for us
		if len(parent.SubGroups) == 1 {
			// this group was an only child, so clear the whole list
			parent.SubGroups = nil
		} else {
			// we have to delete the child but keep the other children
			newSubgroups := make([]string, 0, len(parent.SubGroups)-1)
			for _, sub := range parent.SubGroups {
				if sub != action.QuotaName {
					newSubgroups = append(newSubgroups, sub)
				}
			}

			parent.SubGroups = newSubgroups
		}

		allGrps[grp.ParentGroup] = parent
	}

	// now delete the group from state - do this first for convenience to ensure
	// that we can just use SnapServiceOptions below and since it operates via
	// state, it will immediately reflect the deletion
	delete(allGrps, action.QuotaName)

	// make sure that the group set is consistent before saving it - we may need
	// to delete old links from this group's parent to the child
	if err := quota.ResolveCrossReferences(allGrps); err != nil {
		return nil, nil, false, fmt.Errorf("cannot remove quota group %q: %v", action.QuotaName, err)
	}

	// now set it in state
	st.Set("quotas", allGrps)

	refreshProfiles := grp.JournalLimit != nil
	return grp, allGrps, refreshProfiles, nil
}

func validateQuotaLimitsChange(grp *quota.Group, oldLimits, newLimits quota.Resources) error {
	// Do not allow setting a journal limit on any group which has
	// services in them. Due to mounts being generated per-snap we cannot
	// support per-service journal namespaces currently.
	if newLimits.Journal != nil && len(grp.Services) > 0 {
		return fmt.Errorf("journal quotas are not supported for individual services")
	}
	if err := oldLimits.ValidateChange(newLimits); err != nil {
		return err
	}
	return nil
}

func quotaUpdateGroupLimits(grp *quota.Group, limits quota.Resources) error {
	currentQuotas := grp.GetQuotaResources()
	if err := validateQuotaLimitsChange(grp, currentQuotas, limits); err != nil {
		return fmt.Errorf("cannot update limits for group %q: %v", grp.Name, err)
	}
	if err := currentQuotas.Change(limits); err != nil {
		return fmt.Errorf("cannot update limits for group %q: %v", grp.Name, err)
	}
	return grp.UpdateQuotaLimits(currentQuotas)
}

func quotaUpdate(st *state.State, action QuotaControlAction, allGrps map[string]*quota.Group) (*quota.Group, map[string]*quota.Group, bool, error) {
	// make sure the group exists
	grp, ok := allGrps[action.QuotaName]
	if !ok {
		return nil, nil, false, fmt.Errorf("group %q does not exist", action.QuotaName)
	}

	// check that ParentName is not set, since we don't currently support
	// re-parenting
	if action.ParentName != "" {
		return nil, nil, false, fmt.Errorf("group %q cannot be moved to a different parent (re-parenting not yet supported)", action.QuotaName)
	}

	// verify we are not trying to add a mixture of services and snaps
	if err := groupEnsureOnlySnapsOrServices(action.AddSnaps, action.AddServices, grp); err != nil {
		return nil, nil, false, err
	}

	// now ensure that all of the snaps mentioned in AddSnaps exist as snaps and
	// that they aren't already in an existing quota group
	parentGrp := allGrps[grp.ParentGroup]
	if err := validateSnapForAddingToGroup(st, action.AddSnaps, action.QuotaName, parentGrp, allGrps); err != nil {
		return nil, nil, false, err
	}

	// if services are provided, the make sure they refer to correct snaps and valid
	// services.
	if err := validateSnapServicesForAddingToGroup(st, action.AddServices, action.QuotaName, parentGrp, allGrps); err != nil {
		return nil, nil, false, err
	}

	// append snap list and service list in the group
	grp.Snaps = append(grp.Snaps, action.AddSnaps...)
	grp.Services = append(grp.Services, action.AddServices...)

	// store the current status of journal quota, if it changes we need
	// to refresh the profiles for the snaps in the groups
	hadJournalLimit := grp.JournalLimit != nil

	// update resource limits for the group
	if err := quotaUpdateGroupLimits(grp, action.ResourceLimits); err != nil {
		return nil, nil, false, err
	}

	// update the quota group state
	allGrps, err := internal.PatchQuotas(st, grp)
	if err != nil {
		return nil, nil, false, err
	}

	hasJournalLimit := (grp.JournalLimit != nil)
	refreshProfiles := hadJournalLimit != hasJournalLimit
	return grp, allGrps, refreshProfiles, nil
}

type ensureSnapServicesForGroupOptions struct {
	// allGrps is the updated set of quota groups
	allGrps map[string]*quota.Group

	// extraSnaps is the set of extra snaps to consider when ensuring services,
	// mainly only used when snaps are removed from quota groups
	extraSnaps []string
}

func snapServiceNames(info *snap.Info) []string {
	var appNames []string
	for _, app := range info.Services() {
		appNames = append(appNames, fmt.Sprintf("%s.%s", info.InstanceName(), app.Name))
	}
	return appNames
}

// affectedSnapServices returns a map of snaps and the services affected
// by performing changes to a quota group. For groups that contain just snaps, all
// services are added to the map, for groups that contain specific services, only those
// services listed in the group are affected, as snaps cannot be in same group as services.
func affectedSnapServices(st *state.State, grp *quota.Group, opts *ensureSnapServicesForGroupOptions) (map[*snap.Info]*wrappers.SnapServiceOptions, []string, error) {
	snapSvcMap := map[*snap.Info]*wrappers.SnapServiceOptions{}
	addSnapToMap := func(sn string) (*snap.Info, error) {
		info, err := snapstate.CurrentInfo(st, sn)
		if err != nil {
			return nil, err
		}
		if snapSvcMap[info] != nil {
			return info, nil
		}
		opts, err := SnapServiceOptions(st, info, opts.allGrps)
		if err != nil {
			return nil, err
		}
		snapSvcMap[info] = opts
		return info, nil
	}

	// handle extra snaps also passed here, so that they get included
	// in any case
	var affectedServices []string
	for _, sn := range append(grp.Snaps, opts.extraSnaps...) {
		info, err := addSnapToMap(sn)
		if err != nil {
			return nil, nil, err
		}
		affectedServices = append(affectedServices, snapServiceNames(info)...)
	}

	// if the group is a service group, then grp.Snaps is empty and we
	// need to get the affected snaps from the service names, which are
	// of format 'snap.service'
	for _, svc := range grp.Services {
		parts := strings.SplitN(svc, ".", 2)
		snapName := parts[0]
		if _, err := addSnapToMap(snapName); err != nil {
			return nil, nil, err
		}
		affectedServices = append(affectedServices, svc)
	}
	return snapSvcMap, affectedServices, nil
}

// ensureSnapServicesForGroup will handle updating changes to a given
// quota group on disk, including re-generating systemd slice files,
// as well as starting newly created quota groups and stopping and
// removing removed quota groups.
// It also computes and returns snap services that have moved into or
// out of quota groups and need restarting.
// This function is idempotent, in that it can be called multiple times with
// the same changes to be processed and nothing will be broken. This is mainly
// a consequence of calling wrappers.EnsureSnapServices().
// Currently, it only supports handling a single group change.
// It returns the snap services that needs restarts.
func ensureSnapServicesForGroup(st *state.State, t *state.Task, grp *quota.Group, opts *ensureSnapServicesForGroupOptions) (appsToRestartBySnap map[*snap.Info][]*snap.AppInfo, err error) {
	if opts == nil {
		return nil, fmt.Errorf("internal error: unset group information for ensuring")
	}

	var meterLocked progress.Meter
	if t == nil {
		meterLocked = progress.Null
	} else {
		meterLocked = snapstate.NewTaskProgressAdapterLocked(t)
	}

	// build the map of snap infos to options to provide to EnsureSnapServices
	snapSvcMap, affectedServices, err := affectedSnapServices(st, grp, opts)
	if err != nil {
		return nil, err
	}

	// TODO: the following lines should maybe be EnsureOptionsForDevice() or
	// something since it is duplicated a few places
	ensureOpts := &wrappers.EnsureSnapServicesOptions{
		Preseeding:      snapdenv.Preseeding(),
		IncludeServices: affectedServices,
	}

	// set RequireMountedSnapdSnap if we are on UC18+ only
	deviceCtx, err := snapstate.DeviceCtx(st, nil, nil)
	if err != nil {
		return nil, err
	}

	if !deviceCtx.Classic() && deviceCtx.Model().Base() != "" {
		ensureOpts.RequireMountedSnapdSnap = true
	}

	grpsToStart := []*quota.Group{}
	journalsToRestart := []string{}
	appsToRestartBySnap = map[*snap.Info][]*snap.AppInfo{}
	markAppForRestart := func(info *snap.Info, app *snap.AppInfo) {
		// make sure it is not already in the list
		for _, a := range appsToRestartBySnap[info] {
			if a.Name == app.Name {
				return
			}
		}
		appsToRestartBySnap[info] = append(appsToRestartBySnap[info], app)
	}

	collectModifiedUnits := func(app *snap.AppInfo, grp *quota.Group, unitType string, name, old, new string) {
		switch unitType {
		case "slice":
			// this slice was either modified or written for the first time

			// There are currently 3 possible cases that have different
			// operations required, but we ignore one of them, so there really
			// are just 2 cases we care about:
			// 1. If this slice was initially written, we just need to systemctl
			//    start it
			// 2. If the slice was modified to be given more resources (i.e. a
			//    higher memory limit), then we just need to do a daemon-reload
			//    which causes systemd to modify the cgroup which will always
			//    work since a cgroup can be atomically given more resources
			//    without issue since the cgroup can't be using more than the
			//    current limit.
			// 3. If the slice was modified to be given _less_ resources (i.e. a
			//    lower memory limit), then we need to stop the services before
			//    issuing the daemon-reload to systemd, then do the
			//    daemon-reload which will succeed in modifying the cgroup, then
			//    start the services we stopped back up again. This is because
			//    otherwise if the services are currently running and using more
			//    resources than they would be allowed after the modification is
			//    applied by systemd to the cgroup, the kernel responds with
			//    EBUSY, and it isn't clear if the modification is then properly
			//    in place or not.
			//
			// We will already have called daemon-reload at the end of
			// EnsureSnapServices directly, so handling case 3 is difficult, and
			// for now we disallow making this sort of change to a quota group,
			// that logic is handled at a higher level than this function.
			// Thus the only decision we really have to make is if the slice was
			// newly written or not, and if it was save it for later
			if old == "" {
				grpsToStart = append(grpsToStart, grp)
			}

		case "service":
			// When an app has its service file changed, we should restart the app
			// to take into account that the app might have been moved in/out of a
			// slice, which means limits may have changed.
			if app != nil {
				markAppForRestart(app.Snap, app)
			}

			// If a quota group has it's service file changed, then it's due to the
			// journal quota being set. We do not need to do any further changes here
			// as restart of apps in the journal quota is being handled by case 'journald'

			// TODO: what about sockets and timers? activation units just start
			// the full unit, so as long as the full unit is restarted we should
			// be okay?

		case "journald":
			// this happens when a journal quota is either added, modified or removed, and
			// in this case we need to restart all services in the quota group
			for info := range snapSvcMap {
				for _, app := range info.Apps {
					if app.IsService() {
						markAppForRestart(info, app)
					}
				}
			}

			// If the journal has changed (i.e old and new not being empty) then we
			// need to restart the journal for that namespace. For other cases
			// this is not necessary, as either the journal daemon will be started or
			// stopped as a part of removal.
			if old != "" && new != "" {
				serviceName := fmt.Sprintf("systemd-journald@%s", grp.JournalNamespaceName())
				journalsToRestart = append(journalsToRestart, serviceName)
			}
		}
	}
	if err := wrappers.EnsureSnapServices(snapSvcMap, ensureOpts, collectModifiedUnits, meterLocked); err != nil {
		return nil, err
	}

	if ensureOpts.Preseeding {
		// nothing to restart
		return nil, nil
	}

	// TODO: should this logic move to wrappers in wrappers.RemoveQuotaGroup()?
	systemSysd := systemd.New(systemd.SystemMode, meterLocked)

	// now start the slices
	for _, grp := range grpsToStart {
		// TODO: what should these timeouts for stopping/restart slices be?
		if err := systemSysd.Start([]string{grp.SliceFileName()}); err != nil {
			return nil, err
		}
	}

	// after starting all the grps that we modified from EnsureSnapServices,
	// we need to handle the case where a quota was removed, this will only
	// happen one at a time and can be identified by the grp provided to us
	// not existing in the state
	allGrps := opts.allGrps
	if _, ok := allGrps[grp.Name]; !ok {
		// stop the quota group, then remove it
		if !ensureOpts.Preseeding {
			if err := systemSysd.Stop([]string{grp.SliceFileName()}); err != nil {
				logger.Noticef("unable to stop systemd slice while removing group %q: %v", grp.Name, err)
			}
		}

		// TODO: this results in a second systemctl daemon-reload which is
		// undesirable, we should figure out how to do this operation with a
		// single daemon-reload
		err := wrappers.RemoveQuotaGroup(grp, meterLocked)
		if err != nil {
			return nil, err
		}
	}

	// lastly, lets restart journald services which were affected
	// by the changes to the quota group
	if len(journalsToRestart) > 0 {
		if err := systemSysd.Restart(journalsToRestart); err != nil {
			return nil, err
		}
	}

	return appsToRestartBySnap, nil
}

// restartSnapServices is used to restart the services for snaps that
// have been modified. Snaps and services are sorted before they are
// restarted to provide a consistent ordering of restarts to be testable.
func restartSnapServices(st *state.State, appsToRestartBySnap map[*snap.Info][]*snap.AppInfo) error {
	if len(appsToRestartBySnap) == 0 {
		return nil
	}

	st.Unlock()
	defer st.Lock()

	snaps := make([]*snap.Info, 0, len(appsToRestartBySnap))
	for sn := range appsToRestartBySnap {
		snaps = append(snaps, sn)
	}

	sort.Slice(snaps, func(i, j int) bool {
		return snaps[i].InstanceName() < snaps[j].InstanceName()
	})

	for _, sn := range snaps {
		startupOrdered, err := snap.SortServices(appsToRestartBySnap[sn])
		if err != nil {
			return err
		}

		err = wrappers.RestartServices(startupOrdered, nil,
			&wrappers.RestartServicesOptions{Reload: false},
			progress.Null, &timings.Timings{})
		if err != nil {
			return err
		}
	}
	return nil
}

// ensureSnapServicesStateForGroup combines ensureSnapServicesForGroup and restartSnapServices.
// This does not refresh security profiles for snaps in the quota group, which is required
// for modifications to a journal quota. Currently this function is used when removing a
// snap from the system which will cause an update(removal) of security profiles,
// and thus won't should not cause a conflict.
func ensureSnapServicesStateForGroup(st *state.State, grp *quota.Group, opts *ensureSnapServicesForGroupOptions) error {
	appsToRestartBySnap, err := ensureSnapServicesForGroup(st, nil, grp, opts)
	if err != nil {
		return err
	}
	return restartSnapServices(st, appsToRestartBySnap)
}

// groupEnsureOnlySnapsOrServices is a helper which returns an error if the snaps and/or
// services we are trying to add would result in a mixed group. A group is considered
// mixed if:
// 1. The group would end up containing both snaps and services
// 2. The group would end up containing both services and groups
func groupEnsureOnlySnapsOrServices(snapsToAdd, servicesToAdd []string, grp *quota.Group) error {
	if len(snapsToAdd) > 0 && len(servicesToAdd) > 0 {
		return fmt.Errorf("cannot mix services and snaps in the same quota group")
	}

	if (len(grp.Services) > 0 && len(snapsToAdd) > 0) || (len(grp.Snaps) > 0 && len(servicesToAdd) > 0) {
		return fmt.Errorf("cannot mix services and snaps in the same quota group")
	}
	return nil
}

// groupIsMixed returns whether a group contains both sub-groups and snaps (i.e is mixed)
func groupIsMixed(grp *quota.Group) bool {
	if grp == nil {
		return false
	}
	return len(grp.SubGroups) > 0 && len(grp.Snaps) > 0
}

func validateSnapForAddingToGroup(st *state.State, snaps []string, group string, parentGroup *quota.Group, allGrps map[string]*quota.Group) error {
	// With the new quotas we don't support groups that have a mixture of snaps and
	// subgroups, as this will cause issues with nesting. Groups/subgroups may now
	// only consist of either snaps or subgroups.
	if len(snaps) > 0 {
		if groupIsMixed(parentGroup) {
			return fmt.Errorf("cannot add snaps to group %q: only services are allowed in this sub-group", group)
		}
	}

	for _, name := range snaps {
		// validate that the snap exists
		_, err := snapstate.CurrentInfo(st, name)
		if err != nil {
			return fmt.Errorf("cannot use snap %q in group %q: %v", name, group, err)
		}

		// check that the snap is not already in a group
		for _, grp := range allGrps {
			if strutil.ListContains(grp.Snaps, name) {
				return fmt.Errorf("cannot add snap %q to group %q: snap already in quota group %q", name, group, grp.Name)
			}
		}
	}
	return nil
}

// splitSnapServiceName splits and verifies the snap service reference
// taken in by the frontend. It expects the format snap.service
func splitSnapServiceName(name string) (string, string, error) {
	parts := strings.SplitN(name, ".", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid snap service: %s", name)
	}
	if err := naming.ValidateSnap(parts[0]); err != nil {
		return "", "", err
	}
	return parts[0], parts[1], naming.ValidateApp(parts[1])
}

// ensureAppReferenceIsService returns whether the service referred to in the
// snap is actually a service.
func ensureAppReferenceIsService(st *state.State, snap, service string) error {
	snapInfo, err := snapstate.CurrentInfo(st, snap)
	if err != nil {
		return err
	}

	appInfo, ok := snapInfo.Apps[service]
	if !ok {
		return fmt.Errorf("invalid service %q", service)
	}
	if !appInfo.IsService() {
		return fmt.Errorf("%q is not a service", service)
	}
	return nil
}

// validateSnapServicesForAddingToGroup verifies that the given services can be added
// to the provided group. Services come in the format of snap.service, and we do
// the split and parsing in this function. We need to make sure that the snaps referred
// are related to the current group, and we make sure that the services are valid service names.
//
// We do not allow mixing services and sub-groups. A quota group of service must be
// the final leaf node in the quota group tree.
// State must locked prior to calling this function
func validateSnapServicesForAddingToGroup(st *state.State, services []string, group string, parentGroup *quota.Group, allGrps map[string]*quota.Group) error {
	if len(services) > 0 {
		grp, ok := allGrps[group]
		if ok && len(grp.SubGroups) != 0 {
			return fmt.Errorf("cannot mix services and sub groups in the group %q", group)
		}

		// We do not support services in a group with a journal limit. Due to how we generate mounts,
		// which is currently per-snap, we cannot support individual journal namespaces for services.
		// So services automatically inherit any journal namespace their parent (the group where the
		// actual snap is) has set.
		if ok && grp.JournalLimit != nil {
			return fmt.Errorf("cannot put services into group %q: journal quotas are not supported for individual services", group)
		}
	}

	var svcQuotaMap map[string]*quota.Group
	if parentGroup != nil {
		svcQuotaMap = parentGroup.ServiceMap()
	}

	for _, name := range services {
		snap, service, err := splitSnapServiceName(name)
		if err != nil {
			return err
		}
		if err = ensureAppReferenceIsService(st, snap, service); err != nil {
			return fmt.Errorf("cannot add snap service %q: %v", group, err)
		}
		if parentGroup == nil || !strutil.ListContains(parentGroup.Snaps, snap) {
			return fmt.Errorf("cannot add snap service %q: the snap %q must be in a direct parent group of group %q", service, snap, group)
		}
		if serviceGrp := svcQuotaMap[name]; serviceGrp != nil {
			return fmt.Errorf("cannot add snap service %q: the service is already in group %q", service, serviceGrp.Name)
		}
	}
	return nil
}

func affectedQuotasForQuotaControl(t *state.Task) (quotas []string, err error) {
	qcs := []QuotaControlAction{}
	if err := t.Get("quota-control-actions", &qcs); err != nil {
		return nil, fmt.Errorf("internal error: cannot get quota-control-actions: %v", err)
	}
	quotas = make([]string, 0, len(qcs))
	for _, qc := range qcs {
		// TODO: the affected quotas will expand beyond this
		// if we support reparenting or orphaning
		quotas = append(quotas, qc.QuotaName)
	}
	return quotas, nil
}

func affectedSnapsForQuotaControl(t *state.Task) (snaps []string, err error) {
	qcs := []QuotaControlAction{}
	if err := t.Get("quota-control-actions", &qcs); err != nil {
		return nil, fmt.Errorf("internal error: cannot get quota-control-actions: %v", err)
	}

	// if state-updated was already set we can use it
	var updated quotaStateUpdated
	if err := t.Get("state-updated", &updated); !errors.Is(err, state.ErrNoState) {
		if err != nil {
			return nil, err
		}
		// TODO: consider boot-id as well?
		for snapName := range updated.AppsToRestartBySnap {
			snaps = append(snaps, snapName)
		}
		// all set
		return snaps, nil
	}

	st := t.State()
	for _, qc := range qcs {
		switch qc.Action {
		case "remove":
			// the snaps affected by a remove are implicitly
			// the ones currently in the quota group
			grp, err := GetQuota(st, qc.QuotaName)
			if err != nil && err != ErrQuotaNotFound {
				return nil, err
			}
			if err == nil {
				snaps = append(snaps, grp.Snaps...)
			}
		default:
			// create and update affects only the snaps
			// explicitly mentioned
			// TODO: this will cease to be true
			// if we support reparenting or orphaning
			// of quota groups
			snaps = append(snaps, qc.AddSnaps...)
		}
	}
	return snaps, nil
}

func affectedQuotasForQuotaAddSnap(t *state.Task) (quotas []string, err error) {
	var quotaName string
	if err := t.Get("quota-name", &quotaName); err != nil {
		return nil, fmt.Errorf("internal error: cannot get quota-name: %v", err)
	}
	return []string{quotaName}, nil
}
