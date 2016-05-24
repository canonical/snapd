// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015 Canonical Ltd
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

package image

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/integration-tests/testutils/testutils"
)

// Image type encapsulates image actions
type Image struct {
	Release  string
	Channel  string
	Revision string
	BaseDir  string
	Kernel   string
	OS       string
	Gadget   string
}

// UdfCreate forms and executes the UDF command for creating the image
func (img Image) UdfCreate() (string, error) {
	fmt.Println("Creating image...")

	imageDir := filepath.Join(img.BaseDir, "image")

	testutils.PrepareTargetDir(imageDir)

	udfCommand := []string{"sudo", "ubuntu-device-flash", "--verbose"}

	if img.Revision != "" {
		panic("img.revision not supported")
	}

	imagePath := img.imagePath(imageDir)

	coreOptions := []string{
		"core", img.Release,
		"--output", imagePath,
		"--channel", img.Channel,
		"--gadget", img.Gadget,
		"--os", img.OS,
		"--kernel", img.Kernel,
		"--developer-mode",
	}

	err := testutils.ExecCommand(append(udfCommand, coreOptions...)...)

	return imagePath, err
}

func (img Image) imagePath(imageDir string) string {
	revisionTag := img.Revision
	if revisionTag == "" {
		revisionTag = "latest"
	}

	imageName := strings.Join(
		[]string{"snappy", img.Release, img.Channel, revisionTag}, "-") + ".img"

	return filepath.Join(imageDir, imageName)
}

// SetRevision is the setter method for revision
func (img Image) SetRevision(rev string) {
	img.Revision = rev
}
