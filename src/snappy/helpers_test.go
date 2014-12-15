package helpers

import (
	"testing"
	"os/exec"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
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
	err = Unpack(tmpfile, unpackdir)
	if err != nil {
		t.Error("unpack failed: %v", err)
	}
	
	_, err = os.Open(filepath.Join(tmpdir, "t/etc/fstab"))
	if err != nil {
		t.Error("can not find expected file in unpacked dir")
	}
}
