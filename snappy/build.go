package snappy

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
)

func Build(sourceDirectory string) error {
	// FIXME this functions suxx, its just proof-of-concept

	data, err := ioutil.ReadFile(sourceDirectory + "/meta/package.yaml")
	if err != nil {
		return err
	}
	m, err := getMapFromYaml(data)
	if err != nil {
		return err
	}

	arch, ok := m["architecture"]
	if ok {
		arch = arch.(string)
	} else {
		arch = "all"
	}
	outputName := fmt.Sprintf("%s_%s_%s.snap", m["name"], m["version"], arch)

	os.Chdir(sourceDirectory)
	if err := exec.Command("tar", "czf", "meta.tar.gz", "meta/").Run(); err != nil {
		return err
	}

	if err := exec.Command("mksquashfs", ".", "data.squashfs", "-comp", "xz").Run(); err != nil {
		return err
	}

	os.Remove(outputName)

	if err := exec.Command("ar", "q", outputName, "meta.tar.gz", "data.squashfs").Run(); err != nil {
		return err
	}

	defer os.Remove("meta.tar.gz")
	defer os.Remove("data.squashfs")

	return nil
}
