// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package ubootenv

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
)

// textEnv contains the key/value data of a "text" uboot format
type textEnv struct {
	fname string
	data  map[string]string
}

// createText creates a new empty uboot env file
func createText(fname string) (*textEnv, error) {
	f, err := os.Create(fname)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	env := &textEnv{
		fname: fname,
		data:  make(map[string]string),
	}

	return env, nil
}

// openText opens a existing text uboot env file, passing additional flags.
func openText(fname string, flags OpenFlags) (*textEnv, error) {
	f, err := os.Open(fname)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	payload, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}

	data, err := parseData(payload, byte('\n'), flags)
	if err != nil {
		return nil, err
	}

	return &textEnv{
		data:  data,
		fname: fname,
	}, nil
}

// Get the value of the environment variable
func (env *textEnv) Get(name string) string {
	return env.data[name]
}

// String returns the environment as a string.
func (env *textEnv) String() string {
	buf := &bytes.Buffer{}
	writeData(buf, env.data, byte('\n'))
	return buf.String()
}

// Set an environment name to the given value, if the value is empty
// the variable will be removed from the environment
func (env *textEnv) Set(name, value string) {
	if name == "" {
		panic(fmt.Sprintf("Set() can not be called with empty key for value: %q", value))
	}
	if value == "" {
		delete(env.data, name)
		return
	}
	env.data[name] = value
}

func (env *textEnv) Size() int {
	// calculate the size of the needed file
	size := 0
	for k, v := range env.data {
		// +2 for the "=" and the "\n" for each key,value pair
		size += len(k) + len(v) + 2
	}

	return size
}

// Save will write out the environment data
func (env *textEnv) Save() error {
	w := bytes.NewBuffer(nil)

	// will panic if the buffer can't grow, all writes to
	// the buffer will be ok because we sized it correctly
	w.Grow(env.Size())

	// write the payload
	iterEnv(env.data, func(key, value string) {
		w.Write([]byte(fmt.Sprintf("%s=%s\n", key, value)))
	})

	// ensure dir sync
	dir, err := os.Open(filepath.Dir(env.fname))
	if err != nil {
		return err
	}
	defer dir.Close()

	// Note that we overwrite the existing file and do not do
	// the usual write-rename. The rationale is that we want to
	// minimize the amount of writes happening on a potential
	// FAT partition where the env is loaded from. The file will
	// always be of a fixed size so we know the writes will not
	// fail because of ENOSPC.
	//
	// The size of the env file never changes so we do not
	// truncate it.
	//
	// We also do not O_TRUNC to avoid reallocations on the FS
	// to minimize risk of fs corruption.
	f, err := os.OpenFile(env.fname, os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(w.Bytes()); err != nil {
		return err
	}

	if err := f.Sync(); err != nil {
		return err
	}

	return dir.Sync()
}
