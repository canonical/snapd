package snappy

import (
	"fmt"
	"os"
	"io/ioutil"
	"os/exec"
	"gopkg.in/yaml.v2"
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"github.com/blakesmith/ar"
	"io"
	"log"
	"strings"
	"net/http"
	"encoding/json"
	"github.com/olekukonko/tablewriter"
)

const APPS_ROOT = "/apps"

// register all dispatcher functions
func registerCommands() {
	registerCommand("build", "build a snap package", cmdBuild)
	registerCommand("install", "install a snap package", cmdInstall)
	registerCommand("search", "search for snap packages", cmdSearch)
	registerCommand("update", "update installed parts", cmdUpdate)
	registerCommand("versions", "display versions of installed parts", cmdVersions)
}

func cmdVersions(args []string) (err error) {
	// FIXME: implement
	fmt.Printf("FIXME: implement versions\n")

	return err
}

func cmdUpdate(args []string) (err error) {
	// FIXME: implement
	fmt.Printf("FIXME: implement update\n")
	return err
}

func cmdBuild(args []string) error {

	dir := args[0]

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

func get_map_from_yaml(data []byte) (map[string]interface{}, error) {
	m := make(map[string]interface{})
	err := yaml.Unmarshal(data, &m)
	if err != nil {
		return m, err
	}
	return m, nil
}

func audit_snap(snap string) bool {
	// FIXME: we want a bit more here ;)
	return true
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

func cmdInstall(args []string) error {
	snap := args[0]

	// FIXME: Not used atm
	//target := args[1]

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
		if name == "meta.tar.gz" {
			Unpack(basedir+name, basedir)
		}
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

func cmdSearch(args []string) error {
	search_term := args[0]

	const SEARCH_URI = "https://search.apps.ubuntu.com/api/v1/search?q=%s"

	url := fmt.Sprintf(SEARCH_URI, search_term)
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	// set headers
	req.Header.Set("Accept", "application/hal+json")
	//FIXME: hardcoded
	req.Header.Set("X-Ubuntu-Frameworks", "ubuntu-core-15.04-dev1")
	req.Header.Set("X-Ubuntu-Architecture", "amd64")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	searchData := make(map[string]interface{})
	body, err := ioutil.ReadAll(resp.Body)
	//log.Print(string(body))
	if err != nil {
		return err
	}
	err = json.Unmarshal(body, &searchData)
	if err != nil {
		return nil
	}
	embedded := searchData["_embedded"].(map[string]interface{})
	packages :=  embedded["clickindex:package"].([]interface{})

	// FIXME: how to wrap tablewriter.NewWriter() so that we always
	//        get the no row/col/center sepators?
	table := tablewriter.NewWriter(os.Stdout)
	table.SetRowSeparator("")
	table.SetColumnSeparator("")
	table.SetCenterSeparator("")

	for _, raw := range(packages) {
		pkg := raw.(map[string]interface{})
		//fmt.Printf("%s (%s) - %s \n", pkg["name"], pkg["version"], pkg["title"])
		table.Append([]string{pkg["name"].(string), pkg["version"].(string), pkg["title"].(string)})
	}
	table.Render()

	return nil;
}

