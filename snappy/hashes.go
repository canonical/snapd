package snappy

import (
	"fmt"
	"os"
)

type yamlFileMode struct {
	mode os.FileMode
}

func newYamlFileMode(mode os.FileMode) *yamlFileMode {
	return &yamlFileMode{mode: mode}
}

func (v *yamlFileMode) MarshalYAML() (interface{}, error) {
	return v.mode.String(), nil
}

func (v *yamlFileMode) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var modeAsStr string
	err := unmarshal(&modeAsStr)
	if err != nil {
		return err
	}

	var m os.FileMode
	switch modeAsStr[0] {
	case 'd':
		m |= os.ModeDir
	case '-':
		// default
		m |= 0
	case 'L':
		m |= os.ModeSymlink
	default:
		return fmt.Errorf("Unknown file mode %s", modeAsStr)
	}

	const rwx = "rwxrwxrwx"
	for i, c := range modeAsStr[1:] {
		if byte(c) == rwx[i] {
			m |= (1 << uint(9-1-i))
		}
	}
	v.mode = m

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
