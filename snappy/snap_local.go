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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/pkg"
	"github.com/ubuntu-core/snappy/pkg/remote"
	"github.com/ubuntu-core/snappy/pkg/squashfs"
	"github.com/ubuntu-core/snappy/policy"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/systemd"
)

// SnapPart represents a generic snap type
type SnapPart struct {
	m           *packageYaml
	remoteM     *remote.Snap
	origin      string
	hash        string
	isActive    bool
	isInstalled bool
	description string
	deb         pkg.File
	basedir     string
}

// NewInstalledSnapPart returns a new SnapPart from the given yamlPath
func NewInstalledSnapPart(yamlPath, origin string) (*SnapPart, error) {
	m, err := parsePackageYamlFile(yamlPath)
	if err != nil {
		return nil, err
	}

	part, err := NewSnapPartFromYaml(yamlPath, origin, m)
	if err != nil {
		return nil, err
	}
	part.isInstalled = true

	return part, nil
}

// NewSnapPartFromSnapFile loads a snap from the given (clickdeb) snap file.
// Caller should call Close on the pkg.
// TODO: expose that Close.
func NewSnapPartFromSnapFile(snapFile string, origin string, unauthOk bool) (*SnapPart, error) {
	d, err := pkg.Open(snapFile)
	if err != nil {
		return nil, err
	}

	if err := d.Verify(unauthOk); err != nil {
		return nil, err
	}

	yamlData, err := d.MetaMember("package.yaml")
	if err != nil {
		return nil, err
	}

	_, err = d.MetaMember("hooks/config")
	hasConfig := err == nil

	m, err := parsePackageYamlData(yamlData, hasConfig)
	if err != nil {
		return nil, err
	}

	targetDir := dirs.SnapAppsDir
	// the "gadget" parts are special
	if m.Type == pkg.TypeGadget {
		targetDir = dirs.SnapGadgetDir
	}

	if origin == SideloadedOrigin {
		m.Version = helpers.NewSideloadVersion()
	}

	fullName := m.qualifiedName(origin)
	instDir := filepath.Join(targetDir, fullName, m.Version)

	return &SnapPart{
		basedir: instDir,
		origin:  origin,
		m:       m,
		deb:     d,
	}, nil
}

// NewSnapPartFromYaml returns a new SnapPart from the given *packageYaml at yamlPath
func NewSnapPartFromYaml(yamlPath, origin string, m *packageYaml) (*SnapPart, error) {
	if _, err := os.Stat(yamlPath); err != nil {
		return nil, err
	}

	part := &SnapPart{
		basedir: filepath.Dir(filepath.Dir(yamlPath)),
		origin:  origin,
		m:       m,
	}

	// override the package's idea of its version
	// because that could have been rewritten on sideload
	// and origin is empty for frameworks, even sideloaded ones.
	m.Version = filepath.Base(part.basedir)

	// check if the part is active
	allVersionsDir := filepath.Dir(part.basedir)
	p, err := filepath.EvalSymlinks(filepath.Join(allVersionsDir, "current"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	if p == part.basedir {
		part.isActive = true
	}

	// get the click *title* from readme.md and use that as the *description*.
	if description, _, err := parseReadme(filepath.Join(part.basedir, "meta", "readme.md")); err == nil {
		part.description = description
	}

	// read hash, its ok if its not there, some older versions of
	// snappy did not write this file
	hashesData, err := ioutil.ReadFile(filepath.Join(part.basedir, "meta", "hashes.yaml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	var h hashesYaml
	err = yaml.Unmarshal(hashesData, &h)
	if err != nil {
		return nil, &ErrInvalidYaml{File: "hashes.yaml", Err: err, Yaml: hashesData}
	}
	part.hash = h.ArchiveSha512

	remoteManifestPath := RemoteManifestPath(part)
	if helpers.FileExists(remoteManifestPath) {
		content, err := ioutil.ReadFile(remoteManifestPath)
		if err != nil {
			return nil, err
		}

		var r remote.Snap
		if err := yaml.Unmarshal(content, &r); err != nil {
			return nil, &ErrInvalidYaml{File: remoteManifestPath, Err: err, Yaml: content}
		}
		part.remoteM = &r
	}

	return part, nil
}

// Type returns the type of the SnapPart (app, gadget, ...)
func (s *SnapPart) Type() pkg.Type {
	if s.m.Type != "" {
		return s.m.Type
	}

	// FIXME: remove once we move to all-snaps
	if s.m.Type == pkg.TypeLegacyOem {
		return pkg.TypeGadget
	}

	// if not declared its a app
	return "app"
}

// Name returns the name
func (s *SnapPart) Name() string {
	return s.m.Name
}

// Version returns the version
func (s *SnapPart) Version() string {
	if s.basedir != "" {
		return filepath.Base(s.basedir)
	}

	return s.m.Version
}

// Description returns the description
func (s *SnapPart) Description() string {
	if r := s.remoteM; r != nil {
		return r.Description
	}

	return s.description
}

// Origin returns the origin
func (s *SnapPart) Origin() string {
	if r := s.remoteM; r != nil {
		return r.Origin
	}

	if s.origin == "" {
		return SideloadedOrigin
	}

	return s.origin
}

// Hash returns the hash
func (s *SnapPart) Hash() string {
	return s.hash
}

// Channel returns the channel used
func (s *SnapPart) Channel() string {
	if r := s.remoteM; r != nil {
		return r.Channel
	}

	// default for compat with older installs
	return "stable"
}

// Icon returns the path to the icon
func (s *SnapPart) Icon() string {
	if s.m.Icon == "" {
		return ""
	}

	return filepath.Join(s.basedir, s.m.Icon)
}

// IsActive returns true if the snap is active
func (s *SnapPart) IsActive() bool {
	return s.isActive
}

// IsInstalled returns true if the snap is installed
func (s *SnapPart) IsInstalled() bool {
	return s.isInstalled
}

// InstalledSize returns the size of the installed snap
func (s *SnapPart) InstalledSize() int64 {
	// FIXME: cache this at install time maybe?
	totalSize := int64(0)
	f := func(_ string, info os.FileInfo, err error) error {
		totalSize += info.Size()
		return err
	}
	filepath.Walk(s.basedir, f)
	return totalSize
}

// DownloadSize returns the dowload size
func (s *SnapPart) DownloadSize() int64 {
	if r := s.remoteM; r != nil {
		return r.DownloadSize
	}

	return -1
}

// Date returns the last update date
func (s *SnapPart) Date() time.Time {
	st, err := os.Stat(s.basedir)
	if err != nil {
		return time.Time{}
	}

	return st.ModTime()
}

// ServiceYamls return a list of ServiceYamls the package declares
func (s *SnapPart) ServiceYamls() []ServiceYaml {
	return s.m.ServiceYamls
}

// Binaries return a list of BinaryDescription the package declares
func (s *SnapPart) Binaries() []Binary {
	return s.m.Binaries
}

// GadgetConfig return a list of packages to configure
func (s *SnapPart) GadgetConfig() SystemConfig {
	return s.m.Config
}

// Install installs the snap
func (s *SnapPart) Install(inter progress.Meter, flags InstallFlags) (name string, err error) {
	allowGadget := (flags & AllowGadget) != 0
	inhibitHooks := (flags & InhibitHooks) != 0

	if s.IsInstalled() {
		return "", ErrAlreadyInstalled
	}

	if err := s.CanInstall(allowGadget, inter); err != nil {
		return "", err
	}

	// the "gadget" parts are special
	if s.Type() == pkg.TypeGadget {
		if err := installGadgetHardwareUdevRules(s.m); err != nil {
			return "", err
		}
	}

	fullName := QualifiedName(s)
	dataDir := filepath.Join(dirs.SnapDataDir, fullName, s.Version())

	var oldPart *SnapPart
	if currentActiveDir, _ := filepath.EvalSymlinks(filepath.Join(s.basedir, "..", "current")); currentActiveDir != "" {
		oldPart, err = NewInstalledSnapPart(filepath.Join(currentActiveDir, "meta", "package.yaml"), s.origin)
		if err != nil {
			return "", err
		}
	}

	if err := os.MkdirAll(s.basedir, 0755); err != nil {
		logger.Noticef("Can not create %q: %v", s.basedir, err)
		return "", err
	}

	// if anything goes wrong here we cleanup
	defer func() {
		if err != nil {
			if e := os.RemoveAll(s.basedir); e != nil && !os.IsNotExist(e) {
				logger.Noticef("Failed to remove %q: %v", s.basedir, e)
			}
		}
	}()

	// we need to call the external helper so that we can reliable drop
	// privs
	if err := s.deb.UnpackWithDropPrivs(s.basedir, dirs.GlobalRootDir); err != nil {
		return "", err
	}

	// generate the mount unit for the squashfs
	if s.deb.NeedsMountUnit() {
		if err := s.m.addSquashfsMount(s.basedir, inhibitHooks, inter); err != nil {
			return "", err
		}
	}

	// write the hashes now
	if err := s.deb.ExtractHashes(filepath.Join(s.basedir, "meta")); err != nil {
		return "", err
	}

	// FIXME: special handling is bad 'mkay
	if s.m.Type == pkg.TypeKernel {
		if err := extractKernelAssets(s, inter, flags); err != nil {
			return "", fmt.Errorf("failed to install kernel %s", err)
		}
	}

	// deal with the data:
	//
	// if there was a previous version, stop it
	// from being active so that it stops running and can no longer be
	// started then copy the data
	//
	// otherwise just create a empty data dir
	if oldPart != nil {
		// we need to stop making it active
		err = oldPart.deactivate(inhibitHooks, inter)
		defer func() {
			if err != nil {
				if cerr := oldPart.activate(inhibitHooks, inter); cerr != nil {
					logger.Noticef("Setting old version back to active failed: %v", cerr)
				}
			}
		}()
		if err != nil {
			return "", err
		}

		err = copySnapData(fullName, oldPart.Version(), s.Version())
	} else {
		err = os.MkdirAll(dataDir, 0755)
	}

	defer func() {
		if err != nil {
			if cerr := removeSnapData(fullName, s.Version()); cerr != nil {
				logger.Noticef("When cleaning up data for %s %s: %v", s.Name(), s.Version(), cerr)
			}
		}
	}()

	if err != nil {
		return "", err
	}

	if !inhibitHooks {
		// and finally make active
		err = s.activate(inhibitHooks, inter)
		defer func() {
			if err != nil && oldPart != nil {
				if cerr := oldPart.activate(inhibitHooks, inter); cerr != nil {
					logger.Noticef("When setting old %s version back to active: %v", s.Name(), cerr)
				}
			}
		}()
		if err != nil {
			return "", err
		}

		// oh, one more thing: refresh the security bits
		deps, err := s.Dependents()
		if err != nil {
			return "", err
		}

		sysd := systemd.New(dirs.GlobalRootDir, inter)
		stopped := make(map[string]time.Duration)
		defer func() {
			if err != nil {
				for serviceName := range stopped {
					if e := sysd.Start(serviceName); e != nil {
						inter.Notify(fmt.Sprintf("unable to restart %s with the old %s: %s", serviceName, s.Name(), e))
					}
				}
			}
		}()

		for _, dep := range deps {
			if !dep.IsActive() {
				continue
			}
			for _, svc := range dep.ServiceYamls() {
				serviceName := filepath.Base(generateServiceFileName(dep.m, svc))
				timeout := time.Duration(svc.StopTimeout)
				if err = sysd.Stop(serviceName, timeout); err != nil {
					inter.Notify(fmt.Sprintf("unable to stop %s; aborting install: %s", serviceName, err))
					return "", err
				}
				stopped[serviceName] = timeout
			}
		}

		if err := s.RefreshDependentsSecurity(oldPart, inter); err != nil {
			return "", err
		}

		started := make(map[string]time.Duration)
		defer func() {
			if err != nil {
				for serviceName, timeout := range started {
					if e := sysd.Stop(serviceName, timeout); e != nil {
						inter.Notify(fmt.Sprintf("unable to stop %s with the old %s: %s", serviceName, s.Name(), e))
					}
				}
			}
		}()
		for serviceName, timeout := range stopped {
			if err = sysd.Start(serviceName); err != nil {
				inter.Notify(fmt.Sprintf("unable to restart %s; aborting install: %s", serviceName, err))
				return "", err
			}
			started[serviceName] = timeout
		}
	}

	return s.Name(), nil
}

// SetActive sets the snap active
func (s *SnapPart) SetActive(active bool, pb progress.Meter) (err error) {
	if active {
		return s.activate(false, pb)
	}

	return s.deactivate(false, pb)
}

func (s *SnapPart) activate(inhibitHooks bool, inter interacter) error {
	currentActiveSymlink := filepath.Join(s.basedir, "..", "current")
	currentActiveDir, _ := filepath.EvalSymlinks(currentActiveSymlink)

	// already active, nothing to do
	if s.basedir == currentActiveDir {
		return nil
	}

	// there is already an active part
	if currentActiveDir != "" {
		// TODO: support switching origins
		oldYaml := filepath.Join(currentActiveDir, "meta", "package.yaml")
		oldPart, err := NewInstalledSnapPart(oldYaml, s.origin)
		if err != nil {
			return err
		}
		if err := oldPart.deactivate(inhibitHooks, inter); err != nil {
			return err
		}
	}

	if s.Type() == pkg.TypeFramework {
		if err := policy.Install(s.Name(), s.basedir, dirs.GlobalRootDir); err != nil {
			return err
		}
	}

	// generate the security policy from the package.yaml
	// Note that this must happen before binaries/services are
	// generated because serices may get started
	appsDir := filepath.Join(dirs.SnapAppsDir, QualifiedName(s), s.Version())
	if err := generatePolicy(s.m, appsDir); err != nil {
		return err
	}

	// add the "binaries:" from the package.yaml
	if err := s.m.addPackageBinaries(s.basedir); err != nil {
		return err
	}
	// add the "services:" from the package.yaml
	if err := s.m.addPackageServices(s.basedir, inhibitHooks, inter); err != nil {
		return err
	}

	if err := os.Remove(currentActiveSymlink); err != nil && !os.IsNotExist(err) {
		logger.Noticef("Failed to remove %q: %v", currentActiveSymlink, err)
	}

	dbase := filepath.Join(dirs.SnapDataDir, QualifiedName(s))
	currentDataSymlink := filepath.Join(dbase, "current")
	if err := os.Remove(currentDataSymlink); err != nil && !os.IsNotExist(err) {
		logger.Noticef("Failed to remove %q: %v", currentDataSymlink, err)
	}

	// symlink is relative to parent dir
	if err := os.Symlink(filepath.Base(s.basedir), currentActiveSymlink); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Join(dbase, s.Version()), 0755); err != nil {
		return err
	}

	// FIXME: create {Os,Kernel}Snap type instead of adding special
	//        cases here
	if err := setNextBoot(s); err != nil {
		return err
	}

	return os.Symlink(filepath.Base(s.basedir), currentDataSymlink)
}

func (s *SnapPart) deactivate(inhibitHooks bool, inter interacter) error {
	currentSymlink := filepath.Join(s.basedir, "..", "current")

	// sanity check
	currentActiveDir, err := filepath.EvalSymlinks(currentSymlink)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrSnapNotActive
		}
		return err
	}
	if s.basedir != currentActiveDir {
		return ErrSnapNotActive
	}

	// remove generated services, binaries, clickHooks, security policy
	if err := s.m.removePackageBinaries(s.basedir); err != nil {
		return err
	}

	if err := s.m.removePackageServices(s.basedir, inter); err != nil {
		return err
	}

	if err := removePolicy(s.m, s.basedir); err != nil {
		return err
	}

	if s.Type() == pkg.TypeFramework {
		if err := policy.Remove(s.Name(), s.basedir, dirs.GlobalRootDir); err != nil {
			return err
		}
	}

	// and finally the current symlink
	if err := os.Remove(currentSymlink); err != nil {
		logger.Noticef("Failed to remove %q: %v", currentSymlink, err)
	}

	currentDataSymlink := filepath.Join(dirs.SnapDataDir, QualifiedName(s), "current")
	if err := os.Remove(currentDataSymlink); err != nil && !os.IsNotExist(err) {
		logger.Noticef("Failed to remove %q: %v", currentDataSymlink, err)
	}

	return nil
}

// Uninstall remove the snap from the system
func (s *SnapPart) Uninstall(pb progress.Meter) (err error) {
	// Gadget snaps should not be removed as they are a key
	// building block for Gadgets. Prunning non active ones
	// is acceptible.
	if s.m.Type == pkg.TypeGadget && s.IsActive() {
		return ErrPackageNotRemovable
	}

	// You never want to remove an active kernel or OS
	if (s.m.Type == pkg.TypeKernel || s.m.Type == pkg.TypeOS) && s.IsActive() {
		return ErrPackageNotRemovable
	}

	if IsBuiltInSoftware(s.Name()) && s.IsActive() {
		return ErrPackageNotRemovable
	}

	deps, err := s.DependentNames()
	if err != nil {
		return err
	}
	if len(deps) != 0 {
		return ErrFrameworkInUse(deps)
	}

	if err := s.remove(pb); err != nil {
		return err
	}

	return RemoveAllHWAccess(QualifiedName(s))
}

func (s *SnapPart) remove(inter interacter) (err error) {
	if err := s.deactivate(false, inter); err != nil && err != ErrSnapNotActive {
		return err
	}

	// ensure mount unit stops
	if err := s.m.removeSquashfsMount(s.basedir, inter); err != nil {
		return err
	}

	err = os.RemoveAll(s.basedir)
	if err != nil {
		return err
	}

	// best effort(?)
	os.Remove(filepath.Dir(s.basedir))

	// remove the snap
	if err := os.RemoveAll(squashfs.BlobPath(s.basedir)); err != nil {
		return err
	}

	// remove the kernel assets (if any)
	if s.m.Type == pkg.TypeKernel {
		if err := removeKernelAssets(s, inter); err != nil {
			logger.Noticef("removing kernel assets failed with %s", err)
		}
	}

	return nil
}

// Config is used to to configure the snap
func (s *SnapPart) Config(configuration []byte) (new string, err error) {
	if s.m.Type == pkg.TypeOS {
		return coreConfig(configuration)
	}

	return snapConfig(s.basedir, s.origin, string(configuration))
}

// NeedsReboot returns true if the snap becomes active on the next reboot
func (s *SnapPart) NeedsReboot() bool {
	return kernelOrOsRebootRequired(s)
}

// Frameworks returns the list of frameworks needed by the snap
func (s *SnapPart) Frameworks() ([]string, error) {
	return s.m.Frameworks, nil
}

// DependentNames returns a list of the names of apps installed that
// depend on this one
//
// /!\ not part of the Part interface.
func (s *SnapPart) DependentNames() ([]string, error) {
	deps, err := s.Dependents()
	if err != nil {
		return nil, err
	}

	names := make([]string, len(deps))
	for i, dep := range deps {
		names[i] = dep.Name()
	}

	return names, nil
}

// Dependents gives the list of apps installed that depend on this one
//
// /!\ not part of the Part interface.
func (s *SnapPart) Dependents() ([]*SnapPart, error) {
	if s.Type() != pkg.TypeFramework {
		// only frameworks are depended on
		return nil, nil
	}

	var needed []*SnapPart

	installed, err := NewMetaRepository().Installed()
	if err != nil {
		return nil, err
	}

	name := s.Name()
	for _, part := range installed {
		fmks, err := part.Frameworks()
		if err != nil {
			return nil, err
		}
		for _, fmk := range fmks {
			if fmk == name {
				part, ok := part.(*SnapPart)
				if !ok {
					return nil, ErrInstalledNonSnapPart
				}
				needed = append(needed, part)
			}
		}
	}

	return needed, nil
}

// CanInstall checks whether the SnapPart passes a series of tests required for installation
func (s *SnapPart) CanInstall(allowGadget bool, inter interacter) error {
	if s.IsInstalled() {
		return ErrAlreadyInstalled
	}

	if err := s.m.checkForPackageInstalled(s.Origin()); err != nil {
		return err
	}

	// verify we have a valid architecture
	if !arch.IsSupportedArchitecture(s.m.Architectures) {
		return &ErrArchitectureNotSupported{s.m.Architectures}
	}

	if err := s.m.checkForNameClashes(); err != nil {
		return err
	}

	if err := s.m.checkForFrameworks(); err != nil {
		return err
	}

	if s.Type() == pkg.TypeGadget {
		if !allowGadget {
			if currentGadget, err := getGadget(); err == nil {
				if currentGadget.Name != s.Name() {
					return ErrGadgetPackageInstall
				}
			} else {
				// there should always be a gadget package now
				return ErrGadgetPackageInstall
			}
		}
	}

	curr, _ := filepath.EvalSymlinks(filepath.Join(s.basedir, "..", "current"))
	if err := s.m.checkLicenseAgreement(inter, s.deb, curr); err != nil {
		return err
	}

	return nil
}

// RequestSecurityPolicyUpdate checks whether changes to the given policies and
// templates impacts the snap, and updates the policy if needed
func (s *SnapPart) RequestSecurityPolicyUpdate(policies, templates map[string]bool) error {
	var foundError error
	for _, svc := range s.ServiceYamls() {
		if svc.NeedsAppArmorUpdate(policies, templates) {
			err := svc.generatePolicyForServiceBinary(s.m, svc.Name, s.basedir)
			if err != nil {
				logger.Noticef("Failed to regenerate policy for %s: %v", svc.Name, err)
				foundError = err
			}
		}
	}
	for _, bin := range s.Binaries() {
		if bin.NeedsAppArmorUpdate(policies, templates) {
			err := bin.generatePolicyForServiceBinary(s.m, bin.Name, s.basedir)
			if err != nil {
				logger.Noticef("Failed to regenerate policy for %s: %v", bin.Name, err)
				foundError = err
			}
		}
	}

	// FIXME: if there are multiple errors only the last one
	//        will be preserved
	if foundError != nil {
		return foundError
	}

	return nil
}

// RefreshDependentsSecurity refreshes the security policies of dependent snaps
func (s *SnapPart) RefreshDependentsSecurity(oldPart *SnapPart, inter interacter) (err error) {
	oldBaseDir := ""
	if oldPart != nil {
		oldBaseDir = oldPart.basedir
	}
	upPol, upTpl := policy.AppArmorDelta(oldBaseDir, s.basedir, s.Name()+"_")

	deps, err := s.Dependents()
	if err != nil {
		return err
	}

	for _, dep := range deps {
		err := dep.RequestSecurityPolicyUpdate(upPol, upTpl)
		if err != nil {
			return err
		}
	}

	return nil
}
