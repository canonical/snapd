package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"github.com/blakesmith/ar"
	"gopkg.in/yaml.v2"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
)

const APPS_ROOT = "/tmp/apps"

func audit_snap(snap string) bool {
	// FIXME: we want a bit more here ;)
	return true
}

func get_map_from_yaml(data []byte) (map[string]interface{}, error) {
	m := make(map[string]interface{})
	err := yaml.Unmarshal(data, &m)
	if err != nil {
		return m, err
	}
	return m, nil
}

func extract_snap_yaml(snap string) ([]byte, error) {
	f, err := os.Open(snap)
	defer f.Close()
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	archive := ar.NewReader(f)
	for {
		hdr, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		// FIXME: this is all we support for now
		if hdr.Name == "meta.tar.gz/" {
			io.Copy(&buf, archive)
			break
		}
	}
	if buf.Len() == 0 {
		return nil, errors.New("no meta.tar.gz")
	}

	// gzip
	gz, err := gzip.NewReader(&buf)
	if err != nil {
		return nil, err
	}
	// and then the tar
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			// end of tar archive
			break
		}
		if err != nil {
			log.Fatalln(err)
		}
		if hdr.Name == "meta/package.yaml" {
			buf := bytes.NewBuffer(nil)
			if _, err := io.Copy(buf, tr); err != nil {
				return nil, err
			}
			return buf.Bytes(), nil
		}
	}
	return nil, errors.New("meta/package.yaml not found")
}

func install_snap(snap string, target string) error {

	if !audit_snap(snap) {
		return errors.New("audit failed")
	}
	yaml, err := extract_snap_yaml(snap)
	if err != nil {
		return err
	}
	m, err := get_map_from_yaml(yaml)
	if err != nil {
		return err
	}
	//log.Print(m["name"])
	basedir := fmt.Sprintf("%s/%s/versions/%s/", APPS_ROOT, m["name"], m["version"])
	err = os.MkdirAll(basedir, 0777)
	if err != nil {
		return err
	}

	// unpack for real
	f, err := os.Open(snap)
	defer f.Close()
	if err != nil {
		return err
	}

	archive := ar.NewReader(f)
	for {
		hdr, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimRight(hdr.Name, "/")
		out, err := os.OpenFile(basedir+name, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0666)
		if err != nil {
			return err
		}
		defer out.Close()
		io.Copy(out, archive)
	}

	// the data dirs
	for _, special_dir := range []string{"backups", "services"} {
		d := fmt.Sprintf("%s/%s/data/%s/%s/", APPS_ROOT, m["name"], m["version"], special_dir)
		err = os.MkdirAll(d, 0777)
		if err != nil {
			return err
		}
	}

	return nil
}

func snap_build(dir string) error {

	// FIXME this functions suxx, its just proof-of-concept

	data, err := ioutil.ReadFile(dir + "/meta/package.yaml")
	if err != nil {
		return err
	}
	m, err := get_map_from_yaml(data)
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

func main() {

	switch os.Args[1] {
	case "build":
		if err := snap_build(os.Args[2]); err != nil {
			log.Fatal("build_snap failed: ", err)
		}
	case "install":
		snap := os.Args[2]
		if err := install_snap(snap, APPS_ROOT); err != nil {
			log.Fatal("install_snap failed: ", err)
		}
	}
}
