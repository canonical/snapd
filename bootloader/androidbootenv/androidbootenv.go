// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package androidbootenv

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

type Env struct {
	// Map with key-value strings
	env map[string]string
	// File for environment storage
	path string
}

func NewEnv(path string) *Env {
	return &Env{
		env:  make(map[string]string),
		path: path,
	}
}

func (a *Env) Get(name string) string {
	return a.env[name]
}

func (a *Env) Set(key, value string) {
	a.env[key] = value
}

func (a *Env) Load() error {
	file := mylog.Check2(os.Open(a.path))

	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		l := strings.SplitN(scanner.Text(), "=", 2)
		// be liberal in what you accept
		if len(l) < 2 {
			logger.Noticef("WARNING: bad value while parsing %v (line: %q)",
				a.path, scanner.Text())
			continue
		}
		a.env[l[0]] = l[1]
	}
	mylog.Check(scanner.Err())

	return nil
}

func (a *Env) Save() error {
	var w bytes.Buffer

	for k, v := range a.env {
		mylog.Check2(fmt.Fprintf(&w, "%s=%s\n", k, v))
	}

	return osutil.AtomicWriteFile(a.path, w.Bytes(), 0644, 0)
}
