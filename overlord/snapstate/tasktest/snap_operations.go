package tasktest

import (
	"fmt"

	"github.com/snapcore/snapd/overlord/snapstate"
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

	downloadSnap := Empty()
	prepareSnap := Empty()
	validateSnap := Empty()
	mountSnap := Empty()
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
		downloadSnap = tasks.SelectOptional(sn.WithKind("download-snap"))
		prepareSnap = tasks.SelectOptional(sn.WithKind("prepare-snap"))
		validateSnap = tasks.SelectOptional(sn.WithKind("validate-snap"))
		mountSnap = tasks.SelectOptional(sn.WithKind("mount-snap"))
	}

	syncPrerequisites := tasks.Select(sn.WithKind("prerequisites").WithField("prerequisites-sync", true))
	copySnapData := tasks.Select(sn.WithKind("copy-snap-data"))
	setupProfiles := tasks.Select(sn.WithKind("setup-profiles"))
	linkSnap := tasks.Select(sn.WithKind("link-snap"))
	autoConnect := tasks.Select(sn.WithKind("auto-connect"))
	setAutoAliases := tasks.Select(sn.WithKind("set-auto-aliases"))
	setupAliases := tasks.Select(sn.WithKind("setup-aliases"))

	preferAliases := Empty()
	if opts.Flags.Prefer {
		preferAliases = tasks.Select(sn.WithKind("prefer-aliases"))
	}

	quotaAddSnap := Empty()
	if opts.Flags.QuotaGroupName != "" {
		quotaAddSnap = tasks.Select(sn.WithKind("quota-add-snap"))
	}

	installHook := Empty()
	defaultConfigureHook := Empty()
	preRefreshHook := Empty()
	postRefreshHook := Empty()
	stopSnapServices := Empty()
	removeAliases := Empty()
	unlinkCurrentSnap := Empty()
	cleanup := Empty()

	configureAllowed := snapst.SnapType != "base" && snapst.SnapType != "snapd"
	switch op.Kind {
	case "install":
		installHook = tasks.Select(sn.WithSnapHook("install"))
		if configureAllowed && name != "core" {
			defaultConfigureHook = tasks.Select(sn.WithSnapHook("default-configure"))
		}
	case "refresh":
		preRefreshHook = tasks.SelectOptional(sn.WithSnapHook("pre-refresh"))
		postRefreshHook = tasks.SelectOptional(sn.WithSnapHook("post-refresh"))
		stopSnapServices = tasks.Select(sn.WithKind("stop-snap-services"))
		removeAliases = tasks.Select(sn.WithKind("remove-aliases"))
		unlinkCurrentSnap = tasks.Select(sn.WithKind("unlink-current-snap"))
		cleanup = tasks.Select(sn.WithKind("cleanup"))
	}

	startSnapServices := tasks.Select(sn.WithKind("start-snap-services"))

	configureHook := Empty()
	if configureAllowed && !opts.Flags.SkipConfigure {
		configureHook = tasks.Select(sn.WithSnapHook("configure"))
	}

	checkHealthHook := Empty()
	if !opts.Flags.SkipConfigure {
		checkHealthHook = tasks.Select(sn.WithSnapHook("check-health"))
	}

	downloadComponents := Empty()
	prepareComponents := Empty()
	validateComponents := Empty()
	mountComponents := Empty()
	linkComponents := Empty()
	unlinkCurrentComponents := Empty()
	preRefreshComponentHooks := Empty()
	installComponentHooks := Empty()
	postRefreshComponentHooks := Empty()

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
				downloadComponents = tasks.SelectOptional(comps.WithKind("download-component").All())
				prepareComponents = tasks.SelectOptional(comps.WithKind("prepare-component").All())
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
			mountComponents = tasks.SelectOptional(comps.WithKind("mount-component").All())
			linkComponents = tasks.SelectOptional(comps.WithKind("link-component").All())
			unlinkCurrentComponents = tasks.SelectOptional(comps.WithKind("unlink-current-component").All())
			preRefreshComponentHooks = tasks.SelectOptional(sn.WithComponentHook("pre-refresh").All())
			installComponentHooks = tasks.SelectOptional(sn.WithComponentHook("install").All())
			postRefreshComponentHooks = tasks.SelectOptional(sn.WithComponentHook("post-refresh").All())
		}
	}

	// these belong to the target revision, rather than revision cleanup.
	unlinkExtraComponents := Empty()
	discardComponents := Empty()
	for _, preparation := range Union(downloadSnap, prepareSnap).Tasks() {
		target := sn.Components().WithField("snap-setup-task", preparation.ID())
		unlinkExtraComponents = Union(unlinkExtraComponents,
			tasks.SelectOptional(target.WithKind("unlink-component").All()))
		discardComponents = Union(discardComponents,
			tasks.SelectOptional(target.WithKind("discard-component").All()))
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
		if err := assertComponentTaskOrder(tasks, cq); err != nil {
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
	clearSnaps := tasks.SelectOptional(sn.WithKind("clear-snap").All())
	for _, clearTask := range clearSnaps.Tasks() {
		clearSnap := clearSnaps.Select(ID(clearTask.ID()))

		removal := sn.WithField("snap-setup-task", clearTask.ID())
		unlinkComponents := tasks.SelectOptional(removal.WithKind("unlink-component").All())
		discardRemovedComponents := tasks.SelectOptional(removal.WithKind("discard-component").All())
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

func assertComponentTaskOrder(tasks Selection, component SnapQuery) error {
	downloadComponent := tasks.SelectOptional(component.WithKind("download-component"))
	prepareComponent := tasks.SelectOptional(component.WithKind("prepare-component"))
	validateComponent := tasks.SelectOptional(component.WithKind("validate-component"))
	mountComponent := tasks.SelectOptional(component.WithKind("mount-component"))
	preRefreshComponentHook := tasks.SelectOptional(component.WithComponentHook("pre-refresh"))
	unlinkCurrentComponent := tasks.SelectOptional(component.WithKind("unlink-current-component"))
	linkComponent := tasks.SelectOptional(component.WithKind("link-component"))
	installComponentHook := tasks.SelectOptional(component.WithComponentHook("install"))
	postRefreshComponentHook := tasks.SelectOptional(component.WithComponentHook("post-refresh"))

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
