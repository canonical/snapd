// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2021 Canonical Ltd
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

package wrappers

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timeout"
	"github.com/snapcore/snapd/timings"
	"github.com/snapcore/snapd/usersession/client"
	"github.com/snapcore/snapd/wrappers/internal"
)

// ServiceScope indicates the scope of services that can be present in a snap, which
// may be affected by an operation.
type ServiceScope string

const (
	// ServiceScopeAll indicates both system and user services of a snap.
	ServiceScopeAll ServiceScope = ""
	// ServiceScopeSystem indicates just the system services of a snap.
	ServiceScopeSystem ServiceScope = "system"
	// ServiceScopeUser indicates just the user services of a snap.
	ServiceScopeUser ServiceScope = "user"
)

func (sc ServiceScope) matches(dscope snap.DaemonScope) bool {
	switch sc {
	case ServiceScopeAll:
		return true
	case ServiceScopeSystem:
		return dscope == snap.SystemDaemon
	case ServiceScopeUser:
		return dscope == snap.UserDaemon
	}
	return false
}

type Interacter interface {
	Notify(status string)
}

// wait this time between TERM and KILL
var killWait = 5 * time.Second

// ScopeOptions provides ways to limit the effects of service operations
// to a certain scope, including which users and service type.
type ScopeOptions struct {
	// Scope determines the types of services affected. This can be either
	// or both of system services and user services.
	Scope ServiceScope `json:"scope,omitempty"`
	// Users if set, determines which users the operation should include, if
	// the scope includes user services.
	Users []string `json:"users,omitempty"`
}

type userServiceClient struct {
	cli   *client.Client
	inter Interacter
}

var userLookup = user.Lookup

func usersToUids(users []string) (map[int]string, error) {
	uids := make(map[int]string)
	for _, username := range users {
		usr, err := userLookup(username)
		if err != nil {
			return nil, err
		}
		uid, err := strconv.Atoi(usr.Uid)
		if err != nil {
			return nil, err
		}
		uids[uid] = username
	}
	return uids, nil
}

func newUserServiceClientUids(uids []int, inter Interacter) (*userServiceClient, error) {
	return &userServiceClient{
		cli:   client.NewForUids(uids...),
		inter: inter,
	}, nil
}

func newUserServiceClientNames(users []string, inter Interacter) (*userServiceClient, error) {
	uids, err := usersToUids(users)
	if err != nil {
		return nil, err
	}
	var keys []int
	for uid := range uids {
		keys = append(keys, uid)
	}
	return newUserServiceClientUids(keys, inter)
}

func (c *userServiceClient) stopServices(services ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout.DefaultTimeout))
	defer cancel()
	failures, err := c.cli.ServicesStop(ctx, services)
	for _, f := range failures {
		c.inter.Notify(fmt.Sprintf("Could not stop service %q for uid %d: %s", f.Service, f.Uid, f.Error))
	}
	return err
}

func (c *userServiceClient) startServices(services ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout.DefaultTimeout))
	defer cancel()
	startFailures, stopFailures, err := c.cli.ServicesStart(ctx, services)
	for _, f := range startFailures {
		c.inter.Notify(fmt.Sprintf("Could not start service %q for uid %d: %s", f.Service, f.Uid, f.Error))
	}
	for _, f := range stopFailures {
		c.inter.Notify(fmt.Sprintf("While trying to stop previously started service %q for uid %d: %s", f.Service, f.Uid, f.Error))
	}
	return err
}

func (c *userServiceClient) restartServices(reload bool, services ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout.DefaultTimeout))
	defer cancel()
	var failures []client.ServiceFailure
	var err error
	if reload {
		failures, err = c.cli.ServicesReloadOrRestart(ctx, services)
	} else {
		failures, err = c.cli.ServicesRestart(ctx, services)
	}
	for _, f := range failures {
		c.inter.Notify(fmt.Sprintf("Could not restart service %q for uid %d: %s", f.Service, f.Uid, f.Error))
	}
	return err
}

func reloadOrRestartServices(sysd systemd.Systemd, cli *userServiceClient, reload bool, scope snap.DaemonScope, svcs []string) error {
	switch scope {
	case snap.SystemDaemon:
		if reload {
			if err := sysd.ReloadOrRestart(svcs); err != nil {
				return err
			}
		} else {
			if err := sysd.Restart(svcs); err != nil {
				return err
			}
		}
	case snap.UserDaemon:
		if err := cli.restartServices(reload, svcs...); err != nil {
			return err
		}
	default:
		panic("unknown app.DaemonScope")
	}

	return nil
}

func stopService(sysd systemd.Systemd, cli *userServiceClient, scope snap.DaemonScope, svcs []string) error {
	switch scope {
	case snap.SystemDaemon:
		if err := sysd.Stop(svcs); err != nil {
			return err
		}

	case snap.UserDaemon:
		if err := cli.stopServices(svcs...); err != nil {
			return err
		}
	default:
		panic("unknown app.DaemonScope")
	}

	return nil
}

func serviceIsActivated(app *snap.AppInfo) bool {
	return len(app.Sockets) > 0 || app.Timer != nil || len(app.ActivatesOn) > 0
}

func serviceIsSlotActivated(app *snap.AppInfo) bool {
	return len(app.ActivatesOn) > 0
}

// StartServicesFlags carries additional parameters for StartService.
// XXX: Rename to StartServiceOptions
type StartServicesFlags struct {
	Enable bool
	ScopeOptions
}

// StartServices starts service units for the applications from the snap which
// are services. Service units will be started in the order provided by the
// caller.
func StartServices(apps []*snap.AppInfo, disabledSvcs []string, flags *StartServicesFlags, inter Interacter, tm timings.Measurer) (err error) {
	if flags == nil {
		flags = &StartServicesFlags{}
	}

	systemSysd := systemd.New(systemd.SystemMode, inter)
	userSysd := systemd.New(systemd.GlobalUserMode, inter)
	cli, err := newUserServiceClientNames(flags.Users, inter)
	if err != nil {
		return err
	}

	var toEnableSystem []string
	var toEnableUser []string
	systemServices := make([]string, 0, len(apps))
	userServices := make([]string, 0, len(apps))
	servicesStarted := false

	defer func() {
		if err == nil {
			return
		}
		// apps could have been sorted according to their startup
		// ordering, stop them in reverse order
		if servicesStarted {
			for i := len(apps) - 1; i >= 0; i-- {
				app := apps[i]
				svc, activators := internal.SnapServiceUnits(app)
				if e := stopService(systemSysd, cli, app.DaemonScope, append(activators, svc)); e != nil {
					inter.Notify(fmt.Sprintf("While trying to stop previously started service %q: %v", app.ServiceName(), e))
				}
			}
		}
		if len(toEnableSystem) > 0 {
			if e := systemSysd.DisableNoReload(toEnableSystem); e != nil {
				inter.Notify(fmt.Sprintf("While trying to disable previously enabled services %q: %v", toEnableSystem, e))
			}
			if e := systemSysd.DaemonReload(); e != nil {
				inter.Notify(fmt.Sprintf("While trying to do daemon-reload: %v", e))
			}
		}
		if len(toEnableUser) > 0 {
			if e := userSysd.DisableNoReload(toEnableUser); e != nil {
				inter.Notify(fmt.Sprintf("while trying to disable previously enabled user services %q: %v", toEnableUser, e))
			}
		}
	}()
	// process all services of the snap in the order specified by the
	// caller; before batched calls were introduced, the sockets and timers
	// were started first, followed by other non-activated services
	markServicesForStart := func(svcs []string, scope snap.DaemonScope) {
		switch scope {
		case snap.SystemDaemon:
			systemServices = append(systemServices, svcs...)
		case snap.UserDaemon:
			userServices = append(userServices, svcs...)
		}
	}
	markServicesForEnable := func(svcs []string, scope snap.DaemonScope) {
		switch scope {
		case snap.SystemDaemon:
			toEnableSystem = append(toEnableSystem, svcs...)
		case snap.UserDaemon:
			toEnableUser = append(toEnableUser, svcs...)
		}
	}
	// first, gather all socket and timer units
	for _, app := range apps {
		if !app.IsService() {
			continue
		}
		// Verify that scope covers this service
		if !flags.Scope.matches(app.DaemonScope) {
			continue
		}
		if strutil.ListContains(disabledSvcs, app.Name) {
			continue
		}
		// Get all units for the service, but we only deal with
		// the activators here.
		_, activators := internal.SnapServiceUnits(app)
		if len(activators) == 0 {
			// just skip if there are no activated units
			continue
		}
		markServicesForStart(activators, app.DaemonScope)
		if flags.Enable {
			markServicesForEnable(activators, app.DaemonScope)
		}
	}

	// now collect all services
	for _, app := range apps {
		if !app.IsService() {
			continue
		}
		// Verify that scope covers this service
		if !flags.Scope.matches(app.DaemonScope) {
			continue
		}
		if serviceIsActivated(app) {
			continue
		}
		if strutil.ListContains(disabledSvcs, app.Name) {
			continue
		}
		svcName := app.ServiceName()
		markServicesForStart([]string{svcName}, app.DaemonScope)
		if flags.Enable {
			markServicesForEnable([]string{svcName}, app.DaemonScope)
		}
	}

	timings.Run(tm, "enable-services", fmt.Sprintf("enable services %q", toEnableSystem), func(nested timings.Measurer) {
		if len(toEnableSystem) > 0 {
			if err = systemSysd.EnableNoReload(toEnableSystem); err != nil {
				return
			}
			if err = systemSysd.DaemonReload(); err != nil {
				return
			}
		}
		if len(toEnableUser) > 0 {
			err = userSysd.EnableNoReload(toEnableUser)
		}
	})
	if err != nil {
		return err
	}

	timings.Run(tm, "start-services", "start services", func(nestedTm timings.Measurer) {
		for _, srv := range systemServices {
			// let the cleanup know some services may have been started
			servicesStarted = true
			// starting all services at once does not create a
			// single transaction, but instead spawns multiple jobs,
			// make sure the services started in the original order
			// by bringing them up one by one, see:
			// https://github.com/systemd/systemd/issues/8102
			// https://lists.freedesktop.org/archives/systemd-devel/2018-January/040152.html
			timings.Run(nestedTm, "start-service", fmt.Sprintf("start service %q", srv), func(_ timings.Measurer) {
				err = systemSysd.Start([]string{srv})
			})
			if err != nil {
				return
			}
		}
	})
	if servicesStarted && err != nil {
		// cleanup is handled in a defer
		return err
	}

	if len(userServices) != 0 {
		timings.Run(tm, "start-user-services", "start user services", func(nested timings.Measurer) {
			err = cli.startServices(userServices...)
		})
		// let the cleanup know some services may have been started
		servicesStarted = true
		if err != nil {
			return err
		}
	}

	return nil
}

func userDaemonReload() error {
	cli := client.New()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout.DefaultTimeout))
	defer cancel()
	return cli.ServicesDaemonReload(ctx)
}

func tryFileUpdate(path string, desiredContent []byte) (old *osutil.MemoryFileState, modified bool, err error) {
	newFileState := osutil.MemoryFileState{
		Content: desiredContent,
		Mode:    os.FileMode(0644),
	}

	// get the existing content (if any) of the file to have something to
	// rollback to if we have any errors

	// note we can't use FileReference here since we may be modifying
	// the file, and the FileReference.State() wouldn't be evaluated
	// until _after_ we attempted modification
	oldFileState := osutil.MemoryFileState{}

	if st, err := os.Stat(path); err == nil {
		b, err := ioutil.ReadFile(path)
		if err != nil {
			return nil, false, err
		}
		oldFileState.Content = b
		oldFileState.Mode = st.Mode()
		newFileState.Mode = st.Mode()

		// save the old state of the file
		old = &oldFileState
	}

	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0755); mkdirErr != nil {
		return nil, false, mkdirErr
	}
	ensureErr := osutil.EnsureFileState(path, &newFileState)
	switch ensureErr {
	case osutil.ErrSameState:
		// didn't change the file
		return old, false, nil
	case nil:
		// we successfully modified the file
		return old, true, nil
	default:
		// some other fatal error trying to write the file
		return nil, false, ensureErr
	}
}

type SnapServiceOptions struct {
	// VitalityRank is the rank of all services in the specified snap used by
	// the OOM killer when OOM conditions are reached.
	VitalityRank int

	// QuotaGroup is the quota group for the specified snap.
	QuotaGroup *quota.Group
}

// ObserveChangeCallback can be invoked by EnsureSnapServices to observe
// the previous content of a unit and the new on a change.
// unitType can be "service", "socket", "timer". name is empty for a timer.
type ObserveChangeCallback func(app *snap.AppInfo, grp *quota.Group, unitType string, name, old, new string)

// EnsureSnapServicesOptions is the set of options applying to the
// EnsureSnapServices operation. It does not include per-snap specific options
// such as VitalityRank or RequireMountedSnapdSnap from AddSnapServiceOptions,
// since those are expected to be provided in the snaps argument.
type EnsureSnapServicesOptions struct {
	// Preseeding is whether the system is currently being preseeded, in which
	// case there is not a running systemd for EnsureSnapServicesOptions to
	// issue commands like systemctl daemon-reload to.
	Preseeding bool

	// RequireMountedSnapdSnap is whether the generated units should depend on
	// the snapd snap being mounted, this is specific to systems like UC18 and
	// UC20 which have the snapd snap and need to have units generated
	RequireMountedSnapdSnap bool

	// IncludeServices provides the option for allowing filtering which services
	// that should be configured. The service list must be in the format my-snap.my-service.
	// If this list is not provided, then all services will be included.
	IncludeServices []string
}

// ensureSnapServicesContext is the context for EnsureSnapServices.
// EnsureSnapServices supports transactional update/write of systemd service
// files and slice files. A part of this is to support rollback of files and
// also keep track of whether a restart of systemd daemon is required.
type ensureSnapServicesContext struct {
	// snaps, observeChange, opts and inter are the arguments
	// taken by EnsureSnapServices. They are here to allow sub-functions
	// to easier access these, and keep parameter lists shorter.
	snaps         map[*snap.Info]*SnapServiceOptions
	observeChange ObserveChangeCallback
	opts          *EnsureSnapServicesOptions
	inter         Interacter

	// note: is not used when preseeding is set in opts.Preseeding
	sysd                     systemd.Systemd
	systemDaemonReloadNeeded bool
	userDaemonReloadNeeded   bool
	// modifiedUnits is the set of units that were modified and the previous
	// state of the unit before modification that we can roll back to if there
	// are any issues.
	// note that the rollback is best effort, if we are rebooted in the middle,
	// there is no guarantee about the state of files, some may have been
	// updated and some may have been rolled back, higher level tasks/changes
	// should have do/undo handlers to properly handle the case where this
	// function is interrupted midway
	modifiedUnits map[string]*osutil.MemoryFileState
}

// restore is a helper function which should be called in case any errors happen
// during the write/update of systemd files
func (es *ensureSnapServicesContext) restore() {
	for file, state := range es.modifiedUnits {
		if state == nil {
			// we don't have anything to rollback to, so just remove the
			// file
			if err := os.Remove(file); err != nil {
				es.inter.Notify(fmt.Sprintf("while trying to remove %s due to previous failure: %v", file, err))
			}
		} else {
			// rollback the file to the previous state
			if err := osutil.EnsureFileState(file, state); err != nil {
				es.inter.Notify(fmt.Sprintf("while trying to rollback %s due to previous failure: %v", file, err))
			}
		}
	}

	if !es.opts.Preseeding {
		if es.systemDaemonReloadNeeded {
			if err := es.sysd.DaemonReload(); err != nil {
				es.inter.Notify(fmt.Sprintf("while trying to perform systemd daemon-reload due to previous failure: %v", err))
			}
		}
		if es.userDaemonReloadNeeded {
			if err := userDaemonReload(); err != nil {
				es.inter.Notify(fmt.Sprintf("while trying to perform user systemd daemon-reload due to previous failure: %v", err))
			}
		}
	}
}

// reloadModified uses the modifiedSystemServices/userDaemonReloadNeeded to determine whether a reload
// is required from systemd to take the new systemd files into effect. This is a NOP
// if opts.Preseeding is set
func (es *ensureSnapServicesContext) reloadModified() error {
	if es.opts.Preseeding {
		return nil
	}

	if es.systemDaemonReloadNeeded {
		if err := es.sysd.DaemonReload(); err != nil {
			return err
		}
	}
	if es.userDaemonReloadNeeded {
		if err := userDaemonReload(); err != nil {
			return err
		}
	}
	return nil
}

// ensureSnapServiceSystemdUnits takes care of writing .service files for all services
// registered in snap.Info apps.
func (es *ensureSnapServicesContext) ensureSnapServiceSystemdUnits(snapInfo *snap.Info, opts *internal.SnapServicesUnitOptions) error {
	handleFileModification := func(app *snap.AppInfo, unitType string, name, path string, content []byte) error {
		old, modifiedFile, err := tryFileUpdate(path, content)
		if err != nil {
			return err
		}

		if modifiedFile {
			if es.observeChange != nil {
				var oldContent []byte
				if old != nil {
					oldContent = old.Content
				}
				es.observeChange(app, nil, unitType, name, string(oldContent), string(content))
			}
			es.modifiedUnits[path] = old

			// also mark that we need to reload either the system or
			// user instance of systemd
			switch app.DaemonScope {
			case snap.SystemDaemon:
				es.systemDaemonReloadNeeded = true
			case snap.UserDaemon:
				es.userDaemonReloadNeeded = true
			}
		}

		return nil
	}

	// lets sort the service list before generating them for
	// consistency when testing
	services := snapInfo.Services()
	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})

	var svcQuotaMap map[string]*quota.Group
	if opts.QuotaGroup != nil {
		svcQuotaMap = opts.QuotaGroup.ServiceMap()
	}

	// note that the Preseeding option is not used here at all
	for _, svc := range services {
		// if an inclusion list is provided, then we want to make sure this service
		// is included.
		// TODO: add an AppInfo.FullName member
		fullServiceName := fmt.Sprintf("%s.%s", snapInfo.InstanceName(), svc.Name)
		if len(es.opts.IncludeServices) > 0 && !strutil.ListContains(es.opts.IncludeServices, fullServiceName) {
			continue
		}

		// create services first; this doesn't trigger systemd

		// get the correct quota group for the service we are generating.
		quotaGrp := opts.QuotaGroup
		if quotaGrp != nil {
			// the service map contains quota group overrides if the service
			// entry exists.
			quotaGrp = svcQuotaMap[fullServiceName]
			if quotaGrp == nil {
				// default to the parent quota group, which may also be nil.
				quotaGrp = opts.QuotaGroup
			}
		}

		// Generate new service file state, make an app-specific generateSnapServicesOptions
		// to avoid modifying the original copy, if we were to override the quota group.
		content, err := internal.GenerateSnapServiceUnitFile(svc, &internal.SnapServicesUnitOptions{
			QuotaGroup:              quotaGrp,
			VitalityRank:            opts.VitalityRank,
			CoreMountedSnapdSnapDep: opts.CoreMountedSnapdSnapDep,
		})
		if err != nil {
			return err
		}

		path := svc.ServiceFile()
		if err := handleFileModification(svc, "service", svc.Name, path, content); err != nil {
			return err
		}

		// Generate systemd .socket files if needed
		socketFiles, err := internal.GenerateSnapSocketUnitFiles(svc)
		if err != nil {
			return err
		}
		for name, content := range socketFiles {
			path := svc.Sockets[name].File()
			if err := handleFileModification(svc, "socket", name, path, content); err != nil {
				return err
			}
		}

		if svc.Timer != nil {
			content, err := internal.GenerateSnapServiceTimerUnitFile(svc)
			if err != nil {
				return err
			}
			path := svc.Timer.File()
			if err := handleFileModification(svc, "timer", "", path, content); err != nil {
				return err
			}
		}
	}
	return nil
}

// ensureSnapsSystemdServices takes care of writing .service files for all apps in the provided snaps
// list, and also returns a quota group set that represents all quota groups for the set of snaps
// provided if they are a part of any.
func (es *ensureSnapServicesContext) ensureSnapsSystemdServices() (*quota.QuotaGroupSet, error) {
	neededQuotaGrps := &quota.QuotaGroupSet{}

	for s, snapSvcOpts := range es.snaps {
		if s.Type() == snap.TypeSnapd {
			return nil, fmt.Errorf("internal error: adding explicit services for snapd snap is unexpected")
		}
		if snapSvcOpts == nil {
			snapSvcOpts = &SnapServiceOptions{}
		}

		// always use RequireMountedSnapdSnap options from the global options
		genServiceOpts := &internal.SnapServicesUnitOptions{
			VitalityRank: snapSvcOpts.VitalityRank,
			QuotaGroup:   snapSvcOpts.QuotaGroup,
		}
		if es.opts.RequireMountedSnapdSnap {
			// on core 18+ systems, the snapd tooling is exported
			// into the host system via a special mount unit, which
			// also adds an implicit dependency on the snapd snap
			// mount thus /usr/bin/snap points
			genServiceOpts.CoreMountedSnapdSnapDep = SnapdToolingMountUnit
		}

		if snapSvcOpts.QuotaGroup != nil {
			// AddAllNecessaryGroups also adds all sub-groups to the quota group set. So this
			// automatically covers any other quota group that might be set in snapSvcOpts.ServiceQuotaMap
			if err := neededQuotaGrps.AddAllNecessaryGroups(snapSvcOpts.QuotaGroup); err != nil {
				// this error can basically only be a circular reference
				// in the quota group tree
				return nil, err
			}
		}

		if err := es.ensureSnapServiceSystemdUnits(s, genServiceOpts); err != nil {
			return nil, err
		}
	}
	return neededQuotaGrps, nil
}

func (es *ensureSnapServicesContext) ensureSnapSlices(quotaGroups *quota.QuotaGroupSet) error {
	handleSliceModification := func(grp *quota.Group, path string, content []byte) error {
		old, modifiedFile, err := tryFileUpdate(path, content)
		if err != nil {
			return err
		}

		if modifiedFile {
			if es.observeChange != nil {
				var oldContent []byte
				if old != nil {
					oldContent = old.Content
				}
				es.observeChange(nil, grp, "slice", grp.Name, string(oldContent), string(content))
			}

			es.modifiedUnits[path] = old

			// also mark that we need to reload the system instance of systemd
			// TODO: also handle reloading the user instance of systemd when
			// needed
			es.systemDaemonReloadNeeded = true
		}

		return nil
	}

	// now make sure that all of the slice units exist
	for _, grp := range quotaGroups.AllQuotaGroups() {
		content := internal.GenerateQuotaSliceUnitFile(grp)

		sliceFileName := grp.SliceFileName()
		path := filepath.Join(dirs.SnapServicesDir, sliceFileName)
		if err := handleSliceModification(grp, path, content); err != nil {
			return err
		}
	}
	return nil
}

func (es *ensureSnapServicesContext) ensureSnapJournaldUnits(quotaGroups *quota.QuotaGroupSet) error {
	handleJournalModification := func(grp *quota.Group, path string, content []byte) error {
		old, fileModified, err := tryFileUpdate(path, content)
		if err != nil {
			return err
		}

		if !fileModified {
			return nil
		}

		// suppress any event and restart if we actually did not do anything
		// as it seems modifiedFile is set even when the file does not exist
		// and when the new content is nil.
		if (old == nil || len(old.Content) == 0) && len(content) == 0 {
			return nil
		}

		if es.observeChange != nil {
			var oldContent []byte
			if old != nil {
				oldContent = old.Content
			}
			es.observeChange(nil, grp, "journald", grp.Name, string(oldContent), string(content))
		}

		es.modifiedUnits[path] = old
		return nil
	}

	for _, grp := range quotaGroups.AllQuotaGroups() {
		if len(grp.Services) > 0 {
			// ignore service sub-groups
			continue
		}

		contents := internal.GenerateQuotaJournaldConfFile(grp)
		fileName := grp.JournalConfFileName()

		path := filepath.Join(dirs.SnapSystemdDir, fileName)
		if err := handleJournalModification(grp, path, contents); err != nil {
			return err
		}
	}
	return nil
}

// ensureJournalQuotaServiceUnits takes care of writing service drop-in files for all journal namespaces.
func (es *ensureSnapServicesContext) ensureJournalQuotaServiceUnits(quotaGroups *quota.QuotaGroupSet) error {
	handleFileModification := func(grp *quota.Group, path string, content []byte) error {
		old, fileModified, err := tryFileUpdate(path, content)
		if err != nil {
			return err
		}

		if fileModified {
			if es.observeChange != nil {
				var oldContent []byte
				if old != nil {
					oldContent = old.Content
				}
				es.observeChange(nil, grp, "service", grp.Name, string(oldContent), string(content))
			}
			es.modifiedUnits[path] = old
		}
		return nil
	}

	for _, grp := range quotaGroups.AllQuotaGroups() {
		if grp.JournalLimit == nil {
			continue
		}

		if err := os.MkdirAll(grp.JournalServiceDropInDir(), 0755); err != nil {
			return err
		}

		dropInPath := grp.JournalServiceDropInFile()
		content := internal.GenerateQuotaJournalServiceFile(grp)
		if err := handleFileModification(grp, dropInPath, content); err != nil {
			return err
		}
	}

	return nil
}

// EnsureSnapServices will ensure that the specified snap services' file states
// are up to date with the specified options and infos. It will add new services
// if those units don't already exist, but it does not delete existing service
// units that are not present in the snap's Info structures.
// There are two sets of options; there are global options which apply to the
// entire transaction and to every snap service that is ensured, and options
// which are per-snap service and specified in the map argument.
// If any errors are encountered trying to update systemd units, then all
// changes performed up to that point are rolled back, meaning newly written
// units are deleted and modified units are attempted to be restored to their
// previous state.
// To observe which units were added or modified a
// ObserveChangeCallback calllback can be provided. The callback is
// invoked while processing the changes. Because of that it should not
// produce immediate side-effects, as the changes are in effect only
// if the function did not return an error.
// This function is idempotent.
func EnsureSnapServices(snaps map[*snap.Info]*SnapServiceOptions, opts *EnsureSnapServicesOptions, observeChange ObserveChangeCallback, inter Interacter) (err error) {
	if opts == nil {
		opts = &EnsureSnapServicesOptions{}
	}

	context := &ensureSnapServicesContext{
		snaps:         snaps,
		observeChange: observeChange,
		opts:          opts,
		inter:         inter,
		sysd:          systemd.New(systemd.SystemMode, inter),
		modifiedUnits: make(map[string]*osutil.MemoryFileState),
	}

	defer func() {
		if err == nil {
			return
		}
		context.restore()
	}()

	quotaGroups, err := context.ensureSnapsSystemdServices()
	if err != nil {
		return err
	}

	if err := context.ensureSnapSlices(quotaGroups); err != nil {
		return err
	}

	if err := context.ensureSnapJournaldUnits(quotaGroups); err != nil {
		return err
	}

	if err := context.ensureJournalQuotaServiceUnits(quotaGroups); err != nil {
		return err
	}

	return context.reloadModified()
}

// StopServicesFlags carries additional parameters for StopServices.
// XXX: Rename to StopServicesOptions
type StopServicesFlags struct {
	Disable bool
	ScopeOptions
}

// StopServices stops and optionally disables service units for the applications
// from the snap which are services.
func StopServices(apps []*snap.AppInfo, flags *StopServicesFlags, reason snap.ServiceStopReason, inter Interacter, tm timings.Measurer) error {
	sysd := systemd.New(systemd.SystemMode, inter)
	if flags == nil {
		flags = &StopServicesFlags{}
	}

	if reason != snap.StopReasonOther {
		logger.Debugf("StopServices called for %q, reason: %v", apps, reason)
	} else {
		logger.Debugf("StopServices called for %q", apps)
	}

	cli, err := newUserServiceClientNames(flags.Users, inter)
	if err != nil {
		return err
	}

	disableServices := []string{}
	for _, app := range apps {
		// Handle the case where service file doesn't exist and don't try to stop it as it will fail.
		// This can happen with snap try when snap.yaml is modified on the fly and a daemon line is added.
		if !app.IsService() || !osutil.FileExists(app.ServiceFile()) {
			continue
		}
		// Skip stop on refresh when refresh mode is set to something
		// other than "restart" (or "" which is the same)
		if reason == snap.StopReasonRefresh {
			logger.Debugf(" %s refresh-mode: %v", app.Name, app.StopMode)
			switch app.RefreshMode {
			case "endure":
				// skip this service
				continue
			}
		}
		// Verify that scope covers this service
		if !flags.Scope.matches(app.DaemonScope) {
			continue
		}

		// Is the service slot activated, then lets warn the user this doesn't have any
		// real effect if a disable was requested
		if flags.Disable && serviceIsSlotActivated(app) {
			logger.Noticef("Disabling %s may not have the intended effect as the service is currently always activated by a slot", app.Name)
		}

		// Get services including any activation mechanisms. When stopping and disabling
		// services we do it on both the primary service, and it's activation mechanisms. The
		// StartServices logic does actually not enable/start any service which are activated,
		// but rather only the activation services themselves, so one might argue if it
		// is really necessary to disable the primary service.
		svc, activators := internal.SnapServiceUnits(app)

		var err error
		timings.Run(tm, "stop-service", fmt.Sprintf("stop service %q", app.ServiceName()), func(nested timings.Measurer) {
			err = stopService(sysd, cli, app.DaemonScope, append(activators, svc))
			if err == nil && flags.Disable {
				disableServices = append(disableServices, append(activators, svc)...)
			}
		})
		if err != nil {
			return err
		}
	}

	if len(disableServices) > 0 {
		if err := sysd.DisableNoReload(disableServices); err != nil {
			return err
		}
		if err := sysd.DaemonReload(); err != nil {
			return err
		}
	}
	return nil
}

// RemoveQuotaGroup ensures that the slice file for a quota group is removed. It
// assumes that the slice corresponding to the group is not in use anymore by
// any services or sub-groups of the group when it is invoked. To remove a group
// with sub-groups, one must remove all the sub-groups first.
// This function is idempotent, if the slice file doesn't exist no error is
// returned.
func RemoveQuotaGroup(grp *quota.Group, inter Interacter) error {
	// TODO: it only works on leaf sub-groups currently
	if len(grp.SubGroups) != 0 {
		return fmt.Errorf("internal error: cannot remove quota group with sub-groups")
	}

	systemSysd := systemd.New(systemd.SystemMode, inter)

	// remove the slice file
	err := os.Remove(filepath.Join(dirs.SnapServicesDir, grp.SliceFileName()))
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if err == nil {
		// we deleted the slice unit, so we need to daemon-reload
		if err := systemSysd.DaemonReload(); err != nil {
			return err
		}
	}
	return nil
}

// RemoveSnapServices disables and removes service units for the applications
// from the snap which are services. The optional flag indicates whether
// services are removed as part of undoing of first install of a given snap.
func RemoveSnapServices(s *snap.Info, inter Interacter) error {
	if s.Type() == snap.TypeSnapd {
		return fmt.Errorf("internal error: removing explicit services for snapd snap is unexpected")
	}
	systemSysd := systemd.New(systemd.SystemMode, inter)
	userSysd := systemd.New(systemd.GlobalUserMode, inter)
	var removedSystem, removedUser bool
	systemUnits := []string{}
	userUnits := []string{}
	systemUnitFiles := []string{}

	// collect list of system units to disable and remove
	for _, app := range s.Apps {
		if !app.IsService() || !osutil.FileExists(app.ServiceFile()) {
			continue
		}

		switch app.DaemonScope {
		case snap.SystemDaemon:
			removedSystem = true
		case snap.UserDaemon:
			removedUser = true
		}
		serviceName := filepath.Base(app.ServiceFile())

		for _, socket := range app.Sockets {
			path := socket.File()
			socketServiceName := filepath.Base(path)
			logger.Noticef("RemoveSnapServices - socket %s", socketServiceName)
			switch app.DaemonScope {
			case snap.SystemDaemon:
				systemUnits = append(systemUnits, socketServiceName)
			case snap.UserDaemon:
				userUnits = append(userUnits, socketServiceName)
			}
			systemUnitFiles = append(systemUnitFiles, path)
		}

		if app.Timer != nil {
			path := app.Timer.File()

			timerName := filepath.Base(path)
			logger.Noticef("RemoveSnapServices - timer %s", timerName)
			switch app.DaemonScope {
			case snap.SystemDaemon:
				systemUnits = append(systemUnits, timerName)
			case snap.UserDaemon:
				userUnits = append(userUnits, timerName)
			}
			systemUnitFiles = append(systemUnitFiles, path)
		}

		logger.Noticef("RemoveSnapServices - disabling %s", serviceName)
		switch app.DaemonScope {
		case snap.SystemDaemon:
			systemUnits = append(systemUnits, serviceName)
		case snap.UserDaemon:
			userUnits = append(userUnits, serviceName)
		}
		systemUnitFiles = append(systemUnitFiles, app.ServiceFile())
	}

	// disable all collected systemd units
	if err := systemSysd.DisableNoReload(systemUnits); err != nil {
		return err
	}

	// disable all collected user units
	if err := userSysd.DisableNoReload(userUnits); err != nil {
		return err
	}

	// remove unit filenames
	for _, systemUnitFile := range systemUnitFiles {
		if err := os.Remove(systemUnitFile); err != nil && !os.IsNotExist(err) {
			logger.Noticef("Failed to remove socket file %q: %v", systemUnitFile, err)
		}
	}

	// only reload if we actually had services
	if removedSystem {
		if err := systemSysd.DaemonReload(); err != nil {
			return err
		}
	}
	if removedUser {
		if err := userDaemonReload(); err != nil {
			return err
		}
	}

	return nil
}

// RestartServicesFlags carries additional parameters for RestartServices.
// XXX: Rename to RestartServicesOptions
type RestartServicesFlags struct {
	// Reload set if we might need to reload the service definitions.
	Reload bool
	// AlsoEnabledNonActive set if we to restart also enabled but not running units
	AlsoEnabledNonActive bool
	ScopeOptions
}

func restartServicesByStatus(svcsSts []*internal.ServiceStatus, explicitServices []string,
	flags *RestartServicesFlags, sysd systemd.Systemd, cli *userServiceClient, tm timings.Measurer) error {
	for _, st := range svcsSts {
		unitName := st.ServiceUnitStatus().Name
		unitActive := st.ServiceUnitStatus().Active
		unitEnabled := st.IsEnabled()
		unitScope := snap.SystemDaemon
		if st.IsUserService() {
			unitScope = snap.UserDaemon
		}

		// If the unit was explicitly mentioned in the command line, restart it
		// even if it is disabled; otherwise, we only restart units which are
		// currently enabled or running. Reference:
		// https://forum.snapcraft.io/t/command-line-interface-to-manipulate-services/262/47
		if !unitActive && !strutil.ListContains(explicitServices, unitName) {
			if !flags.AlsoEnabledNonActive {
				logger.Noticef("not restarting inactive unit %s", unitName)
				continue
			} else if !unitEnabled {
				logger.Noticef("not restarting disabled and inactive unit %s", unitName)
				continue
			}
		}

		var err error
		timings.Run(tm, "restart-service", fmt.Sprintf("restart service %s", unitName), func(nested timings.Measurer) {
			err = reloadOrRestartServices(sysd, cli, flags.Reload, unitScope, []string{unitName})
		})
		if err != nil {
			// there is nothing we can do about failed service
			return err
		}
	}
	return nil
}

// Restart or reload active services in `svcs`.
// If reload flag is set then "systemctl reload-or-restart" is attempted.
// The services mentioned in `explicitServices` should be a subset of the
// services in svcs. The services included in explicitServices are always
// restarted, regardless of their state. The services in the `svcs` argument
// are only restarted if they are active, so if a service is meant to be
// restarted no matter it's state, it should be included in the
// explicitServices list.
// The list of explicitServices needs to use systemd unit names.
// TODO: change explicitServices format to be less unusual, more consistent
// (introduce AppRef?)
func RestartServices(apps []*snap.AppInfo, explicitServices []string,
	flags *RestartServicesFlags, inter Interacter, tm timings.Measurer) error {
	if flags == nil {
		flags = &RestartServicesFlags{}
	}
	sysd := systemd.New(systemd.SystemMode, inter)

	// Get service statuses for each of the apps
	sysSvcs, usrSvcsMap, err := internal.QueryServiceStatusMany(apps, sysd)
	if err != nil {
		return err
	}

	// Handle restart of system services if scope was set
	if flags.Scope != ServiceScopeUser {
		if err := restartServicesByStatus(sysSvcs, explicitServices, flags, sysd, nil, tm); err != nil {
			return err
		}
	}

	// Handle restart of the user services if scope was set
	if flags.Scope != ServiceScopeSystem {
		// Get a list of the uids that we are affecting
		uids, err := usersToUids(flags.Users)
		if err != nil {
			return err
		}

		for uid, stss := range usrSvcsMap {
			// If specific users were specified, i.e self, then make sure we only
			// restart services for that user
			if len(uids) > 0 && uids[uid] == "" {
				continue
			}

			// Create a new client, only targeting that user
			cli, err := newUserServiceClientUids([]int{uid}, inter)
			if err != nil {
				return err
			}

			if err := restartServicesByStatus(stss, explicitServices, flags, sysd, cli, tm); err != nil {
				return err
			}
		}
	}
	return nil
}

// QueryDisabledServices returns a list of all currently disabled snap services
// in the snap.
func QueryDisabledServices(info *snap.Info, pb progress.Meter) ([]string, error) {
	sysd := systemd.New(systemd.SystemMode, pb)

	// TODO: support user-daemons being reported back here, as it will be possible
	// for services to have different enablement status on different users.
	sts, _, err := internal.QueryServiceStatusMany(info.Services(), sysd)
	if err != nil {
		return nil, err
	}

	// add all disabled services to the list
	disabledSnapSvcs := []string{}
	for _, st := range sts {
		if !st.IsEnabled() {
			disabledSnapSvcs = append(disabledSnapSvcs, st.Name())
		}
	}

	// sort for easier testing
	sort.Strings(disabledSnapSvcs)

	return disabledSnapSvcs, nil
}
