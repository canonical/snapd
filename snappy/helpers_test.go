package snappy

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestUnpack(t *testing.T) {

	// setup tmpdir
	tmpdir, err := ioutil.TempDir(os.TempDir(), "meep")
	if err != nil {
		t.Error("tmpdir failed")
	}
	defer os.RemoveAll(tmpdir)
	tmpfile := filepath.Join(tmpdir, "foo.tar.gz")

	// ok, slightly silly
	path := "/etc/fstab"

	// create test data
	cmd := exec.Command("tar", "cvzf", tmpfile, path)
	output, err := cmd.CombinedOutput()
	if !strings.Contains(string(output), "/etc/fstab") {
		t.Error("Can not find expected output from tar")
	}
	if err != nil {
		t.Error("failed to create tmp archive")
	}

	// unpack
	unpackdir := filepath.Join(tmpdir, "t")
	err = unpackTar(tmpfile, unpackdir)
	if err != nil {
		t.Error("unpack failed: %v", err)
	}

	_, err = os.Open(filepath.Join(tmpdir, "t/etc/fstab"))
	if err != nil {
		t.Error("can not find expected file in unpacked dir")
	}
}

func TestGetMapFromValidYaml(t *testing.T) {
	m, err := getMapFromYaml([]byte("name: value"))
	if err != nil {
		t.Error("Failed to convert yaml")
	}
	me := map[string]interface{}{"name": "value"}
	if !reflect.DeepEqual(m, me) {
		t.Error(fmt.Sprintf("Unexpected map %v != %v", m, me))
	}
}

func TestGetMapFromInvalidYaml(t *testing.T) {
	_, err := getMapFromYaml([]byte("%lala%"))
	if err == nil {
		t.Error("invalid yaml is a error")
	}
}
