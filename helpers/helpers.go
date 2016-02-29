// -*- Mode: Go; indent-tabs-mode: t -*-

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

package helpers

import (
	"bytes"
	"math/rand"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/ubuntu-core/snappy/logger"
)

func init() {
	// golang does not init Seed() itself
	rand.Seed(time.Now().UTC().UnixNano())
}

// MakeMapFromEnvList takes a string list of the form "key=value"
// and returns a map[string]string from that list
// This is useful for os.Environ() manipulation
func MakeMapFromEnvList(env []string) map[string]string {
	envMap := map[string]string{}
	for _, l := range env {
		split := strings.SplitN(l, "=", 2)
		if len(split) != 2 {
			return nil
		}
		envMap[split[0]] = split[1]
	}
	return envMap
}

const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYabcdefghijklmnopqrstuvwxy"

// MakeRandomString returns a random string of length length
var MakeRandomString = func(length int) string {

	out := ""
	for i := 0; i < length; i++ {
		out += string(letters[rand.Intn(len(letters))])
	}

	return out
}

// NewSideloadVersion returns a version number such that later calls
// should return versions that compare larger.
func NewSideloadVersion() string {
	n := time.Now().UTC().UnixNano()
	bs := make([]byte, 12)
	for i := 11; i >= 0; i-- {
		bs[i] = letters[n&31]
		n = n >> 5
	}

	return string(bs)
}

// AtomicWriteFlags are a bitfield of flags for AtomicWriteFile
type AtomicWriteFlags uint

const (
	// AtomicWriteFollow makes AtomicWriteFile follow symlinks
	AtomicWriteFollow AtomicWriteFlags = 1 << iota
)

// AtomicWriteFile updates the filename atomically and works otherwise
// like io/ioutil.WriteFile()
//
// Note that it won't follow symlinks and will replace existing symlinks
// with the real file
func AtomicWriteFile(filename string, data []byte, perm os.FileMode, flags AtomicWriteFlags) (err error) {
	if flags&AtomicWriteFollow != 0 {
		if fn, err := os.Readlink(filename); err == nil || (fn != "" && os.IsNotExist(err)) {
			if filepath.IsAbs(fn) {
				filename = fn
			} else {
				filename = filepath.Join(filepath.Dir(filename), fn)
			}
		}
	}
	tmp := filename + "." + MakeRandomString(12)

	// XXX: if go switches to use aio_fsync, we need to open the dir for writing
	dir, err := os.Open(filepath.Dir(filename))
	if err != nil {
		return err
	}
	defer dir.Close()

	fd, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	defer func() {
		e := fd.Close()
		if err == nil {
			err = e
		}
		if err != nil {
			os.Remove(tmp)
		}
	}()

	// according to the docs, Write returns a non-nil error when n !=
	// len(b), so don't worry about short writes.
	if _, err := fd.Write(data); err != nil {
		return err
	}

	if err := fd.Sync(); err != nil {
		return err
	}

	if err := os.Rename(tmp, filename); err != nil {
		return err
	}

	return dir.Sync()
}

// CurrentHomeDir returns the homedir of the current user. It looks at
// $HOME first and then at passwd
func CurrentHomeDir() (string, error) {
	home := os.Getenv("HOME")
	if home != "" {
		return home, nil
	}

	user, err := user.Current()
	if err != nil {
		return "", err
	}

	return user.HomeDir, nil
}

// Getattr get the attribute of the given name
func Getattr(i interface{}, name string) interface{} {
	v := reflect.ValueOf(i)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	return v.FieldByName(name).Interface()
}

func fillSnapEnvVars(desc interface{}, vars []string) []string {
	for i, v := range vars {
		var templateOut bytes.Buffer
		t := template.Must(template.New("wrapper").Parse(v))
		if err := t.Execute(&templateOut, desc); err != nil {
			// this can never happen, except we forget a variable
			logger.Panicf("Unable to execute template: %v", err)
		}
		vars[i] = templateOut.String()
	}
	return vars
}

// GetBasicSnapEnvVars returns the app-level environment variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func GetBasicSnapEnvVars(desc interface{}) []string {
	return fillSnapEnvVars(desc, []string{
		"SNAP={{.AppPath}}",
		"SNAP_DATA=/var/lib{{.AppPath}}",
		"TMPDIR=/tmp/snaps/{{.UdevAppName}}/{{.Version}}/tmp",
		"TEMPDIR=/tmp/snaps/{{.UdevAppName}}/{{.Version}}/tmp",
		"SNAP_NAME={{.AppName}}",
		"SNAP_VERSION={{.Version}}",
		"SNAP_ORIGIN={{.Origin}}",
		"SNAP_FULLNAME={{.UdevAppName}}",
		"SNAP_ARCH={{.AppArch}}",
	})
}

// GetUserSnapEnvVars returns the user-level environment variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func GetUserSnapEnvVars(desc interface{}) []string {
	return fillSnapEnvVars(desc, []string{
		"SNAP_USER_DATA={{.Home}}{{.AppPath}}",
	})
}

// GetDeprecatedBasicSnapEnvVars returns the app-level deprecated environment
// variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func GetDeprecatedBasicSnapEnvVars(desc interface{}) []string {
	return fillSnapEnvVars(desc, []string{
		// SNAP_
		"SNAP_APP_PATH={{.AppPath}}",
		"SNAP_APP_DATA_PATH=/var/lib{{.AppPath}}",
		"SNAP_APP_TMPDIR=/tmp/snaps/{{.UdevAppName}}/{{.Version}}/tmp",
	})
}

// GetDeprecatedUserSnapEnvVars returns the user-level deprecated environment
// variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func GetDeprecatedUserSnapEnvVars(desc interface{}) []string {
	return fillSnapEnvVars(desc, []string{
		"SNAP_APP_USER_DATA_PATH={{.Home}}{{.AppPath}}",
	})
}
