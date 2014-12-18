package snappy

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
	m, err := getMapFromYaml(yaml)
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
			unpackTar(basedir+name, basedir)
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
