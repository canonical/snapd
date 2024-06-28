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
	"context"
	"errors"
	"fmt"
	"io"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/overlord/cmdstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
	usc "github.com/snapcore/snapd/usersession/client"
	"github.com/snapcore/snapd/wrappers"
)

type Instruction struct {
	Action string               `json:"action"`
	Names  []string             `json:"names"`
	Scope  client.ScopeSelector `json:"scope"`
	Users  client.UserSelector  `json:"users"`
	client.StartOptions
	client.StopOptions
	client.RestartOptions
}

func (i *Instruction) ServiceScope() wrappers.ServiceScope {
	var hasUser, hasSystem bool
	for _, opt := range i.Scope {
		switch opt {
		case "user":
			hasUser = true
		case "system":
			hasSystem = true
		}
	}
	switch {
	case hasSystem && !hasUser:
		return wrappers.ServiceScopeSystem
	case hasUser && !hasSystem:
		return wrappers.ServiceScopeUser
	default:
		return wrappers.ServiceScopeAll
	}
}

func (i *Instruction) hasUserService(apps []*snap.AppInfo) bool {
	for _, app := range apps {
		if app.IsService() && app.DaemonScope == snap.UserDaemon {
			return true
		}
	}
	return false
}

// EnsureDefaultScopeForUser sets up default scopes based on the type of user
// if none were provided.
// Make sure to call Instruction.Validate() before calling this.
func (i *Instruction) EnsureDefaultScopeForUser(u *user.User) {
	// Set default scopes if not provided
	if len(i.Scope) == 0 {
		// If root is making this request, implied scopes are all
		if u.Uid == "0" {
			i.Scope = client.ScopeSelector{"system", "user"}
		} else {
			// Otherwise imply the service scope only
			i.Scope = client.ScopeSelector{"system"}
		}
	}
}

func (i *Instruction) validateScope(u *user.User, apps []*snap.AppInfo) error {
	if len(i.Scope) == 0 {
		// Providing no scope is only an issue for non-root users if the
		// target is user-daemons.
		if u.Uid != "0" && i.hasUserService(apps) {
			return fmt.Errorf("non-root users must specify service scope when targeting user services")
		}
	}
	return nil
}

func (i *Instruction) validateUsers(u *user.User, apps []*snap.AppInfo) error {
	// Perform some additional user checks
	if i.Users.Selector == client.UserSelectionList && len(i.Users.Names) == 0 {
		// It is an error for a non-root to not specify any users if we are targeting
		// user daemons
		if u.Uid != "0" && i.hasUserService(apps) {
			return fmt.Errorf("non-root users must specify users when targeting user services")
		}
	}
	return nil
}

// Validate validates the some of the data members in the Instruction. Currently
// this validates user-list and scope. This should only be called once when the structure
// is initialized/deserialized.
func (i *Instruction) Validate(u *user.User, apps []*snap.AppInfo) error {
	if err := i.validateScope(u, apps); err != nil {
		return err
	}
	if err := i.validateUsers(u, apps); err != nil {
		return err
	}
	return nil
}

type ServiceActionConflictError struct{ error }

func computeExplicitServices(appInfos []*snap.AppInfo, names []string) map[string][]string {
	explicitServices := make(map[string][]string, len(appInfos))
	// requested maps "snapname.appname" to app name.
	requested := make(map[string]bool, len(names))
	for _, name := range names {
		// Name might also be a snap name (or other strings the user wrote on
		// the command line), but the loop below ensures that this function
		// considers only application names.
		requested[name] = true
	}

	for _, app := range appInfos {
		snapName := app.Snap.InstanceName()
		// app.String() gives "snapname.appname"
		if requested[app.String()] {
			explicitServices[snapName] = append(explicitServices[snapName], app.Name)
		}
	}

	return explicitServices
}

// serviceControlTs creates "service-control" task for every snap derived from appInfos.
func serviceControlTs(st *state.State, appInfos []*snap.AppInfo, inst *Instruction, cu *user.User) (*state.TaskSet, error) {
	servicesBySnap := make(map[string][]string, len(appInfos))
	explicitServices := computeExplicitServices(appInfos, inst.Names)
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
			if errors.Is(err, state.ErrNoState) {
				return nil, fmt.Errorf("snap not found: %s", snapName)
			}
			return nil, err
		}

		users, err := inst.Users.UserList(cu)
		if err != nil {
			return nil, err
		}

		cmd := &ServiceAction{
			SnapName: snapName,
			ScopeOptions: wrappers.ScopeOptions{
				Scope: inst.ServiceScope(),
				Users: users,
			},
		}
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
			cmd.RestartEnabledNonActive = true
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
		explicitSvcs := explicitServices[snapName]
		sort.Strings(explicitSvcs)
		cmd.ExplicitServices = explicitSvcs

		// When composing the task summary, prefer using the explicit
		// services, if that's not empty
		var summary string
		if len(explicitSvcs) > 0 {
			svcs = explicitSvcs
		} else if inst.Action == "restart" {
			// Use a generic message, since we cannot know the exact list of
			// services affected
			summary = fmt.Sprintf("Run service command %q for running services of snap %q", cmd.Action, cmd.SnapName)
		}

		if summary == "" {
			summary = fmt.Sprintf("Run service command %q for services %q of snap %q", cmd.Action, svcs, cmd.SnapName)
		}
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
func Control(st *state.State, appInfos []*snap.AppInfo, inst *Instruction, cu *user.User, flags *Flags, context *hookstate.Context) ([]*state.TaskSet, error) {
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
	ts, err := serviceControlTs(st, appInfos, inst, cu)
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
	context        context.Context
	uid            string
}

// NewStatusDecorator returns a new StatusDecorator.
//
// Using NewStatusDecorator will only allow for global status of user-services
// as StatusDecorator is designed to contain a single set of results for a single
// user.
func NewStatusDecorator(rep interface {
	Notify(string)
}) clientutil.StatusDecorator {
	return &StatusDecorator{
		sysd:           systemd.New(systemd.SystemMode, rep),
		globalUserSysd: systemd.New(systemd.GlobalUserMode, rep),
	}
}

// NewStatusDecoratorForUid returns a new StatusDecorator, but configured
// for a specific uid. This allows the StatusDecorator to get statuses for
// user-services for a specific user.
func NewStatusDecoratorForUid(rep interface {
	Notify(string)
}, context context.Context, uid string) clientutil.StatusDecorator {
	return &StatusDecorator{
		sysd:           systemd.New(systemd.SystemMode, rep),
		globalUserSysd: systemd.New(systemd.GlobalUserMode, rep),
		context:        context,
		uid:            uid,
	}
}

func (sd *StatusDecorator) hasEnabledActivator(appInfo *client.AppInfo) bool {
	// Just one activator should be enabled in order for the service to be able
	// to become enabled. For slot activated services this is always true as we
	// have no way currently of disabling this.
	for _, act := range appInfo.Activators {
		if act.Enabled {
			return true
		}
	}
	return false
}

// queryUserServiceStatus returns a list of service-statuses for the configured users.
func (sd *StatusDecorator) queryUserServiceStatus(units []string) ([]*systemd.UnitStatus, error) {
	// Avoid any expensive call if there are no user daemons
	if len(units) == 0 {
		return nil, nil
	}

	uid, err := strconv.Atoi(sd.uid)
	if err != nil {
		return nil, err
	}

	cli := usc.NewForUids(uid)
	sts, failures, err := cli.ServiceStatus(sd.context, units)
	if err != nil {
		return nil, err
	}

	// Return the first service failure, if any failures were reported
	if len(failures[uid]) > 0 {
		return nil, fmt.Errorf("cannot retrieve service %q status: %v",
			failures[uid][0].Service, failures[uid][0].Error)
	}

	// Convert the received unit statuses to systemd-unit statuses
	var sysdStatuses []*systemd.UnitStatus
	for _, sts := range sts[uid] {
		sysdStatuses = append(sysdStatuses, sts.SystemdUnitStatus())
	}
	return sysdStatuses, nil
}

func (sd *StatusDecorator) queryServiceStatus(scope snap.DaemonScope, units []string) ([]*systemd.UnitStatus, error) {
	var sts []*systemd.UnitStatus
	var err error
	switch scope {
	case snap.SystemDaemon:
		// sysd.Status() makes sure that we get only the units we asked
		// for and raises an error otherwise.
		sts, err = sd.sysd.Status(units)
	case snap.UserDaemon:
		// Support the previous behavior of retrieving the global enablement
		// status of user services if no uid is configured for this status
		// decorator.
		if sd.uid != "" {
			sts, err = sd.queryUserServiceStatus(units)
		} else {
			sts, err = sd.globalUserSysd.Status(units)
		}
	default:
		return nil, fmt.Errorf("internal error: unknown daemon-scope %q", scope)
	}
	if err != nil {
		return nil, err
	}

	// ensure we get the correct unit count, otherwise report an error.
	if len(sts) != len(units) {
		return nil, fmt.Errorf("expected %d results, got %d", len(units), len(sts))
	}
	return sts, nil
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

	sts, err := sd.queryServiceStatus(snapApp.DaemonScope, serviceNames)
	if err != nil {
		return fmt.Errorf("cannot get status of services of app %q: %v", appInfo.Name, err)
	}

	for _, st := range sts {
		switch filepath.Ext(st.Name) {
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
				Name:    sockSvcFileToName[st.Name],
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
	// For activated services, the service tends to be reported as Static, meaning
	// it can't be disabled. However, if all the activators are disabled, then we change
	// this to appear disabled.
	if len(appInfo.Activators) > 0 {
		appInfo.Enabled = sd.hasEnabledActivator(appInfo)
	}
	return nil
}

// SnapServiceOptions computes the options to configure services for
// the given snap. It also takes as argument a map of all quota groups as an
// optimization, the map if non-nil is used in place of checking state for
// whether or not the specified snap is in a quota group or not. If nil, state
// is consulted directly instead.
func SnapServiceOptions(st *state.State, snapInfo *snap.Info, quotaGroups map[string]*quota.Group) (opts *wrappers.SnapServiceOptions, err error) {
	// if quotaGroups was not provided to us, then go get that
	if quotaGroups == nil {
		allGrps, err := AllQuotas(st)
		if err != nil && !errors.Is(err, state.ErrNoState) {
			return nil, err
		}
		quotaGroups = allGrps
	}

	opts = &wrappers.SnapServiceOptions{}

	tr := config.NewTransaction(st)
	var vitalityStr string
	err = tr.GetMaybe("core", "resilience.vitality-hint", &vitalityStr)
	if err != nil {
		return nil, err
	}
	for i, s := range strings.Split(vitalityStr, ",") {
		if s == snapInfo.InstanceName() {
			opts.VitalityRank = i + 1
			break
		}
	}

	// also check for quota group for this instance name
	for _, grp := range quotaGroups {
		if strutil.ListContains(grp.Snaps, snapInfo.InstanceName()) {
			opts.QuotaGroup = grp
			break
		}
	}

	return opts, nil
}

// LogReader returns an io.ReadCloser which produce logs for the provided
// snap AppInfo's. It is a convenience wrapper around the systemd.LogReader
// implementation.
func LogReader(appInfos []*snap.AppInfo, n int, follow bool) (io.ReadCloser, error) {
	serviceNames := make([]string, len(appInfos))
	for i, appInfo := range appInfos {
		if !appInfo.IsService() {
			return nil, fmt.Errorf("cannot read logs for app %q: not a service", appInfo.Name)
		}
		serviceNames[i] = appInfo.ServiceName()
	}

	// Include journal namespaces if supported. The --namespace option was
	// introduced in systemd version 245. If systemd is older than that then
	// we cannot use journal quotas in any case and don't include them.
	includeNamespaces := false
	if err := systemd.EnsureAtLeast(245); err == nil {
		includeNamespaces = true
	} else if !systemd.IsSystemdTooOld(err) {
		return nil, fmt.Errorf("cannot get systemd version: %v", err)
	}

	sysd := systemd.New(systemd.SystemMode, progress.Null)
	return sysd.LogReader(serviceNames, n, follow, includeNamespaces)
}
