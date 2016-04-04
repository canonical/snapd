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

package snappy

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/remote"
)

// Snap represents a generic snap type
type Snap struct {
	m        *snapYaml
	remoteM  *remote.Snap
	hash     string
	isActive bool

	basedir string
}

// NewInstalledSnap returns a new Snap from the given yamlPath
func NewInstalledSnap(yamlPath string) (*Snap, error) {
	m, err := parseSnapYamlFile(yamlPath)
	if err != nil {
		return nil, err
	}

	snap, err := newSnapFromYaml(yamlPath, m)
	if err != nil {
		return nil, err
	}

	return snap, nil
}

// newSnapFromYaml returns a new Snap from the given *snapYaml at yamlPath
func newSnapFromYaml(yamlPath string, m *snapYaml) (*Snap, error) {
	snap := &Snap{
		basedir: filepath.Dir(filepath.Dir(yamlPath)),
		m:       m,
	}

	// override the package's idea of its version
	// because that could have been rewritten on sideload
	// and developer is empty sideloaded ones.
	m.Version = filepath.Base(snap.basedir)

	// check if the snap is active
	allVersionsDir := filepath.Dir(snap.basedir)
	p, err := filepath.EvalSymlinks(filepath.Join(allVersionsDir, "current"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	if p == snap.basedir {
		snap.isActive = true
	}

	remoteManifestPath := RemoteManifestPath(snap.Info())
	if osutil.FileExists(remoteManifestPath) {
		content, err := ioutil.ReadFile(remoteManifestPath)
		if err != nil {
			return nil, err
		}

		var r remote.Snap
		if err := yaml.Unmarshal(content, &r); err != nil {
			return nil, &ErrInvalidYaml{File: remoteManifestPath, Err: err, Yaml: content}
		}
		snap.remoteM = &r
	}

	return snap, nil
}

// Type returns the type of the Snap (app, gadget, ...)
func (s *Snap) Type() snap.Type {
	if s.m.Type != "" {
		return s.m.Type
	}

	// if not declared its a app
	return "app"
}

// Name returns the name
func (s *Snap) Name() string {
	return s.m.Name
}

// Version returns the version
func (s *Snap) Version() string {
	if s.basedir != "" {
		return filepath.Base(s.basedir)
	}

	return s.m.Version
}

// Revision returns the revision
func (s *Snap) Revision() int {
	if s.remoteM != nil {
		return s.remoteM.Revision
	}

	return 0
}

// Developer returns the developer
func (s *Snap) Developer() string {
	if r := s.remoteM; r != nil {
		return r.Developer
	}

	return SideloadedDeveloper
}

// Hash returns the hash
func (s *Snap) Hash() string {
	return s.hash
}

// Channel returns the channel used
func (s *Snap) Channel() string {
	if r := s.remoteM; r != nil {
		return r.Channel
	}

	// default for compat with older installs
	return "stable"
}

// Icon returns the path to the icon
func (s *Snap) Icon() string {
	found, _ := filepath.Glob(filepath.Join(s.basedir, "meta", "gui", "icon.*"))
	if len(found) == 0 {
		return ""
	}

	return found[0]
}

// IsActive returns true if the snap is active
func (s *Snap) IsActive() bool {
	return s.isActive
}

// IsInstalled returns true if the snap is installed
func (s *Snap) IsInstalled() bool {
	return true
}

// InstalledSize returns the size of the installed snap
func (s *Snap) InstalledSize() int64 {
	// FIXME: cache this at install time maybe?
	totalSize := int64(0)
	f := func(_ string, info os.FileInfo, err error) error {
		totalSize += info.Size()
		return err
	}
	filepath.Walk(s.basedir, f)
	return totalSize
}

func (s *Snap) description() string {
	// store edits win!
	if r := s.remoteM; r != nil {
		return r.Description
	}

	return s.m.Description
}

// Info returns the snap.Info data.
func (s *Snap) Info() *snap.Info {
	return &snap.Info{
		Name:        s.Name(),
		Developer:   s.Developer(),
		Version:     s.Version(),
		Revision:    s.Revision(),
		Type:        s.Type(),
		Channel:     s.Channel(),
		Summary:     s.m.Summary, // XXX: doesn't exist in the store yet anyway
		Description: s.description(),
	}
}

// DownloadSize returns the dowload size
func (s *Snap) DownloadSize() int64 {
	if r := s.remoteM; r != nil {
		return r.DownloadSize
	}

	return -1
}

// Date returns the last update date
func (s *Snap) Date() time.Time {
	st, err := os.Stat(s.basedir)
	if err != nil {
		return time.Time{}
	}

	return st.ModTime()
}

// Apps return a list of AppsYamls the package declares
func (s *Snap) Apps() map[string]*AppYaml {
	return s.m.Apps
}

// GadgetConfig return a list of packages to configure
func (s *Snap) GadgetConfig() SystemConfig {
	return s.m.Config
}

// Install installs the snap (which does not make sense for an already
// installed snap
func (s *Snap) Install(inter progress.Meter, flags InstallFlags) (name string, err error) {
	return "", ErrAlreadyInstalled
}

// NeedsReboot returns true if the snap becomes active on the next reboot
func (s *Snap) NeedsReboot() bool {
	return kernelOrOsRebootRequired(s)
}
