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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timings"
	"github.com/snapcore/snapd/wrappers"
)

// ServiceManager is responsible for starting and stopping snap services.
type ServiceManager struct {
	state *state.State

	ensuredSnapSvcs bool
}

// Manager returns a new service manager.
func Manager(st *state.State, runner *state.TaskRunner) *ServiceManager {
	delayedCrossMgrInit()
	m := &ServiceManager{
		state: st,
	}
	// TODO: undo handler
	runner.AddHandler("service-control", m.doServiceControl, nil)

	// TODO: undo handler
	runner.AddHandler("quota-control", m.doQuotaControl, nil)
	RegisterAffectedQuotasByKind("quota-control", affectedQuotasForQuotaControl)
	snapstate.RegisterAffectedSnapsByKind("quota-control", affectedSnapsForQuotaControl)

	// We can't directly refer to the servicestate internals from snapstate,
	// so this task encapsulate taking care of calling quotaUpdate
	// with the correct setup. This task also supports proper handling of
	// failure during install and correctly removes the snap again.
	runner.AddHandler("quota-add-snap", m.doQuotaAddSnap, m.undoQuotaAddSnap)
	RegisterAffectedQuotasByKind("quota-add-snap", affectedQuotasForQuotaAddSnap)
	// quota-add-snap uses snap-setup and because of this retrieving the snap
	// that is being added is implicitly already supported by snapstate/conflict.go

	return m
}

func MockEnsuredSnapServices(mgr *ServiceManager, ensured bool) (restore func()) {
	osutil.MustBeTestBinary("ensured snap services can only be mocked from tests")
	old := mgr.ensuredSnapSvcs
	mgr.ensuredSnapSvcs = ensured
	return func() {
		mgr.ensuredSnapSvcs = old
	}
}

func (m *ServiceManager) ensureSnapServicesUpdated() (err error) {
	m.state.Lock()
	defer m.state.Unlock()
	if m.ensuredSnapSvcs {
		return nil
	}

	// only run after we are seeded
	var seeded bool
	mylog.Check(m.state.Get("seeded", &seeded))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if !seeded {
		return nil
	}

	// we are seeded, now we need to find all snap services and re-generate
	// services as necessary

	// ensure all snap services are updated
	allStates := mylog.Check2(snapstate.All(m.state))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	// if we have no snaps we can exit early
	if len(allStates) == 0 {
		m.ensuredSnapSvcs = true
		return nil
	}

	allGrps := mylog.Check2(AllQuotas(m.state))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	snapsMap := map[*snap.Info]*wrappers.SnapServiceOptions{}

	for _, snapSt := range allStates {
		info := mylog.Check2(snapSt.CurrentInfo())

		// don't use EnsureSnapServices with the snapd snap
		if info.Type() == snap.TypeSnapd {
			continue
		}

		// use the cached copy of all quota groups
		snapSvcOpts := mylog.Check2(SnapServiceOptions(m.state, info, allGrps))

		snapsMap[info] = snapSvcOpts
	}

	// setup ensure options
	ensureOpts := &wrappers.EnsureSnapServicesOptions{
		Preseeding: snapdenv.Preseeding(),
	}

	// set RequireMountedSnapdSnap if we are on UC18+ only
	deviceCtx := mylog.Check2(snapstate.DeviceCtx(m.state, nil, nil))

	if !deviceCtx.Classic() && deviceCtx.Model().Base() != "" {
		ensureOpts.RequireMountedSnapdSnap = true
	}

	rewrittenServices := make(map[*snap.Info][]*snap.AppInfo)
	serviceKillingMightHaveOccurred := false
	observeChange := func(app *snap.AppInfo, _ *quota.Group, unitType, name string, old, new string) {
		if unitType == "service" {
			rewrittenServices[app.Snap] = append(rewrittenServices[app.Snap], app)
			if !serviceKillingMightHaveOccurred {
				if strings.Contains(old, "\nRequires=usr-lib-snapd.mount\n") {
					serviceKillingMightHaveOccurred = true
				}
			}
		}
	}
	mylog.Check(wrappers.EnsureSnapServices(snapsMap, ensureOpts, observeChange, progress.Null))

	// if nothing was modified or we are not on UC18+, we are done
	if len(rewrittenServices) == 0 || deviceCtx.Classic() || deviceCtx.Model().Base() == "" || !serviceKillingMightHaveOccurred {
		m.ensuredSnapSvcs = true
		return nil
	}
	mylog.Check(

		// otherwise, we know now that we have rewritten some snap services, we need
		// to handle the case of LP #1924805, and restart any services that were
		// accidentally killed when we refreshed snapd
		restartServicesKilledInSnapdSnapRefresh(rewrittenServices))
	// we failed to restart services that were killed by a snapd refresh, so
	// we need to immediately reboot in the hopes that this restores
	// services to a functioning state

	m.ensuredSnapSvcs = true

	return nil
}

// Ensure implements StateManager.Ensure.
func (m *ServiceManager) Ensure() error {
	mylog.Check(m.ensureSnapServicesUpdated())

	return nil
}

func delayedCrossMgrInit() {
	// hook into conflict checks mechanisms
	snapstate.RegisterAffectedSnapsByAttr("service-action", serviceControlAffectedSnaps)
	snapstate.SnapServiceOptions = SnapServiceOptions
	snapstate.EnsureSnapAbsentFromQuotaGroup = EnsureSnapAbsentFromQuota
}

func serviceControlAffectedSnaps(t *state.Task) ([]string, error) {
	var serviceAction ServiceAction
	mylog.Check(t.Get("service-action", &serviceAction))

	return []string{serviceAction.SnapName}, nil
}

func getBootTime() (time.Time, error) {
	cmd := exec.Command("uptime", "-s")
	cmd.Env = append(cmd.Env, "TZ=UTC")
	out, stderr := mylog.Check3(osutil.RunCmd(cmd))

	// parse the output from the command as a time
	t := mylog.Check2(time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(string(out)), time.UTC))

	return t, nil
}

func restartServicesKilledInSnapdSnapRefresh(modified map[*snap.Info][]*snap.AppInfo) error {
	// we decide on which services to restart by identifying (out of the set of
	// services we just modified) services that were stopped after
	// usr-lib-snapd.mount was written, but before usr-lib-snapd.mount was last
	// stopped - this is the time window in which snapd (accidentally) killed
	// all snap services using Requires=, see LP #1924805 for full details, so
	// we need to undo that by restarting those snaps

	st := mylog.Check2(os.Stat(filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit)))

	// always truncate all times to second precision, since that is the least
	// precise time we have of all the times we consider, due to using systemctl
	// for getting the InactiveEnterTimestamp for systemd units
	// TODO: we should switch back to using D-Bus for this, where we get much
	// more accurate times, down to the microsecond, which is the same precision
	// we have for the modification time here, and thus we can more easily avoid
	// the truncation issue, and we can ensure that we are minimizing the risk
	// of inadvertently starting services that just so happened to have been
	// stopped in the same second that we modified and usr-lib-snapd.mount.
	lowerTimeBound := st.ModTime().Truncate(time.Second)

	// if the time that the usr-lib-snapd.mount was modified is before the time
	// that this device was booted up, then we can skip this since we know we
	// that a refresh is not being performed
	bootTime := mylog.Check2(getBootTime())

	// don't fail if we can't get the boot time, if we don't get it the
	// below check will be always false (no time can be before zero time)

	if lowerTimeBound.Before(bootTime) {
		return nil
	}

	// Get the InactiveEnterTimestamp property for the usr-lib-snapd.mount unit,
	// this is the time that usr-lib-snapd.mount was transitioned from
	// deactivating to inactive and was done being started. This is the correct
	// upper bound for our window in which systemd killed snap services because
	// systemd orders the transactions when we stop usr-lib-snapd.mount thusly:
	//
	// 1. Find all units which have Requires=usr-lib-snapd.mount (all snap
	//    services which would have been refreshed during snapd 2.49.2)
	// 2. Stop all such services found in 1.
	// 3. Stop usr-lib-snapd.mount itself.
	//
	// Thus the time after all the services were killed is given by the time
	// that systemd transitioned usr-lib-snapd.mount to inactive, which is given
	// by InactiveEnterTimestamp.

	// TODO: pass a real interactor here?
	sysd := systemd.New(systemd.SystemMode, progress.Null)

	upperTimeBound := mylog.Check2(sysd.InactiveEnterTimestamp(wrappers.SnapdToolingMountUnit))

	if upperTimeBound.IsZero() {
		// this means that the usr-lib-snapd.mount unit never exited during this
		// boot, which means we are done in this ensure because the bug we care
		// about (LP #1924805) here was never triggered
		return nil
	}

	upperTimeBound = upperTimeBound.Truncate(time.Second)

	// if the lower time bound is ever in the future past the upperTimeBound,
	// then  just use the upperTimeBound as both limits, since we know that the
	// upper bound and the time for each service being stopped are of the same
	// precision
	if lowerTimeBound.After(upperTimeBound) {
		lowerTimeBound = upperTimeBound
	}

	candidateAppsToRestartBySnap := make(map[*snap.Info][]*snap.AppInfo)

	for sn, apps := range modified {
		for _, app := range apps {
			// get the InactiveEnterTimestamp for the service
			t := mylog.Check2(sysd.InactiveEnterTimestamp(app.ServiceName()))

			// always truncate to second precision
			t = t.Truncate(time.Second)

			// check if this unit entered the inactive state between the time
			// range, but be careful about time precision here, we want an
			// inclusive range i.e. [lower,upper] not (lower,upper) in case the
			// time that systemd saves these events as is imprecise or slow and
			// things get saved as having happened at the exact same time
			if !t.Before(lowerTimeBound) && !t.After(upperTimeBound) {
				candidateAppsToRestartBySnap[sn] = append(candidateAppsToRestartBySnap[sn], app)
			}
		}
	}

	// Second loop actually restarts the services per-snap by sorting them and
	// removing disabled services. Note that we could have disabled services
	// here because a service could have been running, but disabled when snapd
	// was refreshed, hence it got killed, but we don't want to restart it,
	// since it is disabled, and so that disabled running service is just SOL.
	for sn, apps := range candidateAppsToRestartBySnap {
		// TODO: should we try to start as many services as possible here before
		// giving up given the severity of the bug?
		disabledSvcs := mylog.Check2(wrappers.QueryDisabledServices(sn, progress.Null))

		startupOrdered := mylog.Check2(snap.SortServices(apps))

		// TODO: what to do about timings here?
		nullPerfTimings := &timings.Timings{}
		mylog.Check(wrappers.StartServices(startupOrdered, disabledSvcs, nil, progress.Null, nullPerfTimings))

	}

	return nil
}
