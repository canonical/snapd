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

	t := snapstate.PathInstallGoal(sn.SideInfo.RealName, sn.Path, sn.SideInfo, snapstate.RevisionOptions{})
	info, ts, err := snapstate.InstallOne(context.Background(), st, t, snapstate.Options{
		Flags:         flags,
		PrereqTracker: prqt,
		Seed:          true,
	})
	if err != nil {
		return nil, nil, err
	}

	return ts, info, nil
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

	essentialSeedSnaps := deviceSeed.EssentialSnaps()
	seedSnaps, err := deviceSeed.ModeSnaps(mode)
	if err != nil {
		return nil, err
	}

	tsAll := []*state.TaskSet{}
	configTss := []*state.TaskSet{}

	var lastBeforeHooksTask *state.Task
	var chainTs func(all []*state.TaskSet, ts *state.TaskSet) []*state.TaskSet

	var preseedDoneTask *state.Task
	if preseed {
		preseedDoneTask = st.NewTask("mark-preseeded", i18n.G("Mark system pre-seeded"))
	}

	chainTsPreseeding := func(all []*state.TaskSet, ts *state.TaskSet) []*state.TaskSet {
		// mark-preseeded task needs to be inserted between preliminary setup and hook tasks
		beginTask, beforeHooksTask, hooksTask, err := criticalTaskEdges(ts)
		if err != nil {
			// XXX: internal error?
			panic(err)
		}
		// we either have all edges or none
		if beginTask != nil {
			// hooks must wait for mark-preseeded
			hooksTask.WaitFor(preseedDoneTask)
			if n := len(all); n > 0 {
				// the first hook of the snap waits for all tasks of previous snap
				hooksTask.WaitAll(all[n-1])
			}
			if lastBeforeHooksTask != nil {
				beginTask.WaitFor(lastBeforeHooksTask)
			}
			preseedDoneTask.WaitFor(beforeHooksTask)
			lastBeforeHooksTask = beforeHooksTask
		} else {
			n := len(all)
			// no edges: it is a configure snap taskset for core/gadget/kernel
			if n != 0 {
				ts.WaitAll(all[n-1])
			}
		}
		return append(all, ts)
	}

	chainTsFullSeeding := func(all []*state.TaskSet, ts *state.TaskSet) []*state.TaskSet {
		n := len(all)
		if n != 0 {
			ts.WaitAll(all[n-1])
		}
		return append(all, ts)
	}

	if preseed {
		chainTs = chainTsPreseeding
	} else {
		chainTs = chainTsFullSeeding
	}

	chainSorted := func(infos []*snap.Info, infoToTs map[*snap.Info]*state.TaskSet) {
		sort.Stable(snap.ByType(infos))
		for _, info := range infos {
			ts := infoToTs[info]
			tsAll = chainTs(tsAll, ts)
		}
	}

	// collected snap infos
	infos := make([]*snap.Info, 0, len(essentialSeedSnaps)+len(seedSnaps))

	infoToTs := make(map[*snap.Info]*state.TaskSet, len(essentialSeedSnaps))

	prqt := snap.NewSelfContainedSetPrereqTracker()

	if len(essentialSeedSnaps) != 0 {
		// we *always* configure "core" here even if bases are used
		// for booting. "core" is where the system config lives.
		configTss = chainTs(configTss, snapstate.ConfigureSnap(st, "core", snapstate.UseConfigDefaults))
	}

	modelIsDangerous := model.Grade() == asserts.ModelDangerous
	for _, seedSnap := range essentialSeedSnaps {
		flags := snapstate.Flags{
			SkipConfigure: true,
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
		if info.Type() == snap.TypeKernel || info.Type() == snap.TypeGadget {
			configTs := snapstate.ConfigureSnap(st, info.SnapName(), snapstate.UseConfigDefaults)
			// wait for the previous configTss
			configTss = chainTs(configTss, configTs)
		}
		infos = append(infos, info)
		infoToTs[info] = ts
	}
	// now add/chain the tasksets in the right order based on essential
	// snap types
	chainSorted(infos, infoToTs)

	// chain together configuring core, kernel, and gadget after
	// installing them so that defaults are availabble from gadget
	if len(configTss) > 0 {
		if preseed {
			configTss[0].WaitFor(preseedDoneTask)
		}
		configTss[0].WaitAll(tsAll[len(tsAll)-1])
		tsAll = append(tsAll, configTss...)
	}

	// ensure we install in the right order
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
		infos = append(infos, info)
		infoToTs[info] = ts
	}

	// validate that all snaps have bases and providers are fulfilled
	// using the PrereqTracker
	warns, errs := prqt.Check()
	if errs != nil {
		// only report the first error encountered
		return nil, errs[0]
	}
	// XXX do better, use the warnings to setup checks at end of the seeding
	// and log onlys plug not connected or explicitly disconnected there
	for _, w := range warns {
		logger.Noticef("seed prerequisites: %v", w)
	}

	// now add/chain the tasksets in the right order, note that we
	// only have tasksets that we did not already seeded
	chainSorted(infos[len(essentialSeedSnaps):], infoToTs)

	if len(tsAll) == 0 {
		return nil, fmt.Errorf("cannot proceed, no snaps to seed")
	}

	// ts is the taskset of the last snap
	ts := tsAll[len(tsAll)-1]
	endTs := state.NewTaskSet()

	// Start tracking any validation sets included in the seed after
	// installing the included snaps.
	if trackVss, err := maybeEnforceValidationSetsTask(st, model, mode); err != nil {
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
