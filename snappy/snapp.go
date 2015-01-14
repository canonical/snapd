package snappy

import (
	"log"
	"os"
	"io/ioutil"
)

type SnappPart struct {
	name string
	version string
	description string
	hash string 
	isActive bool 
	isInstalled bool 
	
}

func NewLocalSnappPart(path string) *SnappPart {
	part := SnappPart{}

	yaml_path := path + "/meta/package.yaml"
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
	
	yaml, err := getMapFromYaml(yaml_data)
	part.name = yaml["name"].(string)
	part.version = yaml["version"].(string)
	part.isInstalled = true
	// FIXME: figure out if we are active
	
	
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
