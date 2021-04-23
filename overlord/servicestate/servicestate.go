// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/overlord/cmdstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/wrappers"
)

type Instruction struct {
	Action string   `json:"action"`
	Names  []string `json:"names"`
	client.StartOptions
	client.StopOptions
	client.RestartOptions
}

type ServiceActionConflictError struct{ error }

// serviceControlTs creates "service-control" task for every snap derived from appInfos.
func serviceControlTs(st *state.State, appInfos []*snap.AppInfo, inst *Instruction) (*state.TaskSet, error) {
	servicesBySnap := make(map[string][]string, len(appInfos))
	sortedNames := make([]string, 0, len(appInfos))

	// group services by snap, we need to create one task for every affected snap
	for _, app := range appInfos {
		snapName := app.Snap.InstanceName()
		if _, ok := servicesBySnap[snapName]; !ok {
			sortedNames = append(sortedNames, snapName)
		}
		servicesBySnap[snapName] = append(servicesBySnap[snapName], app.Name)
	}
	sort.Strings(sortedNames)

	ts := state.NewTaskSet()
	var prev *state.Task
	for _, snapName := range sortedNames {
		var snapst snapstate.SnapState
		if err := snapstate.Get(st, snapName, &snapst); err != nil {
			if err == state.ErrNoState {
				return nil, fmt.Errorf("snap not found: %s", snapName)
			}
			return nil, err
		}

		cmd := &ServiceAction{SnapName: snapName}
		switch {
		case inst.Action == "start":
			cmd.Action = "start"
			if inst.Enable {
				cmd.ActionModifier = "enable"
			}
		case inst.Action == "stop":
			cmd.Action = "stop"
			if inst.Disable {
				cmd.ActionModifier = "disable"
			}
		case inst.Action == "restart":
			if inst.Reload {
				cmd.Action = "reload-or-restart"
			} else {
				cmd.Action = "restart"
			}
		default:
			return nil, fmt.Errorf("unknown action %q", inst.Action)
		}

		svcs := servicesBySnap[snapName]
		sort.Strings(svcs)
		cmd.Services = svcs

		summary := fmt.Sprintf("Run service command %q for services %q of snap %q", cmd.Action, svcs, cmd.SnapName)
		task := st.NewTask("service-control", summary)
		task.Set("service-action", cmd)
		if prev != nil {
			task.WaitFor(prev)
		}
		prev = task
		ts.AddTask(task)
	}
	return ts, nil
}

// Flags carries extra flags for Control
type Flags struct {
	// CreateExecCommandTasks tells Control method to create exec-command tasks
	// (alongside service-control tasks) for compatibility with old snapd.
	CreateExecCommandTasks bool
}

// Control creates a taskset for starting/stopping/restarting services via systemctl.
// The appInfos and inst define the services and the command to execute.
// Context is used to determine change conflicts - we will not conflict with
// tasks from same change as that of context's.
func Control(st *state.State, appInfos []*snap.AppInfo, inst *Instruction, flags *Flags, context *hookstate.Context) ([]*state.TaskSet, error) {
	var tts []*state.TaskSet
	var ctlcmds []string

	// create exec-command tasks for compatibility with old snapd
	if flags != nil && flags.CreateExecCommandTasks {
		switch {
		case inst.Action == "start":
			if inst.Enable {
				ctlcmds = []string{"enable"}
			}
			ctlcmds = append(ctlcmds, "start")
		case inst.Action == "stop":
			if inst.Disable {
				ctlcmds = []string{"disable"}
			}
			ctlcmds = append(ctlcmds, "stop")
		case inst.Action == "restart":
			if inst.Reload {
				ctlcmds = []string{"reload-or-restart"}
			} else {
				ctlcmds = []string{"restart"}
			}
		default:
			return nil, fmt.Errorf("unknown action %q", inst.Action)
		}
	}

	svcs := make([]string, 0, len(appInfos))
	snapNames := make([]string, 0, len(appInfos))
	lastName := ""
	names := make([]string, len(appInfos))
	for i, svc := range appInfos {
		svcs = append(svcs, svc.ServiceName())
		snapName := svc.Snap.InstanceName()
		names[i] = snapName + "." + svc.Name
		if snapName != lastName {
			snapNames = append(snapNames, snapName)
			lastName = snapName
		}
	}

	var ignoreChangeID string
	if context != nil {
		ignoreChangeID = context.ChangeID()
	}
	if err := snapstate.CheckChangeConflictMany(st, snapNames, ignoreChangeID); err != nil {
		return nil, &ServiceActionConflictError{err}
	}

	for _, cmd := range ctlcmds {
		argv := append([]string{"systemctl", cmd}, svcs...)
		desc := fmt.Sprintf("%s of %v", cmd, names)
		// Give the systemctl a maximum time of 61 for now.
		//
		// Longer term we need to refactor this code and
		// reuse the snapd/systemd and snapd/wrapper packages
		// to control the timeout in a single place.
		ts := cmdstate.ExecWithTimeout(st, desc, argv, 61*time.Second)

		// set ignore flag on the tasks, new snapd uses service-control tasks.
		ignore := true
		for _, t := range ts.Tasks() {
			t.Set("ignore", ignore)
		}
		tts = append(tts, ts)
	}

	// XXX: serviceControlTs could be merged with above logic at the cost of
	// slightly more complicated logic.
	ts, err := serviceControlTs(st, appInfos, inst)
	if err != nil {
		return nil, err
	}
	tts = append(tts, ts)

	// make a taskset wait for its predecessor
	for i := 1; i < len(tts); i++ {
		tts[i].WaitAll(tts[i-1])
	}

	return tts, nil
}

// StatusDecorator supports decorating client.AppInfos with service status.
type StatusDecorator struct {
	sysd           systemd.Systemd
	globalUserSysd systemd.Systemd
}

// NewStatusDecorator returns a new StatusDecorator.
func NewStatusDecorator(rep interface {
	Notify(string)
}) *StatusDecorator {
	return &StatusDecorator{
		sysd:           systemd.New(systemd.SystemMode, rep),
		globalUserSysd: systemd.New(systemd.GlobalUserMode, rep),
	}
}

// DecorateWithStatus adds service status information to the given
// client.AppInfo associated with the given snap.AppInfo.
// If the snap is inactive or the app is not service it does nothing.
func (sd *StatusDecorator) DecorateWithStatus(appInfo *client.AppInfo, snapApp *snap.AppInfo) error {
	if appInfo.Snap != snapApp.Snap.InstanceName() || appInfo.Name != snapApp.Name {
		return fmt.Errorf("internal error: misassociated app info %v and client app info %s.%s", snapApp, appInfo.Snap, appInfo.Name)
	}
	if !snapApp.Snap.IsActive() || !snapApp.IsService() {
		// nothing to do
		return nil
	}
	var sysd systemd.Systemd
	switch snapApp.DaemonScope {
	case snap.SystemDaemon:
		sysd = sd.sysd
	case snap.UserDaemon:
		sysd = sd.globalUserSysd
	default:
		return fmt.Errorf("internal error: unknown daemon-scope %q", snapApp.DaemonScope)
	}

	// collect all services for a single call to systemctl
	extra := len(snapApp.Sockets)
	if snapApp.Timer != nil {
		extra++
	}
	serviceNames := make([]string, 0, 1+extra)
	serviceNames = append(serviceNames, snapApp.ServiceName())

	sockSvcFileToName := make(map[string]string, len(snapApp.Sockets))
	for _, sock := range snapApp.Sockets {
		sockUnit := filepath.Base(sock.File())
		sockSvcFileToName[sockUnit] = sock.Name
		serviceNames = append(serviceNames, sockUnit)
	}
	if snapApp.Timer != nil {
		timerUnit := filepath.Base(snapApp.Timer.File())
		serviceNames = append(serviceNames, timerUnit)
	}

	// sysd.Status() makes sure that we get only the units we asked
	// for and raises an error otherwise
	sts, err := sysd.Status(serviceNames...)
	if err != nil {
		return fmt.Errorf("cannot get status of services of app %q: %v", appInfo.Name, err)
	}
	if len(sts) != len(serviceNames) {
		return fmt.Errorf("cannot get status of services of app %q: expected %d results, got %d", appInfo.Name, len(serviceNames), len(sts))
	}
	for _, st := range sts {
		switch filepath.Ext(st.UnitName) {
		case ".service":
			appInfo.Enabled = st.Enabled
			appInfo.Active = st.Active
		case ".timer":
			appInfo.Activators = append(appInfo.Activators, client.AppActivator{
				Name:    snapApp.Name,
				Enabled: st.Enabled,
				Active:  st.Active,
				Type:    "timer",
			})
		case ".socket":
			appInfo.Activators = append(appInfo.Activators, client.AppActivator{
				Name:    sockSvcFileToName[st.UnitName],
				Enabled: st.Enabled,
				Active:  st.Active,
				Type:    "socket",
			})
		}
	}
	// Decorate with D-Bus names that activate this service
	for _, slot := range snapApp.ActivatesOn {
		var busName string
		if err := slot.Attr("name", &busName); err != nil {
			return fmt.Errorf("cannot get D-Bus bus name of slot %q: %v", slot.Name, err)
		}
		// D-Bus activators do not correspond to systemd
		// units, so don't have the concept of being disabled
		// or deactivated.  As the service activation file is
		// created when the snap is installed, report as
		// enabled/active.
		appInfo.Activators = append(appInfo.Activators, client.AppActivator{
			Name:    busName,
			Enabled: true,
			Active:  true,
			Type:    "dbus",
		})
	}

	return nil
}

// SnapServiceOptions computes the options to configure services for
// the given snap. This function might not check for the existence
// of instanceName.
func SnapServiceOptions(st *state.State, instanceName string) (opts *wrappers.SnapServiceOptions, err error) {
	opts = &wrappers.SnapServiceOptions{}

	tr := config.NewTransaction(st)
	var vitalityStr string
	err = tr.GetMaybe("core", "resilience.vitality-hint", &vitalityStr)
	if err != nil {
		return nil, err
	}
	for i, s := range strings.Split(vitalityStr, ",") {
		if s == instanceName {
			opts.VitalityRank = i + 1
			break
		}
	}

	// also check for quota group for this instance name
	allGrps, err := AllQuotas(st)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}

	for _, grp := range allGrps {
		if strutil.ListContains(grp.Snaps, instanceName) {
			opts.QuotaGroup = grp
			break
		}
	}

	return opts, nil
}
