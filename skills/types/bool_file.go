// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package types

import (
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/ubuntu-core/snappy/skills"
)

// BoolFileType is the type of all the bool-file skills.
type BoolFileType struct{}

// String returns the same value as Name().
func (t *BoolFileType) String() string {
	return t.Name()
}

// Name returns the name of the bool-file type.
func (t *BoolFileType) Name() string {
	return "bool-file"
}

var boolFileGPIOValuePattern = regexp.MustCompile(
	"^/sys/class/gpio/gpio[0-9]+/value$")
var boolFileAllowedPathPatterns = []*regexp.Regexp{
	// The brightness of standard LED class device
	regexp.MustCompile("^/sys/class/leds/[^/]+/brightness$"),
	// The value of standard exported GPIO
	boolFileGPIOValuePattern,
}

// SanitizeSkill checks and possibly modifies a skill.
// Valid "bool-file" skills must contain the attribute "path".
func (t *BoolFileType) SanitizeSkill(skill *skills.Skill) error {
	if t.Name() != skill.Type {
		panic(fmt.Sprintf("skill is not of type %q", t))
	}
	path, ok := skill.Attrs["path"].(string)
	if !ok || path == "" {
		return fmt.Errorf("bool-file must contain the path attribute")
	}
	path = filepath.Clean(path)
	for _, pattern := range boolFileAllowedPathPatterns {
		if pattern.MatchString(path) {
			return nil
		}
	}
	return fmt.Errorf("bool-file can only point at LED brightness or GPIO value")
}

// SanitizeSlot checks and possibly modifies a skill slot.
func (t *BoolFileType) SanitizeSlot(skill *skills.Slot) error {
	if t.Name() != skill.Type {
		panic(fmt.Sprintf("skill slot is not of type %q", t))
	}
	// NOTE: currently we don't check anything on the slot side.
	return nil
}

// SkillSecuritySnippet returns the configuration snippet required to provide a bool-file skill.
// Producers gain control over exporting, importing GPIOs as well as
// controlling the direction of particular pins.
func (t *BoolFileType) SkillSecuritySnippet(skill *skills.Skill, securitySystem skills.SecuritySystem) ([]byte, error) {
	gpioSnippet := []byte(`
/sys/class/gpio/export rw,
/sys/class/gpio/unexport rw,
/sys/class/gpio/gpio[0-9]+/direction rw,
`)
	switch securitySystem {
	case skills.SecurityAppArmor:
		// To provide GPIOs we need extra permissions to export/unexport and to
		// set the direction of each pin.
		if t.isGPIO(skill) {
			return gpioSnippet, nil
		}
		return nil, nil
	case skills.SecuritySecComp, skills.SecurityDBus:
		return nil, nil
	default:
		return nil, skills.ErrUnknownSecurity
	}
}

// SlotSecuritySnippet returns the configuration snippet required to use a bool-file skill.
// Consumers gain permission to read, write and lock the designated file.
func (t *BoolFileType) SlotSecuritySnippet(skill *skills.Skill, securitySystem skills.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case skills.SecurityAppArmor:
		// Allow write and lock on the file designated by the path.
		// Dereference symbolic links to file path handed out to apparmor since
		// sysfs is full of symlinks and apparmor requires uses real path for
		// filtering.
		path, err := t.dereferencedPath(skill)
		if err != nil {
			return nil, fmt.Errorf("cannot compute skill slot security snippet: %v", err)
		}
		return []byte(fmt.Sprintf("%s rwk,\n", path)), nil
	case skills.SecuritySecComp, skills.SecurityDBus:
		return nil, nil
	default:
		return nil, skills.ErrUnknownSecurity
	}
}

func (t *BoolFileType) dereferencedPath(skill *skills.Skill) (string, error) {
	if path, ok := skill.Attrs["path"].(string); ok {
		path, err := evalSymlinks(path)
		if err != nil {
			return "", err
		}
		return filepath.Clean(path), nil
	}
	panic("skill is not sanitized")
}

// isGPIO checks if a given bool-file skill refers to a GPIO pin.
func (t *BoolFileType) isGPIO(skill *skills.Skill) bool {
	if path, ok := skill.Attrs["path"].(string); ok {
		path = filepath.Clean(path)
		return boolFileGPIOValuePattern.MatchString(path)
	}
	panic("skill is not sanitized")
}
