package main

import (
	"fmt"

	"launchpad.net/snappy/snappy"
)

type CmdUpdate struct {
}

var cmdUpdate CmdUpdate

func init() {
	cmd, _ := Parser.AddCommand("update",
		"Update all installed parts",
		"Ensures system is running with latest parts",
		&cmdUpdate)

	cmd.Aliases = append(cmd.Aliases, "up")
}

func (x *CmdUpdate) Execute(args []string) (err error) {
	if !isRoot() {
		return ErrRequiresRoot
	}

	return update()
}

func update() error {
	if !isRoot() {
		return ErrRequiresRoot
	}

	// FIXME: handle args
	updates, err := snappy.ListUpdates()
	if err != nil {
		return err
	}

	for _, part := range updates {
		pbar := snappy.NewTextProgress(part.Name())

		fmt.Printf("Installing %s (%s)\n", part.Name(), part.Version())
		if err := part.Install(pbar); err != nil {
			return err
		}
	}

	return nil
}
