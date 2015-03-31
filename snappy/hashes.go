/*
 * Copyright (C) 2014-2015 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

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
	buf := []byte("----------")

	switch {
	case (v.mode & os.ModeDir) != 0:
		buf[0] = 'd'
	case (v.mode & os.ModeSymlink) != 0:
		buf[0] = 'l'
	case (v.mode & os.ModeType) == 0:
		buf[0] = 'f'
	default:
		return "", fmt.Errorf("Unknown file mode %s", v.mode)
	}

	const rwx = "rwxrwxrwx"
	for i, c := range rwx {
		if v.mode&(1<<uint(9-1-i)) != 0 {
			buf[i+1] = byte(c)
		} else {
			buf[i+1] = '-'
		}
	}

	return string(buf), nil
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
	case 'f':
		// default
		m |= 0
	case 'l':
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
