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

package tasktest

import (
	"fmt"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
)

// SnapOp describes an expected snap operation and its resulting state.
type SnapOp struct {
	Kind      string
	Source    string
	SnapState *snapstate.SnapState
}

// AssertSnapOperations checks task ordering for snap installations and refreshes.
// Callers must separately check the presence of optional tasks for their cases.
func AssertSnapOperations(tasks Selection, ops []SnapOp, opts snapstate.Options) error {
	for _, op := range ops {
		switch op.Source {
		case "store", "path":
		default:
			return fmt.Errorf("snap operation source %q is not implemented", op.Source)
		}

		switch op.Kind {
		case "install", "refresh":
			if err := assertSnapOperation(tasks, op, opts); err != nil {
				return err
			}
		default:
			return fmt.Errorf("snap operation %q is not implemented", op.Kind)
		}
	}
	return nil
}

func assertSnapOperation(tasks Selection, op SnapOp, opts snapstate.Options) error {
	snapst := op.SnapState
	name := snapst.InstanceName().String()
	sn := Snap(name)

	prerequisites := tasks.Select(sn.WithKind("prerequisites").WithField("prerequisites-sync", nil))

	downloadSnap := Missing()
	prepareSnap := Missing()
	validateSnap := Missing()
	mountSnap := Missing()
	switch op.Kind {
	case "install":
		switch op.Source {
		case "store":
			downloadSnap = tasks.Select(sn.WithKind("download-snap"))
			validateSnap = tasks.Select(sn.WithKind("validate-snap"))
		case "path":
			prepareSnap = tasks.Select(sn.WithKind("prepare-snap"))
		}
		mountSnap = tasks.Select(sn.WithKind("mount-snap"))
	case "refresh":
		switch op.Source {
		case "store":
			downloadSnap = tasks.Select(sn.WithKind("download-snap").Optional())
			prepareSnap = tasks.Select(sn.WithKind("prepare-snap").Optional())
			validateSnap = tasks.Select(sn.WithKind("validate-snap").Optional())
		case "path":
			prepareSnap = tasks.Select(sn.WithKind("prepare-snap"))
		}
		mountSnap = tasks.Select(sn.WithKind("mount-snap").Optional())
	}

	syncPrerequisites := tasks.Select(sn.WithKind("prerequisites").WithField("prerequisites-sync", true))
	copySnapData := tasks.Select(sn.WithKind("copy-snap-data"))
	setupProfiles := tasks.Select(sn.WithKind("setup-profiles"))
	linkSnap := tasks.Select(sn.WithKind("link-snap"))
	autoConnect := tasks.Select(sn.WithKind("auto-connect"))
	setAutoAliases := tasks.Select(sn.WithKind("set-auto-aliases"))
	setupAliases := tasks.Select(sn.WithKind("setup-aliases"))

	preferAliases := Missing()
	if opts.Flags.Prefer {
		preferAliases = tasks.Select(sn.WithKind("prefer-aliases"))
	}

	quotaAddSnap := Missing()
	if opts.Flags.QuotaGroupName != "" {
		quotaAddSnap = tasks.Select(sn.WithKind("quota-add-snap"))
	}

	installHook := Missing()
	defaultConfigureHook := Missing()
	preRefreshHook := Missing()
	postRefreshHook := Missing()
	stopSnapServices := Missing()
	removeAliases := Missing()
	unlinkCurrentSnap := Missing()
	cleanup := Missing()

	configureAllowed := snapst.SnapType != "base" && snapst.SnapType != "snapd"
	switch op.Kind {
	case "install":
		installHook = tasks.Select(sn.WithSnapHook("install"))
		if configureAllowed && name != "core" {
			defaultConfigureHook = tasks.Select(sn.WithSnapHook("default-configure"))
		}
	case "refresh":
		preRefreshHook = tasks.Select(sn.WithSnapHook("pre-refresh").Optional())
		postRefreshHook = tasks.Select(sn.WithSnapHook("post-refresh").Optional())
		stopSnapServices = tasks.Select(sn.WithKind("stop-snap-services"))
		removeAliases = tasks.Select(sn.WithKind("remove-aliases"))
		unlinkCurrentSnap = tasks.Select(sn.WithKind("unlink-current-snap"))
		cleanup = tasks.Select(sn.WithKind("cleanup"))
	}

	startSnapServices := tasks.Select(sn.WithKind("start-snap-services"))

	configureHook := Missing()
	if configureAllowed && !opts.Flags.SkipConfigure {
		configureHook = tasks.Select(sn.WithSnapHook("configure"))
	}

	checkHealthHook := Missing()
	if !opts.Flags.SkipConfigure {
		checkHealthHook = tasks.Select(sn.WithSnapHook("check-health"))
	}

	downloadComponents := Missing()
	prepareComponents := Missing()
	validateComponents := Missing()
	mountComponents := Missing()
	linkComponents := Missing()
	unlinkCurrentComponents := Missing()
	preRefreshComponentHooks := Missing()
	installComponentHooks := Missing()
	postRefreshComponentHooks := Missing()

	components := snapst.Sequence.ComponentsForRevision(snapst.Current)
	compCount := len(components)
	if compCount != 0 {
		comps := sn.Components()

		switch op.Source {
		case "path":
			prepareComponents = tasks.Select(comps.WithKind("prepare-component").TaskCount(compCount))
		case "store":
			switch op.Kind {
			case "install":
				downloadComponents = tasks.Select(comps.WithKind("download-component").TaskCount(compCount))
			case "refresh":
				downloadComponents = tasks.Select(comps.WithKind("download-component").All().Optional())
				prepareComponents = tasks.Select(comps.WithKind("prepare-component").All().Optional())
			}
		}

		validatedComps := 0
		for _, component := range components {
			if component.SideInfo.Revision.Store() {
				validatedComps++
			}
		}
		if validatedComps != 0 {
			validateComponents = tasks.Select(comps.
				WithKind("validate-component").TaskCount(validatedComps))
		}

		switch op.Kind {
		case "install":
			mountComponents = tasks.Select(comps.WithKind("mount-component").TaskCount(compCount))
			linkComponents = tasks.Select(comps.WithKind("link-component").TaskCount(compCount))
			installComponentHooks = tasks.Select(sn.WithComponentHook("install").TaskCount(compCount))
		case "refresh":
			mountComponents = tasks.Select(comps.WithKind("mount-component").All().Optional())
			linkComponents = tasks.Select(comps.WithKind("link-component").All().Optional())
			unlinkCurrentComponents = tasks.Select(comps.WithKind("unlink-current-component").All().Optional())
			preRefreshComponentHooks = tasks.Select(sn.WithComponentHook("pre-refresh").All().Optional())
			installComponentHooks = tasks.Select(sn.WithComponentHook("install").All().Optional())
			postRefreshComponentHooks = tasks.Select(sn.WithComponentHook("post-refresh").All().Optional())
		}
	}

	// these belong to the target revision, rather than revision cleanup.
	unlinkExtraComponents := Missing()
	discardComponents := Missing()
	for _, preparation := range Union(downloadSnap, prepareSnap).Tasks() {
		target := sn.Components().WithField("snap-setup-task", preparation.ID())
		unlinkExtraComponents = Union(unlinkExtraComponents,
			tasks.Select(target.WithKind("unlink-component").All().Optional()))
		discardComponents = Union(discardComponents,
			tasks.Select(target.WithKind("discard-component").All().Optional()))
	}

	if err := AssertOrdered(
		prerequisites,
		Union(downloadSnap, prepareSnap),
		validateSnap,
		Union(downloadComponents, prepareComponents, validateComponents),
		syncPrerequisites,
		mountSnap,
		unlinkExtraComponents,
		Union(mountComponents, preRefreshComponentHooks, unlinkCurrentComponents),
		preRefreshHook,
		stopSnapServices,
		removeAliases,
		unlinkCurrentSnap,
		copySnapData,
		setupProfiles,
		linkSnap,
		linkComponents,
		autoConnect,
		setAutoAliases,
		setupAliases,
		preferAliases,
		installHook,
		postRefreshHook,
		Union(installComponentHooks, postRefreshComponentHooks),
		quotaAddSnap,
		defaultConfigureHook,
		startSnapServices,
		discardComponents,
		cleanup,
		configureHook,
		checkHealthHook,
	); err != nil {
		return err
	}

	for _, component := range components {
		cq := sn.WithComponent(component.SideInfo.Component.ComponentName)
		if err := assertComponentTaskOrder(tasks, op, cq, component.SideInfo); err != nil {
			return err
		}
	}

	return assertRevisionCleanupTaskOrder(tasks, sn,
		Union(startSnapServices, discardComponents),
		Union(cleanup, configureHook, checkHealthHook))
}

// assertRevisionCleanupTaskOrder checks removal of old snap revisions and their
// components. Every task in before must precede each revision's removal, and
// every task in after must follow it.
func assertRevisionCleanupTaskOrder(tasks Selection, sn SnapQuery, before, after Selection) error {
	// group removal tasks by their clear-snap task
	clearSnaps := tasks.Select(sn.WithKind("clear-snap").All().Optional())
	for _, clearTask := range clearSnaps.Tasks() {
		clearSnap := clearSnaps.Select(ID(clearTask.ID()))

		removal := sn.WithField("snap-setup-task", clearTask.ID())
		unlinkComponents := tasks.Select(removal.WithKind("unlink-component").All().Optional())
		discardRemovedComponents := tasks.Select(removal.WithKind("discard-component").All().Optional())
		discardSnap := tasks.Select(removal.WithKind("discard-snap"))

		if err := AssertOrdered(
			before,
			clearSnap,
			Union(unlinkComponents, discardRemovedComponents),
			discardSnap,
			after,
		); err != nil {
			return err
		}

		for _, unlinkTask := range unlinkComponents.Tasks() {
			unlinkComponent := unlinkComponents.Select(ID(unlinkTask.ID()))
			discardComponent := tasks.Select(removal.WithKind("discard-component").
				WithField("component-setup-task", unlinkTask.ID()))

			if err := AssertOrdered(unlinkComponent, discardComponent); err != nil {
				return err
			}
		}
	}

	return nil
}

// assertComponentTaskOrder checks the task order for one component. Tasks that
// depend on the state before the operation are optional.
func assertComponentTaskOrder(tasks Selection, op SnapOp, component SnapQuery, csi *snap.ComponentSideInfo) error {
	validateComponent := Missing()
	if csi.Revision.Store() {
		validateComponent = tasks.Select(component.WithKind("validate-component"))
	}

	downloadComponent := Missing()
	prepareComponent := Missing()
	mountComponent := Missing()
	preRefreshComponentHook := Missing()
	unlinkCurrentComponent := Missing()
	linkComponent := Missing()
	installComponentHook := Missing()
	postRefreshComponentHook := Missing()
	switch op.Kind {
	case "install":
		switch op.Source {
		case "store":
			downloadComponent = tasks.Select(component.WithKind("download-component"))
		case "path":
			prepareComponent = tasks.Select(component.WithKind("prepare-component"))
		}
		mountComponent = tasks.Select(component.WithKind("mount-component"))
		linkComponent = tasks.Select(component.WithKind("link-component"))
		installComponentHook = tasks.Select(component.WithComponentHook("install"))
	case "refresh":
		switch op.Source {
		case "store":
			// a store refresh can reuse a component revision that is already
			// present
			downloadComponent = tasks.Select(component.WithKind("download-component").Optional())
			prepareComponent = tasks.Select(component.WithKind("prepare-component").Optional())
		case "path":
			prepareComponent = tasks.Select(component.WithKind("prepare-component"))
		}
		mountComponent = tasks.Select(component.WithKind("mount-component").Optional())
		preRefreshComponentHook = tasks.Select(component.WithComponentHook("pre-refresh").Optional())
		unlinkCurrentComponent = tasks.Select(component.WithKind("unlink-current-component").Optional())
		linkComponent = tasks.Select(component.WithKind("link-component").Optional())
		installComponentHook = tasks.Select(component.WithComponentHook("install").Optional())
		postRefreshComponentHook = tasks.Select(component.WithComponentHook("post-refresh").Optional())
	}

	return AssertOrdered(
		Union(downloadComponent, prepareComponent),
		validateComponent,
		mountComponent,
		preRefreshComponentHook,
		unlinkCurrentComponent,
		linkComponent,
		Union(installComponentHook, postRefreshComponentHook),
	)
}
