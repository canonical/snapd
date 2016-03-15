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

package daemon

import (
	"time"

	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snappy"
)

// a for-testing Part
type tP struct {
	name          string
	version       string
	description   string
	developer     string
	hash          string
	isActive      bool
	isInstalled   bool
	needsReboot   bool
	date          time.Time
	channel       string
	icon          string
	_type         snap.Type
	installedSize int64
	downloadSize  int64

	installName   string
	installErr    error
	uninstallErr  error
	config        string
	configErr     error
	setActiveErr  error
	frameworks    []string
	frameworksErr error

	appYamls map[string]*snappy.AppYaml
}

func (p *tP) Name() string         { return p.name }
func (p *tP) Version() string      { return p.version }
func (p *tP) Description() string  { return p.description }
func (p *tP) Developer() string    { return p.developer }
func (p *tP) Hash() string         { return p.hash }
func (p *tP) IsActive() bool       { return p.isActive }
func (p *tP) IsInstalled() bool    { return p.isInstalled }
func (p *tP) NeedsReboot() bool    { return p.needsReboot }
func (p *tP) Date() time.Time      { return p.date }
func (p *tP) Channel() string      { return p.channel }
func (p *tP) Icon() string         { return p.icon }
func (p *tP) Type() snap.Type      { return p._type }
func (p *tP) InstalledSize() int64 { return p.installedSize }
func (p *tP) DownloadSize() int64  { return p.downloadSize }

func (p *tP) Install(progress.Meter, snappy.InstallFlags) (string, error) {
	return p.installName, p.installErr
}
func (p *tP) Config(cfg []byte) (string, error) {
	if len(cfg) > 0 {
		p.config = string(cfg)
	}
	return p.config, p.configErr
}
func (p *tP) SetActive(bool, progress.Meter) error { return p.setActiveErr }
func (p *tP) Frameworks() ([]string, error)        { return p.frameworks, p.frameworksErr }

// for ServiceYamler interface:
func (p *tP) Apps() map[string]*snappy.AppYaml { return p.appYamls }
