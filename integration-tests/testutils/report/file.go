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

package report

import (
	"os"
	"path/filepath"
)

const reporterFilePath = "results.subunit"

// FileReporter is a type implementing io.Writer that
// writes the data passed to its Writer method
// in a file
type FileReporter struct{}

func (fr *FileReporter) Write(data []byte) (n int, err error) {
	file, err := fr.getFileHandler(reporterFilePath)
	defer file.Close()

	n, err = file.Write(data)

	return
}

func (fr *FileReporter) getFileHandler(path string) (file *os.File, err error) {
	absolutePath := getFilePath(path)
	if _, err := os.Stat(absolutePath); err != nil {
		file, err = os.Create(absolutePath)
	}
	if file == nil {
		file, err = os.OpenFile(absolutePath, os.O_APPEND|os.O_WRONLY, 0600)
	}
	return
}

func getFilePath(path string) string {
	return filepath.Join(os.Getenv("ADT_ARTIFACTS"), path)
}
