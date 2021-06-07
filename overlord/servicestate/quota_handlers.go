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
