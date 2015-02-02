package snappy

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
	"path"
)

var (
	SnapAuditError   error = errors.New("Snap audit error")
	SnapExtractError error = errors.New("Snap extract error")
)

type clickAppHook map[string]string

type clickManifest struct {
	Name    string                  `json:"name"`
	Version string                  `json:"version"`
	Hooks   map[string]clickAppHook `json:"hooks,omitempty"`
}

func auditSnap(snapFile string) bool {
	// FIXME: we want a bit more here ;)
	return true
}

func readClickManifest(data []byte) (manifest clickManifest, err error) {
	r := bytes.NewReader(data)
	dec := json.NewDecoder(r)
	err = dec.Decode(&manifest)
	return
}

func handleClickHooks(manifest clickManifest) (err error) {

	return
}

func installSnap(snapFile, targetDir string) (err error) {
	// FIXME: drop privs to "snap:snap" here

	if !auditSnap(snapFile) {
		return SnapAuditError
	}

	cmd := exec.Command("dpkg-deb", "-I", snapFile, "manifest")
	manifestData, err := cmd.Output()
	if err != nil {
		return SnapExtractError
	}
	manifest, err := readClickManifest([]byte(manifestData))
	if err != nil {
		return SnapExtractError
	}

	instDir := path.Join(targetDir, manifest.Name, manifest.Version)
	if _, err := os.Stat(instDir); err != nil {
		os.MkdirAll(instDir, 0755)
	}
	// FIXME: replace this with a native extractor to avoid attack
	//        surface
	cmd = exec.Command("dpkg-deb", "--extract", snapFile, instDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// FIXME: make the output part of the SnapExtractError
		log.Printf("Snap install failed with: %s", output)
		os.RemoveAll(instDir)
		return SnapExtractError
	}

	err = handleClickHooks(manifest)
	if err != nil {
		// FIXME: make the output part of the SnapExtractError
		log.Printf("Snap install failed with: %s", output)
		os.RemoveAll(instDir)
		return SnapExtractError
	}

	return err
}

/*
import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/blakesmith/ar"
)

func auditSnap(snap string) bool {
	// FIXME: we want a bit more here ;)
	return true
}

func extractYamlFromSnap(snap string) ([]byte, error) {
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

func xxxCmdInstall(args []string) error {
	snap := args[0]

	// FIXME: Not used atm
	//target := args[1]

	if !auditSnap(snap) {
		return errors.New("audit failed")
	}
	yaml, err := extractYamlFromSnap(snap)
	if err != nil {
		return err
	}
	m, err := getMapFromYaml(yaml)
	if err != nil {
		return err
	}
	//log.Print(m["name"])
	basedir := fmt.Sprintf("%s/%s/versions/%s/", snapBaseDir, m["name"], m["version"])
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
			unpackTar(basedir+name, basedir)
		}
	}

	// the data dirs
	for _, special_dir := range []string{"backups", "services"} {
		d := fmt.Sprintf("%s/%s/data/%s/%s/", snapBaseDir, m["name"], m["version"], special_dir)
		err = os.MkdirAll(d, 0777)
		if err != nil {
			return err
		}
	}

	return nil
}

func Install(args []string) (err error) {
	m := NewMetaRepository()
	for _, name := range args {
		found, _ := m.Details(name)
		for _, part := range found {
			// act only on parts that are downloadable
			if !part.IsInstalled() {
				pbar := NewTextProgress(part.Name())
				fmt.Printf("Installing %s\n", part.Name())
				err = part.Install(pbar)
				if err != nil {
					return err
				}
			}
		}
	}
	return err
}
*/
