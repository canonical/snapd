// -*- Mode: Go; indent-tabs-mode: t -*-

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

	"launchpad.net/snappy/_integration-tests/testutils"
)

// Image type encapsulates image actions
type Image struct {
	release  string
	channel  string
	revision string
	baseDir  string
}

// NewImage is the Image constructor
func NewImage(release, channel, revision, baseDir string) *Image {
	return &Image{release: release, channel: channel, revision: revision, baseDir: baseDir}
}

// UdfCreate forms and executes the UDF command for creating the image
func (img Image) UdfCreate() (string, error) {
	fmt.Println("Creating image...")

	imageDir := filepath.Join(img.baseDir, "image")

	testutils.PrepareTargetDir(imageDir)

	udfCommand := []string{"sudo", "ubuntu-device-flash", "--verbose"}

	if img.revision != "" {
		udfCommand = append(udfCommand, "--revision="+img.revision)
	}

	imagePath := img.imagePath(imageDir)

	coreOptions := []string{
		"core", img.release,
		"--output", imagePath,
		"--channel", img.channel,
		"--developer-mode",
	}

	err := testutils.ExecCommand(append(udfCommand, coreOptions...)...)

	return imagePath, err
}

func (img Image) imagePath(imageDir string) string {
	revisionTag := img.revision
	if revisionTag == "" {
		revisionTag = "latest"
	}

	imageName := strings.Join(
		[]string{"snappy", img.release, img.channel, revisionTag}, "-") + ".img"

	return filepath.Join(imageDir, imageName)
}

// SetRevision is the setter method for revision
func (img Image) SetRevision(rev string) {
	img.revision = rev
}
