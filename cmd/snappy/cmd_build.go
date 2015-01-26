package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"

	"gopkg.in/yaml.v1"
)

type CmdBuild struct {
}

var cmdBuild CmdBuild

func init() {
	cmd, _ := Parser.AddCommand("build",
		"Build a package",
		"Creates a snapp package",
		&cmdBuild)

	cmd.Aliases = append(cmd.Aliases, "bu")
}

func (x *CmdBuild) Execute(args []string) (err error) {
	return x.build(args)
}

func (x *CmdBuild) build(args []string) error {
	dir := args[0]

	// FIXME this functions suxx, its just proof-of-concept

	data, err := ioutil.ReadFile(dir + "/meta/package.yaml")
	if err != nil {
		return err
	}
	m, err := getMapFromYaml(data)
	if err != nil {
		return err
	}

	arch := m["architecture"]
	if arch == nil {
		arch = "all"
	} else {
		arch = arch.(string)
	}
	output_name := fmt.Sprintf("%s_%s_%s.snap", m["name"], m["version"], arch)

	os.Chdir(dir)
	cmd := exec.Command("tar", "czf", "meta.tar.gz", "meta/")
	err = cmd.Start()
	if err != nil {
		return err
	}
	err = cmd.Wait()
	if err != nil {
		return err
	}

	cmd = exec.Command("mksquashfs", ".", "data.squashfs", "-comp", "xz")
	err = cmd.Start()
	if err != nil {
		return err
	}
	err = cmd.Wait()
	if err != nil {
		return err
	}

	os.Remove(output_name)
	cmd = exec.Command("ar", "q", output_name, "meta.tar.gz", "data.squashfs")
	err = cmd.Start()
	if err != nil {
		return err
	}
	err = cmd.Wait()
	if err != nil {
		return err
	}

	os.Remove("meta.tar.gz")
	os.Remove("data.squashfs")

	return nil
}

func getMapFromYaml(data []byte) (map[string]interface{}, error) {
	m := make(map[string]interface{})
	err := yaml.Unmarshal(data, &m)
	if err != nil {
		return m, err
	}
	return m, nil
}
