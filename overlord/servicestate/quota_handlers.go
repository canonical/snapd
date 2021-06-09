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

	meter := snapstate.NewTaskProgressAdapterUnlocked(t)

	qcs := []QuotaControlAction{}
	err := t.Get("quota-control-actions", &qcs)
	if err != nil {
		return fmt.Errorf("internal error: cannot get quota-control-action: %v", err)
	}

	// TODO: support more than one action
	switch {
	case len(qcs) > 1:
		return fmt.Errorf("multiple quota group actions not supported yet")
	case len(qcs) == 0:
		return fmt.Errorf("internal error: no quota group actions for quota-control task")
	}

	qc := qcs[0]

	allGrps, err := AllQuotas(st)
	if err != nil {
		return err
	}

	switch qc.Action {
	case "create":
		err = doCreateQuota(st, qc, allGrps, meter, perfTimings)
	case "remove":
		err = doRemoveQuota(st, qc, allGrps, meter, perfTimings)
	case "update":
		err = doUpdateQuota(st, qc, allGrps, meter, perfTimings)
	default:
		err = fmt.Errorf("unknown action %q requested", qc.Action)
	}

	if err != nil {
		return err
	}

	t.SetStatus(state.DoneStatus)

	return nil
}

func doCreateQuota(st *state.State, action QuotaControlAction, allGrps map[string]*quota.Group, meter progress.Meter, perfTimings *timings.Timings) error {
	// make sure the group does not exist yet
	if _, ok := allGrps[action.QuotaName]; ok {
		return fmt.Errorf("group %q already exists", action.QuotaName)
	}

	// make sure the memory limit is not zero
	// TODO: this needs to be updated to 4K when PR snapcore/snapd#10346 lands
	// and an equivalent check needs to be put back into CreateQuota() before
	// the tasks are created
	if action.MemoryLimit == 0 {
		return fmt.Errorf("internal error, MemoryLimit option is mandatory for create action")
	}

	// make sure the specified snaps exist and aren't currently in another group
	if err := validateSnapForAddingToGroup(st, action.AddSnaps, action.QuotaName, allGrps); err != nil {
		return err
	}

	grp, allGrps, err := createQuotaImpl(st, action, allGrps)
	if err != nil {
		return err
	}

	// ensure the snap services with the group
	opts := &ensureSnapServicesForGroupOptions{
		allGrps: allGrps,
	}
	return ensureSnapServicesForGroup(st, grp, opts, meter, perfTimings)
}

func createQuotaImpl(st *state.State, action QuotaControlAction, allGrps map[string]*quota.Group) (*quota.Group, map[string]*quota.Group, error) {
	// make sure that the parent group exists if we are creating a sub-group
	var grp *quota.Group
	var err error
	updatedGrps := []*quota.Group{}
	if action.ParentName != "" {
		parentGrp, ok := allGrps[action.ParentName]
		if !ok {
			return nil, nil, fmt.Errorf("cannot create group under non-existent parent group %q", action.ParentName)
		}

		grp, err = parentGrp.NewSubGroup(action.QuotaName, action.MemoryLimit)
		if err != nil {
			return nil, nil, err
		}

		updatedGrps = append(updatedGrps, parentGrp)
	} else {
		// make a new group
		grp, err = quota.NewGroup(action.QuotaName, action.MemoryLimit)
		if err != nil {
			return nil, nil, err
		}
	}
	updatedGrps = append(updatedGrps, grp)

	// put the snaps in the group
	grp.Snaps = action.AddSnaps
	// update the modified groups in state
	newAllGrps, err := patchQuotas(st, updatedGrps...)
	if err != nil {
		return nil, nil, err
	}

	return grp, newAllGrps, nil
}

func doRemoveQuota(st *state.State, action QuotaControlAction, allGrps map[string]*quota.Group, meter progress.Meter, perfTimings *timings.Timings) error {
	// make sure the group exists
	grp, ok := allGrps[action.QuotaName]
	if !ok {
		return fmt.Errorf("cannot remove non-existent quota group %q", action.QuotaName)
	}

	// make sure some of the options are not set, it's an internal error if
	// anything other than the name and action are set for a removal
	if action.ParentName != "" {
		return fmt.Errorf("internal error, ParentName option cannot be used with remove action")
	}

	if len(action.AddSnaps) != 0 {
		return fmt.Errorf("internal error, AddSnaps option cannot be used with remove action")
	}

	if action.MemoryLimit != 0 {
		return fmt.Errorf("internal error, MemoryLimit option cannot be used with remove action")
	}

	// XXX: remove this limitation eventually
	if len(grp.SubGroups) != 0 {
		return fmt.Errorf("cannot remove quota group with sub-groups, remove the sub-groups first")
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
		return fmt.Errorf("cannot remove quota %q: %v", action.QuotaName, err)
	}

	// now set it in state
	st.Set("quotas", allGrps)

	// update snap service units that may need to be re-written because they are
	// not in a slice anymore
	opts := &ensureSnapServicesForGroupOptions{
		allGrps: allGrps,
	}
	return ensureSnapServicesForGroup(st, grp, opts, meter, perfTimings)
}

func doUpdateQuota(st *state.State, action QuotaControlAction, allGrps map[string]*quota.Group, meter progress.Meter, perfTimings *timings.Timings) error {
	// make sure the group exists
	grp, ok := allGrps[action.QuotaName]
	if !ok {
		return fmt.Errorf("group %q does not exist", action.QuotaName)
	}

	// check that ParentName is not set, since we don't currently support
	// re-parenting
	if action.ParentName != "" {
		return fmt.Errorf("group %q cannot be moved to a different parent (re-parenting not yet supported)", action.QuotaName)
	}

	modifiedGrps := []*quota.Group{grp}

	// now ensure that all of the snaps mentioned in AddSnaps exist as snaps and
	// that they aren't already in an existing quota group
	if err := validateSnapForAddingToGroup(st, action.AddSnaps, action.QuotaName, allGrps); err != nil {
		return err
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
			return fmt.Errorf("cannot decrease memory limit of existing quota-group, remove and re-create it to decrease the limit")
		}
		grp.MemoryLimit = action.MemoryLimit
	}

	// update the quota group state
	allGrps, err := patchQuotas(st, modifiedGrps...)
	if err != nil {
		return err
	}

	// ensure service states are updated
	opts := &ensureSnapServicesForGroupOptions{
		allGrps: allGrps,
	}
	return ensureSnapServicesForGroup(st, grp, opts, meter, perfTimings)
}

type ensureSnapServicesForGroupOptions struct {
	// allGrps is the updated set of quota groups
	allGrps map[string]*quota.Group

	// extraSnaps is the set of extra snaps to consider when ensuring services,
	// mainly only used when snaps are removed from quota groups
	extraSnaps []string
}

func ensureSnapServicesForGroup(st *state.State, grp *quota.Group, opts *ensureSnapServicesForGroupOptions, meter progress.Meter, perfTimings *timings.Timings) error {
	if opts == nil {
		return fmt.Errorf("internal error: unset group information for ensuring")
	}

	allGrps := opts.allGrps

	if meter == nil {
		meter = progress.Null
	}

	if perfTimings == nil {
		perfTimings = &timings.Timings{}
	}

	// extraSnaps []string, meter progress.Meter, perfTimings *timings.Timings
	// build the map of snap infos to options to provide to EnsureSnapServices
	snapSvcMap := map[*snap.Info]*wrappers.SnapServiceOptions{}
	for _, sn := range append(grp.Snaps, opts.extraSnaps...) {
		info, err := snapstate.CurrentInfo(st, sn)
		if err != nil {
			return err
		}

		opts, err := SnapServiceOptions(st, sn, allGrps)
		if err != nil {
			return err
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
		return err
	}

	if !deviceCtx.Classic() && deviceCtx.Model().Base() != "" {
		ensureOpts.RequireMountedSnapdSnap = true
	}

	grpsToStart := []*quota.Group{}
	appsToRestartBySnap := map[*snap.Info][]*snap.AppInfo{}

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
	if err := wrappers.EnsureSnapServices(snapSvcMap, ensureOpts, collectModifiedUnits, meter); err != nil {
		return err
	}

	if ensureOpts.Preseeding {
		return nil
	}

	// TODO: should this logic move to wrappers in wrappers.RestartGroups()?
	systemSysd := systemd.New(systemd.SystemMode, meter)

	// now start the slices
	for _, grp := range grpsToStart {
		// TODO: what should these timeouts for stopping/restart slices be?
		if err := systemSysd.Start(grp.SliceFileName()); err != nil {
			return err
		}
	}

	// after starting all the grps that we modified from EnsureSnapServices,
	// we need to handle the case where a quota was removed, this will only
	// happen one at a time and can be identified by the grp provided to us
	// not existing in the state
	if _, ok := allGrps[grp.Name]; !ok {
		// stop the quota group, then remove it
		if !ensureOpts.Preseeding {
			if err := systemSysd.Stop(grp.SliceFileName(), 5*time.Second); err != nil {
				logger.Noticef("unable to stop systemd slice while removing group %q: %v", grp.Name, err)
			}
		}

		// TODO: this results in a second systemctl daemon-reload which is
		// undesirable, we should figure out how to do this operation with a
		// single daemon-reload
		st.Unlock()
		err := wrappers.RemoveQuotaGroup(grp, meter)
		st.Lock()
		if err != nil {
			return err
		}
	}

	// now restart the services for each snap that was newly moved into a quota
	// group

	// iterate in a sorted order over the snaps to restart their apps for easy
	// tests
	snaps := make([]*snap.Info, 0, len(appsToRestartBySnap))
	for sn := range appsToRestartBySnap {
		snaps = append(snaps, sn)
	}

	sort.Slice(snaps, func(i, j int) bool {
		return snaps[i].InstanceName() < snaps[j].InstanceName()
	})

	for _, sn := range snaps {
		st.Unlock()
		disabledSvcs, err := wrappers.QueryDisabledServices(sn, meter)
		st.Lock()
		if err != nil {
			return err
		}

		isDisabledSvc := make(map[string]bool, len(disabledSvcs))
		for _, svc := range disabledSvcs {
			isDisabledSvc[svc] = true
		}

		startupOrdered, err := snap.SortServices(appsToRestartBySnap[sn])
		if err != nil {
			return err
		}

		// drop disabled services from the startup ordering
		startupOrderedMinusDisabled := make([]*snap.AppInfo, 0, len(startupOrdered)-len(disabledSvcs))

		for _, svc := range startupOrdered {
			if !isDisabledSvc[svc.ServiceName()] {
				startupOrderedMinusDisabled = append(startupOrderedMinusDisabled, svc)
			}
		}

		st.Unlock()
		err = wrappers.RestartServices(startupOrderedMinusDisabled, nil, meter, perfTimings)
		st.Lock()

		if err != nil {
			return err
		}
	}
	return nil
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
