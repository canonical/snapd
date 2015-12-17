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

package tpl

import (
	"os"
	"text/template"
)

// Execute inserts the given data in the given template file, saving the results
// in the given output file
func Execute(tplFile, outputFile string, data interface{}) (err error) {
	t, err := template.ParseFiles(tplFile)
	if err != nil {
		return
	}

	fileHandler, err := os.Create(outputFile)
	if err != nil {
		return
	}
	defer fileHandler.Close()

	return t.Execute(fileHandler, data)
}
