package snappy

import (
	"io/ioutil"
	"os"
	"path/filepath"
)

func makeMockSnap(tempdir string) (yamlFile string, err error) {
	metaDir := filepath.Join(tempdir, "apps", "hello-app", "1.10", "meta")
	err = os.MkdirAll(metaDir, 0777)
	if err != nil {
		return "", err
	}
	yamlFile = filepath.Join(metaDir, "package.yaml")
	ioutil.WriteFile(yamlFile, []byte(packageHello), 0666)

	return yamlFile, err
}
