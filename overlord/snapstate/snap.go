// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package snapstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/strutil"
)

// installContext carries per-invocation settings that affect how installation
// task graphs are constructed.
type installContext struct {
	SkipConfigure       bool
	NoRestartBoundaries bool
	FromChange          string
	DeviceCtx           DeviceContext
}

// snapInstallTaskSet captures the task ranges involved in a snap installation.
type snapInstallTaskSet struct {
	ts      *state.TaskSet
	snapsup *SnapSetup

	beforeLocalSystemModificationsTasks []*state.Task
	upToLinkSnapAndBeforeReboot         []*state.Task
	afterLinkSnapAndPostReboot          []*state.Task
}

// snapInstallChoreographer orchestrates the construction of a task graph for
// installing a snap. It provides methods that correspond to different stages of
// the installation: before local system modifications, up to link-snap, and
// after link-snap.
type snapInstallChoreographer struct {
	snapst   *SnapState
	snapsup  *SnapSetup
	compsups []ComponentSetup

	componentTSS multiComponentInstallTaskSet
}

func newSnapInstallChoreographer(
	snapst *SnapState,
	snapsup *SnapSetup,
	compsups []ComponentSetup,
) *snapInstallChoreographer {
	return &snapInstallChoreographer{
		snapst:   snapst,
		snapsup:  snapsup,
		compsups: compsups,
	}
}

func (sc *snapInstallChoreographer) requiresKmodSetup() bool {
	return requiresKmodSetup(sc.snapst, sc.compsups)
}

// revisionString returns the formatted revision string for task descriptions.
func (sc *snapInstallChoreographer) revisionString() string {
	return fmt.Sprintf(" (%s)", sc.snapsup.Revision())
}

// revisionIsPresent checks if the snap revision already exists on disk.
func (sc *snapInstallChoreographer) revisionIsPresent() bool {
	return sc.snapst.LastIndex(sc.snapsup.Revision()) >= 0
}

func (sc *snapInstallChoreographer) runRefreshHooks() bool {
	// if the snap is already installed, and the revision we are refreshing to
	// is the same as the current revision, and we're not forcing an update,
	// then we know that we're really modifying the state of components.
	componentOnlyUpdate := sc.snapst.IsInstalled() &&
		sc.snapsup.Revision() == sc.snapst.Current &&
		!sc.snapsup.AlwaysUpdate
	return sc.snapst.IsInstalled() && !componentOnlyUpdate && !sc.snapsup.Flags.Revert
}

func (sc *snapInstallChoreographer) BeforeLocalSystemMod(st *state.State, s *taskChainSpan, ic installContext) ([]*state.Task, error) {
	prereq := st.NewTask("prerequisites", fmt.Sprintf(
		i18n.G("Ensure prerequisites for %q are available"), sc.snapsup.InstanceName()))
	prereq.Set("snap-setup", sc.snapsup)
	s.AppendWithoutData(prereq)
	s.UpdateEdge(prereq, BeginEdge)

	var prepare *state.Task
	// if we have a local revision here we go back to that
	if sc.snapsup.SnapPath != "" || sc.revisionIsPresent() {
		prepare = st.NewTask("prepare-snap", fmt.Sprintf(
			i18n.G("Prepare snap %q%s"), sc.snapsup.SnapPath, sc.revisionString()))
	} else {
		prepare = st.NewTask("download-snap", fmt.Sprintf(
			i18n.G("Download snap %q%s from channel %q"),
			sc.snapsup.InstanceName(), sc.revisionString(), sc.snapsup.Channel))
	}
	prepare.Set("snap-setup", sc.snapsup)
	prepare.WaitFor(prereq)
	s.AppendWithoutData(prepare)
	s.UpdateEdge(prepare, SnapSetupEdge)
	s.UpdateEdge(prepare, LastBeforeLocalModificationsEdge)

	// Let subsequent tasks inherit the snap-setup task id.
	s.SetTaskData(map[string]any{
		"snap-setup-task": prepare.ID(),
	})

	componentTSS, err := splitComponentTasksForInstall(
		sc.compsups, st, sc.snapst, sc.snapsup, prepare, ic.FromChange)
	if err != nil {
		return nil, err
	}
	sc.componentTSS = componentTSS
	prepare.Set("component-setup-tasks", componentTSS.compSetupTaskIDs)

	if prepare.Kind() == "download-snap" {
		// fetch and check assertions
		validate := st.NewTask("validate-snap", fmt.Sprintf(
			i18n.G("Fetch and check assertions for snap %q%s"),
			sc.snapsup.InstanceName(), sc.revisionString()))
		s.Append(validate)
		s.UpdateEdge(validate, LastBeforeLocalModificationsEdge)
	}

	for _, t := range componentTSS.beforeLocalSystemModificationsTasks {
		s.Append(t)
		s.UpdateEdge(t, LastBeforeLocalModificationsEdge)
	}

	return s.Close()
}

func (sc *snapInstallChoreographer) UpToLinkSnapAndBeforeReboot(st *state.State, s *taskChainSpan, ic installContext) ([]*state.Task, error) {
	// mount
	if !sc.revisionIsPresent() {
		mount := st.NewTask("mount-snap", fmt.Sprintf(
			i18n.G("Mount snap %q%s"), sc.snapsup.InstanceName(), sc.revisionString()))
		s.Append(mount)
	} else if sc.snapsup.Flags.RemoveSnapPath {
		// If the revision is local, we will not need the temporary snap. This
		// can happen when e.g. side-loading a local revision again. The
		// SnapPath is only needed in the "mount-snap" handler and that is
		// skipped for local revisions.
		if err := os.Remove(sc.snapsup.SnapPath); err != nil {
			return nil, err
		}
	}

	removeExtraComps, discardExtraComps, err := removeExtraComponentsTasks(st, sc.snapst, sc.snapsup.Revision(), sc.compsups)
	if err != nil {
		return nil, err
	}
	for _, t := range removeExtraComps {
		s.Append(t)
	}
	sc.componentTSS.discardTasks = append(sc.componentTSS.discardTasks, discardExtraComps...)

	for _, t := range sc.componentTSS.beforeLinkTasks {
		s.Append(t)
	}

	if sc.runRefreshHooks() {
		// run refresh hooks when updating existing snap, otherwise run install hook
		// further down.
		hook := SetupPreRefreshHook(st, sc.snapsup.InstanceName())
		s.Append(hook)
	}

	if sc.snapst.IsInstalled() {
		// unlink-current-snap (will stop services for copy-data)
		stop := st.NewTask("stop-snap-services", fmt.Sprintf(
			i18n.G("Stop snap %q services"), sc.snapsup.InstanceName()))
		stop.Set("stop-reason", snap.StopReasonRefresh)
		s.Append(stop)

		removeAliases := st.NewTask("remove-aliases", fmt.Sprintf(
			i18n.G("Remove aliases for snap %q"), sc.snapsup.InstanceName()))
		removeAliases.Set("remove-reason", removeAliasesReasonRefresh)
		s.Append(removeAliases)

		unlink := st.NewTask("unlink-current-snap", fmt.Sprintf(
			i18n.G("Make current revision for snap %q unavailable"), sc.snapsup.InstanceName()))
		unlink.Set("unlink-reason", unlinkReasonRefresh)
		s.Append(unlink)
	}

	// This task is necessary only for UC24+ and hybrid 24.04+
	if sc.snapsup.Type == snap.TypeKernel && kernel.NeedsKernelDriversTree(ic.DeviceCtx.Model()) {
		setupKernel := st.NewTask("prepare-kernel-snap", fmt.Sprintf(
			i18n.G("Prepare kernel driver tree for %q%s"), sc.snapsup.InstanceName(), sc.revisionString()))
		s.Append(setupKernel)
	}

	// gadget update currently for core boot systems only
	if ic.DeviceCtx.IsCoreBoot() && (sc.snapsup.Type == snap.TypeGadget || (sc.snapsup.Type == snap.TypeKernel && !TestingLeaveOutKernelUpdateGadgetAssets)) {
		gadgetUpdate := st.NewTask("update-gadget-assets", fmt.Sprintf(
			i18n.G("Update assets from %s %q%s"), sc.snapsup.Type, sc.snapsup.InstanceName(), sc.revisionString()))
		s.Append(gadgetUpdate)
	}

	// kernel command line from gadget is for core boot systems only
	if ic.DeviceCtx.IsCoreBoot() && sc.snapsup.Type == snap.TypeGadget {
		// check whether there are other changes that need to run exclusively
		if err := CheckChangeConflictExclusiveKinds(st, ic.FromChange); err != nil {
			return nil, err
		}
		cmdline := st.NewTask("update-gadget-cmdline", fmt.Sprintf(
			i18n.G("Update kernel command line from gadget %q%s"),
			sc.snapsup.InstanceName(), sc.revisionString()))
		s.Append(cmdline)
	}

	// copy-data (needs stopped services by unlink)
	if !sc.snapsup.Flags.Revert {
		copyData := st.NewTask("copy-snap-data", fmt.Sprintf(
			i18n.G("Copy snap %q data"), sc.snapsup.InstanceName()))
		s.Append(copyData)
	}

	// Insert the profiles task that will restore profiles if needed.
	prepareSecurity := st.NewTask("prepare-profiles", fmt.Sprintf(
		i18n.G("Prepare snap %q%s for security profile setup"),
		sc.snapsup.InstanceName(), sc.revisionString()))
	s.Append(prepareSecurity)

	// finalize (wrappers+current symlink)
	//
	// For essential snaps that require reboots, 'link-snap' is currently
	// marked as the edge of that reboot sequence. This means that we currently
	// expect 'link-snap' to request the reboot and be the last task to run
	// before the reboot takes place (for that lane/change). This task is
	// assigned the edge 'MaybeRebootEdge' to indicate this.
	//
	// 'link-snap' is the last task to run before a reboot for cases like the kernel
	// where we would like to try to make sure it boots correctly before we perform
	// additional tasks.
	linkSnap := st.NewTask("link-snap", fmt.Sprintf(
		i18n.G("Make snap %q%s available to the system"), sc.snapsup.InstanceName(), sc.revisionString()))
	linkSnap.Set("set-next-boot", !sc.requiresKmodSetup())
	s.Append(linkSnap)
	s.UpdateEdge(linkSnap, MaybeRebootEdge)

	if sc.requiresKmodSetup() {
		logger.Noticef("kernel-modules components present, delaying reboot after hooks are run")

		// when we are installing/updating kernel module components we run hooks
		// before we schedule the reboot. otherwise, hooks are scheduled for
		// after the reboot
		const postReboot = false
		if err := sc.addLinkComponentThroughHooks(st, s, ic, postReboot, ic.DeviceCtx); err != nil {
			return nil, err
		}

		// TODO move the setupKernel task here and make it configure
		// kernel-modules components too so we can remove this task.
		setupKmodComponents := st.NewTask("prepare-kernel-modules-components", fmt.Sprintf(
			i18n.G("Prepare kernel-modules components for %q%s"),
			sc.snapsup.InstanceName(), sc.revisionString()))
		setupKmodComponents.Set("set-next-boot", true)
		s.Append(setupKmodComponents)
		s.UpdateEdge(setupKmodComponents, MaybeRebootEdge)
	}

	return s.Close()
}

// removeExtraComponentsTasks creates tasks that will remove unwanted components
// that are currently installed alongside the target snap revision. If the new
// snap revision is not in the sequence, then we don't have anything to do. If
// the revision is in the sequence, then we generate tasks that will unlink
// components that are not in compsups.
//
// This is mostly relevant when we're moving from one snap revision to another
// snap revision that was installed in past and is still in the sequence. The
// target snap might have had components that were installed alongside it in the
// past, and they are not wanted anymore.
func removeExtraComponentsTasks(st *state.State, snapst *SnapState, targetRevision snap.Revision, compsups []ComponentSetup) (
	unlinkTasks, discardTasks []*state.Task, err error,
) {
	idx := snapst.LastIndex(targetRevision)
	if idx < 0 {
		return nil, nil, nil
	}

	keep := make(map[naming.ComponentRef]bool, len(compsups))
	for _, compsup := range compsups {
		keep[compsup.CompSideInfo.Component] = true
	}

	linkedForRevision, err := snapst.ComponentInfosForRevision(targetRevision)
	if err != nil {
		return nil, nil, err
	}

	for _, ci := range linkedForRevision {
		if keep[ci.Component] {
			continue
		}

		// note that we shouldn't ever be able to lose components during a
		// refresh without a snap revision change. this might be able to happen
		// once we introduce components and validation sets? if that is the
		// case, we'll need to take care here to use "unlink-current-component"
		// and point it to the correct snap setup task.
		if snapst.Current == targetRevision {
			return nil, nil, errors.New("internal error: cannot lose a component during a refresh without a snap revision change")
		}

		// note that we don't need to worry about kernel module components here,
		// since the components that we are removing are not associated with the
		// current snap revision. unlink-component differs from
		// unlink-current-component in that it doesn't save the state of kernel
		// module components on the the SnapSetup.
		unlink := st.NewTask("unlink-component", fmt.Sprintf(
			i18n.G("Unlink component %q for snap revision %s"), ci.Component, targetRevision,
		))

		unlink.Set("component-setup", ComponentSetup{
			CompSideInfo: &ci.ComponentSideInfo,
			CompType:     ci.Type,
		})
		unlinkTasks = append(unlinkTasks, unlink)

		if !snapst.Sequence.IsComponentRevInRefSeqPtInAnyOtherSeqPt(ci.Component, idx) {
			discard := st.NewTask("discard-component", fmt.Sprintf(
				i18n.G("Discard previous revision for component %q"), ci.Component,
			))
			discard.Set("component-setup-task", unlink.ID())
			discardTasks = append(discardTasks, discard)
		}
	}

	return unlinkTasks, discardTasks, nil
}

func (sc *snapInstallChoreographer) AfterLinkSnapAndPostReboot(st *state.State, s *taskChainSpan, ic installContext) ([]*state.Task, error) {
	if !sc.requiresKmodSetup() {
		// Let tasks know if they have to do something about restarts
		// No kernel modules, reboot after link snap
		const postReboot = true
		if err := sc.addLinkComponentThroughHooks(st, s, ic, postReboot, ic.DeviceCtx); err != nil {
			return nil, err
		}
	}

	if sc.snapsup.Type == snap.TypeKernel && kernel.NeedsKernelDriversTree(ic.DeviceCtx.Model()) {
		// This task needs to run after we're back and running the new
		// kernel after a reboot was requested in link-snap handler.
		discardOldKernelSnapSetup := st.NewTask("discard-old-kernel-snap-setup", fmt.Sprintf(
			i18n.G("Discard previous kernel driver tree for %q%s"), sc.snapsup.InstanceName(), sc.revisionString()))
		s.Append(discardOldKernelSnapSetup)
		discardOldKernelSnapSetup.Set("finish-restart", sc.requiresKmodSetup())
		// Note that if requiresKmodSetup is true, NeedsKernelDriversTree must
		// be too
		if sc.requiresKmodSetup() {
			s.UpdateEdge(discardOldKernelSnapSetup, MaybeRebootWaitEdge)
		}
	}

	if sc.snapsup.QuotaGroupName != "" {
		quotaAddSnapTask, err := AddSnapToQuotaGroup(st, sc.snapsup.InstanceName(), sc.snapsup.QuotaGroupName)
		if err != nil {
			return nil, err
		}
		s.Append(quotaAddSnapTask)
	}

	// only run default-configure hook if installing the snap for the first time and
	// default-configure is allowed
	if !sc.snapst.IsInstalled() && isDefaultConfigureAllowed(sc.snapsup) {
		defaultCfg := DefaultConfigure(st, sc.snapsup.InstanceName())
		s.AppendTSWithoutData(defaultCfg)
	}

	// run new services
	startSnapServices := st.NewTask("start-snap-services", fmt.Sprintf(
		i18n.G("Start snap %q%s services"), sc.snapsup.InstanceName(), sc.revisionString()))
	s.Append(startSnapServices)
	s.UpdateEdge(startSnapServices, EndEdge)

	for _, t := range sc.componentTSS.discardTasks {
		s.Append(t)
		s.UpdateEdge(t, EndEdge)
	}

	// Do not do that if we are reverting to a local revision
	if sc.snapst.IsInstalled() && !sc.snapsup.Flags.Revert {
		// addCleanupTasks will set EndEdge on the last task
		if err := sc.addCleanupTasks(st, s, ic); err != nil {
			return nil, err
		}
	}

	if ic.SkipConfigure {
		return s.Close()
	}

	if isConfigureAllowed(sc.snapsup) {
		confFlags := configureSnapFlags(sc.snapst, sc.snapsup)
		configSet := ConfigureSnap(st, sc.snapsup.InstanceName(), confFlags)
		s.AppendTSWithoutData(configSet)
	}

	healthCheck := CheckHealthHook(st, sc.snapsup.InstanceName(), sc.snapsup.Revision())
	s.Append(healthCheck)
	s.UpdateEdge(healthCheck, EndEdge)

	return s.Close()
}

// addLinkComponentThroughHooks builds the chain of tasks that starts with any
// component linking tasks, continues through auto-connect, and then runs through
// any post-refresh/install hooks. Depending on whether kernel-module preparation
// is required, this chain may belong either to the up-to-link-snap or
// after-link-snap taskChainSpan, so the taskChainSpan is provided by the caller.
func (sc *snapInstallChoreographer) addLinkComponentThroughHooks(
	st *state.State,
	s *taskChainSpan,
	ic installContext,
	postReboot bool,
	deviceCtx DeviceContext,
) error {
	// link components
	for _, t := range sc.componentTSS.linkTasks {
		s.Append(t)
		if postReboot {
			s.UpdateEdgeIfUnset(t, MaybeRebootWaitEdge)
		}
	}

	// auto-connections
	//
	// For essential snaps that require reboots, 'auto-connect' is marked
	// as edge 'MaybeRebootWaitEdge' to indicate that this task is expected
	// to be the first to run after the reboot (for that lane/change). This
	// is noted here to make sure we consider any changes between 'link-snap'
	// and 'auto-connect', as that need the edges to be modified as well.
	//
	// 'auto-connect' is expected to run first after the reboot as it also
	// performs some reboot-verification code.
	autoConnect := st.NewTask("auto-connect", fmt.Sprintf(
		i18n.G("Automatically connect eligible plugs and slots of snap %q"), sc.snapsup.InstanceName()))
	autoConnect.Set("finish-restart", postReboot)
	s.Append(autoConnect)
	if postReboot {
		s.UpdateEdgeIfUnset(autoConnect, MaybeRebootWaitEdge)
	}

	// setup aliases
	setAutoAliases := st.NewTask("set-auto-aliases", fmt.Sprintf(
		i18n.G("Set automatic aliases for snap %q"), sc.snapsup.InstanceName()))
	s.Append(setAutoAliases)

	setupAliases := st.NewTask("setup-aliases", fmt.Sprintf(
		i18n.G("Setup snap %q aliases"), sc.snapsup.InstanceName()))
	s.Append(setupAliases)
	// BeforeHooksEdge is used by preseeding to know up to which task to run
	s.UpdateEdge(setupAliases, BeforeHooksEdge)

	if snapdenv.Preseeding() && sc.requiresKmodSetup() {
		// We need this task as the other
		// prepare-kernel-modules-components defined below will not be
		// run when creating a preseeding tarball, but we still need to
		// have a correct driver tree in the tarball. This implies that
		// if some kernel module is created by the install hook, it
		// will be available only after full installation on first
		// boot, but static modules in the components where be
		// available early.
		// TODO move the setupKernel task here and make it configure
		// kernel-modules components too so we can remove this task.
		preseedKmod := st.NewTask("prepare-kernel-modules-components", fmt.Sprintf(
			i18n.G("Prepare kernel-modules components for %q%s"),
			sc.snapsup.InstanceName(), sc.revisionString()))
		s.Append(preseedKmod)
		s.UpdateEdge(preseedKmod, BeforeHooksEdge)
	}

	if sc.snapsup.Flags.Prefer {
		prefer := st.NewTask("prefer-aliases", fmt.Sprintf(
			i18n.G("Prefer aliases for snap %q"), sc.snapsup.InstanceName()))
		s.Append(prefer)
	}

	if deviceCtx.IsCoreBoot() && sc.snapsup.Type == snap.TypeSnapd {
		// check whether there are other changes that need to run exclusively
		if err := CheckChangeConflictExclusiveKinds(st, ic.FromChange); err != nil {
			return err
		}
		// only run for core devices and the snapd snap, run late enough
		// so that the task is executed by the new snapd
		bootCfg := st.NewTask("update-managed-boot-config", fmt.Sprintf(
			i18n.G("Update managed boot config assets from %q%s"),
			sc.snapsup.InstanceName(), sc.revisionString()))
		s.Append(bootCfg)
	}

	if sc.runRefreshHooks() {
		hook := SetupPostRefreshHook(st, sc.snapsup.InstanceName())
		s.Append(hook)
	}

	if !sc.snapst.IsInstalled() {
		// only run install hook if installing the snap for the first time
		hook := SetupInstallHook(st, sc.snapsup.InstanceName())
		s.Append(hook)
		s.UpdateEdge(hook, HooksEdge)
	}

	for _, t := range sc.componentTSS.postHookToDiscardTasks {
		s.Append(t)
	}

	return nil
}

func (sc *snapInstallChoreographer) addCleanupTasks(st *state.State, s *taskChainSpan, ic installContext) error {
	retain := refreshRetain(st)
	// if we're not using an already present revision, account for the one being added
	if !sc.revisionIsPresent() {
		retain--
	}

	seq := sc.snapst.Sequence.Revisions
	currentIndex := sc.snapst.LastIndex(sc.snapst.Current)

	// discard everything after "current" (we may have reverted to
	// a previous versions earlier)
	for i := currentIndex + 1; i < len(seq); i++ {
		si := seq[i]
		if si.Snap.Revision == sc.snapsup.Revision() {
			// but don't discard this one; its' the thing we're switching to!
			continue
		}
		ts, err := removeInactiveRevision(st, sc.snapst, sc.snapsup.InstanceName(), si.Snap.SnapID, si.Snap.Revision, sc.snapsup.Type)
		if err != nil {
			return err
		}
		s.AppendTSWithoutData(ts)
	}

	// make sure we're not scheduling the removal of the target revision in the
	// case where the target revision is already in the sequence.
	for i := 0; i < currentIndex; i++ {
		si := seq[i]
		if si.Snap.Revision == sc.snapsup.Revision() {
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
			inUse, err = boot.InUse(sc.snapsup.Type, ic.DeviceCtx)
			if err != nil {
				return err
			}
		}
		si := seq[i]
		if inUse(sc.snapsup.InstanceName(), si.Snap.Revision) {
			continue
		}
		ts, err := removeInactiveRevision(st, sc.snapst, sc.snapsup.InstanceName(), si.Snap.SnapID, si.Snap.Revision, sc.snapsup.Type)
		if err != nil {
			return err
		}
		s.AppendTSWithoutData(ts)
	}

	cleanup := st.NewTask("cleanup", fmt.Sprintf(
		i18n.G("Clean up %q (%s) install"), sc.snapsup.InstanceName(), sc.snapsup.Revision()))
	s.Append(cleanup)
	s.UpdateEdge(cleanup, EndEdge)

	return nil
}

func (sc *snapInstallChoreographer) choreograph(st *state.State, ic installContext) (snapInstallTaskSet, error) {
	b := newTaskChainBuilder()

	beforeLocalSystemMods, err := sc.BeforeLocalSystemMod(st, b.OpenSpan(), ic)
	if err != nil {
		return snapInstallTaskSet{}, err
	}

	upToLinkSnapAndBeforeReboot, err := sc.UpToLinkSnapAndBeforeReboot(st, b.OpenSpan(), ic)
	if err != nil {
		return snapInstallTaskSet{}, err
	}

	afterLinkSnapAndPostReboot, err := sc.AfterLinkSnapAndPostReboot(st, b.OpenSpan(), ic)
	if err != nil {
		return snapInstallTaskSet{}, err
	}

	if !ic.NoRestartBoundaries {
		if err := SetEssentialSnapsRestartBoundaries(st, nil, []*state.TaskSet{b.TaskSet()}); err != nil {
			return snapInstallTaskSet{}, err
		}
	}

	return snapInstallTaskSet{
		ts:      b.TaskSet(),
		snapsup: sc.snapsup,

		beforeLocalSystemModificationsTasks: beforeLocalSystemMods,
		upToLinkSnapAndBeforeReboot:         upToLinkSnapAndBeforeReboot,
		afterLinkSnapAndPostReboot:          afterLinkSnapAndPostReboot,
	}, nil
}

// doInstallOrPreDownload either returns a task set that contains all tasks
// needed to install the given snap, or it returns a task set that contains the
// tasks needed to initiate a pre-download. In the pre-download case, an error
// is also returned that the caller can use to differentiate between the two
// conditions.
func doInstallOrPreDownload(st *state.State, snapst *SnapState, snapsup *SnapSetup, compsups []ComponentSetup, ic installContext) (snapInstallTaskSet, error) {
	if ic.DeviceCtx == nil {
		dctx, err := DeviceCtx(st, nil, nil)
		if err != nil {
			return snapInstallTaskSet{}, err
		}
		ic.DeviceCtx = dctx
	}

	if err := checkInstallPreconditions(st, snapst, snapsup, ic); err != nil {
		return snapInstallTaskSet{}, err
	}

	// TODO: this feels like a hack that we could drop in some way?
	if snapst.IsInstalled() {
		info, err := snapst.CurrentInfo()
		if err != nil {
			return snapInstallTaskSet{}, err
		}

		// adjust plugs-only hint to match existing behavior
		snapsup.PlugsOnly = snapsup.PlugsOnly && len(info.Slots) == 0
	}

	// note that because we are modifying the snap state inside of
	// shouldPreDownloadSnap, this check must be located after the precondition
	// checks done above
	busyErr, err := shouldPreDownloadSnap(st, snapsup, snapst)
	if err != nil {
		return snapInstallTaskSet{}, err
	}

	// snap is busy, return a pre-download task set and the busyErr for the
	// caller to handle
	if busyErr != nil {
		existing, err := findTasksMatchingKindAndSnap(st, "pre-download-snap", snapsup.InstanceName(), snapsup.Revision())
		if err != nil {
			return snapInstallTaskSet{}, err
		}
		for _, task := range existing {
			switch task.Status() {
			case state.DoStatus, state.DoingStatus:
				return snapInstallTaskSet{}, busyErr
			}
		}

		ts := state.NewTaskSet()

		preDownload := st.NewTask("pre-download-snap", fmt.Sprintf(
			i18n.G("Pre-download snap %q (%s) from channel %q"),
			snapsup.InstanceName(), snapsup.Revision(), snapsup.Channel))
		preDownload.Set("snap-setup", snapsup)

		preDownload.Set("refresh-info", busyErr.PendingSnapRefreshInfo())
		ts.AddTask(preDownload)

		return snapInstallTaskSet{
			ts:      ts,
			snapsup: snapsup,
		}, busyErr
	}

	tr := config.NewTransaction(st)
	experimentalGateAutoRefreshHook, err := features.Flag(tr, features.GateAutoRefreshHook)
	if err != nil && !config.IsNoOption(err) {
		return snapInstallTaskSet{}, err
	}
	if experimentalGateAutoRefreshHook && snapst.IsInstalled() {
		// If this snap was held, then remove it from snaps-hold.
		if err := resetGatingForRefreshed(st, snapsup.InstanceName()); err != nil {
			return snapInstallTaskSet{}, err
		}
	}

	choreo := newSnapInstallChoreographer(snapst, snapsup, compsups)
	installTS, err := choreo.choreograph(st, ic)
	if err != nil {
		return snapInstallTaskSet{}, err
	}

	return installTS, nil
}

// shouldPreDownloadSnap returns a timedBusySnapError when we should enqueue a
// pre-download-snap task for the given snap revision. A nil busyErr means no
// pre-download is needed. Errors unrelated to a busy snap are returned via err.
func shouldPreDownloadSnap(st *state.State, snapsup *SnapSetup, snapst *SnapState) (*timedBusySnapError, error) {
	if !snapst.IsInstalled() {
		return nil, nil
	}

	tr := config.NewTransaction(st)
	experimentalRefreshAppAwareness, err := features.Flag(tr, features.RefreshAppAwareness)
	if err != nil && !config.IsNoOption(err) {
		return nil, err
	}
	if !experimentalRefreshAppAwareness || excludeFromRefreshAppAwareness(snapsup.Type) || snapsup.Flags.IgnoreRunning {
		return nil, nil
	}

	info, err := snapst.CurrentInfo()
	if err != nil {
		return nil, err
	}

	if err := softCheckNothingRunningForRefresh(st, snapst, snapsup, info); err != nil {
		var busyErr *timedBusySnapError
		if !errors.As(err, &busyErr) || !snapsup.IsAutoRefresh {
			return nil, err
		}
		return busyErr, nil
	}

	return nil, nil
}

// checkInstallPreconditions performs the pre-flight checks currently done at the
// start of doInstall. It may mutate snapsup (e.g. PlugsOnly tweak) to keep
// semantics identical to the existing flow.
func checkInstallPreconditions(st *state.State, snapst *SnapState, snapsup *SnapSetup, ic installContext) error {
	if snapsup.InstanceName() == "system" {
		return fmt.Errorf("cannot install reserved snap name 'system'")
	}

	if snapst.IsInstalled() && !snapst.Active {
		return fmt.Errorf("cannot update disabled snap %q", snapsup.InstanceName())
	}

	if snapsup.Flags.Classic {
		if !release.OnClassic {
			return fmt.Errorf("classic confinement is only supported on classic systems")
		}
		if !dirs.SupportsClassicConfinement() {
			return fmt.Errorf(i18n.G("classic confinement requires snaps under /snap or symlink from /snap to %s"), dirs.SnapMountDir)
		}
	}

	if !snapst.IsInstalled() {
		if err := checkSnapAliasConflict(st, snapsup.InstanceName()); err != nil {
			return err
		}
	}

	if err := isParallelInstallable(snapsup); err != nil {
		return err
	}

	if err := checkChangeConflictIgnoringOneChange(st, snapsup.InstanceName(), snapst, ic.FromChange); err != nil {
		return err
	}

	if snapst.IsInstalled() {
		info, err := snapst.CurrentInfo()
		if err != nil {
			return err
		}

		// When downgrading snapd we want to make sure that it's an exclusive change.
		if snapsup.SnapName() == "snapd" {
			res, err := strutil.VersionCompare(info.Version, snapsup.Version)
			if err != nil {
				return fmt.Errorf("cannot compare versions of snapd [cur: %s, new: %s]: %v", info.Version, snapsup.Version, err)
			}
			// If snapsup.Version was smaller, 1 is returned.
			if res == 1 {
				if err := checkChangeConflictExclusiveKinds(st, "snapd downgrade", ic.FromChange); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func requiresKmodSetup(snapst *SnapState, compsups []ComponentSetup) bool {
	current := snapst.Sequence.ComponentsWithTypeForRev(snapst.Current, snap.KernelModulesComponent)
	if len(current) > 0 {
		return true
	}

	for _, compsup := range compsups {
		if compsup.CompType == snap.KernelModulesComponent {
			return true
		}
	}
	return false
}

// multiComponentInstallTaskSet contains the tasks that are needed to install
// multiple components. The tasks are partitioned into groups so that they can
// be easily spliced into the chain of tasks created to install a snap.
type multiComponentInstallTaskSet struct {
	compSetupTaskIDs                    []string
	beforeLocalSystemModificationsTasks []*state.Task
	beforeLinkTasks                     []*state.Task
	linkTasks                           []*state.Task
	postHookToDiscardTasks              []*state.Task
	discardTasks                        []*state.Task
}

func newMultiComponentInstallTaskSet(ctss ...componentInstallTaskSet) multiComponentInstallTaskSet {
	var mcts multiComponentInstallTaskSet
	for _, cts := range ctss {
		mcts.compSetupTaskIDs = append(mcts.compSetupTaskIDs, cts.compSetupTaskID)
		mcts.beforeLocalSystemModificationsTasks = append(mcts.beforeLocalSystemModificationsTasks, cts.beforeLocalSystemModificationsTasks...)
		mcts.beforeLinkTasks = append(mcts.beforeLinkTasks, cts.beforeLinkTasks...)
		if cts.maybeLinkTask != nil {
			mcts.linkTasks = append(mcts.linkTasks, cts.maybeLinkTask)
		}
		mcts.postHookToDiscardTasks = append(mcts.postHookToDiscardTasks, cts.postHookToDiscardTasks...)
		if cts.maybeDiscardTask != nil {
			mcts.discardTasks = append(mcts.discardTasks, cts.maybeDiscardTask)
		}
	}
	return mcts
}

func splitComponentTasksForInstall(
	compsups []ComponentSetup,
	st *state.State,
	snapst *SnapState,
	snapsup *SnapSetup,
	snapsupTask *state.Task,
	fromChange string,
) (multiComponentInstallTaskSet, error) {
	componentTSS := make([]componentInstallTaskSet, 0, len(compsups))
	for _, compsup := range compsups {
		cts, err := doInstallComponent(st, snapst, compsup, snapsup, snapsupTask, nil, nil, fromChange)
		if err != nil {
			return multiComponentInstallTaskSet{}, fmt.Errorf("cannot install component %q: %v", compsup.CompSideInfo.Component, err)
		}
		componentTSS = append(componentTSS, cts)
	}
	return newMultiComponentInstallTaskSet(componentTSS...), nil
}

func findTasksMatchingKindAndSnap(st *state.State, kind string, snapName string, revision snap.Revision) ([]*state.Task, error) {
	var tasks []*state.Task
	for _, t := range st.Tasks() {
		if t.Kind() != kind {
			continue
		}

		snapsup, _, err := snapSetupAndState(t)
		if err != nil {
			return nil, err
		}

		if snapsup.InstanceName() == snapName && snapsup.Revision() == revision {
			tasks = append(tasks, t)
		}
	}

	return tasks, nil
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

// refreshRetain returns refresh.retain value if set, or the default value (different for core and classic).
// It deals with potentially wrong type due to lax validation.
func refreshRetain(st *state.State) int {
	var val any
	// due to lax validation of refresh.retain on set we might end up having a string representing a number here; handle it gracefully
	// for backwards compatibility.
	err := config.NewTransaction(st).Get("core", "refresh.retain", &val)
	var retain int
	if err == nil {
		switch v := val.(type) {
		// this is the expected value; confusingly, since we pass any to Get(), we get json.Number type; if int reference was passed,
		// we would get an int instead of json.Number.
		case json.Number:
			retain, err = strconv.Atoi(string(v))
		// not really expected when requesting any.
		case int:
			retain = v
		// we can get string here due to lax validation of refresh.retain on Set in older releases.
		case string:
			retain, err = strconv.Atoi(v)
		default:
			logger.Noticef("internal error: refresh.retain system option has unexpected type: %T", v)
		}
	}

	// this covers error from Get() and strconv above.
	if err != nil && !config.IsNoOption(err) {
		logger.Noticef("internal error: refresh.retain system option is not valid: %v", err)
	}

	// not set, use default value
	if retain == 0 {
		// on classic we only keep 2 copies by default
		if release.OnClassic {
			retain = 2
		} else {
			retain = 3
		}
	}
	return retain
}

var excludeFromRefreshAppAwareness = func(t snap.Type) bool {
	return t == snap.TypeSnapd || t == snap.TypeOS
}

func isDefaultConfigureAllowed(snapsup *SnapSetup) bool {
	return isConfigureAllowed(snapsup) && !isCoreSnap(snapsup.InstanceName())
}

func isConfigureAllowed(snapsup *SnapSetup) bool {
	// we do not support configuration for bases or the "snapd" snap yet
	return snapsup.Type != snap.TypeBase && snapsup.Type != snap.TypeSnapd
}

func configureSnapFlags(snapst *SnapState, snapsup *SnapSetup) int {
	confFlags := 0
	// config defaults cannot be retrieved without a snap ID
	hasSnapID := snapsup.SideInfo != nil && snapsup.SideInfo.SnapID != ""

	if !snapst.IsInstalled() && hasSnapID && !isCoreSnap(snapsup.InstanceName()) {
		// installation, run configure using the gadget defaults if available, system config defaults (attached to
		// "core") are consumed only during seeding, via an explicit configure step separate from installing
		confFlags |= UseConfigDefaults
	}
	return confFlags
}

func isCoreSnap(snapName string) bool {
	return snapName == defaultCoreSnapName
}
