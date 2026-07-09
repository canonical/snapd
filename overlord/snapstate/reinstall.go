package snapstate

import (
	"fmt"
	"github.com/snapcore/snapd/overlord/state"
)

type ReinstallOptions struct {
	Channel string
	Purge   bool
}

// Reinstall generates a taskset to reinstall the given snap.
// It effectively chains a removal and an installation.
func Reinstall(st *state.State, name string, opts *ReinstallOptions) (*state.TaskSet, error) {
	if opts == nil {
		opts = &ReinstallOptions{}
	}

	// 1. Generate Remove TaskSet
	removeOpts := &RemoveFlags{
		Purge: opts.Purge,
	}
	removeTs, err := Remove(st, name, Revision{}, removeOpts)
	if err != nil {
		return nil, fmt.Errorf("cannot prepare removal tasks for reinstall: %v", err)
	}

	var finalRemoveTask *state.Task
	for _, t := range removeTs.Tasks() {
		if t.Kind() == "remove-snap" {
			finalRemoveTask = t
		}
	}

	// 2. Generate Install TaskSet
	installOpts := &Flags{
		Channel: opts.Channel,
	}
	installTs, err := Install(st, name, installOpts, 0, Flags{})
	if err != nil {
		return nil, fmt.Errorf("cannot prepare installation tasks for reinstall: %v", err)
	}

	// 3. Make install tasks wait for remove tasks
	if finalRemoveTask != nil {
		for _, installTask := range installTs.Tasks() {
			if installTask.Kind() == "download-snap" || installTask.Kind() == "prepare-snap" {
				installTask.WaitFor(finalRemoveTask)
			}
		}
	}

	// 4. Combine
	ts := state.NewTaskSet()
	ts.AddAll(removeTs)
	ts.AddAll(installTs)

	return ts, nil
}
