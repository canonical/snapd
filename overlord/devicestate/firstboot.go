// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2023 Canonical Ltd
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

package devicestate

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/devicestate/internal"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
)

var errNothingToDo = errors.New("nothing to do")

var runtimeNumCPU = runtime.NumCPU

func installSeedSnap(st *state.State, sn *seed.Snap, flags snapstate.Flags, prqt snapstate.PrereqTracker) (*state.TaskSet, *snap.Info, error) {
	if sn.Required {
		flags.Required = true
	}
	if sn.Classic {
		flags.Classic = true
	}
	if sn.DevMode {
		flags.DevMode = true
	}

	components := make([]snapstate.PathComponent, 0, len(sn.Components))
	for _, comp := range sn.Components {
		// Prevent reusing loop variable
		comp := comp
		components = append(components, snapstate.PathComponent{
			Path:     comp.Path,
			SideInfo: &comp.CompSideInfo,
		})
	}

	goal := snapstate.PathInstallGoal(snapstate.PathSnap{
		Path:       sn.Path,
		SideInfo:   sn.SideInfo,
		Components: components,
		RevOpts:    snapstate.RevisionOptions{Channel: sn.Channel},
	})
	info, ts, err := snapstate.InstallOne(context.Background(), st, goal, snapstate.Options{
		Flags:         flags,
		PrereqTracker: prqt,
		Seed:          true,
	})
	if err != nil {
		return nil, nil, err
	}

	return ts, info, nil
}

// criticalTaskEdges returns the tasks associated with each of the critical edges.
// No tasks (e.g. injected configure tasksets) and a task for every edge are valid
// scenarios, other returns empty tasks and an error. Note that task beforeConfigureEdge
// is not based on an actual edge, but calculated relative to ConfigureEdge.
//
// XXX: Consider if the BeforeHooksEdge can be completely removed, because the the BeforeEdge can arguable replace it fully.
func criticalTaskEdges(ts *state.TaskSet) (beginEdge, beforeHooksEdge, hooksEdge, beforeConfigureEdge, configureEdge, endEdge *state.Task, err error) {
	// we expect all edges, or none (the latter is the case with config task sets).
	beginEdge, err = ts.Edge(snapstate.BeginEdge)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil
	}
	beforeHooksEdge, err = ts.Edge(snapstate.BeforeHooksEdge) /*ts.BeforeEdge(snapstate.HooksEdge)*/
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}
	hooksEdge, err = ts.Edge(snapstate.HooksEdge)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}
	beforeConfigureEdge, err = ts.BeforeEdge(snapstate.ConfigureEdge)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}

	configureEdge, err = ts.Edge(snapstate.ConfigureEdge)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}

	// last task of the set
	tasks := ts.Tasks()
	endEdge = tasks[len(tasks)-1]

	return beginEdge, beforeHooksEdge, hooksEdge, beforeConfigureEdge, configureEdge, endEdge, nil
}

// maybeEnforceValidationSetsTask returns a task for tracking validation-sets. This may
// return nil if no validation-sets are present.
func maybeEnforceValidationSetsTask(st *state.State, model *asserts.Model, mode string) (*state.Task, error) {
	// Only enforce validation-sets in run-mode after installing all required snaps
	if mode != "run" {
		logger.Debugf("Postponing enforcement of validation-sets in mode %s", mode)
		return nil, nil
	}

	// Encode validation-sets included in the seed
	db := assertstate.DB(st)
	as, err := db.FindMany(asserts.ValidationSetType, nil)
	if err != nil {
		// If none are included, then skip this
		if errors.Is(err, &asserts.NotFoundError{}) {
			return nil, nil
		}
		return nil, err
	}

	vsKeys := make(map[string][]string)
	for _, a := range as {
		vsa := a.(*asserts.ValidationSet)
		vsKeys[vsa.SequenceKey()] = a.Ref().PrimaryKey
	}

	// Set up pins from the model
	pins := make(map[string]int)
	for _, vs := range model.ValidationSets() {
		if vs.Sequence > 0 {
			pins[vs.SequenceKey()] = vs.Sequence
		}
	}

	t := st.NewTask("enforce-validation-sets", i18n.G("Track validation sets"))
	t.Set("validation-set-keys", vsKeys)
	t.Set("pinned-sequence-numbers", pins)
	return t, nil
}

func markSeededTask(st *state.State) *state.Task {
	return st.NewTask("mark-seeded", i18n.G("Mark system seeded"))
}

func trivialSeeding(st *state.State) []*state.TaskSet {
	// give the internal core config a chance to run (even if core is
	// not used at all we put system configuration there)
	configTs := snapstate.ConfigureSnap(st, "core", 0)
	markSeeded := markSeededTask(st)
	markSeeded.WaitAll(configTs)
	return []*state.TaskSet{configTs, state.NewTaskSet(markSeeded)}
}

// XXX: temporary test helper
//func printGoroutineStackTrace() {
//	stackBuf := make([]byte, 10000)
//	n := runtime.Stack(stackBuf, false)
//	fmt.Printf("Goroutine stack trace:\n%s\n", stackBuf[:n])
//}

func (m *DeviceManager) populateStateFromSeedImpl(tm timings.Measurer) ([]*state.TaskSet, error) {
	st := m.state
	// only populate state from seed if "seeded" state is missing or false
	var seeded bool
	err := st.Get("seeded", &seeded)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		// error other than no "seeded" key
		return nil, err
	}
	if seeded {
		// in practice seeded state is only set when "seed-system",
		// so this is not expected in the wild
		return nil, fmt.Errorf("cannot populate state: already seeded")
	}

	// XXX: Is it correct to say mode != "" then hasModeenv. Is it possible that
	// reading mode environment could also produce mode ""?
	preseed := m.preseed
	sysLabel, mode, err := m.seedLabelAndMode()
	if err != nil {
		return nil, err
	}
	// XXX: Is it correct to say mode != "" then hasModeenv. Is it possible that
	// Is it possible that reading mode environment could also produce mode ""?
	// Is it possible to move this into seedLabelAndMode?
	hasModeenv := false
	if mode != "" {
		hasModeenv = true
	} else {
		mode = "run"
	}

	// ack all initial assertions
	var deviceSeed seed.Seed
	timings.Run(tm, "import-assertions[finish]", "finish importing assertions from seed", func(nested timings.Measurer) {
		isCoreBoot := hasModeenv || !release.OnClassic
		deviceSeed, err = m.importAssertionsFromSeed(mode, isCoreBoot)
	})
	if err != nil && err != errNothingToDo {
		return nil, err
	}
	if err == errNothingToDo {
		// test coverage:
		// - TestPopulateFromSeedOnClassicNoop
		// - TestDoMarkPreseeded
		// - TestDoMarkPreseededAfterFirstboot
		return trivialSeeding(st), nil
	}

	// load the seed and seed snap metadata and verify underlying snaps against assertions
	timings.Run(tm, "load-verified-snap-metadata", "load verified snap metadata from seed", func(nested timings.Measurer) {
		err = deviceSeed.LoadMeta(mode, nil, nested)
	})
	// ErrNoMeta can happen only with Core 16/18-style seeds
	if err == seed.ErrNoMeta && release.OnClassic {
		if preseed {
			return nil, fmt.Errorf("cannot proceed, no snaps to preseed")
		}
		// test coverage:
		// - TestPopulateFromSeedOnClassicNoSeedYaml
		// - TestPopulateFromSeedOnClassicNoSeedYamlWithCloudInstanceData
		// on classic it is ok to not seed any snaps
		return trivialSeeding(st), nil
	}
	if err != nil {
		return nil, err
	}
	model := deviceSeed.Model()
	modelIsDangerous := model.Grade() == asserts.ModelDangerous

	essentialSeedSnaps := deviceSeed.EssentialSnaps()
	seedSnaps, err := deviceSeed.ModeSnaps(mode)
	if err != nil {
		return nil, err
	}

	var preseedDoneTask *state.Task
	if preseed {
		preseedDoneTask = st.NewTask("mark-preseeded", i18n.G("Mark system pre-seeded"))
	}

	// XXX: add descriptions next to each placeholder
	var injectedConfigureTaskSet *state.TaskSet
	var injectedConfigureTask *state.Task
	var firstConfigureTask *state.Task
	var lastBeforeHooksTask *state.Task
	var lastBeforeConfigureTask *state.Task
	var lastEndTask *state.Task
	var lastSnapTaskSet *state.TaskSet

	injectCoreConfigureTaskSet := func() {
		injectedConfigureTaskSet = snapstate.ConfigureSnap(st, "core", snapstate.UseConfigDefaults)
	}

	// chainSnapTasksPreseed chains install task sets together for the purpose of preseeding. It also
	// modifies dependencies between specific tasks in order to achieve the required overall task order.
	chainSnapTasksPreseed := func(all []*state.TaskSet, ts *state.TaskSet, isEssentialSnap bool) []*state.TaskSet {
		beginTask, beforeHooksTask, hooksTask, beforeConfigureTask, configureTask, endTask, err := criticalTaskEdges(ts)
		if err != nil {
			panic(err)
		}

		// check if the required task edges was found
		if beginTask != nil {
			// add dependencies
			if len(all) == 0 {
				if isEssentialSnap {
					firstConfigureTask = configureTask
					if injectedConfigureTaskSet != nil {
						injectedConfigureTask = injectedConfigureTaskSet.Tasks()[0]
						injectedConfigureTask.WaitFor(beforeConfigureTask)
						firstConfigureTask.WaitFor(injectedConfigureTaskSet.Tasks()[len(injectedConfigureTaskSet.Tasks())-1])
						//all = append(all, injectedConfigureTaskSet)
					}
				}
			} else {
				beginTask.WaitFor(lastBeforeHooksTask)
				if isEssentialSnap {
					configureTask.WaitFor(lastEndTask)
					hooksTask.WaitFor(lastBeforeConfigureTask)
					firstConfigureTask.WaitFor(beforeConfigureTask)
					if injectedConfigureTaskSet != nil {
						injectedConfigureTask.WaitFor(beforeConfigureTask)
					}
				} else {
					hooksTask.WaitFor(lastEndTask)
				}
			}
			preseedDoneTask.WaitFor(beforeHooksTask)
			hooksTask.WaitFor(preseedDoneTask)

			// save last tasks
			lastBeforeHooksTask = beforeHooksTask
			if isEssentialSnap {
				lastBeforeConfigureTask = beforeConfigureTask
			}
			lastEndTask = endTask
			lastSnapTaskSet = ts
		}
		return append(all, ts)
	}

	// chainSnapTasksSeed chains install task set together for the purpose of seeding. It also
	// modifies depdendencies between specific tasks in order to achieve the required overall task order.
	// XXX: Consider renaming these functions to something better
	chainSnapTasksSeed := func(all []*state.TaskSet, ts *state.TaskSet, isEssentialSnap bool) []*state.TaskSet {
		beginTask, _, _, beforeConfigureTask, configureTask, endTask, err := criticalTaskEdges(ts)
		if err != nil {
			panic(err)
		}

		// check if the required task edges was found
		if beginTask != nil {
			// add dependencies
			if len(all) == 0 {
				if isEssentialSnap {
					firstConfigureTask = configureTask
					if injectedConfigureTaskSet != nil {
						injectedConfigureTask = injectedConfigureTaskSet.Tasks()[0]
						injectedConfigureTask.WaitFor(beforeConfigureTask)
						firstConfigureTask.WaitFor(injectedConfigureTaskSet.Tasks()[len(injectedConfigureTaskSet.Tasks())-1])
						//all = append(all, injectedConfigureTaskSet)
					}
				}
			} else {
				if isEssentialSnap {
					beginTask.WaitFor(lastBeforeConfigureTask)
					firstConfigureTask.WaitFor(beforeConfigureTask)
					configureTask.WaitFor(lastEndTask)
					if injectedConfigureTaskSet != nil {
						injectedConfigureTask.WaitFor(beforeConfigureTask)
					}
				} else {
					beginTask.WaitFor(lastEndTask)
				}
			}

			// save last tasks
			if isEssentialSnap {
				lastBeforeConfigureTask = beforeConfigureTask
			}
			lastEndTask = endTask
			lastSnapTaskSet = ts
		}
		return append(all, ts)
	}

	// chainTs provides the chaining required for either preseeding or seeding scenario
	var chainTs func(all []*state.TaskSet, ts *state.TaskSet, isEssentialSnap bool) []*state.TaskSet
	if preseed {
		// test coverage:
		// >> core | Classic <<
		// - TestPreseedOnClassicHappy
		// - TestPreseedClassicWithSnapdOnlyHappy
		// - TestPopulatePreseedWithConnectHook
		chainTs = chainSnapTasksPreseed
	} else {
		// test coverage:
		// >> core | Classic <<
		// - TestPopulateFromSeedOnClassicEmptySeedYaml
		// - TestPopulateFromSeedOnClassicWithSnapdOnlyAndGadgetHappy
		// - TestPopulateFromSeedOnClassicWithSnapdOnlyHappy
		// - TestPopulateFromSeedOnClassicWithConnectHook

		// >> core20 | Classic <<
		// - TestPopulateFromSeedClassicWithModesDangerousRunModeNoKernelAndGadgetClassicSnap
		// - TestPopulateFromSeedClassicWithModesRunMode
		// - TestPopulateFromSeedClassicWithModesRunModeNoKernelAndGadget
		// - TestPopulateFromSeedClassicWithModesSignedRunModeNoKernelAndGadgetClassicSnap
		// - TestPopulateFromSeedClassicWithModesSignedRunModeNoKernelAndGadgetClassicSnapImplicitFails

		// >> core <<
		// - TestPopulateFromSeedAlternativeContentProviderAndOrder
		// - TestPopulateFromSeedConfigureHappy
		// - TestPopulateFromSeedDefaultConfigureHappy
		// - TestPopulateFromSeedGadgetConnectHappy
		// - TestPopulateFromSeedHappy
		// - TestPopulateFromSeedHappyMultiAssertsFiles
		// - TestPopulateFromSeedMissingBase
		// - TestPopulateFromSeedMissingBootloader
		// - TestPopulateFromSeedWrongContentProviderOrder

		// >> core18 <<
		// - TestPopulateFromSeedCore18ValidationSetTrackingHappy
		// - TestPopulateFromSeedCore18ValidationSetTrackingUnmetCriteria
		// - TestPopulateFromSeedCore18WithBaseHappy
		// - TestPopulateFromSeedCore18Ordering

		// >> core20 <<
		// - TestPopulateFromSeedCore20InstallMode
		// - TestPopulateFromSeedCore20InstallModeWithComps
		// - TestPopulateFromSeedCore20RecoverMode
		// - TestPopulateFromSeedCore20RecoverModeWithComps
		// - TestPopulateFromSeedCore20RunMode
		// - TestPopulateFromSeedCore20RunModeDangerousWithDevmode
		// - TestPopulateFromSeedCore20RunModeUserServiceTasks
		// - TestPopulateFromSeedCore20RunModeWithComps
		// - TestPopulateFromSeedCore20ValidationSetTrackingFailsUnmetCriterias
		// - TestPopulateFromSeedCore20ValidationSetTrackingFailsUnmetCriterias
		// - TestPopulateFromSeedCore20ValidationSetTrackingHappy
		// - TestPopulateFromSeedCore20ValidationSetTrackingNotAddedInInstallMode
		chainTs = chainSnapTasksSeed
	}

	// chainSorted sorts snap install task sets in order of snap type priority and chains it together
	tsAll := []*state.TaskSet{}
	chainSorted := func(infos []*snap.Info, infoToTs map[*snap.Info]*state.TaskSet, isEssentialSnap bool) {
		// This is the order in which snaps will be installed in the
		// system. We want the boot base to be installed before the
		// kernel so any existing kernel hook can execute with the boot
		// base as rootfs.
		// XXX: Can this sort be integrated or simplified somehow?
		effectiveType := func(info *snap.Info) snap.Type {
			typ := info.Type()
			if info.RealName == model.Base() {
				typ = snap.InternalTypeBootBase
			}
			return typ
		}
		sort.SliceStable(infos, func(i, j int) bool {
			return effectiveType(infos[i]).SortsBefore(effectiveType(infos[j]))
		})

		for _, info := range infos {
			ts := infoToTs[info]
			tsAll = chainTs(tsAll, ts, isEssentialSnap)
		}
	}

	infos := make([]*snap.Info, 0, len(essentialSeedSnaps)+len(seedSnaps))
	infoToTs := make(map[*snap.Info]*state.TaskSet, len(essentialSeedSnaps))

	prqt := snap.NewSelfContainedSetPrereqTracker()

	// collect the task sets for installing the essential snaps
	var hasCoreSnap bool
	for _, seedSnap := range essentialSeedSnaps {
		// TODO: Double check if this is solid
		if seedSnap.EssentialType == snap.TypeOS {
			hasCoreSnap = true
		}

		flags := snapstate.Flags{
			// When seeding run configure hooks using the gadget defaults if available.
			// This impacts the "core" configuration that should otherwise not use
			// gadget defaults.
			ConfigureDefaults: true,
			// The kernel is already there either from ubuntu-image or from "install"
			// mode so skip extract.
			SkipKernelExtraction: true,
			// for dangerous models, allow all devmode snaps
			// XXX: eventually we may need to allow specific snaps to be devmode for
			// non-dangerous models, we can do that here since that information will
			// probably be in the model assertion which we have here
			ApplySnapDevMode: modelIsDangerous,
		}

		ts, info, err := installSeedSnap(st, seedSnap, flags, prqt)
		if err != nil {
			return nil, err
		}

		// tag tasks with instance name for improve testability
		for _, task := range ts.Tasks() {
			task.Set("instance-name", info.InstanceName())
		}

		infos = append(infos, info)
		infoToTs[info] = ts
	}

	// XXX: If we decide to inject core configuration during doInstall for snapd
	// (which is arguably required), we likely do not need this anymore. This would
	// result in a significant simplification

	// Essential snaps require "core" configuration even if bases are used for booting,
	// because it provides the system configuration. In the case of no "core" essential snap,
	// the "core" configuration task set should be injected.
	if len(essentialSeedSnaps) > 0 && !hasCoreSnap {
		injectCoreConfigureTaskSet()
	}

	// now add/chain the essential snap task sets in the right order
	isEssentialSnap := true
	chainSorted(infos, infoToTs, isEssentialSnap)
	// TODO: This was temporarily moved outside of chainSnapTasksPreseed just to make existing unit test pass.
	// The unit test should be adapted to this reverted.
	if len(essentialSeedSnaps) > 0 && !hasCoreSnap {
		tsAll = append(tsAll, injectedConfigureTaskSet)
	}

	// collect the task sets for installing the non-essential snaps
	infoToTs = make(map[*snap.Info]*state.TaskSet, len(seedSnaps))
	for _, seedSnap := range seedSnaps {
		flags := snapstate.Flags{
			// for dangerous models, allow all devmode snaps
			// XXX: eventually we may need to allow specific snaps to be devmode for
			// non-dangerous models, we can do that here since that information will
			// probably be in the model assertion which we have here
			ApplySnapDevMode: modelIsDangerous,
			// for non-dangerous models snaps need to opt-in explicitly
			// Classic is simply ignored for non-classic snaps, so we do not need to check further
			Classic: release.OnClassic && modelIsDangerous,
		}

		ts, info, err := installSeedSnap(st, seedSnap, flags, prqt)
		if err != nil {
			return nil, err
		}

		// tag tasks with instance name for improve testability
		for _, task := range ts.Tasks() {
			task.Set("instance-name", info.InstanceName())
		}

		infos = append(infos, info)
		infoToTs[info] = ts
	}

	// validate that all snaps have bases and providers are fulfilled
	warns, errs := prqt.Check()
	if errs != nil {
		// only report the first error encountered
		return nil, errs[0]
	}
	// XXX do better, use the warnings to setup checks at end of the seeding
	// and log only plug not connected or explicitly disconnected there
	for _, w := range warns {
		logger.Noticef("seed prerequisites: %v", w)
	}

	// now add/chain the non-essential snap task sets in the right order
	isEssentialSnap = false
	chainSorted(infos[len(essentialSeedSnaps):], infoToTs, isEssentialSnap)

	if len(tsAll) == 0 {
		return nil, fmt.Errorf("cannot proceed, no snaps to seed")
	}

	// start tracking validation sets included in the seed after installation
	endTs := state.NewTaskSet()
	if trackVss, err := maybeEnforceValidationSetsTask(st, model, mode); err != nil {
		return nil, err
	} else if trackVss != nil {
		trackVss.WaitAll(lastSnapTaskSet)
		endTs.AddTask(trackVss)
	}

	markSeeded := markSeededTask(st)
	if preseed {
		endTs.AddTask(preseedDoneTask)
	}
	whatSeeds := &seededSystem{
		System:    sysLabel,
		Model:     model.Model(),
		BrandID:   model.BrandID(),
		Revision:  model.Revision(),
		Timestamp: model.Timestamp(),
	}
	markSeeded.Set("seed-system", whatSeeds)

	// mark-seeded waits for the task set of last snap, and for all the tasks in the endTs as well
	markSeeded.WaitAll(lastSnapTaskSet)
	markSeeded.WaitAll(endTs)
	endTs.AddTask(markSeeded)
	tsAll = append(tsAll, endTs)

	return tsAll, nil
}

func (m *DeviceManager) importAssertionsFromSeed(mode string, isCoreBoot bool) (seed.Seed, error) {
	st := m.state

	// TODO: use some kind of context fo Device/SetDevice?
	device, err := internal.Device(st)
	if err != nil {
		return nil, err
	}

	// collect and
	// set device,model from the model assertion
	_, deviceSeed, err := m.earlyLoadDeviceSeed(nil)
	if err == seed.ErrNoAssertions && !isCoreBoot {
		// if classic boot seeding is optional
		// set the fallback model
		err := setClassicFallbackModel(st, device)
		if err != nil {
			return nil, err
		}
		return nil, errNothingToDo
	}
	if err != nil {
		return nil, err
	}

	modelAssertion := deviceSeed.Model()

	if release.OnClassic && !modelAssertion.Classic() {
		return nil, errors.New("cannot seed a classic system with an all-snaps model")
	}

	if !release.OnClassic && modelAssertion.Classic() && mode != "recover" {
		return nil, errors.New("can only seed an all-snaps system with a classic model in recovery mode")
	}

	// set device,model from the model assertion
	if err := setDeviceFromModelAssertion(st, device, modelAssertion); err != nil {
		return nil, err
	}

	return deviceSeed, nil
}

// processAutoImportAssertions attempts to load the auto import assertions
// and create all knows system users, if and only if the model grade is dangerous.
// Processing of the auto-import assertion is opportunistic and can fail
// for example if system-user-as is serial bound and there is no serial-as yet
func processAutoImportAssertions(st *state.State, deviceSeed seed.Seed, db asserts.RODatabase, commitTo func(batch *asserts.Batch) error) error {
	// only proceed for dangerous model
	if deviceSeed.Model().Grade() != asserts.ModelDangerous {
		return nil
	}
	seed20AssertionsLoader, ok := deviceSeed.(seed.AutoImportAssertionsLoaderSeed)
	if !ok {
		return fmt.Errorf("failed to auto-import assertions, invalid loader")
	}
	err := seed20AssertionsLoader.LoadAutoImportAssertions(commitTo)
	if err != nil {
		return err
	}
	// automatic user creation is meant to imply sudoers
	const sudoer = true
	_, err = createAllKnownSystemUsers(st, db, deviceSeed.Model(), nil, sudoer)
	return err
}

// loadDeviceSeed loads the seed based on sysLabel,
// It is meant to be called by DeviceManager.earlyLoadDeviceSeed.
var loadDeviceSeed = func(st *state.State, sysLabel string) (deviceSeed seed.Seed, err error) {
	deviceSeed, err = seed.Open(dirs.SnapSeedDir, sysLabel)
	if err != nil {
		return nil, err
	}

	if runtimeNumCPU() > 1 {
		// XXX set parallelism experimentally to 2 as I/O
		// itself becomes a bottleneck ultimately
		deviceSeed.SetParallelism(2)
	}

	// collect and
	// set device,model from the model assertion
	commitTo := func(batch *asserts.Batch) error {
		return assertstate.AddBatch(st, batch, nil)
	}

	if err := deviceSeed.LoadAssertions(assertstate.DB(st), commitTo); err != nil {
		return nil, err
	}

	return deviceSeed, nil
}
