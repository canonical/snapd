// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package main

import (
	"embed"
	"log"
	"os"
	"text/template"

	"github.com/snapcore/snapd/nvidia"
)

//go:embed *.tmpl
var content embed.FS

type Nv struct {
	RegularGlobs  []string
	LibGlvndGlobs []string
}

var nv = Nv{
	RegularGlobs:  nvidia.RegularGlobs,
	LibGlvndGlobs: nvidia.LibGlvndGlobs,
}

func main() {
	tmpl, err := template.ParseFS(content, "globs.h.tmpl")
	if err != nil {
		log.Fatal(err)
	}

	if err := tmpl.Execute(os.Stdout, nv); err != nil {
		log.Fatal(err)
	}
}
