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
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timeout"
	"github.com/snapcore/snapd/timeutil"
	"github.com/snapcore/snapd/timings"
	"github.com/snapcore/snapd/usersession/client"
)

type interacter interface {
	Notify(status string)
}

// wait this time between TERM and KILL
var killWait = 5 * time.Second

func serviceStopTimeout(app *snap.AppInfo) time.Duration {
	tout := app.StopTimeout
	if tout == 0 {
		tout = timeout.DefaultTimeout
	}
	return time.Duration(tout)
}

// TODO: this should not accept AddSnapServicesOptions, it should use some other
// subset of options, specifically it should not accept Preseeding as an option
// here
func generateSnapServiceFile(app *snap.AppInfo, opts *AddSnapServicesOptions) ([]byte, error) {
	if err := snap.ValidateApp(app); err != nil {
		return nil, err
	}

	return genServiceFile(app, opts)
}

// generateGroupSliceFile generates a systemd slice unit definition for the
// specified quota group.
func generateGroupSliceFile(grp *quota.Group) []byte {
	buf := bytes.Buffer{}

	template := `[Unit]
Description=Slice for snap quota group %[1]s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
MemoryMax=%[2]d
# for compatibility with older versions of systemd
MemoryLimit=%[2]d

# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
`

	fmt.Fprintf(&buf, template, grp.Name, grp.MemoryLimit)

	return buf.Bytes()
}

func stopUserServices(cli *client.Client, inter interacter, services ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout.DefaultTimeout))
	defer cancel()
	failures, err := cli.ServicesStop(ctx, services)
	for _, f := range failures {
		inter.Notify(fmt.Sprintf("Could not stop service %q for uid %d: %s", f.Service, f.Uid, f.Error))
	}
	return err
}

func startUserServices(cli *client.Client, inter interacter, services ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout.DefaultTimeout))
	defer cancel()
	startFailures, stopFailures, err := cli.ServicesStart(ctx, services)
	for _, f := range startFailures {
		inter.Notify(fmt.Sprintf("Could not start service %q for uid %d: %s", f.Service, f.Uid, f.Error))
	}
	for _, f := range stopFailures {
		inter.Notify(fmt.Sprintf("While trying to stop previously started service %q for uid %d: %s", f.Service, f.Uid, f.Error))
	}
	return err
}

func stopService(sysd systemd.Systemd, app *snap.AppInfo, inter interacter) error {
	serviceName := app.ServiceName()
	tout := serviceStopTimeout(app)

	var extraServices []string
	for _, socket := range app.Sockets {
		extraServices = append(extraServices, filepath.Base(socket.File()))
	}
	if app.Timer != nil {
		extraServices = append(extraServices, filepath.Base(app.Timer.File()))
	}

	switch app.DaemonScope {
	case snap.SystemDaemon:
		stopErrors := []error{}
		for _, service := range extraServices {
			if err := sysd.Stop([]string{service}, tout); err != nil {
				stopErrors = append(stopErrors, err)
			}
		}

		if err := sysd.Stop([]string{serviceName}, tout); err != nil {
			if !systemd.IsTimeout(err) {
				return err
			}
			inter.Notify(fmt.Sprintf("%s refused to stop, killing.", serviceName))
			// ignore errors for kill; nothing we'd do differently at this point
			sysd.Kill(serviceName, "TERM", "")
			time.Sleep(killWait)
			sysd.Kill(serviceName, "KILL", "")
		}

		if len(stopErrors) > 0 {
			return stopErrors[0]
		}

	case snap.UserDaemon:
		extraServices = append(extraServices, serviceName)
		cli := client.New()
		return stopUserServices(cli, inter, extraServices...)
	}

	return nil
}

// enableServices enables services specified by apps. On success the returned
// disable function can be used to undo all the actions. On error all the
// services get disabled automatically (disable is nil).
func enableServices(apps []*snap.AppInfo, inter interacter) (disable func(), err error) {
	var enabled []string
	var userEnabled []string

	systemSysd := systemd.New(systemd.SystemMode, inter)
	userSysd := systemd.New(systemd.GlobalUserMode, inter)

	disableEnabledServices := func() {
		for _, srvName := range enabled {
			if e := systemSysd.Disable([]string{srvName}); e != nil {
				inter.Notify(fmt.Sprintf("While trying to disable previously enabled service %q: %v", srvName, e))
			}
		}
		for _, s := range userEnabled {
			if e := userSysd.Disable([]string{s}); e != nil {
				inter.Notify(fmt.Sprintf("while trying to disable %s due to previous failure: %v", s, e))
			}
		}
	}

	defer func() {
		if err != nil {
			disableEnabledServices()
		}
	}()

	for _, app := range apps {
		var sysd systemd.Systemd
		switch app.DaemonScope {
		case snap.SystemDaemon:
			sysd = systemSysd
		case snap.UserDaemon:
			sysd = userSysd
		}

		svcName := app.ServiceName()

		switch app.DaemonScope {
		case snap.SystemDaemon:
			if err = sysd.Enable([]string{svcName}); err != nil {
				return nil, err

			}
			enabled = append(enabled, svcName)
		case snap.UserDaemon:
			if err = userSysd.Enable([]string{svcName}); err != nil {
				return nil, err
			}
			userEnabled = append(userEnabled, svcName)
		}
	}

	return disableEnabledServices, nil
}

// StartServicesFlags carries extra flags for StartServices.
type StartServicesFlags struct {
	Enable bool
}

// StartServices starts service units for the applications from the snap which
// are services. Service units will be started in the order provided by the
// caller.
func StartServices(apps []*snap.AppInfo, disabledSvcs []string, flags *StartServicesFlags, inter interacter, tm timings.Measurer) (err error) {
	if flags == nil {
		flags = &StartServicesFlags{}
	}

	systemSysd := systemd.New(systemd.SystemMode, inter)
	userSysd := systemd.New(systemd.GlobalUserMode, inter)
	cli := client.New()

	var disableEnabledServices func()

	defer func() {
		if err == nil {
			return
		}
		if disableEnabledServices != nil {
			disableEnabledServices()
		}
	}()

	var toEnable []*snap.AppInfo
	systemServices := make([]string, 0, len(apps))
	userServices := make([]string, 0, len(apps))

	// gather all non-sockets, non-timers, and non-dbus activated
	// services to enable first
	for _, app := range apps {
		// they're *supposed* to be all services, but checking doesn't hurt
		if !app.IsService() {
			continue
		}
		// sockets and timers are enabled and started separately (and unconditionally) further down.
		// dbus activatable services are started on first use.
		if len(app.Sockets) == 0 && app.Timer == nil && len(app.ActivatesOn) == 0 {
			if strutil.ListContains(disabledSvcs, app.Name) {
				continue
			}
			svcName := app.ServiceName()
			switch app.DaemonScope {
			case snap.SystemDaemon:
				systemServices = append(systemServices, svcName)
			case snap.UserDaemon:
				userServices = append(userServices, svcName)
			}
			if flags.Enable {
				toEnable = append(toEnable, app)
			}
		}
	}

	timings.Run(tm, "enable-services", fmt.Sprintf("enable services %q", toEnable), func(nested timings.Measurer) {
		disableEnabledServices, err = enableServices(toEnable, inter)
	})
	if err != nil {
		return err
	}

	// handle sockets and timers
	for _, app := range apps {
		// they're *supposed* to be all services, but checking doesn't hurt
		if !app.IsService() {
			continue
		}

		var sysd systemd.Systemd
		switch app.DaemonScope {
		case snap.SystemDaemon:
			sysd = systemSysd
		case snap.UserDaemon:
			sysd = userSysd
		}

		defer func(app *snap.AppInfo) {
			if err == nil {
				return
			}

			if e := stopService(sysd, app, inter); e != nil {
				inter.Notify(fmt.Sprintf("While trying to stop previously started service %q: %v", app.ServiceName(), e))
			}
			for _, socket := range app.Sockets {
				socketService := filepath.Base(socket.File())
				if e := sysd.Disable([]string{socketService}); e != nil {
					inter.Notify(fmt.Sprintf("While trying to disable previously enabled socket service %q: %v", socketService, e))
				}
			}
			if app.Timer != nil {
				timerService := filepath.Base(app.Timer.File())
				if e := sysd.Disable([]string{timerService}); e != nil {
					inter.Notify(fmt.Sprintf("While trying to disable previously enabled timer service %q: %v", timerService, e))
				}
			}
		}(app)

		for _, socket := range app.Sockets {
			socketService := filepath.Base(socket.File())
			// enable the socket
			if err = sysd.Enable([]string{socketService}); err != nil {
				return err
			}

			switch app.DaemonScope {
			case snap.SystemDaemon:
				timings.Run(tm, "start-system-socket-service", fmt.Sprintf("start system socket service %q", socketService), func(nested timings.Measurer) {
					err = sysd.Start([]string{socketService})
				})
			case snap.UserDaemon:
				timings.Run(tm, "start-user-socket-service", fmt.Sprintf("start user socket service %q", socketService), func(nested timings.Measurer) {
					err = startUserServices(cli, inter, socketService)
				})
			}
			if err != nil {
				return err
			}
		}

		if app.Timer != nil {
			timerService := filepath.Base(app.Timer.File())
			// enable the timer
			if err = sysd.Enable([]string{timerService}); err != nil {
				return err
			}

			switch app.DaemonScope {
			case snap.SystemDaemon:
				timings.Run(tm, "start-system-timer-service", fmt.Sprintf("start system timer service %q", timerService), func(nested timings.Measurer) {
					err = sysd.Start([]string{timerService})
				})
			case snap.UserDaemon:
				timings.Run(tm, "start-user-timer-service", fmt.Sprintf("start user timer service %q", timerService), func(nested timings.Measurer) {
					err = startUserServices(cli, inter, timerService)
				})
			}
			if err != nil {
				return err
			}
		}
	}

	for _, srv := range systemServices {
		// starting all services at once does not create a single
		// transaction, but instead spawns multiple jobs, make sure the
		// services started in the original order by bring them up one
		// by one, see:
		// https://github.com/systemd/systemd/issues/8102
		// https://lists.freedesktop.org/archives/systemd-devel/2018-January/040152.html
		timings.Run(tm, "start-service", fmt.Sprintf("start service %q", srv), func(nested timings.Measurer) {
			err = systemSysd.Start([]string{srv})
		})
		if err != nil {
			// cleanup was set up by iterating over apps
			return err
		}
	}

	if len(userServices) != 0 {
		timings.Run(tm, "start-user-services", "start user services", func(nested timings.Measurer) {
			err = startUserServices(cli, inter, userServices...)
		})
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

	// QuotaGroup is the quota group for all services in the specified snap.
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
func EnsureSnapServices(snaps map[*snap.Info]*SnapServiceOptions, opts *EnsureSnapServicesOptions, observeChange ObserveChangeCallback, inter interacter) (err error) {
	// note, sysd is not used when preseeding
	sysd := systemd.New(systemd.SystemMode, inter)

	if opts == nil {
		opts = &EnsureSnapServicesOptions{}
	}

	// we only consider the global EnsureSnapServicesOptions to decide if we
	// are preseeding or not to reduce confusion about which set of options
	// determines whether we are preseeding or not during the ensure operation
	preseeding := opts.Preseeding

	// modifiedUnitsPreviousState is the set of units that were modified and the previous
	// state of the unit before modification that we can roll back to if there
	// are any issues.
	// note that the rollback is best effort, if we are rebooted in the middle,
	// there is no guarantee about the state of files, some may have been
	// updated and some may have been rolled back, higher level tasks/changes
	// should have do/undo handlers to properly handle the case where this
	// function is interrupted midway
	modifiedUnitsPreviousState := make(map[string]*osutil.MemoryFileState)
	var modifiedSystem, modifiedUser bool

	defer func() {
		if err == nil {
			return
		}
		for file, state := range modifiedUnitsPreviousState {
			if state == nil {
				// we don't have anything to rollback to, so just remove the
				// file
				if e := os.Remove(file); e != nil {
					inter.Notify(fmt.Sprintf("while trying to remove %s due to previous failure: %v", file, e))
				}
			} else {
				// rollback the file to the previous state
				if e := osutil.EnsureFileState(file, state); e != nil {
					inter.Notify(fmt.Sprintf("while trying to rollback %s due to previous failure: %v", file, e))
				}
			}
		}
		if modifiedSystem && !preseeding {
			if e := sysd.DaemonReload(); e != nil {
				inter.Notify(fmt.Sprintf("while trying to perform systemd daemon-reload due to previous failure: %v", e))
			}
		}
		if modifiedUser && !preseeding {
			if e := userDaemonReload(); e != nil {
				inter.Notify(fmt.Sprintf("while trying to perform user systemd daemon-reload due to previous failure: %v", e))
			}
		}
	}()

	handleFileModification := func(app *snap.AppInfo, unitType string, name, path string, content []byte) error {
		old, modifiedFile, err := tryFileUpdate(path, content)
		if err != nil {
			return err
		}

		if modifiedFile {
			if observeChange != nil {
				var oldContent []byte
				if old != nil {
					oldContent = old.Content
				}
				observeChange(app, nil, unitType, name, string(oldContent), string(content))
			}
			modifiedUnitsPreviousState[path] = old

			// also mark that we need to reload either the system or
			// user instance of systemd
			switch app.DaemonScope {
			case snap.SystemDaemon:
				modifiedSystem = true
			case snap.UserDaemon:
				modifiedUser = true
			}
		}

		return nil
	}

	neededQuotaGrps := &quota.QuotaGroupSet{}

	for s, snapSvcOpts := range snaps {
		if s.Type() == snap.TypeSnapd {
			return fmt.Errorf("internal error: adding explicit services for snapd snap is unexpected")
		}

		// always use RequireMountedSnapdSnap options from the global options
		genServiceOpts := &AddSnapServicesOptions{
			RequireMountedSnapdSnap: opts.RequireMountedSnapdSnap,
		}
		if snapSvcOpts != nil {
			// and if there are per-snap options specified, use that for
			// VitalityRank
			genServiceOpts.VitalityRank = snapSvcOpts.VitalityRank
			genServiceOpts.QuotaGroup = snapSvcOpts.QuotaGroup

			if snapSvcOpts.QuotaGroup != nil {
				if err := neededQuotaGrps.AddAllNecessaryGroups(snapSvcOpts.QuotaGroup); err != nil {
					// this error can basically only be a circular reference
					// in the quota group tree
					return err
				}
			}
		}
		// note that the Preseeding option is not used here at all

		for _, app := range s.Apps {
			if !app.IsService() {
				continue
			}

			// create services first; this doesn't trigger systemd

			// Generate new service file state
			path := app.ServiceFile()
			content, err := generateSnapServiceFile(app, genServiceOpts)
			if err != nil {
				return err
			}

			if err := handleFileModification(app, "service", app.Name, path, content); err != nil {
				return err
			}

			// Generate systemd .socket files if needed
			socketFiles, err := generateSnapSocketFiles(app)
			if err != nil {
				return err
			}
			for name, content := range socketFiles {
				path := app.Sockets[name].File()
				if err := handleFileModification(app, "socket", name, path, content); err != nil {
					return err
				}
			}

			if app.Timer != nil {
				content, err := generateSnapTimerFile(app)
				if err != nil {
					return err
				}
				path := app.Timer.File()
				if err := handleFileModification(app, "timer", "", path, content); err != nil {
					return err
				}
			}
		}
	}

	handleSliceModification := func(grp *quota.Group, path string, content []byte) error {
		old, modifiedFile, err := tryFileUpdate(path, content)
		if err != nil {
			return err
		}

		if modifiedFile {
			if observeChange != nil {
				var oldContent []byte
				if old != nil {
					oldContent = old.Content
				}
				observeChange(nil, grp, "slice", grp.Name, string(oldContent), string(content))
			}

			modifiedUnitsPreviousState[path] = old

			// also mark that we need to reload the system instance of systemd
			// TODO: also handle reloading the user instance of systemd when
			// needed
			modifiedSystem = true
		}

		return nil
	}

	// now make sure that all of the slice units exist
	for _, grp := range neededQuotaGrps.AllQuotaGroups() {
		content := generateGroupSliceFile(grp)

		sliceFileName := grp.SliceFileName()
		path := filepath.Join(dirs.SnapServicesDir, sliceFileName)
		if err := handleSliceModification(grp, path, content); err != nil {
			return err
		}
	}

	if !preseeding {
		if modifiedSystem {
			if err = sysd.DaemonReload(); err != nil {
				return err
			}
		}
		if modifiedUser {
			if err = userDaemonReload(); err != nil {
				return err
			}
		}
	}

	return nil
}

// AddSnapServicesOptions is a struct for controlling the generated service
// definition for a snap service.
type AddSnapServicesOptions struct {
	// VitalityRank is the rank of all services in the specified snap used by
	// the OOM killer when OOM conditions are reached.
	VitalityRank int

	// QuotaGroup is the quota group for all services in the specified snap.
	QuotaGroup *quota.Group

	// RequireMountedSnapdSnap is whether the generated units should depend on
	// the snapd snap being mounted, this is specific to systems like UC18 and
	// UC20 which have the snapd snap and need to have units generated
	RequireMountedSnapdSnap bool

	// Preseeding is whether the system is currently being preseeded, in which
	// case there is not a running systemd for EnsureSnapServicesOptions to
	// issue commands like systemctl daemon-reload to.
	Preseeding bool
}

// AddSnapServices adds service units for the applications from the snap which
// are services. The services do not get enabled or started.
func AddSnapServices(s *snap.Info, opts *AddSnapServicesOptions, inter interacter) error {
	m := map[*snap.Info]*SnapServiceOptions{
		s: {},
	}
	ensureOpts := &EnsureSnapServicesOptions{}
	if opts != nil {
		// set the per-snap service options
		m[s].VitalityRank = opts.VitalityRank
		m[s].QuotaGroup = opts.QuotaGroup

		// copy the globally applicable opts from AddSnapServicesOptions to
		// EnsureSnapServicesOptions, since those options override the per-snap opts
		// we put in the map argument
		ensureOpts.Preseeding = opts.Preseeding
		ensureOpts.RequireMountedSnapdSnap = opts.RequireMountedSnapdSnap
	}

	return EnsureSnapServices(m, ensureOpts, nil, inter)
}

// StopServicesFlags carries extra flags for StopServices.
type StopServicesFlags struct {
	Disable bool
}

// StopServices stops and optionally disables service units for the applications
// from the snap which are services.
func StopServices(apps []*snap.AppInfo, flags *StopServicesFlags, reason snap.ServiceStopReason, inter interacter, tm timings.Measurer) error {
	sysd := systemd.New(systemd.SystemMode, inter)
	if flags == nil {
		flags = &StopServicesFlags{}
	}

	if reason != snap.StopReasonOther {
		logger.Debugf("StopServices called for %q, reason: %v", apps, reason)
	} else {
		logger.Debugf("StopServices called for %q", apps)
	}
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

		var err error
		timings.Run(tm, "stop-service", fmt.Sprintf("stop service %q", app.ServiceName()), func(nested timings.Measurer) {
			err = stopService(sysd, app, inter)
			if err == nil && flags.Disable {
				err = sysd.Disable([]string{app.ServiceName()})
			}
		})
		if err != nil {
			return err
		}

		// ensure the service is really stopped on remove regardless
		// of stop-mode
		if reason == snap.StopReasonRemove && !app.StopMode.KillAll() && app.DaemonScope == snap.SystemDaemon {
			// FIXME: make this smarter and avoid the killWait
			//        delay if not needed (i.e. if all processes
			//        have died)
			sysd.Kill(app.ServiceName(), "TERM", "all")
			time.Sleep(killWait)
			sysd.Kill(app.ServiceName(), "KILL", "")
		}
	}
	return nil
}

// ServicesEnableState returns a map of service names from the given snap,
// together with their enable/disable status.
func ServicesEnableState(s *snap.Info, inter interacter) (map[string]bool, error) {
	sysd := systemd.New(systemd.SystemMode, inter)

	// loop over all services in the snap, querying systemd for the current
	// systemd state of the snaps
	snapSvcsState := make(map[string]bool, len(s.Apps))
	for name, app := range s.Apps {
		if !app.IsService() {
			continue
		}
		// FIXME: handle user daemons
		if app.DaemonScope != snap.SystemDaemon {
			continue
		}
		state, err := sysd.IsEnabled(app.ServiceName())
		if err != nil {
			return nil, err
		}
		snapSvcsState[name] = state
	}
	return snapSvcsState, nil
}

// RemoveQuotaGroup ensures that the slice file for a quota group is removed. It
// assumes that the slice corresponding to the group is not in use anymore by
// any services or sub-groups of the group when it is invoked. To remove a group
// with sub-groups, one must remove all the sub-groups first.
// This function is idempotent, if the slice file doesn't exist no error is
// returned.
func RemoveQuotaGroup(grp *quota.Group, inter interacter) error {
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
func RemoveSnapServices(s *snap.Info, inter interacter) error {
	if s.Type() == snap.TypeSnapd {
		return fmt.Errorf("internal error: removing explicit services for snapd snap is unexpected")
	}
	systemSysd := systemd.New(systemd.SystemMode, inter)
	userSysd := systemd.New(systemd.GlobalUserMode, inter)
	var removedSystem, removedUser bool

	for _, app := range s.Apps {
		if !app.IsService() || !osutil.FileExists(app.ServiceFile()) {
			continue
		}

		var sysd systemd.Systemd
		switch app.DaemonScope {
		case snap.SystemDaemon:
			sysd = systemSysd
			removedSystem = true
		case snap.UserDaemon:
			sysd = userSysd
			removedUser = true
		}
		serviceName := filepath.Base(app.ServiceFile())

		for _, socket := range app.Sockets {
			path := socket.File()
			socketServiceName := filepath.Base(path)
			if err := sysd.Disable([]string{socketServiceName}); err != nil {
				return err
			}

			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				logger.Noticef("Failed to remove socket file %q for %q: %v", path, serviceName, err)
			}
		}

		if app.Timer != nil {
			path := app.Timer.File()

			timerName := filepath.Base(path)
			if err := sysd.Disable([]string{timerName}); err != nil {
				return err
			}

			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				logger.Noticef("Failed to remove timer file %q for %q: %v", path, serviceName, err)
			}
		}

		if err := sysd.Disable([]string{serviceName}); err != nil {
			return err
		}

		if err := os.Remove(app.ServiceFile()); err != nil && !os.IsNotExist(err) {
			logger.Noticef("Failed to remove service file for %q: %v", serviceName, err)
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

func genServiceNames(snap *snap.Info, appNames []string) []string {
	names := make([]string, 0, len(appNames))

	for _, name := range appNames {
		if app := snap.Apps[name]; app != nil {
			names = append(names, app.ServiceName())
		}
	}
	return names
}

// TODO: this should not accept AddSnapServicesOptions, it should use some other
// subset of options, specifically it should not accept Preseeding as an option
// here
func genServiceFile(appInfo *snap.AppInfo, opts *AddSnapServicesOptions) ([]byte, error) {
	if opts == nil {
		opts = &AddSnapServicesOptions{}
	}

	// assemble all of the service directive snippets for all interfaces that
	// this service needs to include in the generated systemd file

	// use an ordered set to ensure we don't duplicate any keys from interfaces
	// that specify the same snippet

	// TODO: maybe we should error if multiple interfaces specify different
	// values for the same directive, otherwise one of them will overwrite the
	// other? What happens right now is that the snippet from the plug that
	// comes last will win in the case of directives that can have only one
	// value, but for some directives, systemd combines their values into a
	// list.
	ifaceServiceSnippets := &strutil.OrderedSet{}

	for _, plug := range appInfo.Plugs {
		iface, err := interfaces.ByName(plug.Interface)
		if err != nil {
			return nil, fmt.Errorf("error processing plugs while generating service unit for %v: %v", appInfo.SecurityTag(), err)
		}
		snips, err := interfaces.PermanentPlugServiceSnippets(iface, plug)
		if err != nil {
			return nil, fmt.Errorf("error processing plugs while generating service unit for %v: %v", appInfo.SecurityTag(), err)
		}
		for _, snip := range snips {
			ifaceServiceSnippets.Put(snip)
		}
	}

	// join the service snippets into one string to be included in the
	// template
	ifaceSpecifiedServiceSnippet := strings.Join(ifaceServiceSnippets.Items(), "\n")

	serviceTemplate := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application {{.App.Snap.InstanceName}}.{{.App.Name}}
{{- if .MountUnit }}
Requires={{.MountUnit}}
{{- end }}
{{- if .PrerequisiteTarget}}
Wants={{.PrerequisiteTarget}}
{{- end}}
{{- if .After}}
After={{ stringsJoin .After " " }}
{{- end}}
{{- if .Before}}
Before={{ stringsJoin .Before " "}}
{{- end}}
{{- if .CoreMountedSnapdSnapDep}}
Wants={{ stringsJoin .CoreMountedSnapdSnapDep " "}}
After={{ stringsJoin .CoreMountedSnapdSnapDep " "}}
{{- end}}
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart={{.App.LauncherCommand}}
SyslogIdentifier={{.App.Snap.InstanceName}}.{{.App.Name}}
Restart={{.Restart}}
{{- if .App.RestartDelay}}
RestartSec={{.App.RestartDelay.Seconds}}
{{- end}}
WorkingDirectory={{.WorkingDir}}
{{- if .App.StopCommand}}
ExecStop={{.App.LauncherStopCommand}}
{{- end}}
{{- if .App.ReloadCommand}}
ExecReload={{.App.LauncherReloadCommand}}
{{- end}}
{{- if .App.PostStopCommand}}
ExecStopPost={{.App.LauncherPostStopCommand}}
{{- end}}
{{- if .StopTimeout}}
TimeoutStopSec={{.StopTimeout.Seconds}}
{{- end}}
{{- if .StartTimeout}}
TimeoutStartSec={{.StartTimeout.Seconds}}
{{- end}}
Type={{.App.Daemon}}
{{- if .Remain}}
RemainAfterExit={{.Remain}}
{{- end}}
{{- if .BusName}}
BusName={{.BusName}}
{{- end}}
{{- if .App.WatchdogTimeout}}
WatchdogSec={{.App.WatchdogTimeout.Seconds}}
{{- end}}
{{- if .KillMode}}
KillMode={{.KillMode}}
{{- end}}
{{- if .KillSignal}}
KillSignal={{.KillSignal}}
{{- end}}
{{- if .OOMAdjustScore }}
OOMScoreAdjust={{.OOMAdjustScore}}
{{- end}}
{{- if .InterfaceServiceSnippets}}
{{.InterfaceServiceSnippets}}
{{- end}}
{{- if .SliceUnit}}
Slice={{.SliceUnit}}
{{- end}}
{{- if not (or .App.Sockets .App.Timer .App.ActivatesOn) }}

[Install]
WantedBy={{.ServicesTarget}}
{{- end}}
`
	var templateOut bytes.Buffer
	tmpl := template.New("service-wrapper")
	tmpl.Funcs(template.FuncMap{
		"stringsJoin": strings.Join,
	})
	t := template.Must(tmpl.Parse(serviceTemplate))

	restartCond := appInfo.RestartCond.String()
	if restartCond == "" {
		restartCond = snap.RestartOnFailure.String()
	}

	// use score -900+vitalityRank, where vitalityRank starts at 1
	// and considering snapd itself has OOMScoreAdjust=-900
	const baseOOMAdjustScore = -900
	var oomAdjustScore int
	if opts.VitalityRank > 0 {
		oomAdjustScore = baseOOMAdjustScore + opts.VitalityRank
	}

	var remain string
	if appInfo.Daemon == "oneshot" {
		// any restart condition other than "no" is invalid for oneshot daemons
		restartCond = "no"
		// If StopExec is present for a oneshot service than we also need
		// RemainAfterExit=yes
		if appInfo.StopCommand != "" {
			remain = "yes"
		}
	}
	var killMode string
	if !appInfo.StopMode.KillAll() {
		killMode = "process"
	}

	var busName string
	if appInfo.Daemon == "dbus" {
		busName = appInfo.BusName
		if busName == "" && len(appInfo.ActivatesOn) != 0 {
			slot := appInfo.ActivatesOn[len(appInfo.ActivatesOn)-1]
			if err := slot.Attr("name", &busName); err != nil {
				// This should be impossible for a valid AppInfo
				logger.Noticef("Cannot get 'name' attribute of dbus slot %q: %v", slot.Name, err)
			}
		}
	}

	wrapperData := struct {
		App *snap.AppInfo

		Restart                  string
		WorkingDir               string
		StopTimeout              time.Duration
		StartTimeout             time.Duration
		ServicesTarget           string
		PrerequisiteTarget       string
		MountUnit                string
		Remain                   string
		KillMode                 string
		KillSignal               string
		OOMAdjustScore           int
		BusName                  string
		Before                   []string
		After                    []string
		InterfaceServiceSnippets string
		SliceUnit                string

		Home    string
		EnvVars string

		CoreMountedSnapdSnapDep []string
	}{
		App: appInfo,

		InterfaceServiceSnippets: ifaceSpecifiedServiceSnippet,

		Restart:        restartCond,
		StopTimeout:    serviceStopTimeout(appInfo),
		StartTimeout:   time.Duration(appInfo.StartTimeout),
		Remain:         remain,
		KillMode:       killMode,
		KillSignal:     appInfo.StopMode.KillSignal(),
		OOMAdjustScore: oomAdjustScore,
		BusName:        busName,

		Before: genServiceNames(appInfo.Snap, appInfo.Before),
		After:  genServiceNames(appInfo.Snap, appInfo.After),

		// systemd runs as PID 1 so %h will not work.
		Home: "/root",
	}
	switch appInfo.DaemonScope {
	case snap.SystemDaemon:
		wrapperData.ServicesTarget = systemd.ServicesTarget
		wrapperData.PrerequisiteTarget = systemd.PrerequisiteTarget
		wrapperData.MountUnit = filepath.Base(systemd.MountUnitPath(appInfo.Snap.MountDir()))
		wrapperData.WorkingDir = appInfo.Snap.DataDir()
		wrapperData.After = append(wrapperData.After, "snapd.apparmor.service")
	case snap.UserDaemon:
		wrapperData.ServicesTarget = systemd.UserServicesTarget
		// FIXME: ideally use UserDataDir("%h"), but then the
		// unit fails if the directory doesn't exist.
		wrapperData.WorkingDir = appInfo.Snap.DataDir()
	default:
		panic("unknown snap.DaemonScope")
	}

	// check the quota group slice
	if opts.QuotaGroup != nil {
		wrapperData.SliceUnit = opts.QuotaGroup.SliceFileName()
	}

	// Add extra "After" targets
	if wrapperData.PrerequisiteTarget != "" {
		wrapperData.After = append([]string{wrapperData.PrerequisiteTarget}, wrapperData.After...)
	}
	if wrapperData.MountUnit != "" {
		wrapperData.After = append([]string{wrapperData.MountUnit}, wrapperData.After...)
	}

	if opts.RequireMountedSnapdSnap {
		// on core 18+ systems, the snapd tooling is exported
		// into the host system via a special mount unit, which
		// also adds an implicit dependency on the snapd snap
		// mount thus /usr/bin/snap points
		wrapperData.CoreMountedSnapdSnapDep = []string{SnapdToolingMountUnit}
	}

	if err := t.Execute(&templateOut, wrapperData); err != nil {
		// this can never happen, except we forget a variable
		logger.Panicf("Unable to execute template: %v", err)
	}

	return templateOut.Bytes(), nil
}

func genServiceSocketFile(appInfo *snap.AppInfo, socketName string) []byte {
	socketTemplate := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Socket {{.SocketName}} for snap application {{.App.Snap.InstanceName}}.{{.App.Name}}
{{- if .MountUnit}}
Requires={{.MountUnit}}
After={{.MountUnit}}
{{- end}}
X-Snappy=yes

[Socket]
Service={{.ServiceFileName}}
FileDescriptorName={{.SocketInfo.Name}}
ListenStream={{.ListenStream}}
{{- if .SocketInfo.SocketMode}}
SocketMode={{.SocketInfo.SocketMode | printf "%04o"}}
{{- end}}

[Install]
WantedBy={{.SocketsTarget}}
`
	var templateOut bytes.Buffer
	t := template.Must(template.New("socket-wrapper").Parse(socketTemplate))

	socket := appInfo.Sockets[socketName]
	listenStream := renderListenStream(socket)
	wrapperData := struct {
		App             *snap.AppInfo
		ServiceFileName string
		SocketsTarget   string
		MountUnit       string
		SocketName      string
		SocketInfo      *snap.SocketInfo
		ListenStream    string
	}{
		App:             appInfo,
		ServiceFileName: filepath.Base(appInfo.ServiceFile()),
		SocketsTarget:   systemd.SocketsTarget,
		SocketName:      socketName,
		SocketInfo:      socket,
		ListenStream:    listenStream,
	}
	switch appInfo.DaemonScope {
	case snap.SystemDaemon:
		wrapperData.MountUnit = filepath.Base(systemd.MountUnitPath(appInfo.Snap.MountDir()))
	case snap.UserDaemon:
		// nothing
	default:
		panic("unknown snap.DaemonScope")
	}

	if err := t.Execute(&templateOut, wrapperData); err != nil {
		// this can never happen, except we forget a variable
		logger.Panicf("Unable to execute template: %v", err)
	}

	return templateOut.Bytes()
}

func generateSnapSocketFiles(app *snap.AppInfo) (map[string][]byte, error) {
	if err := snap.ValidateApp(app); err != nil {
		return nil, err
	}

	socketFiles := make(map[string][]byte)
	for name := range app.Sockets {
		socketFiles[name] = genServiceSocketFile(app, name)
	}
	return socketFiles, nil
}

func renderListenStream(socket *snap.SocketInfo) string {
	s := socket.App.Snap
	listenStream := socket.ListenStream
	switch socket.App.DaemonScope {
	case snap.SystemDaemon:
		listenStream = strings.Replace(listenStream, "$SNAP_DATA", s.DataDir(), -1)
		// TODO: when we support User/Group in the generated
		// systemd unit, adjust this accordingly
		serviceUserUid := sys.UserID(0)
		runtimeDir := s.UserXdgRuntimeDir(serviceUserUid)
		listenStream = strings.Replace(listenStream, "$XDG_RUNTIME_DIR", runtimeDir, -1)
		listenStream = strings.Replace(listenStream, "$SNAP_COMMON", s.CommonDataDir(), -1)
	case snap.UserDaemon:
		// TODO: use SnapDirOpts here. User daemons are also an experimental
		// feature so, for simplicity, we can not pass opts here for now
		listenStream = strings.Replace(listenStream, "$SNAP_USER_DATA", s.UserDataDir("%h", nil), -1)
		listenStream = strings.Replace(listenStream, "$SNAP_USER_COMMON", s.UserCommonDataDir("%h", nil), -1)
		// FIXME: find some way to share code with snap.UserXdgRuntimeDir()
		listenStream = strings.Replace(listenStream, "$XDG_RUNTIME_DIR", fmt.Sprintf("%%t/snap.%s", s.InstanceName()), -1)
	default:
		panic("unknown snap.DaemonScope")
	}
	return listenStream
}

func generateSnapTimerFile(app *snap.AppInfo) ([]byte, error) {
	timerTemplate := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Timer {{.TimerName}} for snap application {{.App.Snap.InstanceName}}.{{.App.Name}}
{{- if .MountUnit}}
Requires={{.MountUnit}}
After={{.MountUnit}}
{{- end}}
X-Snappy=yes

[Timer]
Unit={{.ServiceFileName}}
{{ range .Schedules }}OnCalendar={{ . }}
{{ end }}
[Install]
WantedBy={{.TimersTarget}}
`
	var templateOut bytes.Buffer
	t := template.Must(template.New("timer-wrapper").Parse(timerTemplate))

	timerSchedule, err := timeutil.ParseSchedule(app.Timer.Timer)
	if err != nil {
		return nil, err
	}

	schedules := generateOnCalendarSchedules(timerSchedule)

	wrapperData := struct {
		App             *snap.AppInfo
		ServiceFileName string
		TimersTarget    string
		TimerName       string
		MountUnit       string
		Schedules       []string
	}{
		App:             app,
		ServiceFileName: filepath.Base(app.ServiceFile()),
		TimersTarget:    systemd.TimersTarget,
		TimerName:       app.Name,
		Schedules:       schedules,
	}
	switch app.DaemonScope {
	case snap.SystemDaemon:
		wrapperData.MountUnit = filepath.Base(systemd.MountUnitPath(app.Snap.MountDir()))
	case snap.UserDaemon:
		// nothing
	default:
		panic("unknown snap.DaemonScope")
	}

	if err := t.Execute(&templateOut, wrapperData); err != nil {
		// this can never happen, except we forget a variable
		logger.Panicf("Unable to execute template: %v", err)
	}

	return templateOut.Bytes(), nil
}

func makeAbbrevWeekdays(start time.Weekday, end time.Weekday) []string {
	out := make([]string, 0, 7)
	for w := start; w%7 != (end + 1); w++ {
		out = append(out, time.Weekday(w % 7).String()[0:3])
	}
	return out
}

// daysRange generates a string representing a continuous range between given
// day numbers, which due to compatiblilty with old systemd version uses a
// verbose syntax of x,y,z instead of x..z
func daysRange(start, end uint) string {
	var buf bytes.Buffer
	for i := start; i <= end; i++ {
		buf.WriteString(strconv.FormatInt(int64(i), 10))
		if i < end {
			buf.WriteRune(',')
		}
	}
	return buf.String()
}

// generateOnCalendarSchedules converts a schedule into OnCalendar schedules
// suitable for use in systemd *.timer units using systemd.time(7)
// https://www.freedesktop.org/software/systemd/man/systemd.time.html
// XXX: old systemd versions do not support x..y ranges
func generateOnCalendarSchedules(schedule []*timeutil.Schedule) []string {
	calendarEvents := make([]string, 0, len(schedule))
	for _, sched := range schedule {
		days := make([]string, 0, len(sched.WeekSpans))
		for _, week := range sched.WeekSpans {
			abbrev := strings.Join(makeAbbrevWeekdays(week.Start.Weekday, week.End.Weekday), ",")

			if week.Start.Pos == timeutil.EveryWeek && week.End.Pos == timeutil.EveryWeek {
				// eg: mon, mon-fri, fri-mon
				days = append(days, fmt.Sprintf("%s *-*-*", abbrev))
				continue
			}
			// examples:
			// mon1 - Mon *-*-1..7 (Monday during the first 7 days)
			// fri1 - Fri *-*-1..7 (Friday during the first 7 days)

			// entries below will make systemd timer expire more
			// frequently than the schedule suggests, however snap
			// runner evaluates current time and gates the actual
			// action
			//
			// mon1-tue - *-*-1..7 *-*-8 (anchored at first
			// Monday; Monday happens during the 7 days,
			// Tuesday can possibly happen on the 8th day if
			// the month started on Tuesday)
			//
			// mon-tue1 - *-*~1 *-*-1..7 (anchored at first
			// Tuesday; matching Monday can happen on the
			// last day of previous month if Tuesday is the
			// 1st)
			//
			// mon5-tue - *-*~1..7 *-*-1 (anchored at last
			// Monday, the matching Tuesday can still happen
			// within the last 7 days, or on the 1st of the
			// next month)
			//
			// fri4-mon - *-*-22-31 *-*-1..7 (anchored at 4th
			// Friday, can span onto the next month, extreme case in
			// February when 28th is Friday)
			//
			// XXX: since old versions of systemd, eg. 229 available
			// in 16.04 does not support x..y ranges, days need to
			// be enumerated like so:
			// Mon *-*-1..7 -> Mon *-*-1,2,3,4,5,6,7
			//
			// XXX: old systemd versions do not support the last n
			// days syntax eg, *-*~1, thus the range needs to be
			// generated in more verbose way like so:
			// Mon *-*~1..7 -> Mon *-*-22,23,24,25,26,27,28,29,30,31
			// (22-28 is the last week, but the month can have
			// anywhere from 28 to 31 days)
			//
			startPos := week.Start.Pos
			endPos := startPos
			if !week.AnchoredAtStart() {
				startPos = week.End.Pos
				endPos = startPos
			}
			startDay := (startPos-1)*7 + 1
			endDay := (endPos) * 7

			if week.IsSingleDay() {
				// single day, can use the 'weekday' filter
				if startPos == timeutil.LastWeek {
					// last week of a month, which can be
					// 22-28 in case of February, while
					// month can have between 28 and 31 days
					days = append(days,
						fmt.Sprintf("%s *-*-%s", abbrev, daysRange(22, 31)))
				} else {
					days = append(days,
						fmt.Sprintf("%s *-*-%s", abbrev, daysRange(startDay, endDay)))
				}
				continue
			}

			if week.AnchoredAtStart() {
				// explore the edge cases first
				switch startPos {
				case timeutil.LastWeek:
					// starts in the last week of the month and
					// possibly spans into the first week of the
					// next month;
					// month can have between 28 and 31
					// days
					days = append(days,
						// trailing 29-31 that are not part of a full week
						fmt.Sprintf("*-*-%s", daysRange(29, 31)),
						fmt.Sprintf("*-*-%s", daysRange(1, 7)))
				case 4:
					// a range in the 4th week can span onto
					// the next week, which is either 28-31
					// or in extreme case (eg. February with
					// 28 days) 1-7 of the next month
					days = append(days,
						// trailing 29-31 that are not part of a full week
						fmt.Sprintf("*-*-%s", daysRange(29, 31)),
						fmt.Sprintf("*-*-%s", daysRange(1, 7)))
				default:
					// can possibly spill into the next week
					days = append(days,
						fmt.Sprintf("*-*-%s", daysRange(startDay+7, endDay+7)))
				}

				if startDay < 28 {
					days = append(days,
						fmt.Sprintf("*-*-%s", daysRange(startDay, endDay)))
				} else {
					// from the end of the month
					days = append(days,
						fmt.Sprintf("*-*-%s", daysRange(startDay-7, endDay-7)))
				}
			} else {
				switch endPos {
				case timeutil.LastWeek:
					// month can have between 28 and 31
					// days, add trailing 29-31 that are not
					// part of a full week
					days = append(days, fmt.Sprintf("*-*-%s", daysRange(29, 31)))
				case 1:
					// possibly spans from the last week of the
					// previous month and ends in the first week of
					// current month
					days = append(days, fmt.Sprintf("*-*-%s", daysRange(22, 31)))
				default:
					// can possibly spill into the previous week
					days = append(days,
						fmt.Sprintf("*-*-%s", daysRange(startDay-7, endDay-7)))
				}
				if endDay < 28 {
					days = append(days,
						fmt.Sprintf("*-*-%s", daysRange(startDay, endDay)))
				} else {
					days = append(days,
						fmt.Sprintf("*-*-%s", daysRange(startDay-7, endDay-7)))
				}
			}
		}

		if len(days) == 0 {
			// no weekday spec, meaning the timer runs every day
			days = []string{"*-*-*"}
		}

		startTimes := make([]string, 0, len(sched.ClockSpans))
		for _, clocks := range sched.ClockSpans {
			// use expanded clock spans
			for _, span := range clocks.ClockSpans() {
				when := span.Start
				if span.Spread {
					length := span.End.Sub(span.Start)
					if length < 0 {
						// span Start wraps around, so we have '00:00.Sub(23:45)'
						length = -length
					}
					if length > 5*time.Minute {
						// replicate what timeutil.Next() does
						// and cut some time at the end of the
						// window so that events do not happen
						// directly one after another
						length -= 5 * time.Minute
					}
					when = when.Add(randutil.RandomDuration(length))
				}
				if when.Hour == 24 {
					// 24:00 for us means the other end of
					// the day, for systemd we need to
					// adjust it to the 0-23 hour range
					when.Hour -= 24
				}

				startTimes = append(startTimes, when.String())
			}
		}

		for _, day := range days {
			if len(startTimes) == 0 {
				// current schedule is days only
				calendarEvents = append(calendarEvents, day)
				continue
			}

			for _, startTime := range startTimes {
				calendarEvents = append(calendarEvents, fmt.Sprintf("%s %s", day, startTime))
			}
		}
	}
	return calendarEvents
}

type RestartServicesFlags struct {
	Reload bool
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
func RestartServices(svcs []*snap.AppInfo, explicitServices []string,
	flags *RestartServicesFlags, inter interacter, tm timings.Measurer) error {
	sysd := systemd.New(systemd.SystemMode, inter)

	unitNames := make([]string, 0, len(svcs))
	for _, srv := range svcs {
		// they're *supposed* to be all services, but checking doesn't hurt
		if !srv.IsService() {
			continue
		}
		unitNames = append(unitNames, srv.ServiceName())
	}

	unitStatuses, err := sysd.Status(unitNames)
	if err != nil {
		return err
	}

	for _, unit := range unitStatuses {
		// If the unit was explicitly mentioned in the command line, restart it
		// even if it is disabled; otherwise, we only restart units which are
		// currently running. Reference:
		// https://forum.snapcraft.io/t/command-line-interface-to-manipulate-services/262/47
		if !unit.Active && !strutil.ListContains(explicitServices, unit.Name) {
			continue
		}

		var err error
		timings.Run(tm, "restart-service", fmt.Sprintf("restart service %s", unit.Name), func(nested timings.Measurer) {
			if flags != nil && flags.Reload {
				err = sysd.ReloadOrRestart(unit.Name)
			} else {
				// note: stop followed by start, not just 'restart'
				err = sysd.Restart([]string{unit.Name}, 5*time.Second)
			}
		})
		if err != nil {
			// there is nothing we can do about failed service
			return err
		}
	}
	return nil
}

// QueryDisabledServices returns a list of all currently disabled snap services
// in the snap.
func QueryDisabledServices(info *snap.Info, pb progress.Meter) ([]string, error) {
	// save the list of services that are in the disabled state before unlinking
	// and thus removing the snap services
	snapSvcStates, err := ServicesEnableState(info, pb)
	if err != nil {
		return nil, err
	}

	disabledSnapSvcs := []string{}
	// add all disabled services to the list
	for svc, isEnabled := range snapSvcStates {
		if !isEnabled {
			disabledSnapSvcs = append(disabledSnapSvcs, svc)
		}
	}

	// sort for easier testing
	sort.Strings(disabledSnapSvcs)

	return disabledSnapSvcs, nil
}
