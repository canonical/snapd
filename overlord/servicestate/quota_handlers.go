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
	"sort"
	"time"

	tomb "gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/servicestate/internal"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
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
	QuotaName string `json:"quota-name"`

	// Action is the action being taken on the quota group. It can be either
	// "create", "update", or "remove".
	Action string `json:"action"`

	// AddSnaps is the set of snaps to add to the quota group, valid for either
	// the "update" or the "create" actions.
	AddSnaps []string `json:"snaps"`

	// MemoryLimit is the memory limit for the quota group being controlled,
	// either the initial limit the group is created with for the "create"
	// action, or if non-zero for the "update" the memory limit, then the new
	// value to be set.
	MemoryLimit quantity.Size

	// ParentName is the name of the parent for the quota group if it is being
	// created. Eventually this could be used with the "update" action to
	// support moving quota groups from one parent to another, but that is
	// currently not supported.
	ParentName string
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

	updated, appsToRestartBySnap, err := quotaStateAlreadyUpdated(t)
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
			grp, allGrps, err = quotaCreate(st, qc, allGrps)
		case "remove":
			grp, allGrps, err = quotaRemove(st, qc, allGrps)
		case "update":
			grp, allGrps, err = quotaUpdate(st, qc, allGrps)
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
		appsToRestartBySnap, err = ensureSnapServicesForGroup(st, t, grp, opts)
		if err != nil {
			return err
		}

		// All persistent modifications to disk are made and the
		// modifications to state will be committed by the
		// unlocking in restartSnapServices. If snapd gets
		// restarted before the end of this task, all the
		// modifications would be redone, and those
		// non-idempotent parts of the task would fail.
		// For this reason we record together with the changes
		// in state the fact that the changes were made,
		// to avoid repeating them.
		// What remains for this task handler is just to
		// restart services which will happen regardless if we
		// get rebooted after unlocking the state - if we got
		// rebooted before unlocking the state, none of the
		// changes we made to state would be persisted and we
		// would run through everything above here again, but
		// the second time around EnsureSnapServices would end
		// up doing nothing since it is idempotent.  So in the
		// rare case that snapd gets restarted but is not a
		// reboot also record which services do need
		// restarting. There is a small chance that services
		// will be restarted again but is preferable to the
		// quota not applying to them.
		if err := rememberQuotaStateUpdated(t, appsToRestartBySnap); err != nil {
			return err
		}

	}

	if err := restartSnapServices(st, t, appsToRestartBySnap, perfTimings); err != nil {
		return err
	}
	t.SetStatus(state.DoneStatus)
	return nil
}

var osutilBootID = osutil.BootID

type quotaStateUpdated struct {
	BootID              string              `json:"boot-id"`
	AppsToRestartBySnap map[string][]string `json:"apps-to-restart,omitempty"`
}

func rememberQuotaStateUpdated(t *state.Task, appsToRestartBySnap map[*snap.Info][]*snap.AppInfo) error {
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
	})
	return nil
}

func quotaStateAlreadyUpdated(t *state.Task) (ok bool, appsToRestartBySnap map[*snap.Info][]*snap.AppInfo, err error) {
	var updated quotaStateUpdated
	if err := t.Get("state-updated", &updated); err != nil {
		if err == state.ErrNoState {
			return false, nil, nil
		}
		return false, nil, err
	}

	bootID, err := osutilBootID()
	if err != nil {
		return false, nil, err
	}
	if bootID != updated.BootID {
		// rebooted => nothing to restart
		return true, nil, nil
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
			return false, nil, err
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
	return true, appsToRestartBySnap, nil
}

func quotaCreate(st *state.State, action QuotaControlAction, allGrps map[string]*quota.Group) (*quota.Group, map[string]*quota.Group, error) {
	// make sure the group does not exist yet
	if _, ok := allGrps[action.QuotaName]; ok {
		return nil, nil, fmt.Errorf("group %q already exists", action.QuotaName)
	}

	// make sure that the parent group exists if we are creating a sub-group
	var parentGrp *quota.Group
	if action.ParentName != "" {
		var ok bool
		parentGrp, ok = allGrps[action.ParentName]
		if !ok {
			return nil, nil, fmt.Errorf("cannot create group under non-existent parent group %q", action.ParentName)
		}
	}

	// make sure the memory limit is not zero
	if action.MemoryLimit == 0 {
		return nil, nil, fmt.Errorf("internal error, MemoryLimit option is mandatory for create action")
	}

	// make sure the memory limit is at least 4K, that is the minimum size
	// to allow nesting, otherwise groups with less than 4K will trigger the
	// oom killer to be invoked when a new group is added as a sub-group to the
	// larger group.
	if action.MemoryLimit <= 4*quantity.SizeKiB {
		return nil, nil, fmt.Errorf("memory limit for group %q is too small: size must be larger than 4KB", action.QuotaName)
	}

	// make sure the specified snaps exist and aren't currently in another group
	if err := validateSnapForAddingToGroup(st, action.AddSnaps, action.QuotaName, allGrps); err != nil {
		return nil, nil, err
	}

	return internal.CreateQuotaInState(st, action.QuotaName, parentGrp, action.AddSnaps, action.MemoryLimit, allGrps)
}

func quotaRemove(st *state.State, action QuotaControlAction, allGrps map[string]*quota.Group) (*quota.Group, map[string]*quota.Group, error) {
	// make sure the group exists
	grp, ok := allGrps[action.QuotaName]
	if !ok {
		return nil, nil, fmt.Errorf("cannot remove non-existent quota group %q", action.QuotaName)
	}

	// make sure some of the options are not set, it's an internal error if
	// anything other than the name and action are set for a removal
	if action.ParentName != "" {
		return nil, nil, fmt.Errorf("internal error, ParentName option cannot be used with remove action")
	}

	if len(action.AddSnaps) != 0 {
		return nil, nil, fmt.Errorf("internal error, AddSnaps option cannot be used with remove action")
	}

	if action.MemoryLimit != 0 {
		return nil, nil, fmt.Errorf("internal error, MemoryLimit option cannot be used with remove action")
	}

	// XXX: remove this limitation eventually
	if len(grp.SubGroups) != 0 {
		return nil, nil, fmt.Errorf("cannot remove quota group with sub-groups, remove the sub-groups first")
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
		return nil, nil, fmt.Errorf("cannot remove quota group %q: %v", action.QuotaName, err)
	}

	// now set it in state
	st.Set("quotas", allGrps)

	return grp, allGrps, nil
}

func quotaUpdate(st *state.State, action QuotaControlAction, allGrps map[string]*quota.Group) (*quota.Group, map[string]*quota.Group, error) {
	// make sure the group exists
	grp, ok := allGrps[action.QuotaName]
	if !ok {
		return nil, nil, fmt.Errorf("group %q does not exist", action.QuotaName)
	}

	// check that ParentName is not set, since we don't currently support
	// re-parenting
	if action.ParentName != "" {
		return nil, nil, fmt.Errorf("group %q cannot be moved to a different parent (re-parenting not yet supported)", action.QuotaName)
	}

	modifiedGrps := []*quota.Group{grp}

	// now ensure that all of the snaps mentioned in AddSnaps exist as snaps and
	// that they aren't already in an existing quota group
	if err := validateSnapForAddingToGroup(st, action.AddSnaps, action.QuotaName, allGrps); err != nil {
		return nil, nil, err
	}

	// append the snaps list in the group
	grp.Snaps = append(grp.Snaps, action.AddSnaps...)

	// if the memory limit is not zero then change it too
	if action.MemoryLimit != 0 {
		// we disallow decreasing the memory limit because it is difficult to do
		// so correctly with the current state of our code in
		// EnsureSnapServices, see comment in ensureSnapServicesForGroup for
		// full details
		if action.MemoryLimit < grp.MemoryLimit {
			return nil, nil, fmt.Errorf("cannot decrease memory limit of existing quota-group, remove and re-create it to decrease the limit")
		}
		grp.MemoryLimit = action.MemoryLimit
	}

	// update the quota group state
	allGrps, err := internal.PatchQuotas(st, modifiedGrps...)
	if err != nil {
		return nil, nil, err
	}
	return grp, allGrps, nil
}

type ensureSnapServicesForGroupOptions struct {
	// allGrps is the updated set of quota groups
	allGrps map[string]*quota.Group

	// extraSnaps is the set of extra snaps to consider when ensuring services,
	// mainly only used when snaps are removed from quota groups
	extraSnaps []string
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

	allGrps := opts.allGrps

	var meterLocked progress.Meter
	if t == nil {
		meterLocked = progress.Null
	} else {
		meterLocked = snapstate.NewTaskProgressAdapterLocked(t)
	}

	// build the map of snap infos to options to provide to EnsureSnapServices
	snapSvcMap := map[*snap.Info]*wrappers.SnapServiceOptions{}
	for _, sn := range append(grp.Snaps, opts.extraSnaps...) {
		info, err := snapstate.CurrentInfo(st, sn)
		if err != nil {
			return nil, err
		}

		opts, err := SnapServiceOptions(st, sn, allGrps)
		if err != nil {
			return nil, err
		}

		snapSvcMap[info] = opts
	}

	// TODO: the following lines should maybe be EnsureOptionsForDevice() or
	// something since it is duplicated a few places
	ensureOpts := &wrappers.EnsureSnapServicesOptions{
		Preseeding: snapdenv.Preseeding(),
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
	appsToRestartBySnap = map[*snap.Info][]*snap.AppInfo{}

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
			// in this case, the only way that a service could have been changed
			// was if it was moved into or out of a slice, in both cases we need
			// to restart the service
			sn := app.Snap
			appsToRestartBySnap[sn] = append(appsToRestartBySnap[sn], app)

			// TODO: what about sockets and timers? activation units just start
			// the full unit, so as long as the full unit is restarted we should
			// be okay?
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
	if _, ok := allGrps[grp.Name]; !ok {
		// stop the quota group, then remove it
		if !ensureOpts.Preseeding {
			if err := systemSysd.Stop([]string{grp.SliceFileName()}, 5*time.Second); err != nil {
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

	return appsToRestartBySnap, nil
}

// restartSnapServices is used to restart the services for each snap
// that was newly moved into a quota group iterate in a sorted order
// over the snaps to restart their apps for easy tests.
func restartSnapServices(st *state.State, t *state.Task, appsToRestartBySnap map[*snap.Info][]*snap.AppInfo, perfTimings *timings.Timings) error {
	if len(appsToRestartBySnap) == 0 {
		return nil
	}

	var meterUnlocked progress.Meter
	if t == nil {
		meterUnlocked = progress.Null
	} else {
		meterUnlocked = snapstate.NewTaskProgressAdapterUnlocked(t)
	}

	if perfTimings == nil {
		perfTimings = &timings.Timings{}
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

		err = wrappers.RestartServices(startupOrdered, nil, nil, meterUnlocked, perfTimings)
		if err != nil {
			return err
		}
	}
	return nil
}

// ensureSnapServicesStateForGroup combines ensureSnapServicesForGroup and restartSnapServices
func ensureSnapServicesStateForGroup(st *state.State, grp *quota.Group, opts *ensureSnapServicesForGroupOptions) error {
	appsToRestartBySnap, err := ensureSnapServicesForGroup(st, nil, grp, opts)
	if err != nil {
		return err
	}
	return restartSnapServices(st, nil, appsToRestartBySnap, nil)
}

func validateSnapForAddingToGroup(st *state.State, snaps []string, group string, allGrps map[string]*quota.Group) error {
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

func quotaControlAffectedSnaps(t *state.Task) (snaps []string, err error) {
	qcs := []QuotaControlAction{}
	if err := t.Get("quota-control-actions", &qcs); err != nil {
		return nil, fmt.Errorf("internal error: cannot get quota-control-actions: %v", err)
	}

	// if state-updated was already set we can use it
	var updated quotaStateUpdated
	if err := t.Get("state-updated", &updated); err != state.ErrNoState {
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
