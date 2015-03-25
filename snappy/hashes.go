package snappy

import (
	"fmt"
	"os"
	"strconv"
)

type yamlFileMode struct {
	mode os.FileMode
}

func newYamlFileMode(mode os.FileMode) *yamlFileMode {
	return &yamlFileMode{mode: mode}
}

func (v *yamlFileMode) MarshalYAML() (interface{}, error) {
	s := fmt.Sprintf("0%o", v.mode)
	return s, nil
}

func (v *yamlFileMode) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var modeAsStr string
	err := unmarshal(&modeAsStr)
	if err != nil {
		return err
	}

	mode, err := strconv.ParseInt(modeAsStr, 8, 0)
	if err != nil {
		return err
	}
	v.mode = os.FileMode(mode)

	return nil
}

// the file securiy information for a individual file, note that we do
// not store the Uid/Gid here because its irrelevant, on unpack the
// uid/gid is set to "snap"
type fileHash struct {
	Name   string        `yaml:"name"`
	Size   *int64        `yaml:"size,omitempty"`
	Sha512 string        `yaml:"sha512,omitempty"`
	Mode   *yamlFileMode `yaml:"mode"`
	// FIXME: not used yet, our tar implementation does not
	//        support it yet and writeHashes doesn't either
	XAttr map[string]string `yaml:"xattr,omitempty"`
}

// the meta/hashes file
type hashesYaml struct {
	// the archive hash
	ArchiveSha512 string `yaml:"archive-sha512"`

	// the hashes for the files in the archive
	Files []fileHash
}
