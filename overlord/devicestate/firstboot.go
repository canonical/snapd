// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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

func criticalTaskEdges(ts *state.TaskSet) (prereqEdge, aliasesEdge, hooksEdge *state.Task, err error) {
	// we expect all three edges, or none (the latter is the case with config tasks).
	prereqEdge, err = ts.Edge(snapstate.PrerequisitesEdge)
	if err != nil {
		return nil, nil, nil, nil
	}
	aliasesEdge, err = ts.Edge(snapstate.SetupAliasesEdge)
	if err != nil {
		return nil, nil, nil, err
	}
	hooksEdge, err = ts.Edge(snapstate.InstallHookEdge)
	if err != nil {
		return nil, nil, nil, err
	}

	return prereqEdge, aliasesEdge, hooksEdge, nil
}

func trivialSeeding(st *state.State, markSeeded *state.Task) []*state.TaskSet {
	// give the internal core config a chance to run (even if core is
	// not used at all we put system configuration there)
	configTs := snapstate.ConfigureSnap(st, "core", 0)
	markSeeded.WaitAll(configTs)
	return []*state.TaskSet{configTs, state.NewTaskSet(markSeeded)}
}

type populateStateFromSeedOptions struct {
	Label   string
	Mode    string
	Preseed bool
}

func populateStateFromSeedImpl(st *state.State, opts *populateStateFromSeedOptions, tm timings.Measurer) ([]*state.TaskSet, error) {
	mode := "run"
	sysLabel := ""
	preseed := false
	if opts != nil {
		if opts.Mode != "" {
			mode = opts.Mode
		}
		sysLabel = opts.Label
		preseed = opts.Preseed
	}

	// check that the state is empty
	var seeded bool
	err := st.Get("seeded", &seeded)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if seeded {
		return nil, fmt.Errorf("cannot populate state: already seeded")
	}

	var preseedDone *state.Task
	if preseed {
		preseedDone = st.NewTask("mark-preseeded", i18n.G("Mark system pre-seeded"))
	}
	markSeeded := st.NewTask("mark-seeded", i18n.G("Mark system seeded"))

	deviceSeed, err := seed.Open(dirs.SnapSeedDir, sysLabel)
	if err != nil {
		return nil, err
	}

	// ack all initial assertions
	var model *asserts.Model
	timings.Run(tm, "import-assertions", "import assertions from seed", func(nested timings.Measurer) {
		model, err = importAssertionsFromSeed(st, deviceSeed)
	})
	if err == errNothingToDo {
		return trivialSeeding(st, markSeeded), nil
	}
	if err != nil {
		return nil, err
	}

	err = deviceSeed.LoadMeta(tm)
	if release.OnClassic && err == seed.ErrNoMeta {
		// on classic it is ok to not seed any snaps
		return trivialSeeding(st, markSeeded), nil
	}
	if err != nil {
		return nil, err
	}

	essentialSeedSnaps := deviceSeed.EssentialSnaps()
	seedSnaps, err := deviceSeed.ModeSnaps(mode)
	if err != nil {
		return nil, err
	}

	// allSnapInfos are collected for cross-check validation of bases
	allSnapInfos := make(map[string]*snap.Info, len(essentialSeedSnaps)+len(seedSnaps))

	tsAll := []*state.TaskSet{}
	configTss := []*state.TaskSet{}

	var lastPrereqTask, lastAliasesTask *state.Task
	var chainTs func(all []*state.TaskSet, ts *state.TaskSet) []*state.TaskSet

	chainTsPreseeding := func(all []*state.TaskSet, ts *state.TaskSet) []*state.TaskSet {
		n := len(all)
		// mark-preseeded task needs to be inserted between preliminary setup and hook tasks
		prereqTask, aliasesTask, hookTask, err := criticalTaskEdges(ts)
		if err != nil {
			// XXX: internal error?
			panic(err)
		}
		if prereqTask != nil {
			hookTask.WaitFor(preseedDone)
			if lastAliasesTask != nil {
				prereqTask.WaitFor(lastAliasesTask)
			}
			preseedDone.WaitFor(aliasesTask)
			lastPrereqTask = prereqTask
			lastAliasesTask = aliasesTask
		} else {
			if n != 0 {
				ts.WaitAll(all[n-1])
			}
		}
		return append(all, ts)
	}

	chainTsNormalSeeding := func(all []*state.TaskSet, ts *state.TaskSet) []*state.TaskSet {
		n := len(all)
		if n != 0 {
			ts.WaitAll(all[n-1])
		}
		return append(all, ts)
	}

	if preseed {
		chainTs = chainTsPreseeding
	} else {
		chainTs = chainTsNormalSeeding
	}

	chainSorted := func(infos []*snap.Info, infoToTs map[*snap.Info]*state.TaskSet) {
		sort.Stable(snap.ByType(infos))
		for _, info := range infos {
			ts := infoToTs[info]
			tsAll = chainTs(tsAll, ts)
		}
	}

	essInfoToTs := make(map[*snap.Info]*state.TaskSet, len(essentialSeedSnaps))
	essInfos := make([]*snap.Info, 0, len(essentialSeedSnaps))

	if len(essentialSeedSnaps) != 0 {
		// we *always* configure "core" here even if bases are used
		// for booting. "core" is where the system config lives.
		configTss = chainTs(configTss, snapstate.ConfigureSnap(st, "core", snapstate.UseConfigDefaults))
	}

	for _, seedSnap := range essentialSeedSnaps {
		ts, info, err := installSeedSnap(st, seedSnap, snapstate.Flags{SkipConfigure: true})
		if err != nil {
			return nil, err
		}
		if info.GetType() == snap.TypeKernel || info.GetType() == snap.TypeGadget {
			configTs := snapstate.ConfigureSnap(st, info.SnapName(), snapstate.UseConfigDefaults)
			// wait for the previous configTss
			configTss = chainTs(configTss, configTs)
		}

		essInfos = append(essInfos, info)
		essInfoToTs[info] = ts
		allSnapInfos[info.SnapName()] = info
	}
	// now add/chain the tasksets in the right order based on essential
	// snap types
	chainSorted(essInfos, essInfoToTs)

	// chain together configuring core, kernel, and gadget after
	// installing them so that defaults are availabble from gadget
	if len(configTss) > 0 {
		if preseed {
			configTss[0].WaitFor(preseedDone)
		}
		configTss[0].WaitAll(tsAll[len(tsAll)-1])
		tsAll = append(tsAll, configTss...)
	}

	// ensure we install in the right order
	infoToTs := make(map[*snap.Info]*state.TaskSet, len(seedSnaps))
	infos := make([]*snap.Info, 0, len(seedSnaps))

	for _, seedSnap := range seedSnaps {
		var flags snapstate.Flags
		ts, info, err := installSeedSnap(st, seedSnap, flags)
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
		infoToTs[info] = ts
		allSnapInfos[info.SnapName()] = info
	}

	// validate that all snaps have bases
	errs := snap.ValidateBasesAndProviders(allSnapInfos)
	if errs != nil {
		// only report the first error encountered
		return nil, errs[0]
	}

	// now add/chain the tasksets in the right order, note that we
	// only have tasksets that we did not already seeded
	chainSorted(infos, infoToTs)

	if len(tsAll) == 0 {
		return nil, fmt.Errorf("cannot proceed, no snaps to seed")
	}

	ts := tsAll[len(tsAll)-1]
	endTs := state.NewTaskSet()
	if model.Gadget() != "" {
		// we have a gadget that could have interface
		// connection instructions
		gadgetConnect := st.NewTask("gadget-connect", "Connect plugs and slots as instructed by the gadget")
		if preseed {
			if lastAliasesTask != nil {
				gadgetConnect.WaitFor(lastAliasesTask)
			}
		} else {
			gadgetConnect.WaitAll(ts)
		}
		endTs.AddTask(gadgetConnect)
		ts = endTs
		if preseed {
			preseedDone.WaitFor(gadgetConnect)
		}
	}

	if preseed {
		endTs.AddTask(preseedDone)
	}

	markSeeded.WaitAll(ts)
	endTs.AddTask(markSeeded)
	tsAll = append(tsAll, endTs)

	return tsAll, nil
}

func importAssertionsFromSeed(st *state.State, deviceSeed seed.Seed) (*asserts.Model, error) {
	// TODO: use some kind of context fo Device/SetDevice?
	device, err := internal.Device(st)
	if err != nil {
		return nil, err
	}

	// collect and
	// set device,model from the model assertion
	commitTo := func(batch *asserts.Batch) error {
		return assertstate.AddBatch(st, batch, nil)
	}

	err = deviceSeed.LoadAssertions(assertstate.DB(st), commitTo)
	if err == seed.ErrNoAssertions && release.OnClassic {
		// on classic seeding is optional
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

	modelAssertion, err := deviceSeed.Model()
	if err != nil {
		return nil, err
	}

	classicModel := modelAssertion.Classic()
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

	return modelAssertion, nil
}
