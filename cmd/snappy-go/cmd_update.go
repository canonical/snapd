package main

import (
	"fmt"
	"os"

	"launchpad.net/snappy/logger"
	"launchpad.net/snappy/snappy"
)

type cmdUpdate struct {
}

func init() {
	var cmdUpdateData cmdUpdate
	_, _ = parser.AddCommand("update",
		"Update all installed parts",
		"Ensures system is running with latest parts",
		&cmdUpdateData)
}

func (x *cmdUpdate) Execute(args []string) (err error) {
	if !isRoot() {
		return ErrRequiresRoot
	}

	return update()
}

func update() error {
	// FIXME: handle args
	updates, err := snappy.ListUpdates()
	if err != nil {
		return logger.LogError(err)
	}

	for _, part := range updates {
		pbar := snappy.NewTextProgress(part.Name())

		fmt.Printf("Installing %s (%s)\n", part.Name(), part.Version())
		if err := part.Install(pbar); err != nil {
			return logger.LogError(err)
		}
	}

	if len(updates) > 0 {
		showVerboseList(updates, os.Stdout)
	}

	return nil
}
