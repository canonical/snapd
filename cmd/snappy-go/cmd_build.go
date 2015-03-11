package main

import (
	"fmt"
	"os"
	"os/exec"

	"launchpad.net/snappy/snappy"
)

const clickReview = "click-review"

type cmdBuild struct {
}

func init() {
	var cmdBuildData cmdBuild
	cmd, _ := parser.AddCommand("build",
		"Build a package",
		"Creates a snapp package",
		&cmdBuildData)

	cmd.Aliases = append(cmd.Aliases, "bu")
}

func (x *cmdBuild) Execute(args []string) (err error) {
	if len(args) == 0 {
		args = []string{"."}
	}

	snapPackage, err := snappy.Build(args[0])
	if err != nil {
		return err
	}

	cmd := exec.Command(clickReview, snapPackage)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// we ignore the error for now
	_ = cmd.Run()

	fmt.Printf("Generated '%s' snap\n", snapPackage)
	return nil
}
