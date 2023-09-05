// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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
	"errors"
	"fmt"
	"runtime"
	"sort"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
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

func installSeedSnap(st *state.State, sn *seed.Snap, flags snapstate.Flags) (*state.TaskSet, *snap.Info, error) {
	if sn.Required {
		flags.Required = true
	}
	if sn.Classic {
		flags.Classic = true
	}
	if sn.DevMode {
		flags.DevMode = true
	}

	return snapstate.InstallPath(st, sn.SideInfo, sn.Path, "", sn.Channel, flags)
}

func criticalTaskEdges(ts *state.TaskSet) (beginEdge, beforeHooksEdge, hooksEdge *state.Task, err error) {
	// we expect all three edges, or none (the latter is the case with config tasksets).
	beginEdge, err = ts.Edge(snapstate.BeginEdge)
	if err != nil {
		return nil, nil, nil, nil
	}
	beforeHooksEdge, err = ts.Edge(snapstate.BeforeHooksEdge)
	if err != nil {
		return nil, nil, nil, err
	}
	hooksEdge, err = ts.Edge(snapstate.HooksEdge)
	if err != nil {
		return nil, nil, nil, err
	}

	return beginEdge, beforeHooksEdge, hooksEdge, nil
}

func criticalTaskEdgesNew(ts *state.TaskSet) (beginEdge, beforeHooksEdge, hooksEdge, beforeConfigureEdge, configureEdge, endEdge *state.Task, err error) {
	// we expect all edges, or none (the latter is the case with config tasksets).
	beginEdge, err = ts.Edge(snapstate.BeginEdge)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil
	}
	beforeHooksEdge, err = ts.BeforeEdge(snapstate.HooksEdge)
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

	// The last task of the set
	tasks := ts.Tasks()
	endEdge = tasks[len(tasks)-1]

	return beginEdge, beforeHooksEdge, hooksEdge, beforeConfigureEdge, configureEdge, endEdge, nil
}

// maybeEnforceValidationSetsTask returns a task for tracking validation-sets. This may
// return nil if no validation-sets are present.
func maybeEnforceValidationSetsTask(st *state.State, model *asserts.Model) (*state.Task, error) {
	vsKey := func(accountID, name string) string {
		return fmt.Sprintf("%s/%s", accountID, name)
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
		vsKeys[vsKey(vsa.AccountID(), vsa.Name())] = a.Ref().PrimaryKey
	}

	// Set up pins from the model
	pins := make(map[string]int)
	for _, vs := range model.ValidationSets() {
		key := vsKey(vs.AccountID, vs.Name)
		if vs.Sequence > 0 {
			pins[key] = vs.Sequence
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

func (m *DeviceManager) populateStateFromSeedImpl(tm timings.Measurer) ([]*state.TaskSet, error) {
	st := m.state
	// check that the state is empty
	var seeded bool
	err := st.Get("seeded", &seeded)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if seeded {
		return nil, fmt.Errorf("cannot populate state: already seeded")
	}

	preseed := m.preseed
	sysLabel, mode, err := m.seedLabelAndMode()
	if err != nil {
		return nil, err
	}
	hasModeenv := false
	if mode != "" {
		hasModeenv = true
	} else {
		mode = "run"
	}

	var deviceSeed seed.Seed
	// ack all initial assertions
	timings.Run(tm, "import-assertions[finish]", "finish importing assertions from seed", func(nested timings.Measurer) {
		isCoreBoot := hasModeenv || !release.OnClassic
		/*//TODO: Remove this hack that is used for preseed testing
		if !isCoreBoot {
			isCoreBoot = true
		}*/
		deviceSeed, err = m.importAssertionsFromSeed(isCoreBoot)
	})
	if err != nil && err != errNothingToDo {
		return nil, err
	}

	if err == errNothingToDo {
		return trivialSeeding(st), nil
	}

	timings.Run(tm, "load-verified-snap-metadata", "load verified snap metadata from seed", func(nested timings.Measurer) {
		err = deviceSeed.LoadMeta(mode, nil, nested)
	})
	// ErrNoMeta can happen only with Core 16/18-style seeds
	if err == seed.ErrNoMeta && release.OnClassic {
		if preseed {
			return nil, fmt.Errorf("no snaps to preseed")
		}
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
	var injectedConfigureTaskSet *state.TaskSet
	var injectedConfigureTask *state.Task
	var firstConfigureTask *state.Task
	var lastBeforeHooksTask *state.Task
	var lastBeforeConfigureTask *state.Task
	var lastEndTask *state.Task

	injectCoreConfigureTaskSet := func() {
		injectedConfigureTaskSet = snapstate.ConfigureSnap(st, "core", snapstate.UseConfigDefaults)
	}

	chainSnapTasksPreseed := func(all []*state.TaskSet, ts *state.TaskSet, isEssentialSnap bool) []*state.TaskSet {
		beginTask, beforeHooksTask, hooksTask, beforeConfigureTask, configureTask, endEdgeTask, err := criticalTaskEdgesNew(ts)
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
					injectedConfigureTask.WaitFor(beforeConfigureTask)
					firstConfigureTask.WaitFor(beforeConfigureTask)
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
			lastEndTask = endEdgeTask
		}
		return append(all, ts)
	}

	chainSnapTasksSeed := func(all []*state.TaskSet, ts *state.TaskSet, isEssentialSnap bool) []*state.TaskSet {
		beginTask, _, _, beforeConfigureTask, configureTask, endTask, err := criticalTaskEdgesNew(ts)
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
						all = append(all, injectedConfigureTaskSet)
					}
				}
			} else {
				if isEssentialSnap {
					beginTask.WaitFor(lastBeforeConfigureTask)
					injectedConfigureTask.WaitFor(beforeConfigureTask)
					firstConfigureTask.WaitFor(beforeConfigureTask)
					configureTask.WaitFor(lastEndTask)
				} else {
					beginTask.WaitFor(lastEndTask)
				}
			}

			// save last tasks
			if isEssentialSnap {
				lastBeforeConfigureTask = beforeConfigureTask
			}
			lastEndTask = endTask
		}
		return append(all, ts)
	}

	var chainTs func(all []*state.TaskSet, ts *state.TaskSet, isEssentialSnap bool) []*state.TaskSet
	if preseed {
		chainTs = chainSnapTasksPreseed
	} else {
		chainTs = chainSnapTasksSeed
	}

	tsAll := []*state.TaskSet{}
	chainSorted := func(infos []*snap.Info, infoToTs map[*snap.Info]*state.TaskSet, isEssentialSnap bool) {
		sort.Stable(snap.ByType(infos))
		for _, info := range infos {
			ts := infoToTs[info]
			tsAll = chainTs(tsAll, ts, isEssentialSnap)
		}
	}

	// collected snap infos
	infos := make([]*snap.Info, 0, len(essentialSeedSnaps)+len(seedSnaps))
	infoToTs := make(map[*snap.Info]*state.TaskSet, len(essentialSeedSnaps))

	// collect the tasksets for installing the essential snaps
	var hasCoreSnap bool
	for _, seedSnap := range essentialSeedSnaps {
		// TODO: Double check if this is solid
		if seedSnap.EssentialType == snap.TypeOS {
			hasCoreSnap = true
		}

		flags := snapstate.Flags{
			// The kernel is already there either from ubuntu-image or from "install"
			// mode so skip extract.
			SkipKernelExtraction: true,
			// for dangerous models, allow all devmode snaps
			// XXX: eventually we may need to allow specific snaps to be devmode for
			// non-dangerous models, we can do that here since that information will
			// probably be in the model assertion which we have here
			ApplySnapDevMode: modelIsDangerous,
		}

		ts, info, err := installSeedSnap(st, seedSnap, flags)
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
		infoToTs[info] = ts
	}

	// TODO: If we decide to inject core configuration during doInstall for snapd
	// (which is arguably required), we likely do not need this anymore.

	// Essential snaps require "core" configuration even if bases are used for booting,
	// because it provides the system configuration. In the case of no "core" essential snap,
	// the "core" configuration taskset should be injected.
	if len(essentialSeedSnaps) > 0 && !hasCoreSnap {
		injectCoreConfigureTaskSet()
	}

	// now add/chain the essential snap tasksets in the right order based on essential snap types
	isEssentialSnap := true
	chainSorted(infos, infoToTs, isEssentialSnap)
	// TODO: This was temporarily moved outside of chainSnapTasksPreseed just to make existing unit test pass.
	// The unit test should be adapted to this reverted.
	tsAll = append(tsAll, injectedConfigureTaskSet)

	// collect the tasksets for installing the non-essential snaps
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

		ts, info, err := installSeedSnap(st, seedSnap, flags)
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
		infoToTs[info] = ts
	}

	// validate that all snaps have bases
	errs := snap.ValidateBasesAndProviders(infos)
	if errs != nil {
		// only report the first error encountered
		return nil, errs[0]
	}

	// now add/chain the non-essential snap tasksets in the right order
	isEssentialSnap = false
	chainSorted(infos[len(essentialSeedSnaps):], infoToTs, isEssentialSnap)

	if len(tsAll) == 0 {
		return nil, fmt.Errorf("cannot proceed, no snaps to seed")
	}

	// ts is the taskset of the last snap
	ts := tsAll[len(tsAll)-1]
	endTs := state.NewTaskSet()

	// Start tracking any validation sets included in the seed after
	// installing the included snaps.
	if trackVss, err := maybeEnforceValidationSetsTask(st, model); err != nil {
		return nil, err
	} else if trackVss != nil {
		trackVss.WaitAll(ts)
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

	// mark-seeded waits for the taskset of last snap, and
	// for all the tasks in the endTs as well.
	markSeeded.WaitAll(ts)
	markSeeded.WaitAll(endTs)
	endTs.AddTask(markSeeded)
	tsAll = append(tsAll, endTs)

	return tsAll, nil
}

func (m *DeviceManager) importAssertionsFromSeed(isCoreBoot bool) (seed.Seed, error) {
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

	classicModel := modelAssertion.Classic()
	// FIXME this will not be correct on classic with modes system when
	// mode is not "run".
	if release.OnClassic != classicModel {
		var msg string
		if classicModel {
			msg = "cannot seed an all-snaps system with a classic model"
		} else {
			msg = "cannot seed a classic system with an all-snaps model"
		}
		return nil, fmt.Errorf(msg)
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
