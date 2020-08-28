// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

// Package snapstate implements the manager and state aspects responsible for the installation and removal of snaps.
package snapstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
)

// control flags for doInstall
const (
	skipConfigure = 1 << iota
)

// control flags for "Configure()"
const (
	IgnoreHookError = 1 << iota
	TrackHookError
	UseConfigDefaults
)

const (
	DownloadAndChecksDoneEdge = state.TaskSetEdge("download-and-checks-done")
	BeginEdge                 = state.TaskSetEdge("begin")
	BeforeHooksEdge           = state.TaskSetEdge("before-hooks")
	HooksEdge                 = state.TaskSetEdge("hooks")
)

var ErrNothingToDo = errors.New("nothing to do")

var osutilCheckFreeSpace = osutil.CheckFreeSpace

type ErrInsufficientSpace struct {
	// Path is the filesystem path checked for available disk space
	Path string
	// Snaps affected by the failing operation
	Snaps []string
	// Message is optional, otherwise one is composed from the other information
	Message string
}

func (e *ErrInsufficientSpace) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if len(e.Snaps) > 0 {
		snaps := strings.Join(e.Snaps, ", ")
		return fmt.Sprintf("insufficient space in %q to perform operation for the following snaps: %s", e.Path, snaps)
	}
	return fmt.Sprintf("insufficient space in %q", e.Path)
}

func isParallelInstallable(snapsup *SnapSetup) error {
	if snapsup.InstanceKey == "" {
		return nil
	}
	if snapsup.Type == snap.TypeApp {
		return nil
	}
	return fmt.Errorf("cannot install snap of type %v as %q", snapsup.Type, snapsup.InstanceName())
}

func optedIntoSnapdSnap(st *state.State) (bool, error) {
	tr := config.NewTransaction(st)
	experimentalAllowSnapd, err := features.Flag(tr, features.SnapdSnap)
	if err != nil && !config.IsNoOption(err) {
		return false, err
	}
	return experimentalAllowSnapd, nil
}

func doInstall(st *state.State, snapst *SnapState, snapsup *SnapSetup, flags int, fromChange string, inUseCheck func(snap.Type) (boot.InUseFunc, error)) (*state.TaskSet, error) {
	// NB: we should strive not to need or propagate deviceCtx
	// here, the resulting effects/changes were not pleasant at
	// one point
	tr := config.NewTransaction(st)
	experimentalRefreshAppAwareness, err := features.Flag(tr, features.RefreshAppAwareness)
	if err != nil && !config.IsNoOption(err) {
		return nil, err
	}

	if snapsup.InstanceName() == "system" {
		return nil, fmt.Errorf("cannot install reserved snap name 'system'")
	}
	if snapst.IsInstalled() && !snapst.Active {
		return nil, fmt.Errorf("cannot update disabled snap %q", snapsup.InstanceName())
	}
	if snapst.IsInstalled() && !snapsup.Flags.Revert {
		if inUseCheck == nil {
			return nil, fmt.Errorf("internal error: doInstall: inUseCheck not provided for refresh")
		}
	}

	if snapsup.Flags.Classic {
		if !release.OnClassic {
			return nil, fmt.Errorf("classic confinement is only supported on classic systems")
		} else if !dirs.SupportsClassicConfinement() {
			return nil, fmt.Errorf(i18n.G("classic confinement requires snaps under /snap or symlink from /snap to %s"), dirs.SnapMountDir)
		}
	}
	if !snapst.IsInstalled() { // install?
		// check that the snap command namespace doesn't conflict with an enabled alias
		if err := checkSnapAliasConflict(st, snapsup.InstanceName()); err != nil {
			return nil, err
		}
	}

	if err := isParallelInstallable(snapsup); err != nil {
		return nil, err
	}

	if err := checkChangeConflictIgnoringOneChange(st, snapsup.InstanceName(), snapst, fromChange); err != nil {
		return nil, err
	}

	if snapst.IsInstalled() {
		// consider also the current revision to set plugs-only hint
		info, err := snapst.CurrentInfo()
		if err != nil {
			return nil, err
		}
		snapsup.PlugsOnly = snapsup.PlugsOnly && (len(info.Slots) == 0)

		if experimentalRefreshAppAwareness {
			// Note that because we are modifying the snap state this block
			// must be located after the conflict check done above.
			if err := inhibitRefresh(st, snapst, info, SoftNothingRunningRefreshCheck); err != nil {
				return nil, err
			}
		}
	}

	ts := state.NewTaskSet()

	targetRevision := snapsup.Revision()
	revisionStr := ""
	if snapsup.SideInfo != nil {
		revisionStr = fmt.Sprintf(" (%s)", targetRevision)
	}

	// check if we already have the revision locally (alters tasks)
	revisionIsLocal := snapst.LastIndex(targetRevision) >= 0

	prereq := st.NewTask("prerequisites", fmt.Sprintf(i18n.G("Ensure prerequisites for %q are available"), snapsup.InstanceName()))
	prereq.Set("snap-setup", snapsup)

	var prepare, prev *state.Task
	fromStore := false
	// if we have a local revision here we go back to that
	if snapsup.SnapPath != "" || revisionIsLocal {
		prepare = st.NewTask("prepare-snap", fmt.Sprintf(i18n.G("Prepare snap %q%s"), snapsup.SnapPath, revisionStr))
	} else {
		fromStore = true
		prepare = st.NewTask("download-snap", fmt.Sprintf(i18n.G("Download snap %q%s from channel %q"), snapsup.InstanceName(), revisionStr, snapsup.Channel))
	}
	prepare.Set("snap-setup", snapsup)
	prepare.WaitFor(prereq)

	tasks := []*state.Task{prereq, prepare}
	addTask := func(t *state.Task) {
		t.Set("snap-setup-task", prepare.ID())
		t.WaitFor(prev)
		tasks = append(tasks, t)
	}
	prev = prepare

	var checkAsserts *state.Task
	if fromStore {
		// fetch and check assertions
		checkAsserts = st.NewTask("validate-snap", fmt.Sprintf(i18n.G("Fetch and check assertions for snap %q%s"), snapsup.InstanceName(), revisionStr))
		addTask(checkAsserts)
		prev = checkAsserts
	}

	// mount
	if !revisionIsLocal {
		mount := st.NewTask("mount-snap", fmt.Sprintf(i18n.G("Mount snap %q%s"), snapsup.InstanceName(), revisionStr))
		addTask(mount)
		prev = mount
	}

	// run refresh hooks when updating existing snap, otherwise run install hook further down.
	runRefreshHooks := (snapst.IsInstalled() && !snapsup.Flags.Revert)
	if runRefreshHooks {
		preRefreshHook := SetupPreRefreshHook(st, snapsup.InstanceName())
		addTask(preRefreshHook)
		prev = preRefreshHook
	}

	if snapst.IsInstalled() {
		// unlink-current-snap (will stop services for copy-data)
		stop := st.NewTask("stop-snap-services", fmt.Sprintf(i18n.G("Stop snap %q services"), snapsup.InstanceName()))
		stop.Set("stop-reason", snap.StopReasonRefresh)
		addTask(stop)
		prev = stop

		removeAliases := st.NewTask("remove-aliases", fmt.Sprintf(i18n.G("Remove aliases for snap %q"), snapsup.InstanceName()))
		addTask(removeAliases)
		prev = removeAliases

		unlink := st.NewTask("unlink-current-snap", fmt.Sprintf(i18n.G("Make current revision for snap %q unavailable"), snapsup.InstanceName()))
		addTask(unlink)
		prev = unlink
	}

	if !release.OnClassic && snapsup.Type == snap.TypeGadget {
		// XXX: gadget update currently for core systems only
		gadgetUpdate := st.NewTask("update-gadget-assets", fmt.Sprintf(i18n.G("Update assets from gadget %q%s"), snapsup.InstanceName(), revisionStr))
		addTask(gadgetUpdate)
		prev = gadgetUpdate
	}

	// copy-data (needs stopped services by unlink)
	if !snapsup.Flags.Revert {
		copyData := st.NewTask("copy-snap-data", fmt.Sprintf(i18n.G("Copy snap %q data"), snapsup.InstanceName()))
		addTask(copyData)
		prev = copyData
	}

	// security
	setupSecurity := st.NewTask("setup-profiles", fmt.Sprintf(i18n.G("Setup snap %q%s security profiles"), snapsup.InstanceName(), revisionStr))
	addTask(setupSecurity)
	prev = setupSecurity

	// finalize (wrappers+current symlink)
	linkSnap := st.NewTask("link-snap", fmt.Sprintf(i18n.G("Make snap %q%s available to the system"), snapsup.InstanceName(), revisionStr))
	addTask(linkSnap)
	prev = linkSnap

	// auto-connections
	autoConnect := st.NewTask("auto-connect", fmt.Sprintf(i18n.G("Automatically connect eligible plugs and slots of snap %q"), snapsup.InstanceName()))
	addTask(autoConnect)
	prev = autoConnect

	// setup aliases
	setAutoAliases := st.NewTask("set-auto-aliases", fmt.Sprintf(i18n.G("Set automatic aliases for snap %q"), snapsup.InstanceName()))
	addTask(setAutoAliases)
	prev = setAutoAliases

	setupAliases := st.NewTask("setup-aliases", fmt.Sprintf(i18n.G("Setup snap %q aliases"), snapsup.InstanceName()))
	addTask(setupAliases)
	prev = setupAliases

	if runRefreshHooks {
		postRefreshHook := SetupPostRefreshHook(st, snapsup.InstanceName())
		addTask(postRefreshHook)
		prev = postRefreshHook
	}

	var installHook *state.Task
	// only run install hook if installing the snap for the first time
	if !snapst.IsInstalled() {
		installHook = SetupInstallHook(st, snapsup.InstanceName())
		addTask(installHook)
		prev = installHook
	}

	// run new services
	startSnapServices := st.NewTask("start-snap-services", fmt.Sprintf(i18n.G("Start snap %q%s services"), snapsup.InstanceName(), revisionStr))
	addTask(startSnapServices)
	prev = startSnapServices

	// Do not do that if we are reverting to a local revision
	if snapst.IsInstalled() && !snapsup.Flags.Revert {
		var retain int
		if err := config.NewTransaction(st).Get("core", "refresh.retain", &retain); err != nil {
			// on classic we only keep 2 copies by default
			if release.OnClassic {
				retain = 2
			} else {
				retain = 3
			}
		}
		retain-- //  we're adding one

		seq := snapst.Sequence
		currentIndex := snapst.LastIndex(snapst.Current)

		// discard everything after "current" (we may have reverted to
		// a previous versions earlier)
		for i := currentIndex + 1; i < len(seq); i++ {
			si := seq[i]
			if si.Revision == targetRevision {
				// but don't discard this one; its' the thing we're switching to!
				continue
			}
			ts := removeInactiveRevision(st, snapsup.InstanceName(), si.SnapID, si.Revision)
			ts.WaitFor(prev)
			tasks = append(tasks, ts.Tasks()...)
			prev = tasks[len(tasks)-1]
		}

		// make sure we're not scheduling the removal of the target
		// revision in the case where the target revision is already in
		// the sequence.
		for i := 0; i < currentIndex; i++ {
			si := seq[i]
			if si.Revision == targetRevision {
				// we do *not* want to removeInactiveRevision of this one
				copy(seq[i:], seq[i+1:])
				seq = seq[:len(seq)-1]
				currentIndex--
			}
		}

		// normal garbage collect
		var inUse boot.InUseFunc
		for i := 0; i <= currentIndex-retain; i++ {
			if inUse == nil {
				var err error
				inUse, err = inUseCheck(snapsup.Type)
				if err != nil {
					return nil, err
				}
			}

			si := seq[i]
			if inUse(snapsup.InstanceName(), si.Revision) {
				continue
			}
			ts := removeInactiveRevision(st, snapsup.InstanceName(), si.SnapID, si.Revision)
			ts.WaitFor(prev)
			tasks = append(tasks, ts.Tasks()...)
			prev = tasks[len(tasks)-1]
		}

		addTask(st.NewTask("cleanup", fmt.Sprintf("Clean up %q%s install", snapsup.InstanceName(), revisionStr)))
	}

	installSet := state.NewTaskSet(tasks...)
	installSet.WaitAll(ts)
	installSet.MarkEdge(prereq, BeginEdge)
	installSet.MarkEdge(setupAliases, BeforeHooksEdge)
	if installHook != nil {
		installSet.MarkEdge(installHook, HooksEdge)
	}
	ts.AddAllWithEdges(installSet)
	if checkAsserts != nil {
		ts.MarkEdge(checkAsserts, DownloadAndChecksDoneEdge)
	}

	if flags&skipConfigure != 0 {
		return installSet, nil
	}

	// we do not support configuration for bases or the "snapd" snap yet
	if snapsup.Type != snap.TypeBase && snapsup.Type != snap.TypeSnapd {
		confFlags := 0
		notCore := snapsup.InstanceName() != "core"
		hasSnapID := snapsup.SideInfo != nil && snapsup.SideInfo.SnapID != ""
		if !snapst.IsInstalled() && hasSnapID && notCore {
			// installation, run configure using the gadget defaults
			// if available, system config defaults (attached to
			// "core") are consumed only during seeding, via an
			// explicit configure step separate from installing
			confFlags |= UseConfigDefaults
		}
		configSet := ConfigureSnap(st, snapsup.InstanceName(), confFlags)
		configSet.WaitAll(ts)
		ts.AddAll(configSet)
	}

	healthCheck := CheckHealthHook(st, snapsup.InstanceName(), snapsup.Revision())
	healthCheck.WaitAll(ts)
	ts.AddTask(healthCheck)

	return ts, nil
}

// ConfigureSnap returns a set of tasks to configure snapName as done during installation/refresh.
func ConfigureSnap(st *state.State, snapName string, confFlags int) *state.TaskSet {
	// This is slightly ugly, ideally we would check the type instead
	// of hardcoding the name here. Unfortunately we do not have the
	// type until we actually run the change.
	if snapName == defaultCoreSnapName {
		confFlags |= IgnoreHookError
		confFlags |= TrackHookError
	}
	return Configure(st, snapName, nil, confFlags)
}

var Configure = func(st *state.State, snapName string, patch map[string]interface{}, flags int) *state.TaskSet {
	panic("internal error: snapstate.Configure is unset")
}

var SetupInstallHook = func(st *state.State, snapName string) *state.Task {
	panic("internal error: snapstate.SetupInstallHook is unset")
}

var SetupPreRefreshHook = func(st *state.State, snapName string) *state.Task {
	panic("internal error: snapstate.SetupPreRefreshHook is unset")
}

var SetupPostRefreshHook = func(st *state.State, snapName string) *state.Task {
	panic("internal error: snapstate.SetupPostRefreshHook is unset")
}

var SetupRemoveHook = func(st *state.State, snapName string) *state.Task {
	panic("internal error: snapstate.SetupRemoveHook is unset")
}

var CheckHealthHook = func(st *state.State, snapName string, rev snap.Revision) *state.Task {
	panic("internal error: snapstate.CheckHealthHook is unset")
}

// WaitRestart will return a Retry error if there is a pending restart
// and a real error if anything went wrong (like a rollback across
// restarts)
func WaitRestart(task *state.Task, snapsup *SnapSetup) (err error) {
	if ok, _ := task.State().Restarting(); ok {
		// don't continue until we are in the restarted snapd
		task.Logf("Waiting for automatic snapd restart...")
		return &state.Retry{}
	}

	if snapsup.Type == snap.TypeSnapd && os.Getenv("SNAPD_REVERT_TO_REV") != "" {
		return fmt.Errorf("there was a snapd rollback across the restart")
	}

	deviceCtx, err := DeviceCtx(task.State(), task, nil)
	if err != nil {
		return err
	}

	// Check if there was a rollback. A reboot can be triggered by:
	// - core (old core16 world, system-reboot)
	// - bootable base snap (new core18 world, system-reboot)
	// - kernel
	//
	// On classic and in ephemeral run modes (like install, recover)
	// there can never be a rollback so we can skip the check there.
	//
	// TODO: Detect "snapd" snap daemon-restarts here that
	//       fallback into the old version (once we have
	//       better snapd rollback support in core18).
	if deviceCtx.RunMode() && !release.OnClassic {
		// get the name of the name relevant for booting
		// based on the given type
		model := deviceCtx.Model()
		var bootName string
		switch snapsup.Type {
		case snap.TypeKernel:
			bootName = model.Kernel()
		case snap.TypeOS, snap.TypeBase:
			bootName = "core"
			if model.Base() != "" {
				bootName = model.Base()
			}
		default:
			return nil
		}
		// if it is not a snap related to our booting we are not
		// interested
		if snapsup.InstanceName() != bootName {
			return nil
		}

		// compare what we think is "current" for snapd with what
		// actually booted. The bootloader may revert on a failed
		// boot from a bad os/base/kernel to a good one and in this
		// case we need to catch this and error accordingly
		current, err := boot.GetCurrentBoot(snapsup.Type, deviceCtx)
		if err == boot.ErrBootNameAndRevisionNotReady {
			return &state.Retry{After: 5 * time.Second}
		}
		if err != nil {
			return err
		}
		if snapsup.InstanceName() != current.SnapName() || snapsup.SideInfo.Revision != current.SnapRevision() {
			// TODO: make sure this revision gets ignored for
			//       automatic refreshes
			return fmt.Errorf("cannot finish %s installation, there was a rollback across reboot", snapsup.InstanceName())
		}
	}

	return nil
}

func contentAttr(attrer interfaces.Attrer) string {
	var s string
	err := attrer.Attr("content", &s)
	if err != nil {
		return ""
	}
	return s
}

func contentIfaceAvailable(st *state.State) map[string]bool {
	repo := ifacerepo.Get(st)
	contentSlots := repo.AllSlots("content")
	avail := make(map[string]bool, len(contentSlots))
	for _, slot := range contentSlots {
		contentTag := contentAttr(slot)
		if contentTag == "" {
			continue
		}
		avail[contentTag] = true
	}
	return avail
}

// defaultContentPlugProviders takes a snap.Info and returns what
// default providers there are.
func defaultContentPlugProviders(st *state.State, info *snap.Info) []string {
	needed := snap.NeededDefaultProviders(info)
	if len(needed) == 0 {
		return nil
	}
	avail := contentIfaceAvailable(st)
	out := []string{}
	for snapInstance, contentTags := range needed {
		for _, contentTag := range contentTags {
			if !avail[contentTag] {
				out = append(out, snapInstance)
				break
			}
		}
	}
	return out
}

// validateFeatureFlags validates the given snap only uses experimental
// features that are enabled by the user.
func validateFeatureFlags(st *state.State, info *snap.Info) error {
	tr := config.NewTransaction(st)

	if len(info.Layout) > 0 {
		flag, err := features.Flag(tr, features.Layouts)
		if err != nil {
			return err
		}
		if !flag {
			return fmt.Errorf("experimental feature disabled - test it by setting 'experimental.layouts' to true")
		}
	}

	if info.InstanceKey != "" {
		flag, err := features.Flag(tr, features.ParallelInstances)
		if err != nil {
			return err
		}
		if !flag {
			return fmt.Errorf("experimental feature disabled - test it by setting 'experimental.parallel-instances' to true")
		}
	}

	var hasUserService, usesDbusActivation bool
	for _, app := range info.Apps {
		if app.IsService() && app.DaemonScope == snap.UserDaemon {
			hasUserService = true
		}
		if len(app.ActivatesOn) != 0 {
			usesDbusActivation = true
		}
	}

	if hasUserService {
		flag, err := features.Flag(tr, features.UserDaemons)
		if err != nil {
			return err
		}
		if !flag {
			return fmt.Errorf("experimental feature disabled - test it by setting 'experimental.user-daemons' to true")
		}
		if !release.SystemctlSupportsUserUnits() {
			return fmt.Errorf("user session daemons are not supported on this release")
		}
	}

	if usesDbusActivation {
		flag, err := features.Flag(tr, features.DbusActivation)
		if err != nil {
			return err
		}
		if !flag {
			return fmt.Errorf("experimental feature disabled - test it by setting 'experimental.dbus-activation' to true")
		}
	}

	return nil
}

func ensureInstallPreconditions(st *state.State, info *snap.Info, flags Flags, snapst *SnapState, deviceCtx DeviceContext) (Flags, error) {
	if flags.Classic && !info.NeedsClassic() {
		// snap does not require classic confinement, silently drop the flag
		flags.Classic = false
	}

	if err := validateInfoAndFlags(info, snapst, flags); err != nil {
		return flags, err
	}
	if err := validateFeatureFlags(st, info); err != nil {
		return flags, err
	}
	if err := checkDBusServiceConflicts(st, info); err != nil {
		return flags, err
	}
	return flags, nil
}

// InstallPath returns a set of tasks for installing a snap from a file path
// and the snap.Info for the given snap.
//
// Note that the state must be locked by the caller.
// The provided SideInfo can contain just a name which results in a
// local revision and sideloading, or full metadata in which case it
// the snap will appear as installed from the store.
func InstallPath(st *state.State, si *snap.SideInfo, path, instanceName, channel string, flags Flags) (*state.TaskSet, *snap.Info, error) {
	if si.RealName == "" {
		return nil, nil, fmt.Errorf("internal error: snap name to install %q not provided", path)
	}

	if instanceName == "" {
		instanceName = si.RealName
	}

	deviceCtx, err := DeviceCtxFromState(st, nil)
	if err != nil {
		return nil, nil, err
	}

	var snapst SnapState
	err = Get(st, instanceName, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, nil, err
	}

	if si.SnapID != "" {
		if si.Revision.Unset() {
			return nil, nil, fmt.Errorf("internal error: snap id set to install %q but revision is unset", path)
		}
	}

	channel, err = resolveChannel(st, instanceName, snapst.TrackingChannel, channel, deviceCtx)
	if err != nil {
		return nil, nil, err
	}

	var instFlags int
	if flags.SkipConfigure {
		// extract it as a doInstall flag, this is not passed
		// into SnapSetup
		instFlags |= skipConfigure
	}

	// It is ok do open the snap file here because we either
	// have side info or the user passed --dangerous
	info, container, err := backend.OpenSnapFile(path, si)
	if err != nil {
		return nil, nil, err
	}

	if err := validateContainer(container, info, logger.Noticef); err != nil {
		return nil, nil, err
	}
	if err := snap.ValidateInstanceName(instanceName); err != nil {
		return nil, nil, fmt.Errorf("invalid instance name: %v", err)
	}

	snapName, instanceKey := snap.SplitInstanceName(instanceName)
	if info.SnapName() != snapName {
		return nil, nil, fmt.Errorf("cannot install snap %q, the name does not match the metadata %q", instanceName, info.SnapName())
	}
	info.InstanceKey = instanceKey

	flags, err = ensureInstallPreconditions(st, info, flags, &snapst, deviceCtx)
	if err != nil {
		return nil, nil, err
	}
	// this might be a refresh; check the epoch before proceeding
	if err := earlyEpochCheck(info, &snapst); err != nil {
		return nil, nil, err
	}

	snapsup := &SnapSetup{
		Base:        info.Base,
		Prereq:      defaultContentPlugProviders(st, info),
		SideInfo:    si,
		SnapPath:    path,
		Channel:     channel,
		Flags:       flags.ForSnapSetup(),
		Type:        info.Type(),
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: info.InstanceKey,
	}

	ts, err := doInstall(st, &snapst, snapsup, instFlags, "", inUseFor(deviceCtx))
	return ts, info, err
}

// TryPath returns a set of tasks for trying a snap from a file path.
// Note that the state must be locked by the caller.
func TryPath(st *state.State, name, path string, flags Flags) (*state.TaskSet, error) {
	flags.TryMode = true

	ts, _, err := InstallPath(st, &snap.SideInfo{RealName: name}, path, "", "", flags)
	return ts, err
}

// Install returns a set of tasks for installing a snap.
// Note that the state must be locked by the caller.
//
// The returned TaskSet will contain a DownloadAndChecksDoneEdge.
func Install(ctx context.Context, st *state.State, name string, opts *RevisionOptions, userID int, flags Flags) (*state.TaskSet, error) {
	return InstallWithDeviceContext(ctx, st, name, opts, userID, flags, nil, "")
}

// InstallWithDeviceContext returns a set of tasks for installing a snap.
// It will query for the snap with the given deviceCtx.
// Note that the state must be locked by the caller.
//
// The returned TaskSet will contain a DownloadAndChecksDoneEdge.
func InstallWithDeviceContext(ctx context.Context, st *state.State, name string, opts *RevisionOptions, userID int, flags Flags, deviceCtx DeviceContext, fromChange string) (*state.TaskSet, error) {
	if opts == nil {
		opts = &RevisionOptions{}
	}
	if opts.CohortKey != "" && !opts.Revision.Unset() {
		return nil, errors.New("cannot specify revision and cohort")
	}

	if opts.Channel == "" {
		opts.Channel = "stable"
	}

	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if snapst.IsInstalled() {
		return nil, &snap.AlreadyInstalledError{Snap: name}
	}
	// need to have a model set before trying to talk the store
	deviceCtx, err = DevicePastSeeding(st, deviceCtx)
	if err != nil {
		return nil, err
	}

	if err := snap.ValidateInstanceName(name); err != nil {
		return nil, fmt.Errorf("invalid instance name: %v", err)
	}

	sar, err := installInfo(ctx, st, name, opts, userID, deviceCtx)
	if err != nil {
		return nil, err
	}
	info := sar.Info

	if flags.RequireTypeBase && info.Type() != snap.TypeBase && info.Type() != snap.TypeOS {
		return nil, fmt.Errorf("unexpected snap type %q, instead of 'base'", info.Type())
	}

	flags, err = ensureInstallPreconditions(st, info, flags, &snapst, deviceCtx)
	if err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		Channel:      opts.Channel,
		Base:         info.Base,
		Prereq:       defaultContentPlugProviders(st, info),
		UserID:       userID,
		Flags:        flags.ForSnapSetup(),
		DownloadInfo: &info.DownloadInfo,
		SideInfo:     &info.SideInfo,
		Type:         info.Type(),
		PlugsOnly:    len(info.Slots) == 0,
		InstanceKey:  info.InstanceKey,
		auxStoreInfo: auxStoreInfo{
			Media:   info.Media,
			Website: info.Website,
		},
		CohortKey: opts.CohortKey,
	}

	if sar.RedirectChannel != "" {
		snapsup.Channel = sar.RedirectChannel
	}

	return doInstall(st, &snapst, snapsup, 0, fromChange, nil)
}

// InstallMany installs everything from the given list of names.
// Note that the state must be locked by the caller.
func InstallMany(st *state.State, names []string, userID int) ([]string, []*state.TaskSet, error) {
	// need to have a model set before trying to talk the store
	deviceCtx, err := DevicePastSeeding(st, nil)
	if err != nil {
		return nil, nil, err
	}

	toInstall := make([]string, 0, len(names))
	for _, name := range names {
		var snapst SnapState
		err := Get(st, name, &snapst)
		if err != nil && err != state.ErrNoState {
			return nil, nil, err
		}
		if snapst.IsInstalled() {
			continue
		}

		if err := snap.ValidateInstanceName(name); err != nil {
			return nil, nil, fmt.Errorf("invalid instance name: %v", err)
		}

		toInstall = append(toInstall, name)
	}

	user, err := userFromUserID(st, userID)
	if err != nil {
		return nil, nil, err
	}

	installs, err := installCandidates(st, toInstall, "stable", user)
	if err != nil {
		return nil, nil, err
	}

	tasksets := make([]*state.TaskSet, 0, len(installs))
	for _, sar := range installs {
		info := sar.Info
		var snapst SnapState
		var flags Flags

		flags, err := ensureInstallPreconditions(st, info, flags, &snapst, deviceCtx)
		if err != nil {
			return nil, nil, err
		}

		channel := "stable"
		if sar.RedirectChannel != "" {
			channel = sar.RedirectChannel
		}

		snapsup := &SnapSetup{
			Channel:      channel,
			Base:         info.Base,
			Prereq:       defaultContentPlugProviders(st, info),
			UserID:       userID,
			Flags:        flags.ForSnapSetup(),
			DownloadInfo: &info.DownloadInfo,
			SideInfo:     &info.SideInfo,
			Type:         info.Type(),
			PlugsOnly:    len(info.Slots) == 0,
			InstanceKey:  info.InstanceKey,
		}

		ts, err := doInstall(st, &snapst, snapsup, 0, "", inUseFor(deviceCtx))
		if err != nil {
			return nil, nil, err
		}
		ts.JoinLane(st.NewLane())
		tasksets = append(tasksets, ts)
	}

	return toInstall, tasksets, nil
}

// RefreshCandidates gets a list of candidates for update
// Note that the state must be locked by the caller.
func RefreshCandidates(st *state.State, user *auth.UserState) ([]*snap.Info, error) {
	updates, _, _, err := refreshCandidates(context.TODO(), st, nil, user, nil)
	return updates, err
}

// ValidateRefreshes allows to hook validation into the handling of refresh candidates.
var ValidateRefreshes func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx DeviceContext) (validated []*snap.Info, err error)

// UpdateMany updates everything from the given list of names that the
// store says is updateable. If the list is empty, update everything.
// Note that the state must be locked by the caller.
func UpdateMany(ctx context.Context, st *state.State, names []string, userID int, flags *Flags) ([]string, []*state.TaskSet, error) {
	return updateManyFiltered(ctx, st, names, userID, nil, flags, "")
}

// updateFilter is the type of function that can be passed to
// updateManyFromChange so it filters the updates.
//
// If the filter returns true, the update for that snap proceeds. If
// it returns false, the snap is removed from the list of updates to
// consider.
type updateFilter func(*snap.Info, *SnapState) bool

func updateManyFiltered(ctx context.Context, st *state.State, names []string, userID int, filter updateFilter, flags *Flags, fromChange string) ([]string, []*state.TaskSet, error) {
	if flags == nil {
		flags = &Flags{}
	}
	user, err := userFromUserID(st, userID)
	if err != nil {
		return nil, nil, err
	}

	// need to have a model set before trying to talk the store
	deviceCtx, err := DevicePastSeeding(st, nil)
	if err != nil {
		return nil, nil, err
	}

	refreshOpts := &store.RefreshOptions{IsAutoRefresh: flags.IsAutoRefresh}
	updates, stateByInstanceName, ignoreValidation, err := refreshCandidates(ctx, st, names, user, refreshOpts)
	if err != nil {
		return nil, nil, err
	}

	if filter != nil {
		actual := updates[:0]
		for _, update := range updates {
			if filter(update, stateByInstanceName[update.InstanceName()]) {
				actual = append(actual, update)
			}
		}
		updates = actual
	}

	if ValidateRefreshes != nil && len(updates) != 0 {
		updates, err = ValidateRefreshes(st, updates, ignoreValidation, userID, deviceCtx)
		if err != nil {
			// not doing "refresh all" report the error
			if len(names) != 0 {
				return nil, nil, err
			}
			// doing "refresh all", log the problems
			logger.Noticef("cannot refresh some snaps: %v", err)
		}
	}

	params := func(update *snap.Info) (*RevisionOptions, Flags, *SnapState) {
		snapst := stateByInstanceName[update.InstanceName()]
		// setting options to what's in state as multi-refresh doesn't let you change these
		opts := &RevisionOptions{
			Channel:   snapst.TrackingChannel,
			CohortKey: snapst.CohortKey,
		}
		return opts, snapst.Flags, snapst

	}

	updated, tasksets, err := doUpdate(ctx, st, names, updates, params, userID, flags, deviceCtx, fromChange)
	if err != nil {
		return nil, nil, err
	}
	tasksets = finalizeUpdate(st, tasksets, len(updates) > 0, updated, userID, flags)
	return updated, tasksets, nil
}

func doUpdate(ctx context.Context, st *state.State, names []string, updates []*snap.Info, params func(*snap.Info) (*RevisionOptions, Flags, *SnapState), userID int, globalFlags *Flags, deviceCtx DeviceContext, fromChange string) ([]string, []*state.TaskSet, error) {
	if globalFlags == nil {
		globalFlags = &Flags{}
	}

	tasksets := make([]*state.TaskSet, 0, len(updates)+2) // 1 for auto-aliases, 1 for re-refresh

	refreshAll := len(names) == 0
	var nameSet map[string]bool
	if len(names) != 0 {
		nameSet = make(map[string]bool, len(names))
		for _, name := range names {
			nameSet[name] = true
		}
	}

	newAutoAliases, mustPruneAutoAliases, transferTargets, err := autoAliasesUpdate(st, names, updates)
	if err != nil {
		return nil, nil, err
	}

	reportUpdated := make(map[string]bool, len(updates))
	var pruningAutoAliasesTs *state.TaskSet

	if len(mustPruneAutoAliases) != 0 {
		var err error
		pruningAutoAliasesTs, err = applyAutoAliasesDelta(st, mustPruneAutoAliases, "prune", refreshAll, fromChange, func(snapName string, _ *state.TaskSet) {
			if nameSet[snapName] {
				reportUpdated[snapName] = true
			}
		})
		if err != nil {
			return nil, nil, err
		}
		tasksets = append(tasksets, pruningAutoAliasesTs)
	}

	// wait for the auto-alias prune tasks as needed
	scheduleUpdate := func(snapName string, ts *state.TaskSet) {
		if pruningAutoAliasesTs != nil && (mustPruneAutoAliases[snapName] != nil || transferTargets[snapName]) {
			ts.WaitAll(pruningAutoAliasesTs)
		}
		reportUpdated[snapName] = true
	}

	// first snapd, core, bases, then rest
	sort.Stable(snap.ByType(updates))
	prereqs := make(map[string]*state.TaskSet)
	waitPrereq := func(ts *state.TaskSet, prereqName string) {
		preTs := prereqs[prereqName]
		if preTs != nil {
			ts.WaitAll(preTs)
		}
	}

	// updates is sorted by kind so this will process first core
	// and bases and then other snaps
	for _, update := range updates {
		revnoOpts, flags, snapst := params(update)
		flags.IsAutoRefresh = globalFlags.IsAutoRefresh

		flags, err := ensureInstallPreconditions(st, update, flags, snapst, deviceCtx)
		if err != nil {
			if refreshAll {
				logger.Noticef("cannot update %q: %v", update.InstanceName(), err)
				continue
			}
			return nil, nil, err
		}

		if err := earlyEpochCheck(update, snapst); err != nil {
			if refreshAll {
				logger.Noticef("cannot update %q: %v", update.InstanceName(), err)
				continue
			}
			return nil, nil, err
		}

		snapUserID, err := userIDForSnap(st, snapst, userID)
		if err != nil {
			return nil, nil, err
		}

		snapsup := &SnapSetup{
			Base:         update.Base,
			Prereq:       defaultContentPlugProviders(st, update),
			Channel:      revnoOpts.Channel,
			CohortKey:    revnoOpts.CohortKey,
			UserID:       snapUserID,
			Flags:        flags.ForSnapSetup(),
			DownloadInfo: &update.DownloadInfo,
			SideInfo:     &update.SideInfo,
			Type:         update.Type(),
			PlugsOnly:    len(update.Slots) == 0,
			InstanceKey:  update.InstanceKey,
			auxStoreInfo: auxStoreInfo{
				Website: update.Website,
				Media:   update.Media,
			},
		}

		ts, err := doInstall(st, snapst, snapsup, 0, fromChange, inUseFor(deviceCtx))
		if err != nil {
			if refreshAll {
				// doing "refresh all", just skip this snap
				logger.Noticef("cannot refresh snap %q: %v", update.InstanceName(), err)
				continue
			}
			return nil, nil, err
		}
		ts.JoinLane(st.NewLane())

		// because of the sorting of updates we fill prereqs
		// first (if branch) and only then use it to setup
		// waits (else branch)
		if t := update.Type(); t == snap.TypeOS || t == snap.TypeBase || t == snap.TypeSnapd {
			// prereq types come first in updates, we
			// also assume bases don't have hooks, otherwise
			// they would need to wait on core or snapd
			prereqs[update.InstanceName()] = ts
		} else {
			// prereqs were processed already, wait for
			// them as necessary for the other kind of
			// snaps
			waitPrereq(ts, defaultCoreSnapName)
			waitPrereq(ts, "snapd")
			if update.Base != "" {
				waitPrereq(ts, update.Base)
			}
		}

		scheduleUpdate(update.InstanceName(), ts)
		tasksets = append(tasksets, ts)
	}

	if len(newAutoAliases) != 0 {
		addAutoAliasesTs, err := applyAutoAliasesDelta(st, newAutoAliases, "refresh", refreshAll, fromChange, scheduleUpdate)
		if err != nil {
			return nil, nil, err
		}
		tasksets = append(tasksets, addAutoAliasesTs)
	}

	updated := make([]string, 0, len(reportUpdated))
	for name := range reportUpdated {
		updated = append(updated, name)
	}

	return updated, tasksets, nil
}

func finalizeUpdate(st *state.State, tasksets []*state.TaskSet, hasUpdates bool, updated []string, userID int, globalFlags *Flags) []*state.TaskSet {
	if hasUpdates && !globalFlags.NoReRefresh {
		// re-refresh will check the lanes to decide what to
		// _actually_ re-refresh, but it'll be a subset of updated
		// (and equal to updated if nothing goes wrong)
		rerefresh := st.NewTask("check-rerefresh", fmt.Sprintf("Consider re-refresh of %s", strutil.Quoted(updated)))
		rerefresh.Set("rerefresh-setup", reRefreshSetup{
			UserID: userID,
			Flags:  globalFlags,
		})
		tasksets = append(tasksets, state.NewTaskSet(rerefresh))
	}
	return tasksets
}

func applyAutoAliasesDelta(st *state.State, delta map[string][]string, op string, refreshAll bool, fromChange string, linkTs func(instanceName string, ts *state.TaskSet)) (*state.TaskSet, error) {
	applyTs := state.NewTaskSet()
	kind := "refresh-aliases"
	msg := i18n.G("Refresh aliases for snap %q")
	if op == "prune" {
		kind = "prune-auto-aliases"
		msg = i18n.G("Prune automatic aliases for snap %q")
	}
	for instanceName, aliases := range delta {
		if err := checkChangeConflictIgnoringOneChange(st, instanceName, nil, fromChange); err != nil {
			if refreshAll {
				// doing "refresh all", just skip this snap
				logger.Noticef("cannot %s automatic aliases for snap %q: %v", op, instanceName, err)
				continue
			}
			return nil, err
		}

		snapName, instanceKey := snap.SplitInstanceName(instanceName)
		snapsup := &SnapSetup{
			SideInfo:    &snap.SideInfo{RealName: snapName},
			InstanceKey: instanceKey,
		}
		alias := st.NewTask(kind, fmt.Sprintf(msg, snapsup.InstanceName()))
		alias.Set("snap-setup", &snapsup)
		if op == "prune" {
			alias.Set("aliases", aliases)
		}
		ts := state.NewTaskSet(alias)
		linkTs(instanceName, ts)
		applyTs.AddAll(ts)
	}
	return applyTs, nil
}

func autoAliasesUpdate(st *state.State, names []string, updates []*snap.Info) (changed map[string][]string, mustPrune map[string][]string, transferTargets map[string]bool, err error) {
	changed, dropped, err := autoAliasesDelta(st, nil)
	if err != nil {
		if len(names) != 0 {
			// not "refresh all", error
			return nil, nil, nil, err
		}
		// log and continue
		logger.Noticef("cannot find the delta for automatic aliases for some snaps: %v", err)
	}

	refreshAll := len(names) == 0

	// dropped alias -> snapName
	droppedAliases := make(map[string][]string, len(dropped))
	for instanceName, aliases := range dropped {
		for _, alias := range aliases {
			droppedAliases[alias] = append(droppedAliases[alias], instanceName)
		}
	}

	// filter changed considering only names if set:
	// we add auto-aliases only for mentioned snaps
	if !refreshAll && len(changed) != 0 {
		filteredChanged := make(map[string][]string, len(changed))
		for _, name := range names {
			if changed[name] != nil {
				filteredChanged[name] = changed[name]
			}
		}
		changed = filteredChanged
	}

	// mark snaps that are sources or target of transfers
	transferSources := make(map[string]bool, len(dropped))
	transferTargets = make(map[string]bool, len(changed))
	for instanceName, aliases := range changed {
		for _, alias := range aliases {
			if sources := droppedAliases[alias]; len(sources) != 0 {
				transferTargets[instanceName] = true
				for _, source := range sources {
					transferSources[source] = true
				}
			}
		}
	}

	// snaps with updates
	updating := make(map[string]bool, len(updates))
	for _, info := range updates {
		updating[info.InstanceName()] = true
	}

	// add explicitly auto-aliases only for snaps that are not updated
	for instanceName := range changed {
		if updating[instanceName] {
			delete(changed, instanceName)
		}
	}

	// prune explicitly auto-aliases only for snaps that are mentioned
	// and not updated OR the source of transfers
	mustPrune = make(map[string][]string, len(dropped))
	for instanceName := range transferSources {
		mustPrune[instanceName] = dropped[instanceName]
	}
	if refreshAll {
		for instanceName, aliases := range dropped {
			if !updating[instanceName] {
				mustPrune[instanceName] = aliases
			}
		}
	} else {
		for _, name := range names {
			if !updating[name] && dropped[name] != nil {
				mustPrune[name] = dropped[name]
			}
		}
	}

	return changed, mustPrune, transferTargets, nil
}

// resolveChannel returns the effective channel to use, based on the requested
// channel and constrains set by device model, or an error if switching to
// requested channel is forbidden.
func resolveChannel(st *state.State, snapName, oldChannel, newChannel string, deviceCtx DeviceContext) (effectiveChannel string, err error) {
	if newChannel == "" {
		return "", nil
	}

	// ensure we do not switch away from the kernel-track in the model
	model := deviceCtx.Model()

	var pinnedTrack, which string
	if snapName == model.Kernel() && model.KernelTrack() != "" {
		pinnedTrack, which = model.KernelTrack(), "kernel"
	}
	if snapName == model.Gadget() && model.GadgetTrack() != "" {
		pinnedTrack, which = model.GadgetTrack(), "gadget"
	}

	if pinnedTrack == "" {
		// no pinned track
		return channel.Resolve(oldChannel, newChannel)
	}

	// channel name is valid and consist of risk level or
	// risk/branch only, do the right thing and default to risk (or
	// risk/branch) within the pinned track
	resChannel, err := channel.ResolvePinned(pinnedTrack, newChannel)
	if err == channel.ErrPinnedTrackSwitch {
		// switching to a different track is not allowed
		return "", fmt.Errorf("cannot switch from %s track %q as specified for the (device) model to %q", which, pinnedTrack, newChannel)

	}
	if err != nil {
		return "", err
	}
	return resChannel, nil
}

var errRevisionSwitch = errors.New("cannot switch revision")

func switchSummary(snap, chanFrom, chanTo, cohFrom, cohTo string) string {
	if cohFrom != cohTo {
		if cohTo == "" {
			// leave cohort
			if chanFrom == chanTo {
				return fmt.Sprintf(i18n.G("Switch snap %q away from cohort %q"),
					snap, strutil.ElliptLeft(cohFrom, 10))
			}
			if chanFrom == "" {
				return fmt.Sprintf(i18n.G("Switch snap %q to channel %q and away from cohort %q"),
					snap, chanTo, strutil.ElliptLeft(cohFrom, 10),
				)
			}
			return fmt.Sprintf(i18n.G("Switch snap %q from channel %q to %q and away from cohort %q"),
				snap, chanFrom, chanTo, strutil.ElliptLeft(cohFrom, 10),
			)
		}
		if cohFrom == "" {
			// moving into a cohort
			if chanFrom == chanTo {
				return fmt.Sprintf(i18n.G("Switch snap %q from no cohort to %q"),
					snap, strutil.ElliptLeft(cohTo, 10))
			}
			if chanFrom == "" {
				return fmt.Sprintf(i18n.G("Switch snap %q to channel %q and from no cohort to %q"),
					snap, chanTo, strutil.ElliptLeft(cohTo, 10),
				)
			}
			// chanTo == "" is not interesting
			return fmt.Sprintf(i18n.G("Switch snap %q from channel %q to %q and from no cohort to %q"),
				snap, chanFrom, chanTo, strutil.ElliptLeft(cohTo, 10),
			)
		}
		if chanFrom == chanTo {
			return fmt.Sprintf(i18n.G("Switch snap %q from cohort %q to %q"),
				snap, strutil.ElliptLeft(cohFrom, 10), strutil.ElliptLeft(cohTo, 10))
		}
		if chanFrom == "" {
			return fmt.Sprintf(i18n.G("Switch snap %q to channel %q and from cohort %q to %q"),
				snap, chanTo, strutil.ElliptLeft(cohFrom, 10), strutil.ElliptLeft(cohTo, 10),
			)
		}
		return fmt.Sprintf(i18n.G("Switch snap %q from channel %q to %q and from cohort %q to %q"),
			snap, chanFrom, chanTo,
			strutil.ElliptLeft(cohFrom, 10), strutil.ElliptLeft(cohTo, 10),
		)
	}

	if chanFrom == "" {
		return fmt.Sprintf(i18n.G("Switch snap %q to channel %q"),
			snap, chanTo)
	}
	if chanFrom != chanTo {
		return fmt.Sprintf(i18n.G("Switch snap %q from channel %q to %q"),
			snap, chanFrom, chanTo)
	}
	// a no-change switch is accepted for idempotency
	return "No change switch (no-op)"
}

// Switch switches a snap to a new channel and/or cohort
func Switch(st *state.State, name string, opts *RevisionOptions) (*state.TaskSet, error) {
	if opts == nil {
		opts = &RevisionOptions{}
	}
	if !opts.Revision.Unset() {
		return nil, errRevisionSwitch
	}
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if !snapst.IsInstalled() {
		return nil, &snap.NotInstalledError{Snap: name}
	}

	if err := CheckChangeConflict(st, name, nil); err != nil {
		return nil, err
	}

	deviceCtx, err := DeviceCtxFromState(st, nil)
	if err != nil {
		return nil, err
	}

	opts.Channel, err = resolveChannel(st, name, snapst.TrackingChannel, opts.Channel, deviceCtx)
	if err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		SideInfo:    snapst.CurrentSideInfo(),
		InstanceKey: snapst.InstanceKey,
		// set the from state (i.e. no change), they are overridden from opts as needed below
		CohortKey: snapst.CohortKey,
		Channel:   snapst.TrackingChannel,
	}

	if opts.Channel != "" {
		snapsup.Channel = opts.Channel
	}
	if opts.CohortKey != "" {
		snapsup.CohortKey = opts.CohortKey
	}
	if opts.LeaveCohort {
		snapsup.CohortKey = ""
	}

	summary := switchSummary(snapsup.InstanceName(), snapst.TrackingChannel, snapsup.Channel, snapst.CohortKey, snapsup.CohortKey)
	switchSnap := st.NewTask("switch-snap", summary)
	switchSnap.Set("snap-setup", &snapsup)

	return state.NewTaskSet(switchSnap), nil
}

// RevisionOptions control the selection of a snap revision.
type RevisionOptions struct {
	Channel     string
	Revision    snap.Revision
	CohortKey   string
	LeaveCohort bool
}

// Update initiates a change updating a snap.
// Note that the state must be locked by the caller.
//
// The returned TaskSet will contain a DownloadAndChecksDoneEdge.
func Update(st *state.State, name string, opts *RevisionOptions, userID int, flags Flags) (*state.TaskSet, error) {
	return UpdateWithDeviceContext(st, name, opts, userID, flags, nil, "")
}

// UpdateWithDeviceContext initiates a change updating a snap.
// It will query for the snap with the given deviceCtx.
// Note that the state must be locked by the caller.
//
// The returned TaskSet will contain a DownloadAndChecksDoneEdge.
func UpdateWithDeviceContext(st *state.State, name string, opts *RevisionOptions, userID int, flags Flags, deviceCtx DeviceContext, fromChange string) (*state.TaskSet, error) {
	if opts == nil {
		opts = &RevisionOptions{}
	}
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if !snapst.IsInstalled() {
		return nil, &snap.NotInstalledError{Snap: name}
	}

	// FIXME: snaps that are not active are skipped for now
	//        until we know what we want to do
	if !snapst.Active {
		return nil, fmt.Errorf("refreshing disabled snap %q not supported", name)
	}

	// need to have a model set before trying to talk the store
	deviceCtx, err = DevicePastSeeding(st, deviceCtx)
	if err != nil {
		return nil, err
	}

	opts.Channel, err = resolveChannel(st, name, snapst.TrackingChannel, opts.Channel, deviceCtx)
	if err != nil {
		return nil, err
	}

	if opts.Channel == "" {
		// default to tracking the same channel
		opts.Channel = snapst.TrackingChannel
	}
	if opts.CohortKey == "" {
		// default to being in the same cohort
		opts.CohortKey = snapst.CohortKey
	}
	if opts.LeaveCohort {
		opts.CohortKey = ""
	}

	// TODO: make flags be per revision to avoid this logic (that
	//       leaves corner cases all over the place)
	if !(flags.JailMode || flags.DevMode) {
		flags.Classic = flags.Classic || snapst.Flags.Classic
	}

	var updates []*snap.Info
	info, infoErr := infoForUpdate(st, &snapst, name, opts, userID, flags, deviceCtx)
	switch infoErr {
	case nil:
		updates = append(updates, info)
	case store.ErrNoUpdateAvailable:
		// there may be some new auto-aliases
	default:
		return nil, infoErr
	}

	params := func(update *snap.Info) (*RevisionOptions, Flags, *SnapState) {
		return opts, flags, &snapst
	}

	_, tts, err := doUpdate(context.TODO(), st, []string{name}, updates, params, userID, &flags, deviceCtx, fromChange)
	if err != nil {
		return nil, err
	}

	// see if we need to switch the channel or cohort, or toggle ignore-validation
	switchChannel := snapst.TrackingChannel != opts.Channel
	switchCohortKey := snapst.CohortKey != opts.CohortKey
	toggleIgnoreValidation := snapst.IgnoreValidation != flags.IgnoreValidation
	if infoErr == store.ErrNoUpdateAvailable && (switchChannel || switchCohortKey || toggleIgnoreValidation) {
		if err := checkChangeConflictIgnoringOneChange(st, name, nil, fromChange); err != nil {
			return nil, err
		}

		snapsup := &SnapSetup{
			SideInfo:    snapst.CurrentSideInfo(),
			Flags:       snapst.Flags.ForSnapSetup(),
			InstanceKey: snapst.InstanceKey,
			CohortKey:   opts.CohortKey,
		}

		if switchChannel || switchCohortKey {
			// update the tracked channel and cohort
			snapsup.Channel = opts.Channel
			snapsup.CohortKey = opts.CohortKey
			// Update the current snap channel as well. This ensures that
			// the UI displays the right values.
			snapsup.SideInfo.Channel = opts.Channel

			summary := switchSummary(snapsup.InstanceName(), snapst.TrackingChannel, opts.Channel, snapst.CohortKey, opts.CohortKey)
			switchSnap := st.NewTask("switch-snap-channel", summary)
			switchSnap.Set("snap-setup", &snapsup)

			switchSnapTs := state.NewTaskSet(switchSnap)
			for _, ts := range tts {
				switchSnapTs.WaitAll(ts)
			}
			tts = append(tts, switchSnapTs)
		}

		if toggleIgnoreValidation {
			snapsup.IgnoreValidation = flags.IgnoreValidation
			toggle := st.NewTask("toggle-snap-flags", fmt.Sprintf(i18n.G("Toggle snap %q flags"), snapsup.InstanceName()))
			toggle.Set("snap-setup", &snapsup)

			toggleTs := state.NewTaskSet(toggle)
			for _, ts := range tts {
				toggleTs.WaitAll(ts)
			}
			tts = append(tts, toggleTs)
		}
	}

	if len(tts) == 0 && len(updates) == 0 {
		// really nothing to do, return the original no-update-available error
		return nil, infoErr
	}

	tts = finalizeUpdate(st, tts, len(updates) > 0, []string{name}, userID, &flags)

	flat := state.NewTaskSet()
	for _, ts := range tts {
		// The tasksets we get from "doUpdate" contain important
		// "TaskEdge" information that is needed for "Remodel".
		// To preserve those we need to use "AddAllWithEdges()".
		if err := flat.AddAllWithEdges(ts); err != nil {
			return nil, err
		}
	}
	return flat, nil
}

func infoForUpdate(st *state.State, snapst *SnapState, name string, opts *RevisionOptions, userID int, flags Flags, deviceCtx DeviceContext) (*snap.Info, error) {
	if opts.Revision.Unset() {
		// good ol' refresh
		info, err := updateInfo(st, snapst, opts, userID, flags, deviceCtx)
		if err != nil {
			return nil, err
		}
		if ValidateRefreshes != nil && !flags.IgnoreValidation {
			_, err := ValidateRefreshes(st, []*snap.Info{info}, nil, userID, deviceCtx)
			if err != nil {
				return nil, err
			}
		}
		return info, nil
	}
	var sideInfo *snap.SideInfo
	for _, si := range snapst.Sequence {
		if si.Revision == opts.Revision {
			sideInfo = si
			break
		}
	}
	if sideInfo == nil {
		// refresh from given revision from store
		return updateToRevisionInfo(st, snapst, opts.Revision, userID, deviceCtx)
	}

	// refresh-to-local, this assumes the snap revision is mounted
	return readInfo(name, sideInfo, errorOnBroken)
}

// AutoRefreshAssertions allows to hook fetching of important assertions
// into the Autorefresh function.
var AutoRefreshAssertions func(st *state.State, userID int) error

// AutoRefresh is the wrapper that will do a refresh of all the installed
// snaps on the system. In addition to that it will also refresh important
// assertions.
func AutoRefresh(ctx context.Context, st *state.State) ([]string, []*state.TaskSet, error) {
	userID := 0

	if AutoRefreshAssertions != nil {
		if err := AutoRefreshAssertions(st, userID); err != nil {
			return nil, nil, err
		}
	}

	return UpdateMany(ctx, st, nil, userID, &Flags{IsAutoRefresh: true})
}

// LinkNewBaseOrKernel will create prepare/link-snap tasks for a remodel
func LinkNewBaseOrKernel(st *state.State, name string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err == state.ErrNoState {
		return nil, &snap.NotInstalledError{Snap: name}
	}
	if err != nil {
		return nil, err
	}

	if err := CheckChangeConflict(st, name, nil); err != nil {
		return nil, err
	}

	info, err := snapst.CurrentInfo()
	if err != nil {
		return nil, err
	}

	switch info.Type() {
	case snap.TypeOS, snap.TypeBase, snap.TypeKernel:
		// good
	default:
		// bad
		return nil, fmt.Errorf("cannot link type %v", info.Type())
	}

	snapsup := &SnapSetup{
		SideInfo:    snapst.CurrentSideInfo(),
		Flags:       snapst.Flags.ForSnapSetup(),
		Type:        info.Type(),
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: snapst.InstanceKey,
	}

	prepareSnap := st.NewTask("prepare-snap", fmt.Sprintf(i18n.G("Prepare snap %q (%s) for remodel"), snapsup.InstanceName(), snapst.Current))
	prepareSnap.Set("snap-setup", &snapsup)

	linkSnap := st.NewTask("link-snap", fmt.Sprintf(i18n.G("Make snap %q (%s) available to the system during remodel"), snapsup.InstanceName(), snapst.Current))
	linkSnap.Set("snap-setup-task", prepareSnap.ID())
	linkSnap.WaitFor(prepareSnap)

	// we need this for remodel
	ts := state.NewTaskSet(prepareSnap, linkSnap)
	ts.MarkEdge(prepareSnap, DownloadAndChecksDoneEdge)
	return ts, nil
}

// Enable sets a snap to the active state
func Enable(st *state.State, name string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err == state.ErrNoState {
		return nil, &snap.NotInstalledError{Snap: name}
	}
	if err != nil {
		return nil, err
	}

	if snapst.Active {
		return nil, fmt.Errorf("snap %q already enabled", name)
	}

	if err := CheckChangeConflict(st, name, nil); err != nil {
		return nil, err
	}

	info, err := snapst.CurrentInfo()
	if err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		SideInfo:    snapst.CurrentSideInfo(),
		Flags:       snapst.Flags.ForSnapSetup(),
		Type:        info.Type(),
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: snapst.InstanceKey,
	}

	prepareSnap := st.NewTask("prepare-snap", fmt.Sprintf(i18n.G("Prepare snap %q (%s)"), snapsup.InstanceName(), snapst.Current))
	prepareSnap.Set("snap-setup", &snapsup)

	setupProfiles := st.NewTask("setup-profiles", fmt.Sprintf(i18n.G("Setup snap %q (%s) security profiles"), snapsup.InstanceName(), snapst.Current))
	setupProfiles.Set("snap-setup-task", prepareSnap.ID())
	setupProfiles.WaitFor(prepareSnap)

	linkSnap := st.NewTask("link-snap", fmt.Sprintf(i18n.G("Make snap %q (%s) available to the system"), snapsup.InstanceName(), snapst.Current))
	linkSnap.Set("snap-setup-task", prepareSnap.ID())
	linkSnap.WaitFor(setupProfiles)

	// setup aliases
	setupAliases := st.NewTask("setup-aliases", fmt.Sprintf(i18n.G("Setup snap %q aliases"), snapsup.InstanceName()))
	setupAliases.Set("snap-setup-task", prepareSnap.ID())
	setupAliases.WaitFor(linkSnap)

	startSnapServices := st.NewTask("start-snap-services", fmt.Sprintf(i18n.G("Start snap %q (%s) services"), snapsup.InstanceName(), snapst.Current))
	startSnapServices.Set("snap-setup-task", prepareSnap.ID())
	startSnapServices.WaitFor(setupAliases)

	return state.NewTaskSet(prepareSnap, setupProfiles, linkSnap, setupAliases, startSnapServices), nil
}

// Disable sets a snap to the inactive state
func Disable(st *state.State, name string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err == state.ErrNoState {
		return nil, &snap.NotInstalledError{Snap: name}
	}
	if err != nil {
		return nil, err
	}
	if !snapst.Active {
		return nil, fmt.Errorf("snap %q already disabled", name)
	}

	info, err := Info(st, name, snapst.Current)
	if err != nil {
		return nil, err
	}
	if !canDisable(info) {
		return nil, fmt.Errorf("snap %q cannot be disabled", name)
	}

	if err := CheckChangeConflict(st, name, nil); err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snap.InstanceSnap(name),
			Revision: snapst.Current,
		},
		Type:        info.Type(),
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: snapst.InstanceKey,
	}

	stopSnapServices := st.NewTask("stop-snap-services", fmt.Sprintf(i18n.G("Stop snap %q (%s) services"), snapsup.InstanceName(), snapst.Current))
	stopSnapServices.Set("snap-setup", &snapsup)
	stopSnapServices.Set("stop-reason", snap.StopReasonDisable)

	removeAliases := st.NewTask("remove-aliases", fmt.Sprintf(i18n.G("Remove aliases for snap %q"), snapsup.InstanceName()))
	removeAliases.Set("snap-setup-task", stopSnapServices.ID())
	removeAliases.WaitFor(stopSnapServices)

	unlinkSnap := st.NewTask("unlink-snap", fmt.Sprintf(i18n.G("Make snap %q (%s) unavailable to the system"), snapsup.InstanceName(), snapst.Current))
	unlinkSnap.Set("snap-setup-task", stopSnapServices.ID())
	unlinkSnap.WaitFor(removeAliases)

	removeProfiles := st.NewTask("remove-profiles", fmt.Sprintf(i18n.G("Remove security profiles of snap %q"), snapsup.InstanceName()))
	removeProfiles.Set("snap-setup-task", stopSnapServices.ID())
	removeProfiles.WaitFor(unlinkSnap)

	return state.NewTaskSet(stopSnapServices, removeAliases, unlinkSnap, removeProfiles), nil
}

// canDisable verifies that a snap can be deactivated.
func canDisable(si *snap.Info) bool {
	for _, importantSnapType := range []snap.Type{snap.TypeGadget, snap.TypeKernel, snap.TypeOS} {
		if importantSnapType == si.Type() {
			return false
		}
	}

	return true
}

// canRemove verifies that a snap can be removed.
func canRemove(st *state.State, si *snap.Info, snapst *SnapState, removeAll bool, deviceCtx DeviceContext) error {
	rev := snap.Revision{}
	if !removeAll {
		rev = si.Revision
	}

	return PolicyFor(si.Type(), deviceCtx.Model()).CanRemove(st, snapst, rev, deviceCtx)
}

// RemoveFlags are used to pass additional flags to the Remove operation.
type RemoveFlags struct {
	// Remove the snap without creating snapshot data
	Purge bool
}

// Remove returns a set of tasks for removing snap.
// Note that the state must be locked by the caller.
func Remove(st *state.State, name string, revision snap.Revision, flags *RemoveFlags) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}

	if !snapst.IsInstalled() {
		return nil, &snap.NotInstalledError{Snap: name, Rev: snap.R(0)}
	}

	if err := CheckChangeConflict(st, name, nil); err != nil {
		return nil, err
	}

	deviceCtx, err := DeviceCtxFromState(st, nil)
	if err != nil {
		return nil, err
	}

	active := snapst.Active
	var removeAll bool
	if revision.Unset() {
		revision = snapst.Current
		removeAll = true
	} else {
		if active {
			if revision == snapst.Current {
				msg := "cannot remove active revision %s of snap %q"
				if len(snapst.Sequence) > 1 {
					msg += " (revert first?)"
				}
				return nil, fmt.Errorf(msg, revision, name)
			}
			active = false
		}

		if !revisionInSequence(&snapst, revision) {
			return nil, &snap.NotInstalledError{Snap: name, Rev: revision}
		}

		removeAll = len(snapst.Sequence) == 1
	}

	info, err := Info(st, name, revision)
	if err != nil {
		return nil, err
	}

	// check if this is something that can be removed
	if err := canRemove(st, info, &snapst, removeAll, deviceCtx); err != nil {
		return nil, fmt.Errorf("snap %q is not removable: %v", name, err)
	}

	// main/current SnapSetup
	snapsup := SnapSetup{
		SideInfo: &snap.SideInfo{
			SnapID:   info.SnapID,
			RealName: snap.InstanceSnap(name),
			Revision: revision,
		},
		Type:        info.Type(),
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: snapst.InstanceKey,
	}

	// trigger remove

	full := state.NewTaskSet()
	var chain *state.TaskSet

	addNext := func(ts *state.TaskSet) {
		if chain != nil {
			ts.WaitAll(chain)
		}
		full.AddAll(ts)
		chain = ts
	}

	var prev *state.Task
	var stopSnapServices *state.Task
	if active {
		stopSnapServices = st.NewTask("stop-snap-services", fmt.Sprintf(i18n.G("Stop snap %q services"), name))
		stopSnapServices.Set("snap-setup", snapsup)
		stopSnapServices.Set("stop-reason", snap.StopReasonRemove)
		addNext(state.NewTaskSet(stopSnapServices))
		prev = stopSnapServices
	}

	// only run remove hook if uninstalling the snap completely
	if removeAll {
		removeHook := SetupRemoveHook(st, snapsup.InstanceName())
		addNext(state.NewTaskSet(removeHook))
		prev = removeHook

		// run disconnect hooks
		disconnect := st.NewTask("auto-disconnect", fmt.Sprintf(i18n.G("Disconnect interfaces of snap %q"), snapsup.InstanceName()))
		disconnect.Set("snap-setup", snapsup)
		if prev != nil {
			disconnect.WaitFor(prev)
		}
		addNext(state.NewTaskSet(disconnect))
		prev = disconnect
	}

	// 'purge' flag disables automatic snapshot for given remove op
	if flags == nil || !flags.Purge {
		if tp, _ := snapst.Type(); tp == snap.TypeApp && removeAll {
			ts, err := AutomaticSnapshot(st, name)
			if err == nil {
				sz, err := EstimateSnapshotSize(st, name, nil)
				if err != nil {
					return nil, err
				}
				// require 5Mb extra
				requiredSpace := sz + 5*1024*1024
				path := dirs.SnapdStateDir(dirs.GlobalRootDir)
				if err := osutilCheckFreeSpace(path, requiredSpace); err != nil {
					if _, ok := err.(*osutil.NotEnoughDiskSpaceError); ok {
						return nil, &ErrInsufficientSpace{
							Path:    path,
							Snaps:   []string{name},
							Message: fmt.Sprintf("cannot create automatic snapshot when removing last revision of the snap: %v", err)}
					}
					return nil, err
				}
				addNext(ts)
			} else {
				if err != ErrNothingToDo {
					return nil, err
				}
			}
		}
	}

	if active { // unlink
		var tasks []*state.Task

		removeAliases := st.NewTask("remove-aliases", fmt.Sprintf(i18n.G("Remove aliases for snap %q"), name))
		removeAliases.WaitFor(prev) // prev is not needed beyond here
		removeAliases.Set("snap-setup-task", stopSnapServices.ID())

		unlink := st.NewTask("unlink-snap", fmt.Sprintf(i18n.G("Make snap %q unavailable to the system"), name))
		unlink.Set("snap-setup-task", stopSnapServices.ID())
		unlink.WaitFor(removeAliases)

		removeSecurity := st.NewTask("remove-profiles", fmt.Sprintf(i18n.G("Remove security profile for snap %q (%s)"), name, revision))
		removeSecurity.WaitFor(unlink)
		removeSecurity.Set("snap-setup-task", stopSnapServices.ID())

		tasks = append(tasks, removeAliases, unlink, removeSecurity)
		addNext(state.NewTaskSet(tasks...))
	}

	if removeAll {
		seq := snapst.Sequence
		for i := len(seq) - 1; i >= 0; i-- {
			si := seq[i]
			addNext(removeInactiveRevision(st, name, info.SnapID, si.Revision))
		}
	} else {
		addNext(removeInactiveRevision(st, name, info.SnapID, revision))
	}

	return full, nil
}

func removeInactiveRevision(st *state.State, name, snapID string, revision snap.Revision) *state.TaskSet {
	snapName, instanceKey := snap.SplitInstanceName(name)
	snapsup := SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapName,
			SnapID:   snapID,
			Revision: revision,
		},
		InstanceKey: instanceKey,
	}

	clearData := st.NewTask("clear-snap", fmt.Sprintf(i18n.G("Remove data for snap %q (%s)"), name, revision))
	clearData.Set("snap-setup", snapsup)

	discardSnap := st.NewTask("discard-snap", fmt.Sprintf(i18n.G("Remove snap %q (%s) from the system"), name, revision))
	discardSnap.WaitFor(clearData)
	discardSnap.Set("snap-setup-task", clearData.ID())

	return state.NewTaskSet(clearData, discardSnap)
}

// RemoveMany removes everything from the given list of names.
// Note that the state must be locked by the caller.
func RemoveMany(st *state.State, names []string) ([]string, []*state.TaskSet, error) {
	removed := make([]string, 0, len(names))
	tasksets := make([]*state.TaskSet, 0, len(names))
	for _, name := range names {
		ts, err := Remove(st, name, snap.R(0), nil)
		// FIXME: is this expected behavior?
		if _, ok := err.(*snap.NotInstalledError); ok {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		removed = append(removed, name)
		ts.JoinLane(st.NewLane())
		tasksets = append(tasksets, ts)
	}

	return removed, tasksets, nil
}

// Revert returns a set of tasks for reverting to the previous version of the snap.
// Note that the state must be locked by the caller.
func Revert(st *state.State, name string, flags Flags) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}

	pi := snapst.previousSideInfo()
	if pi == nil {
		return nil, fmt.Errorf("no revision to revert to")
	}

	return RevertToRevision(st, name, pi.Revision, flags)
}

func RevertToRevision(st *state.State, name string, rev snap.Revision, flags Flags) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}

	if snapst.Current == rev {
		return nil, fmt.Errorf("already on requested revision")
	}

	if !snapst.Active {
		return nil, fmt.Errorf("cannot revert inactive snaps")
	}
	i := snapst.LastIndex(rev)
	if i < 0 {
		return nil, fmt.Errorf("cannot find revision %s for snap %q", rev, name)
	}

	flags.Revert = true
	// TODO: make flags be per revision to avoid this logic (that
	//       leaves corner cases all over the place)
	if !(flags.JailMode || flags.DevMode || flags.Classic) {
		if snapst.Flags.DevMode {
			flags.DevMode = true
		}
		if snapst.Flags.JailMode {
			flags.JailMode = true
		}
		if snapst.Flags.Classic {
			flags.Classic = true
		}
	}

	info, err := Info(st, name, rev)
	if err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		Base:        info.Base,
		SideInfo:    snapst.Sequence[i],
		Flags:       flags.ForSnapSetup(),
		Type:        info.Type(),
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: snapst.InstanceKey,
	}
	return doInstall(st, &snapst, snapsup, 0, "", nil)
}

// TransitionCore transitions from an old core snap name to a new core
// snap name. It is used for the ubuntu-core -> core transition (that
// is not just a rename because the two snaps have different snapIDs)
//
// Note that this function makes some assumptions like:
// - no aliases setup for both snaps
// - no data needs to be copied
// - all interfaces are absolutely identical on both new and old
// Do not use this as a general way to transition from snap A to snap B.
func TransitionCore(st *state.State, oldName, newName string) ([]*state.TaskSet, error) {
	var oldSnapst, newSnapst SnapState
	err := Get(st, oldName, &oldSnapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if !oldSnapst.IsInstalled() {
		return nil, fmt.Errorf("cannot transition snap %q: not installed", oldName)
	}

	var all []*state.TaskSet
	// install new core (if not already installed)
	err = Get(st, newName, &newSnapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if !newSnapst.IsInstalled() {
		var userID int
		newInfo, err := installInfo(context.TODO(), st, newName, &RevisionOptions{Channel: oldSnapst.TrackingChannel}, userID, nil)
		if err != nil {
			return nil, err
		}

		// start by installing the new snap
		tsInst, err := doInstall(st, &newSnapst, &SnapSetup{
			Channel:      oldSnapst.TrackingChannel,
			DownloadInfo: &newInfo.DownloadInfo,
			SideInfo:     &newInfo.SideInfo,
			Type:         newInfo.Type(),
		}, 0, "", nil)
		if err != nil {
			return nil, err
		}
		all = append(all, tsInst)
	}

	// then transition the interface connections over
	transIf := st.NewTask("transition-ubuntu-core", fmt.Sprintf(i18n.G("Transition security profiles from %q to %q"), oldName, newName))
	transIf.Set("old-name", oldName)
	transIf.Set("new-name", newName)
	if len(all) > 0 {
		transIf.WaitAll(all[0])
	}
	tsTrans := state.NewTaskSet(transIf)
	all = append(all, tsTrans)

	// FIXME: this is just here for the tests
	transIf.Set("snap-setup", &SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: oldName,
		},
	})

	// then remove the old snap
	tsRm, err := Remove(st, oldName, snap.R(0), nil)
	if err != nil {
		return nil, err
	}
	tsRm.WaitFor(transIf)
	all = append(all, tsRm)

	return all, nil
}

// State/info accessors

// Installing returns whether there's an in-progress installation.
func Installing(st *state.State) bool {
	for _, task := range st.Tasks() {
		k := task.Kind()
		chg := task.Change()
		if k == "mount-snap" && chg != nil && !chg.Status().Ready() {
			return true
		}
	}
	return false
}

// Info returns the information about the snap with given name and revision.
// Works also for a mounted candidate snap in the process of being installed.
func Info(st *state.State, name string, revision snap.Revision) (*snap.Info, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err == state.ErrNoState {
		return nil, &snap.NotInstalledError{Snap: name}
	}
	if err != nil {
		return nil, err
	}

	for i := len(snapst.Sequence) - 1; i >= 0; i-- {
		if si := snapst.Sequence[i]; si.Revision == revision {
			return readInfo(name, si, 0)
		}
	}

	return nil, fmt.Errorf("cannot find snap %q at revision %s", name, revision.String())
}

// CurrentInfo returns the information about the current revision of a snap with the given name.
func CurrentInfo(st *state.State, name string) (*snap.Info, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	info, err := snapst.CurrentInfo()
	if err == ErrNoCurrent {
		return nil, &snap.NotInstalledError{Snap: name}
	}
	return info, err
}

// Get retrieves the SnapState of the given snap.
func Get(st *state.State, name string, snapst *SnapState) error {
	if snapst == nil {
		return fmt.Errorf("internal error: snapst is nil")
	}
	// SnapState is (un-)marshalled from/to JSON, fields having omitempty
	// tag will not appear in the output (if empty) and subsequently will
	// not be unmarshalled to (or cleared); if the caller reuses the same
	// struct though subsequent calls, it is possible that they end up with
	// garbage inside, clear the destination struct so that we always
	// unmarshal to a clean state
	*snapst = SnapState{}

	var snaps map[string]*json.RawMessage
	err := st.Get("snaps", &snaps)
	if err != nil {
		return err
	}
	raw, ok := snaps[name]
	if !ok {
		return state.ErrNoState
	}
	err = json.Unmarshal([]byte(*raw), &snapst)
	if err != nil {
		return fmt.Errorf("cannot unmarshal snap state: %v", err)
	}
	return nil
}

// All retrieves return a map from name to SnapState for all current snaps in the system state.
func All(st *state.State) (map[string]*SnapState, error) {
	// XXX: result is a map because sideloaded snaps carry no name
	// atm in their sideinfos
	var stateMap map[string]*SnapState
	if err := st.Get("snaps", &stateMap); err != nil && err != state.ErrNoState {
		return nil, err
	}
	curStates := make(map[string]*SnapState, len(stateMap))
	for instanceName, snapst := range stateMap {
		curStates[instanceName] = snapst
	}
	return curStates, nil
}

// NumSnaps returns the number of installed snaps.
func NumSnaps(st *state.State) (int, error) {
	var snaps map[string]*json.RawMessage
	if err := st.Get("snaps", &snaps); err != nil && err != state.ErrNoState {
		return -1, err
	}
	return len(snaps), nil
}

// Set sets the SnapState of the given snap, overwriting any earlier state.
// Note that a SnapState with an empty Sequence will be treated as if snapst was
// nil and name will be deleted from the state.
func Set(st *state.State, name string, snapst *SnapState) {
	var snaps map[string]*json.RawMessage
	err := st.Get("snaps", &snaps)
	if err != nil && err != state.ErrNoState {
		panic("internal error: cannot unmarshal snaps state: " + err.Error())
	}
	if snaps == nil {
		snaps = make(map[string]*json.RawMessage)
	}
	if snapst == nil || (len(snapst.Sequence) == 0) {
		delete(snaps, name)
	} else {
		data, err := json.Marshal(snapst)
		if err != nil {
			panic("internal error: cannot marshal snap state: " + err.Error())
		}
		raw := json.RawMessage(data)
		snaps[name] = &raw
	}
	st.Set("snaps", snaps)
}

// ActiveInfos returns information about all active snaps.
func ActiveInfos(st *state.State) ([]*snap.Info, error) {
	var stateMap map[string]*SnapState
	var infos []*snap.Info
	if err := st.Get("snaps", &stateMap); err != nil && err != state.ErrNoState {
		return nil, err
	}
	for instanceName, snapst := range stateMap {
		if !snapst.Active {
			continue
		}
		snapInfo, err := snapst.CurrentInfo()
		if err != nil {
			logger.Noticef("cannot retrieve info for snap %q: %s", instanceName, err)
			continue
		}
		infos = append(infos, snapInfo)
	}
	return infos, nil
}

func HasSnapOfType(st *state.State, snapType snap.Type) (bool, error) {
	var stateMap map[string]*SnapState
	if err := st.Get("snaps", &stateMap); err != nil && err != state.ErrNoState {
		return false, err
	}

	for _, snapst := range stateMap {
		typ, err := snapst.Type()
		if err != nil {
			return false, err
		}
		if typ == snapType {
			return true, nil
		}
	}

	return false, nil
}

func infosForType(st *state.State, snapType snap.Type) ([]*snap.Info, error) {
	var stateMap map[string]*SnapState
	if err := st.Get("snaps", &stateMap); err != nil && err != state.ErrNoState {
		return nil, err
	}

	var res []*snap.Info
	for _, snapst := range stateMap {
		if !snapst.IsInstalled() {
			continue
		}
		typ, err := snapst.Type()
		if err != nil {
			return nil, err
		}
		if typ != snapType {
			continue
		}
		si, err := snapst.CurrentInfo()
		if err != nil {
			return nil, err
		}
		res = append(res, si)
	}

	if len(res) == 0 {
		return nil, state.ErrNoState
	}

	return res, nil
}

func infoForDeviceSnap(st *state.State, deviceCtx DeviceContext, which string, whichName func(*asserts.Model) string) (*snap.Info, error) {
	if deviceCtx == nil {
		return nil, fmt.Errorf("internal error: unset deviceCtx")
	}
	model := deviceCtx.Model()
	snapName := whichName(model)
	if snapName == "" {
		return nil, state.ErrNoState
	}
	var snapst SnapState
	err := Get(st, snapName, &snapst)
	if err != nil {
		return nil, err
	}
	return snapst.CurrentInfo()
}

// GadgetInfo finds the gadget snap's info for the given device context.
func GadgetInfo(st *state.State, deviceCtx DeviceContext) (*snap.Info, error) {
	return infoForDeviceSnap(st, deviceCtx, "gadget", (*asserts.Model).Gadget)
}

// KernelInfo finds the kernel snap's info for the given device context.
func KernelInfo(st *state.State, deviceCtx DeviceContext) (*snap.Info, error) {
	return infoForDeviceSnap(st, deviceCtx, "kernel", (*asserts.Model).Kernel)
}

// BootBaseInfo finds the boot base snap's info for the given device context.
func BootBaseInfo(st *state.State, deviceCtx DeviceContext) (*snap.Info, error) {
	baseName := func(mod *asserts.Model) string {
		base := mod.Base()
		if base == "" {
			return "core"
		}
		return base
	}
	return infoForDeviceSnap(st, deviceCtx, "boot base", baseName)
}

// TODO: reintroduce a KernelInfo(state.State, DeviceContext) if needed
// KernelInfo finds the current kernel snap's info.

// coreInfo finds the current OS snap's info. If both
// "core" and "ubuntu-core" is installed then "core"
// is preferred. Different core names are not supported
// currently and will result in an error.
func coreInfo(st *state.State) (*snap.Info, error) {
	res, err := infosForType(st, snap.TypeOS)
	if err != nil {
		return nil, err
	}

	// a single core: just return it
	if len(res) == 1 {
		return res[0], nil
	}

	// some systems have two cores: ubuntu-core/core
	// we always return "core" in this case
	if len(res) == 2 {
		if res[0].InstanceName() == defaultCoreSnapName && res[1].InstanceName() == "ubuntu-core" {
			return res[0], nil
		}
		if res[0].InstanceName() == "ubuntu-core" && res[1].InstanceName() == defaultCoreSnapName {
			return res[1], nil
		}
		return nil, fmt.Errorf("unexpected cores %q and %q", res[0].InstanceName(), res[1].InstanceName())
	}

	return nil, fmt.Errorf("unexpected number of cores, got %d", len(res))
}

// ConfigDefaults returns the configuration defaults for the snap as
// specified in the gadget for the given device context.
// If gadget is absent or the snap has no snap-id it returns
// ErrNoState.
func ConfigDefaults(st *state.State, deviceCtx DeviceContext, snapName string) (map[string]interface{}, error) {
	info, err := GadgetInfo(st, deviceCtx)
	if err != nil {
		return nil, err
	}

	// system configuration is kept under "core" so apply its defaults when
	// configuring "core"
	isSystemDefaults := snapName == defaultCoreSnapName
	var snapst SnapState
	if err := Get(st, snapName, &snapst); err != nil && err != state.ErrNoState {
		return nil, err
	}

	var snapID string
	if snapst.IsInstalled() {
		snapID = snapst.CurrentSideInfo().SnapID
	}
	// system snaps (core and snapd) snaps can be addressed even without a
	// snap-id via the special "system" value in the config; first-boot
	// always configures the core snap with UseConfigDefaults
	if snapID == "" && !isSystemDefaults {
		return nil, state.ErrNoState
	}

	// no constraints enforced: those should have been checked before already
	gadgetInfo, err := gadget.ReadInfo(info.MountDir(), nil)
	if err != nil {
		return nil, err
	}

	// we support setting core defaults via "system"
	if isSystemDefaults {
		if defaults, ok := gadgetInfo.Defaults["system"]; ok {
			if _, ok := gadgetInfo.Defaults[snapID]; ok && snapID != "" {
				logger.Noticef("core snap configuration defaults found under both 'system' key and core-snap-id, preferring 'system'")
			}

			return defaults, nil
		}
	}

	defaults, ok := gadgetInfo.Defaults[snapID]
	if !ok {
		return nil, state.ErrNoState
	}

	return defaults, nil
}

// GadgetConnections returns the interface connection instructions
// specified in the gadget for the given device context.
// If gadget is absent it returns ErrNoState.
func GadgetConnections(st *state.State, deviceCtx DeviceContext) ([]gadget.Connection, error) {
	info, err := GadgetInfo(st, deviceCtx)
	if err != nil {
		return nil, err
	}

	// no constraints enforced: those should have been checked before already
	gadgetInfo, err := gadget.ReadInfo(info.MountDir(), nil)
	if err != nil {
		return nil, err
	}

	return gadgetInfo.Connections, nil
}
