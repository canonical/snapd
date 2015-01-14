package snappy

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"

	"gopkg.in/yaml.v1"
)

type SnappPart struct {
	name        string
	version     string
	description string
	hash        string
	isActive    bool
	isInstalled bool

	basedir string
}

type packageYaml struct {
	Name    string
	Version string
	Vendor  string
	Icon    string
}

func NewLocalSnappPart(yaml_path string) *SnappPart {
	part := SnappPart{}

	if _, err := os.Stat(yaml_path); os.IsNotExist(err) {
		log.Printf("No '%s' found", yaml_path)
		return nil
	}

	r, err := os.Open(yaml_path)
	if err != nil {
		log.Printf("Can not open '%s'", yaml_path)
		return nil
	}

	yaml_data, err := ioutil.ReadAll(r)
	if err != nil {
		log.Printf("Can not read '%s'", r)
		return nil
	}

	var m packageYaml
	err = yaml.Unmarshal(yaml_data, &m)
	if err != nil {
		log.Printf("Can not parse '%s'", yaml_data)
		return nil
	}
	part.name = m.Name
	part.version = m.Version
	part.isInstalled = true
	// FIXME: figure out if we are active

	part.basedir = path.Dir(path.Dir(yaml_path))
	return &part
}

func (s *SnappPart) Name() string {
	return s.name
}

func (s *SnappPart) Version() string {
	return s.version
}

func (s *SnappPart) Description() string {
	return s.description
}

func (s *SnappPart) Hash() string {
	return s.hash
}

func (s *SnappPart) IsActive() bool {
	return s.isActive
}

func (s *SnappPart) IsInstalled() bool {
	return s.isInstalled
}

func (s *SnappPart) InstalledSize() int {
	return -1
}

func (s *SnappPart) DownloadSize() int {
	return -1
}

func (s *SnappPart) Install() (err error) {
	return err
}

func (s *SnappPart) Uninstall() (err error) {
	return err
}

func (s *SnappPart) Config(configuration []byte) (err error) {
	return err
}

type SnappLocalRepository struct {
	path string
}

func NewLocalSnappRepository(path string) *SnappLocalRepository {
	if s, err := os.Stat(path); err != nil || !s.IsDir() {
		log.Printf("Invalid directory %s (%s)", path, err)
		return nil
	}
	return &SnappLocalRepository{path: path}
}

func (s *SnappLocalRepository) Description() string {
	return fmt.Sprintf("Snapp local repository for %s", s.path)
}

func (s *SnappLocalRepository) Search(terms string) (versions []Part, err error) {
	return versions, err
}

func (s *SnappLocalRepository) GetUpdates() (parts []Part, err error) {

	return parts, err
}

func (s *SnappLocalRepository) GetInstalled() (parts []Part, err error) {

	globExpr := path.Join(s.path, "*", "*", "meta", "package.yaml")
	matches, err := filepath.Glob(globExpr)
	if err != nil {
		return parts, err
	}
	for _, yamlfile := range matches {
		snapp := NewLocalSnappPart(yamlfile)
		if snapp != nil {
			parts = append(parts, snapp)
		}
	}

	return parts, err
}
