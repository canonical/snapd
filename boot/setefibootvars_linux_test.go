// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023-2024 Canonical Ltd
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

package boot_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	. "gopkg.in/check.v1"

	"golang.org/x/text/encoding/unicode"

	"github.com/canonical/go-efilib"
	"github.com/canonical/go-efilib/linux"

	"github.com/snapcore/snapd/boot"
)

type setEfiBootVarsSuite struct {
	baseBootenvSuite
}

var _ = Suite(&setEfiBootVarsSuite{})

func (s *setEfiBootVarsSuite) SetUpTest(c *C) {
	s.baseBootenvSuite.SetUpTest(c)
}

var (
	errShouldNotOverwrite = errors.New("should not overwrite an existing Boot#### variable")
)

type fakeDevicePathNode struct {
	buf []byte
}

func (n *fakeDevicePathNode) String() string {
	return string(n.buf[:])
}

func (n *fakeDevicePathNode) ToString(flags efi.DevicePathToStringFlags) string {
	return n.String()
}

func (n *fakeDevicePathNode) Write(w io.Writer) error {
	_, err := w.Write(n.buf)
	return err
}

func stringToNode(path string) efi.DevicePathNode {
	return efi.DevicePathNode(&fakeDevicePathNode{
		buf: []byte(path),
	})
}

func stringToDevicePath(str string) efi.DevicePath {
	pathComponents := strings.Split(str, "/")
	pathNodes := make([]efi.DevicePathNode, 0, len(pathComponents))
	for _, comp := range pathComponents {
		pathNodes = append(pathNodes, stringToNode(comp))
	}
	return pathNodes
}

func (s *setEfiBootVarsSuite) TestStringToDevicePath(c *C) {
	path := "path/to/dir/with/file.efi"
	devicePath := stringToDevicePath(path)
	c.Assert(devicePath, HasLen, 5)
	pathWithBackslashes := "\\" + strings.ReplaceAll(path, "/", "\\")
	c.Assert(devicePath.String(), Equals, pathWithBackslashes)
}

func stringToUtf16Bytes(c *C, str string) []byte {
	encoder := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewEncoder()
	arr, err := encoder.Bytes([]byte(str))
	c.Assert(err, IsNil)
	return append(arr, []byte{0x00, 0x00}...)
}

func (s *setEfiBootVarsSuite) TestStringToUtf16Bytes(c *C) {
	str := "ubuntu"
	expected := []byte{0x75, 0x00, 0x62, 0x00, 0x75, 0x00, 0x6e, 0x00, 0x74, 0x00, 0x75, 0x00, 0x00, 0x00}
	result := stringToUtf16Bytes(c, str)
	c.Assert(result, DeepEquals, expected)
}

func (s *setEfiBootVarsSuite) TestConstructLoadOption(c *C) {
	restore := boot.MockLinuxFilePathToDevicePath(func(path string, mode linux.FilePathToDevicePathMode) (out efi.DevicePath, err error) {
		return stringToDevicePath(path), nil
	})
	defer restore()

	expectedAttributes := []byte{0x01, 0x00, 0x00, 0x00}

	for _, tc := range []struct {
		description  string
		assetPath    string
		optionalData []byte
	}{
		{
			"default",
			"EFI/boot/bootx64.efi",
			nil,
		},
		{
			"ubuntu",
			"EFI/ubuntu/shimx64.efi",
			[]byte("This is the boot entry for ubuntu"),
		},
		{
			"fallback",
			"EFI/boot/fallback.efi",
			make([]byte, 0),
		},
	} {
		expectedDescription := stringToUtf16Bytes(c, tc.description)
		expectedPath := []byte(strings.ReplaceAll(tc.assetPath, "/", ""))
		expectedRest := append(expectedPath, append([]byte{0x7f, 0xff, 0x04, 0x00}, tc.optionalData...)...)
		result, err := boot.ConstructLoadOption(tc.description, tc.assetPath, tc.optionalData)
		c.Assert(err, IsNil)
		c.Assert(result[:4], DeepEquals, expectedAttributes)
		c.Assert(result[6:6+len(expectedDescription)], DeepEquals, expectedDescription)
		c.Assert(result[len(result)-len(expectedRest):], DeepEquals, expectedRest)
	}
}

func (s *setEfiBootVarsSuite) TestConstructLoadOptionNullOptionalData(c *C) {
	restore := boot.MockLinuxFilePathToDevicePath(func(path string, mode linux.FilePathToDevicePathMode) (out efi.DevicePath, err error) {
		return stringToDevicePath(path), nil
	})
	defer restore()

	desc := "ubuntu"
	path := "EFI/ubuntu/shimx64.efi"

	option1, err := boot.ConstructLoadOption(desc, path, nil)
	c.Assert(err, IsNil)
	option2, err := boot.ConstructLoadOption(desc, path, make([]byte, 0))
	c.Assert(err, IsNil)
	c.Assert(option1, DeepEquals, option2)
}

type varDataAttrs struct {
	data  []byte
	attrs efi.VariableAttributes
}

var defaultVarAttrs = efi.AttributeNonVolatile | efi.AttributeBootserviceAccess | efi.AttributeRuntimeAccess

func (s *setEfiBootVarsSuite) TestSetEfiBootOptionVariable(c *C) {
	boot0Option := efi.LoadOption{
		Attributes:  efi.LoadOptionActive | efi.LoadOptionCategoryBoot,
		Description: "ubuntu",
		FilePath:    stringToDevicePath("/run/mnt/ubuntu-seed/EFI/ubuntu/grubx64.efi"),
	}
	boot0OptionBytes, err := boot0Option.Bytes()
	c.Assert(err, IsNil)

	boot1Option := efi.LoadOption{
		Attributes:  efi.LoadOptionActive | efi.LoadOptionCategoryBoot,
		Description: "ubuntu",
		FilePath:    stringToDevicePath("/run/mnt/ubuntu-seed/EFI/BOOT/BOOTX64.efi"),
	}
	boot1OptionBytes, err := boot1Option.Bytes()
	c.Assert(err, IsNil)

	boot3Option := efi.LoadOption{
		Attributes:  efi.LoadOptionActive | efi.LoadOptionCategoryBoot,
		Description: "ubuntu",
		FilePath:    stringToDevicePath("/run/mnt/ubuntu-seed/EFI/fakedir/shimx64.efi"),
	}
	boot3OptionBytes, err := boot3Option.Bytes()
	c.Assert(err, IsNil)

	fakeVariableData := map[efi.VariableDescriptor]*varDataAttrs{
		{
			Name: "foo",
			GUID: efi.GlobalVariable,
		}: {
			[]byte("foo"),
			defaultVarAttrs,
		},
		{
			Name: "Boot0000",
			GUID: efi.GlobalVariable,
		}: {
			boot0OptionBytes,
			defaultVarAttrs,
		},
		{
			Name: "BootOrder",
			GUID: efi.GlobalVariable,
		}: {
			[]byte{0x02, 0x00, 0x34, 0x12, 0x03, 0x00, 0x01, 0x00},
			defaultVarAttrs,
		},
		{
			Name: "Boot0003",
			GUID: efi.GlobalVariable,
		}: {
			boot3OptionBytes,
			defaultVarAttrs,
		},
		{
			Name: "Boot0001",
			GUID: efi.GlobalVariable,
		}: {
			boot1OptionBytes,
			defaultVarAttrs,
		},
	}
	restore := boot.MockEfiListVariables(func() ([]efi.VariableDescriptor, error) {
		varDescriptorList := make([]efi.VariableDescriptor, 0, len(fakeVariableData))
		for key := range fakeVariableData {
			varDescriptorList = append(varDescriptorList, key)
		}
		return varDescriptorList, nil
	})
	defer restore()
	restore = boot.MockEfiReadVariable(func(name string, guid efi.GUID) ([]byte, efi.VariableAttributes, error) {
		descriptor := efi.VariableDescriptor{
			Name: name,
			GUID: guid,
		}
		if varDA, exists := fakeVariableData[descriptor]; exists {
			return varDA.data, varDA.attrs, nil
		}
		return nil, 0, efi.ErrVarNotExist
	})
	defer restore()
	writeChan := make(chan []byte, 1)
	restore = boot.MockEfiWriteVariable(func(name string, guid efi.GUID, attrs efi.VariableAttributes, data []byte) error {
		for varDesc := range fakeVariableData {
			if varDesc.Name == name && varDesc.GUID == guid {
				return errShouldNotOverwrite
			}
		}
		writeChan <- data
		return nil
	})
	defer restore()

	// Check that existing variables are matched

	initialVarCount := len(fakeVariableData)

	bootNum, err := boot.SetEfiBootOptionVariable(boot1OptionBytes)
	c.Assert(err, IsNil)
	c.Assert(bootNum, Equals, uint16(1))
	c.Assert(len(fakeVariableData), Equals, initialVarCount)

	bootNum, err = boot.SetEfiBootOptionVariable(boot3OptionBytes)
	c.Assert(err, IsNil)
	c.Assert(bootNum, Equals, uint16(3))
	c.Assert(len(fakeVariableData), Equals, initialVarCount)

	// Check that non-matching path adds a new variable at Boot0002

	newOption := efi.LoadOption{
		Attributes:  efi.LoadOptionActive | efi.LoadOptionCategoryBoot,
		Description: "ubuntu",
		FilePath:    stringToDevicePath("/run/mnt/ubuntu-seed/EFI/fedora/shimx64.efi"),
	}
	newOptionBytes, err := newOption.Bytes()
	c.Assert(err, IsNil)
	bootNum, err = boot.SetEfiBootOptionVariable(newOptionBytes)
	c.Assert(err, IsNil)
	c.Assert(bootNum, Equals, uint16(2))
	c.Assert(<-writeChan, DeepEquals, newOptionBytes)
	newPathDesc := efi.VariableDescriptor{
		Name: "Boot0002",
		GUID: efi.GlobalVariable,
	}
	fakeVariableData[newPathDesc] = &varDataAttrs{
		data:  newOptionBytes,
		attrs: defaultVarAttrs,
	}
	c.Assert(len(fakeVariableData), Equals, initialVarCount+1)

	// Check that re-running on the same path chooses the newly-existing variable again
	bootNum, err = boot.SetEfiBootOptionVariable(newOptionBytes)
	c.Assert(err, IsNil)
	c.Assert(bootNum, Equals, uint16(2))
}

func (s *setEfiBootVarsSuite) TestMismatchedGuid(c *C) {
	bootOption := efi.LoadOption{
		Attributes:  efi.LoadOptionActive | efi.LoadOptionCategoryBoot,
		Description: "ubuntu",
		FilePath:    stringToDevicePath("/run/mnt/ubuntu-seed/EFI/BOOT/BOOTX64.efi"),
	}
	bootOptionBytes, err := bootOption.Bytes()
	c.Assert(err, IsNil)

	shimOption := efi.LoadOption{
		Attributes:  efi.LoadOptionActive | efi.LoadOptionCategoryBoot,
		Description: "ubuntu",
		FilePath:    stringToDevicePath("/run/mnt/ubuntu-seed/EFI/ubuntu/shimx64.efi"),
	}
	shimOptionBytes, err := shimOption.Bytes()
	c.Assert(err, IsNil)

	grubOption := efi.LoadOption{
		Attributes:  efi.LoadOptionActive | efi.LoadOptionCategoryBoot,
		Description: "ubuntu",
		FilePath:    stringToDevicePath("/run/mnt/ubuntu-seed/EFI/ubuntu/grubx64.efi"),
	}
	grubOptionBytes, err := grubOption.Bytes()
	c.Assert(err, IsNil)

	fakeVariableData := map[efi.VariableDescriptor]*varDataAttrs{
		{
			Name: "Boot0000",
			GUID: efi.ImageSecurityDatabaseGuid,
		}: {
			bootOptionBytes,
			defaultVarAttrs,
		},
		{
			Name: "Boot0001",
			GUID: efi.GlobalVariable,
		}: {
			shimOptionBytes,
			defaultVarAttrs,
		},
		{
			Name: "Boot0002",
			GUID: efi.GlobalVariable,
		}: {
			bootOptionBytes,
			defaultVarAttrs,
		},
		{
			Name: "Boot0003",
			GUID: efi.ImageSecurityDatabaseGuid,
		}: {
			grubOptionBytes,
			defaultVarAttrs,
		},
	}
	restore := boot.MockEfiListVariables(func() ([]efi.VariableDescriptor, error) {
		varDescriptorList := make([]efi.VariableDescriptor, 0, len(fakeVariableData))
		for key := range fakeVariableData {
			varDescriptorList = append(varDescriptorList, key)
		}
		return varDescriptorList, nil
	})
	defer restore()
	restore = boot.MockEfiReadVariable(func(name string, guid efi.GUID) ([]byte, efi.VariableAttributes, error) {
		descriptor := efi.VariableDescriptor{
			Name: name,
			GUID: guid,
		}
		if varDA, exists := fakeVariableData[descriptor]; exists {
			return varDA.data, varDA.attrs, nil
		}
		return nil, 0, efi.ErrVarNotExist
	})
	defer restore()
	writeChan := make(chan []byte, 1)
	restore = boot.MockEfiWriteVariable(func(name string, guid efi.GUID, attrs efi.VariableAttributes, data []byte) error {
		for varDesc := range fakeVariableData {
			if varDesc.Name == name && varDesc.GUID == guid {
				return errShouldNotOverwrite
			}
		}
		writeChan <- data
		return nil
	})
	defer restore()

	bootNum, err := boot.SetEfiBootOptionVariable(shimOptionBytes)
	c.Assert(err, IsNil)
	c.Assert(bootNum, Equals, uint16(1))

	bootNum, err = boot.SetEfiBootOptionVariable(bootOptionBytes)
	c.Assert(err, IsNil)
	c.Assert(bootNum, Equals, uint16(2))

	bootNum, err = boot.SetEfiBootOptionVariable(grubOptionBytes)
	c.Assert(err, IsNil)
	c.Assert(bootNum, Equals, uint16(0))
	c.Assert(<-writeChan, DeepEquals, grubOptionBytes)
}

func (s *setEfiBootVarsSuite) TestSetEfiBootOptionVarAttrs(c *C) {
	bootOption := efi.LoadOption{
		Attributes:  efi.LoadOptionActive | efi.LoadOptionCategoryBoot,
		Description: "ubuntu",
		FilePath:    stringToDevicePath("/run/mnt/ubuntu-seed/EFI/BOOT/BOOTX64.efi"),
	}
	bootOptionBytes, err := bootOption.Bytes()
	c.Assert(err, IsNil)
	bootAttrs := efi.AttributeNonVolatile | efi.AttributeBootserviceAccess

	shimOption := efi.LoadOption{
		Attributes:  efi.LoadOptionActive | efi.LoadOptionCategoryBoot,
		Description: "ubuntu",
		FilePath:    stringToDevicePath("/run/mnt/ubuntu-seed/EFI/ubuntu/shimx64.efi"),
	}
	shimOptionBytes, err := shimOption.Bytes()
	c.Assert(err, IsNil)
	shimAttrs := defaultVarAttrs | efi.AttributeAuthenticatedWriteAccess

	grubOption := efi.LoadOption{
		Attributes:  efi.LoadOptionActive | efi.LoadOptionCategoryBoot,
		Description: "ubuntu",
		FilePath:    stringToDevicePath("/run/mnt/ubuntu-seed/EFI/ubuntu/grubx64.efi"),
	}
	grubOptionBytes, err := grubOption.Bytes()
	c.Assert(err, IsNil)
	grubAttrs := defaultVarAttrs

	fakeVariableData := map[efi.VariableDescriptor]*varDataAttrs{
		{
			Name: "Boot0000",
			GUID: efi.GlobalVariable,
		}: {
			bootOptionBytes,
			bootAttrs,
		},
		{
			Name: "Boot0001",
			GUID: efi.GlobalVariable,
		}: {
			shimOptionBytes,
			shimAttrs,
		},
		{
			Name: "Boot0002",
			GUID: efi.GlobalVariable,
		}: {
			grubOptionBytes,
			grubAttrs,
		},
	}
	restore := boot.MockEfiListVariables(func() ([]efi.VariableDescriptor, error) {
		varDescriptorList := make([]efi.VariableDescriptor, 0, len(fakeVariableData))
		for key := range fakeVariableData {
			varDescriptorList = append(varDescriptorList, key)
		}
		return varDescriptorList, nil
	})
	defer restore()
	restore = boot.MockEfiReadVariable(func(name string, guid efi.GUID) ([]byte, efi.VariableAttributes, error) {
		descriptor := efi.VariableDescriptor{
			Name: name,
			GUID: guid,
		}
		if varDA, exists := fakeVariableData[descriptor]; exists {
			return varDA.data, varDA.attrs, nil
		}
		return nil, 0, efi.ErrVarNotExist
	})
	defer restore()
	restore = boot.MockEfiWriteVariable(func(name string, guid efi.GUID, attrs efi.VariableAttributes, data []byte) error {
		for varDesc := range fakeVariableData {
			if varDesc.Name == name && varDesc.GUID == guid {
				return errShouldNotOverwrite
			}
		}
		c.Assert(attrs, Equals, defaultVarAttrs)
		return nil
	})
	defer restore()

	bootNum, err := boot.SetEfiBootOptionVariable(bootOptionBytes)
	c.Assert(err, IsNil)
	c.Assert(bootNum, Equals, uint16(0))

	bootNum, err = boot.SetEfiBootOptionVariable(shimOptionBytes)
	c.Assert(err, IsNil)
	c.Assert(bootNum, Equals, uint16(1))

	bootNum, err = boot.SetEfiBootOptionVariable(grubOptionBytes)
	c.Assert(err, IsNil)
	c.Assert(bootNum, Equals, uint16(2))

	newOption := efi.LoadOption{
		Attributes:  efi.LoadOptionActive | efi.LoadOptionCategoryBoot,
		Description: "ubuntu",
		FilePath:    stringToDevicePath("/run/mnt/ubuntu-seed/EFI/foo/bar.efi"),
	}
	newOptionBytes, err := newOption.Bytes()
	c.Assert(err, IsNil)
	bootNum, err = boot.SetEfiBootOptionVariable(newOptionBytes)
	c.Assert(err, IsNil)
	c.Assert(bootNum, Equals, uint16(3))
}

func (s *setEfiBootVarsSuite) TestOutOfBootNumbers(c *C) {
	bootOption := efi.LoadOption{
		Attributes:  efi.LoadOptionActive | efi.LoadOptionCategoryBoot,
		Description: "ubuntu",
		FilePath:    stringToDevicePath("/run/mnt/ubuntu-seed/EFI/BOOT/BOOTX64.efi"),
	}
	bootOptionBytes, err := bootOption.Bytes()
	c.Assert(err, IsNil)

	shimOption := efi.LoadOption{
		Attributes:  efi.LoadOptionActive | efi.LoadOptionCategoryBoot,
		Description: "ubuntu",
		FilePath:    stringToDevicePath("/run/mnt/ubuntu-seed/EFI/newdir/shimx64.efi"),
	}
	shimOptionBytes, err := shimOption.Bytes()
	c.Assert(err, IsNil)

	fakeVariableData := make(map[efi.VariableDescriptor]*varDataAttrs)

	restore := boot.MockEfiListVariables(func() ([]efi.VariableDescriptor, error) {
		varDescriptorList := make([]efi.VariableDescriptor, 0, len(fakeVariableData))
		for key := range fakeVariableData {
			varDescriptorList = append(varDescriptorList, key)
		}
		return varDescriptorList, nil
	})
	defer restore()
	restore = boot.MockEfiReadVariable(func(name string, guid efi.GUID) ([]byte, efi.VariableAttributes, error) {
		descriptor := efi.VariableDescriptor{
			Name: name,
			GUID: guid,
		}
		if varDA, exists := fakeVariableData[descriptor]; exists {
			return varDA.data, varDA.attrs, nil
		}
		return nil, 0, efi.ErrVarNotExist
	})
	defer restore()
	writeChan := make(chan []byte, 1)
	restore = boot.MockEfiWriteVariable(func(name string, guid efi.GUID, attrs efi.VariableAttributes, data []byte) error {
		for varDesc := range fakeVariableData {
			if varDesc.Name == name && varDesc.GUID == guid {
				return errShouldNotOverwrite
			}
		}
		writeChan <- data
		return nil
	})
	defer restore()

	// Set all boot variables <= BootFFFE
	var bootName string
	var descriptor efi.VariableDescriptor
	var dataAttrs *varDataAttrs
	for i := 0; i <= 0xFFFE; i++ {
		bootName = fmt.Sprintf("Boot%04X", i)
		descriptor = efi.VariableDescriptor{
			Name: bootName,
			GUID: efi.GlobalVariable,
		}
		dataAttrs = &varDataAttrs{
			data:  bootOptionBytes,
			attrs: defaultVarAttrs,
		}
		fakeVariableData[descriptor] = dataAttrs
	}

	// Check that it is possible to select BootFFFF if unused
	bootNum, err := boot.SetEfiBootOptionVariable(shimOptionBytes)
	c.Assert(err, IsNil)
	c.Assert(bootNum, Equals, uint16(0xFFFF))
	c.Assert(<-writeChan, DeepEquals, shimOptionBytes)

	bootNum, err = boot.SetEfiBootOptionVariable(bootOptionBytes)
	c.Assert(err, IsNil)
	// If multiple variables match, undefined which one will be returned
	// since map is converted to list in test code.
	c.Assert(bootNum >= uint16(0) && bootNum < uint16(0xFFFF), Equals, true)

	// Add final BootFFFF variable to make all used
	descriptor = efi.VariableDescriptor{
		Name: "BootFFFF",
		GUID: efi.GlobalVariable,
	}
	dataAttrs = &varDataAttrs{
		data:  bootOptionBytes,
		attrs: defaultVarAttrs,
	}
	fakeVariableData[descriptor] = dataAttrs

	// Check that if there's no match and no numbers left, throws error
	_, err = boot.SetEfiBootOptionVariable(shimOptionBytes)
	c.Assert(err, Equals, boot.ErrAllBootNumsUsed)

	// Check that even if all boot nums are used, correct match still occurs
	bootNum, err = boot.SetEfiBootOptionVariable(bootOptionBytes)
	c.Assert(err, IsNil)
	// If multiple variables match, undefined which one will be returned since
	// map is converted to list in test code.
	c.Assert(bootNum >= uint16(0) && bootNum <= uint16(0xFFFF), Equals, true)
}

func (s *setEfiBootVarsSuite) TestSetEfiBootOrderVariable(c *C) {
	allBootNumbers := make([]byte, 0x20000-2) // all but BootFFFF
	for n := 0; n < 0xFFFF; n++ {
		allBootNumbers[n*2] = byte(n & 0xFF)
		allBootNumbers[n*2+1] = byte((n >> 8) & 0xFF)
	}
	allBootNumbersFffeFirst := make([]byte, 0x20000-2)
	allBootNumbersFffeFirst[0] = 0xFE
	allBootNumbersFffeFirst[1] = 0xFF
	copy(allBootNumbersFffeFirst[2:], allBootNumbers[:0x20000-4])
	allBootNumbersFfffFirst := make([]byte, 0x20000)
	allBootNumbersFfffFirst[0] = 0xFF
	allBootNumbersFfffFirst[1] = 0xFF
	copy(allBootNumbersFfffFirst[2:], allBootNumbers)
	testCases := []struct {
		bootNum          uint16
		initialBootOrder []byte
		finalBootOrder   []byte
	}{
		{
			uint16(0),
			[]byte{0, 0, 1, 0, 2, 0, 3, 0},
			[]byte{0, 0, 1, 0, 2, 0, 3, 0},
		},
		{
			uint16(1),
			[]byte{0, 0, 1, 0, 2, 0, 3, 0},
			[]byte{1, 0, 0, 0, 2, 0, 3, 0},
		},
		{
			uint16(2),
			[]byte{0, 0, 1, 0, 2, 0, 3, 0},
			[]byte{2, 0, 0, 0, 1, 0, 3, 0},
		},
		{
			uint16(3),
			[]byte{0, 0, 1, 0, 2, 0, 3, 0},
			[]byte{3, 0, 0, 0, 1, 0, 2, 0},
		},
		{
			uint16(4),
			[]byte{0, 0, 1, 0, 2, 0, 3, 0},
			[]byte{4, 0, 0, 0, 1, 0, 2, 0, 3, 0},
		},
		{
			uint16(2),
			[]byte{0, 0, 1, 0, 3, 0, 4, 0},
			[]byte{2, 0, 0, 0, 1, 0, 3, 0, 4, 0},
		},
		{
			uint16(2),
			[]byte{1, 0, 0, 0, 3, 0},
			[]byte{2, 0, 1, 0, 0, 0, 3, 0},
		},
		{
			uint16(0xFFFE),
			allBootNumbers,
			allBootNumbersFffeFirst,
		},
		{
			uint16(7),
			[]byte{},
			[]byte{7, 0},
		},
		{
			uint16(43),
			nil,
			[]byte{43, 0},
		},
	}
	readChan := make(chan []byte, 1)
	restore := boot.MockEfiReadVariable(func(name string, guid efi.GUID) ([]byte, efi.VariableAttributes, error) {
		value := <-readChan
		if value == nil {
			return nil, 0, efi.ErrVarNotExist
		}
		return value, defaultVarAttrs, nil
	})
	defer restore()
	writeChan := make(chan []byte, 1)
	restore = boot.MockEfiWriteVariable(func(name string, guid efi.GUID, attrs efi.VariableAttributes, data []byte) error {
		c.Assert(name, Equals, "BootOrder")
		c.Assert(guid, Equals, efi.GlobalVariable)
		c.Assert(attrs, Equals, defaultVarAttrs)
		writeChan <- data
		return nil
	})
	defer restore()
	for _, tc := range testCases {
		readChan <- tc.initialBootOrder
		err := boot.SetEfiBootOrderVariable(tc.bootNum)
		c.Assert(err, IsNil)
		select {
		case written := <-writeChan:
			if bytes.Compare(tc.initialBootOrder, tc.finalBootOrder) == 0 {
				c.Fatalf("should not have written BootOrder: %+v", tc)
			}
			c.Assert(written, DeepEquals, tc.finalBootOrder)
		default:
			if bytes.Compare(tc.initialBootOrder, tc.finalBootOrder) == 0 {
				continue // BootOrder unchanged, so there should be no write
			}
			c.Fatalf("BootOrder was not written: %+v", tc)
		}
	}
}

func (s *setEfiBootVarsSuite) TestSetEfiBootOrderVarAttrs(c *C) {
	bootNum := uint16(2)
	initialBootOrder := []byte{0, 0, 1, 0, 2, 0, 3, 0}
	finalBootOrder := []byte{2, 0, 0, 0, 1, 0, 3, 0}
	testAttrs := []efi.VariableAttributes{
		efi.AttributeNonVolatile | efi.AttributeBootserviceAccess,
		defaultVarAttrs,
		defaultVarAttrs | efi.AttributeAuthenticatedWriteAccess,
	}
	attrReadChan := make(chan efi.VariableAttributes, 1)
	restore := boot.MockEfiReadVariable(func(name string, guid efi.GUID) ([]byte, efi.VariableAttributes, error) {
		return initialBootOrder, <-attrReadChan, nil
	})
	defer restore()
	attrWriteChan := make(chan efi.VariableAttributes, 1)
	restore = boot.MockEfiWriteVariable(func(name string, guid efi.GUID, attrs efi.VariableAttributes, data []byte) error {
		c.Assert(name, Equals, "BootOrder")
		c.Assert(guid, Equals, efi.GlobalVariable)
		c.Assert(data, DeepEquals, finalBootOrder)
		attrWriteChan <- attrs
		return nil
	})
	defer restore()
	for _, attrs := range testAttrs {
		attrReadChan <- attrs
		err := boot.SetEfiBootOrderVariable(bootNum)
		c.Assert(err, IsNil)
		select {
		case a := <-attrWriteChan:
			c.Assert(a, Equals, attrs)
		default:
			c.Fatalf("BootOrder was not written with attrs: %+v", attrs)
		}
	}
}

func (s *setEfiBootVarsSuite) TestSetEfiBootVariables(c *C) {
	bootOption := efi.LoadOption{
		Attributes:  efi.LoadOptionActive | efi.LoadOptionCategoryBoot,
		Description: "ubuntu",
		FilePath:    stringToDevicePath("/run/mnt/ubuntu-seed/EFI/BOOT/BOOTX64.efi"),
	}
	bootOptionBytes, err := bootOption.Bytes()
	c.Assert(err, IsNil)

	fakeVariableData := map[efi.VariableDescriptor]*varDataAttrs{
		{
			Name: "Boot0000",
			GUID: efi.GlobalVariable,
		}: {
			bootOptionBytes,
			defaultVarAttrs,
		},
		{
			Name: "BootOrder",
			GUID: efi.GlobalVariable,
		}: {
			[]byte{0x00, 0x00},
			defaultVarAttrs,
		},
	}

	defer boot.MockEfiReadVariable(func(name string, guid efi.GUID) ([]byte, efi.VariableAttributes, error) {
		descriptor := efi.VariableDescriptor{
			Name: name,
			GUID: guid,
		}
		if varDA, exists := fakeVariableData[descriptor]; exists {
			return varDA.data, varDA.attrs, nil
		}
		return nil, 0, efi.ErrVarNotExist
	})()

	defer boot.MockEfiListVariables(func() ([]efi.VariableDescriptor, error) {
		varDescriptorList := make([]efi.VariableDescriptor, 0, len(fakeVariableData))
		for key := range fakeVariableData {
			varDescriptorList = append(varDescriptorList, key)
		}
		return varDescriptorList, nil
	})()

	written := 0
	defer boot.MockEfiWriteVariable(func(name string, guid efi.GUID, attrs efi.VariableAttributes, data []byte) error {
		written += 1
		if name == "BootOrder" && guid == efi.GlobalVariable {
			return nil
		}
		for varDesc := range fakeVariableData {
			if varDesc.Name == name && varDesc.GUID == guid {
				return errShouldNotOverwrite
			}
		}
		return nil
	})()

	defer boot.MockLinuxFilePathToDevicePath(func(path string, mode linux.FilePathToDevicePathMode) (out efi.DevicePath, err error) {
		return stringToDevicePath(path), nil
	})()

	optionalData := []byte("This is the boot entry for ubuntu")

	err = boot.SetEfiBootVariables("myentry", "EFI/myentry/shimx64.efi", optionalData)
	c.Assert(err, IsNil)
	c.Check(written, Equals, 2)
}

func (s *setEfiBootVarsSuite) TestSetEfiBootVariablesConstructError(c *C) {
	bootOption := efi.LoadOption{
		Attributes:  efi.LoadOptionActive | efi.LoadOptionCategoryBoot,
		Description: "ubuntu",
		FilePath:    stringToDevicePath("/run/mnt/ubuntu-seed/EFI/BOOT/BOOTX64.efi"),
	}
	bootOptionBytes, err := bootOption.Bytes()
	c.Assert(err, IsNil)

	fakeVariableData := map[efi.VariableDescriptor]*varDataAttrs{
		{
			Name: "Boot0000",
			GUID: efi.GlobalVariable,
		}: {
			bootOptionBytes,
			defaultVarAttrs,
		},
		{
			Name: "BootOrder",
			GUID: efi.GlobalVariable,
		}: {
			[]byte{0x00, 0x00},
			defaultVarAttrs,
		},
	}

	defer boot.MockEfiReadVariable(func(name string, guid efi.GUID) ([]byte, efi.VariableAttributes, error) {
		descriptor := efi.VariableDescriptor{
			Name: name,
			GUID: guid,
		}
		if varDA, exists := fakeVariableData[descriptor]; exists {
			return varDA.data, varDA.attrs, nil
		}
		return nil, 0, efi.ErrVarNotExist
	})()

	defer boot.MockEfiListVariables(func() ([]efi.VariableDescriptor, error) {
		varDescriptorList := make([]efi.VariableDescriptor, 0, len(fakeVariableData))
		for key := range fakeVariableData {
			varDescriptorList = append(varDescriptorList, key)
		}
		return varDescriptorList, nil
	})()

	defer boot.MockEfiWriteVariable(func(name string, guid efi.GUID, attrs efi.VariableAttributes, data []byte) error {
		c.Fatalf("Not execpted to write variables")
		return nil
	})()

	defer boot.MockLinuxFilePathToDevicePath(func(path string, mode linux.FilePathToDevicePathMode) (out efi.DevicePath, err error) {
		return nil, fmt.Errorf("INJECT ERROR")
	})()

	optionalData := []byte("This is the boot entry for ubuntu")

	err = boot.SetEfiBootVariables("myentry", "EFI/myentry/shimx64.efi", optionalData)
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, `.*INJECT ERROR`)
}

func (s *setEfiBootVarsSuite) TestSetEfiBootVariablesErrorSetVariable(c *C) {
	bootOption := efi.LoadOption{
		Attributes:  efi.LoadOptionActive | efi.LoadOptionCategoryBoot,
		Description: "ubuntu",
		FilePath:    stringToDevicePath("/run/mnt/ubuntu-seed/EFI/BOOT/BOOTX64.efi"),
	}
	bootOptionBytes, err := bootOption.Bytes()
	c.Assert(err, IsNil)

	fakeVariableData := map[efi.VariableDescriptor]*varDataAttrs{
		{
			Name: "Boot0000",
			GUID: efi.GlobalVariable,
		}: {
			bootOptionBytes,
			defaultVarAttrs,
		},
		{
			Name: "BootOrder",
			GUID: efi.GlobalVariable,
		}: {
			[]byte{0x00, 0x00},
			defaultVarAttrs,
		},
	}

	defer boot.MockEfiReadVariable(func(name string, guid efi.GUID) ([]byte, efi.VariableAttributes, error) {
		descriptor := efi.VariableDescriptor{
			Name: name,
			GUID: guid,
		}
		if varDA, exists := fakeVariableData[descriptor]; exists {
			return varDA.data, varDA.attrs, nil
		}
		return nil, 0, efi.ErrVarNotExist
	})()

	defer boot.MockEfiListVariables(func() ([]efi.VariableDescriptor, error) {
		varDescriptorList := make([]efi.VariableDescriptor, 0, len(fakeVariableData))
		for key := range fakeVariableData {
			varDescriptorList = append(varDescriptorList, key)
		}
		return varDescriptorList, nil
	})()

	written := 0
	defer boot.MockEfiWriteVariable(func(name string, guid efi.GUID, attrs efi.VariableAttributes, data []byte) error {
		written += 1
		return fmt.Errorf(`INJECT ERROR`)
	})()

	defer boot.MockLinuxFilePathToDevicePath(func(path string, mode linux.FilePathToDevicePathMode) (out efi.DevicePath, err error) {
		return stringToDevicePath(path), nil
	})()

	optionalData := []byte("This is the boot entry for ubuntu")

	err = boot.SetEfiBootVariables("myenty", "EFI/myentry/shimx64.efi", optionalData)
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, `.*INJECT ERROR`)
	c.Check(written, Equals, 1)
}
