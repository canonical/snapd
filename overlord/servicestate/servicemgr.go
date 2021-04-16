// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snapdenv"
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
	return m
}

// inactiveEnterTimestampForUnit returns the time that a given unit entered the
// inactive state as defined by systemd docs. Specifically this time is the most
// recent time in which the unit transitioned from deactivating ("Stopping") to
// dead ("Stopped"). It may be the zero time if this has never happened during
// the current boot, since this property is only tracked during the current
// boot. It specifically does not return a time that is monotonic, so the time
// returned here may be subject to bugs if there was a discontinuous time jump
// on the system before or during the unit's transition to inactive.
// XXX: move to systemd package ?
func inactiveEnterTimestampForUnit(unit string) (time.Time, error) {
	// XXX: ignore stderr of systemctl command to avoid further infractions
	//      around LP #1885597
	out, err := exec.Command("systemctl", "show", "--property", "InactiveEnterTimestamp", unit).Output()
	if err != nil {
		return time.Time{}, osutil.OutputErr(out, err)
	}

	// the time returned by systemctl here will be formatted like so:
	// InactiveEnterTimestamp=Fri 2021-04-16 15:32:21 UTC
	// so we have to parse the time with a matching Go time format
	splitVal := strings.SplitN(string(out), "=", 2)
	if len(splitVal) != 2 {
		// then we don't have an equals sign in the output, so systemctl must be
		// broken
		return time.Time{}, fmt.Errorf("internal error: systemctl output (%s) is malformed", string(out))
	}

	if splitVal[1] == "" {

		return time.Time{}, nil
	}

	// finally parse the time string
	inactiveEnterTime, err := time.Parse("Mon 2006-01-02 15:04:05 MST", splitVal[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("internal error: systemctl time output (%s) is malformed", splitVal[1])
	}
	return inactiveEnterTime, nil
}

func (m *ServiceManager) ensureSnapServicesUpdated() (err error) {
	m.state.Lock()
	defer m.state.Unlock()
	if m.ensuredSnapSvcs {
		return nil
	}

	// only run after we are seeded
	var seeded bool
	err = m.state.Get("seeded", &seeded)
	if err != nil && err != state.ErrNoState {
		return err
	}
	if !seeded {
		return nil
	}

	// we are seeded, now we need to find all snap services and re-generate
	// services as necessary

	// ensure all snap services are updated
	allStates, err := snapstate.All(m.state)
	if err != nil && err != state.ErrNoState {
		return err
	}

	snapsMap := map[*snap.Info]*wrappers.SnapServiceOptions{}

	for _, snapSt := range allStates {
		info, err := snapSt.CurrentInfo()
		if err != nil {
			return err
		}

		// TODO: handle vitality rank here too from configcore
		snapsMap[info] = nil
	}

	// setup ensure options
	ensureOpts := &wrappers.EnsureSnapServicesOptions{
		Preseeding: snapdenv.Preseeding(),
	}

	// set RequireMountedSnapdSnap if we are on UC18+ only
	deviceCtx, err := devicestate.DeviceCtx(m.state, nil, nil)
	if err != nil {
		return err
	}

	if !deviceCtx.Classic() && deviceCtx.Model().Base() != "" {
		ensureOpts.RequireMountedSnapdSnap = true
	}

	// TODO: should we use an actual interacter here ?
	modified, err := wrappers.EnsureSnapServices(snapsMap, ensureOpts, progress.Null)
	if err != nil {
		return err
	}

	// if nothing was modified or we are not on UC18+, we are done
	if len(modified) == 0 || deviceCtx.Classic() || deviceCtx.Model().Base() == "" {
		m.ensuredSnapSvcs = true
		return nil
	}

	// otherwise we need to check for all the services that were modified if
	// they were recently killed when we modified usr-lib-snapd.mount as part of
	// a snapd snap refresh

	// as a last resort, if we fail in trying to start any services, we should
	// reboot
	defer func() {
		if err == nil {
			return
		}

		// TODO: reboot the system immediately here
	}()

	// we decide on which services to restart by identifying (out of the set of
	// services we just modified) services that were stopped after
	// usr-lib-snapd.mount was written, but before usr-lib-snapd.mount was last
	// stopped - this is the time window in which snapd (accidentally) killed
	// all snap services using Requires=, see LP #1924805 for full details, so
	// we need to undo that by restarting those snaps

	// TODO: use the var from core18.go here instead
	st, err := os.Stat("/etc/systemd/system/usr-lib-snapd.mount")
	if err != nil {
		return err
	}

	// TODO: we should check if usr-lib-snapd.mount was modified before the
	// current boot time, if it was then we can just skip this since we know
	// any service stops that happened were unrelated
	lowerTimeBound := st.ModTime()

	// Get the InactiveEnterTimestamp property for the usr-lib-snapd.mount unit,
	// this is the time that usr-lib-snapd.mount was transitioned from
	// deactivating to inactive and was done being started. This is the correct
	// upper bound for our window in which systemd killed snap services because
	// systemd orders the transactions when we stop usr-lib-snapd.mount thusly:
	//
	// 1. Find all units which have Requires=usr-lib-snapd.mount (all snap
	//    services  which would have been refreshed during snapd 2.49.2)
	// 2. Stop all such services found in 1.
	// 3. Stop usr-lib-snapd.mount itself.
	//
	// Thus the time after all the services were killed is given by the time
	// that systemd transitioned usr-lib-snapd.mount to inactive, which is given
	// by InactiveEnterTimestamp.

	upperTimeBound, err := inactiveEnterTimestampForUnit("usr-lib-snapd.mount")
	if err != nil {
		return err
	}

	if upperTimeBound.IsZero() {
		// this means that the usr-lib-snapd.mount unit never exited during this
		// boot, which means we are done in this ensure because the bug we care
		// about (LP #1924805) here was never triggered
		return nil
	}

	candidateAppsToRestartBySnap := make(map[*snap.Info][]*snap.AppInfo)

	for sn, apps := range modified {
		for _, app := range apps {
			// get the InactiveEnterTimestamp for the service
			t, err := inactiveEnterTimestampForUnit(app.ServiceName())
			if err != nil {
				return err
			}

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
		disabledSvcs, err := wrappers.QueryDisabledServices(sn, progress.Null)
		if err != nil {
			return err
		}

		startupOrdered, err := snap.SortServices(apps)
		if err != nil {
			return err
		}

		if err := wrappers.StartServices(startupOrdered, disabledSvcs, nil, progress.Null, nil); err != nil {
			return err
		}
	}

	m.ensuredSnapSvcs = true

	return nil
}

// Ensure implements StateManager.Ensure.
func (m *ServiceManager) Ensure() error {
	if err := m.ensureSnapServicesUpdated(); err != nil {
		return err
	}
	return nil
}

func delayedCrossMgrInit() {
	// hook into conflict checks mechanisms
	snapstate.AddAffectedSnapsByAttr("service-action", serviceControlAffectedSnaps)
}

func serviceControlAffectedSnaps(t *state.Task) ([]string, error) {
	var serviceAction ServiceAction
	if err := t.Get("service-action", &serviceAction); err != nil {
		return nil, fmt.Errorf("internal error: cannot obtain service action from task: %s", t.Summary())
	}
	return []string{serviceAction.SnapName}, nil
}
