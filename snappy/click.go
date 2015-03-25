package snappy

/* This part of the code implements enough of the click file format
   to install a "snap" package
   Limitations:
   - no per-user registration
   - no user-level hooks
   - more(?)
*/

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"text/template"

	"launchpad.net/snappy/clickdeb"
	"launchpad.net/snappy/helpers"

	"github.com/mvo5/goconfigparser"
)

type clickAppHook map[string]string

type clickManifest struct {
	Name          string                  `json:"name"`
	Version       string                  `json:"version"`
	Type          SnapType                `json:"type,omitempty"`
	Framework     string                  `json:"framework,omitempty"`
	Description   string                  `json:"description,omitempty"`
	Icon          string                  `json:"icon,omitempty"`
	InstalledSize string                  `json:"installed-size,omitempty"`
	Maintainer    string                  `json:"maintainer,omitempty"`
	Title         string                  `json:"title,omitempty"`
	Hooks         map[string]clickAppHook `json:"hooks,omitempty"`
}

type clickHook struct {
	name    string
	exec    string
	user    string
	pattern string
}

const (
	// from debsig-verify-0.9/debsigs.h
	dsSuccess           = 0
	dsFailNosigs        = 10
	dsFailUnknownOrigin = 11
	dsFailNopolicies    = 12
	dsFailBadsig        = 13
	dsFailInternal      = 14
)

// ignore hooks of this type
var ignoreHooks = map[string]bool{
	"bin-path":       true,
	"snappy-systemd": true,
}

// Execute the hook.Exec command
func execHook(execCmd string) (err error) {
	// the spec says this is passed to the shell
	cmd := exec.Command("sh", "-c", execCmd)
	if err = cmd.Run(); err != nil {
		if exitCode, err := helpers.ExitCode(err); err != nil {
			return &ErrHookFailed{cmd: execCmd,
				exitCode: exitCode}
		}
		return err
	}

	return nil
}

// This function checks if the given exitCode is "ok" when running with
// --allow-unauthenticated. We allow package with no signature or with
// a unknown policy or with no policies at all. We do not allow overriding
// bad signatures
func allowUnauthenticatedOkExitCode(exitCode int) bool {
	return (exitCode == dsFailNosigs ||
		exitCode == dsFailUnknownOrigin ||
		exitCode == dsFailNopolicies)
}

// Tiny wrapper around the debsig-verify commandline
func runDebsigVerifyImpl(clickFile string, allowUnauthenticated bool) (err error) {
	cmd := exec.Command("debsig-verify", clickFile)
	if err := cmd.Run(); err != nil {
		exitCode, err := helpers.ExitCode(err)
		if err == nil {
			if allowUnauthenticated && allowUnauthenticatedOkExitCode(exitCode) {
				log.Println("Signature check failed, but installing anyway as requested")
				return nil
			}
			return &ErrSignature{exitCode: exitCode}
		}
		// not a exit code error, something else, pass on
		return err
	}
	return nil
}

var runDebsigVerify = runDebsigVerifyImpl

func auditClick(snapFile string, allowUnauthenticated bool) (err error) {
	// FIXME: check what more we need to do here, click is also doing
	//        permission checks
	return runDebsigVerify(snapFile, allowUnauthenticated)
}

func readClickManifest(data []byte) (manifest clickManifest, err error) {
	r := bytes.NewReader(data)
	dec := json.NewDecoder(r)
	err = dec.Decode(&manifest)
	return manifest, err
}

func readClickHookFile(hookFile string) (hook clickHook, err error) {
	// FIXME: fugly, write deb822 style parser if we keep this
	// FIXME2: the hook file will go probably entirely and gets
	//         implemented natively in go so ok for now :)
	cfg := goconfigparser.New()
	content, err := ioutil.ReadFile(hookFile)
	if err != nil {
		fmt.Printf("WARNING: failed to read %s", hookFile)
		return hook, err
	}
	err = cfg.Read(strings.NewReader("[hook]\n" + string(content)))
	if err != nil {
		fmt.Printf("WARNING: failed to parse %s", hookFile)
		return hook, err
	}
	hook.name, _ = cfg.Get("hook", "Hook-Name")
	hook.exec, _ = cfg.Get("hook", "Exec")
	hook.user, _ = cfg.Get("hook", "User")
	hook.pattern, _ = cfg.Get("hook", "Pattern")
	// FIXME: error on supported hook features like
	//    User-Level: yes
	//    Trigger: yes
	//    Single-Version: yes

	// urgh, click allows empty "Hook-Name"
	if hook.name == "" {
		hook.name = strings.Split(filepath.Base(hookFile), ".")[0]
	}

	return hook, err
}

func systemClickHooks() (hooks map[string]clickHook, err error) {
	hooks = make(map[string]clickHook)

	hookFiles, err := filepath.Glob(path.Join(clickSystemHooksDir, "*.hook"))
	if err != nil {
		return
	}
	for _, f := range hookFiles {
		hook, err := readClickHookFile(f)
		if err != nil {
			log.Printf("Can't read hook file %s: %s", f, err)
			continue
		}
		hooks[hook.name] = hook
	}
	return
}

func expandHookPattern(name, app, version, pattern string) (expanded string) {
	id := fmt.Sprintf("%s_%s_%s", name, app, version)
	// FIXME: support the other patterns (and see if they are used at all):
	//        - short-id
	//        - user (probably not!)
	//        - home (probably not!)
	//        - $$ (?)
	return strings.Replace(pattern, "${id}", id, -1)
}

type iterHooksFunc func(src, dst string, systemHook clickHook) error

// iterHooks will run the callback "f" for the given manifest
// so that the call back can arrange e.g. a new link
func iterHooks(manifest clickManifest, inhibitHooks bool, f iterHooksFunc) error {
	systemHooks, err := systemClickHooks()
	if err != nil {
		return err
	}

	for app, hook := range manifest.Hooks {
		for hookName, hookSourceFile := range hook {
			// ignore hooks that only exist for compatibility
			// with the old snappy-python (like bin-path,
			// snappy-systemd)
			if ignoreHooks[hookName] {
				continue
			}

			systemHook, ok := systemHooks[hookName]
			if !ok {
				log.Printf("WARNING: Skipping hook %s", hookName)
				continue
			}

			dst := filepath.Join(globalRootDir, expandHookPattern(manifest.Name, app, manifest.Version, systemHook.pattern))

			if _, err := os.Stat(dst); err == nil {
				if err := os.Remove(dst); err != nil {
					log.Printf("Warning: failed to remove %s: %s", dst, err)
				}
			}

			// run iter func here
			if err := f(hookSourceFile, dst, systemHook); err != nil {
				return err
			}

			if systemHook.exec != "" && !inhibitHooks {
				if err := execHook(systemHook.exec); err != nil {
					os.Remove(dst)
					return err
				}
			}
		}
	}

	return nil
}

func installClickHooks(targetDir string, manifest clickManifest, inhibitHooks bool) error {
	return iterHooks(manifest, inhibitHooks, func(src, dst string, systemHook clickHook) error {
		// setup the new link target here, iterHooks will take
		// care of running the hook
		realSrc := path.Join(targetDir, src)
		if err := os.Symlink(realSrc, dst); err != nil {
			return err
		}

		return nil
	})
}

func removeClickHooks(manifest clickManifest, inhibitHooks bool) (err error) {
	return iterHooks(manifest, inhibitHooks, func(src, dst string, systemHook clickHook) error {
		// nothing we need to do here, the iterHookss will remove
		// the hook symlink and call the hook itself
		return nil
	})
}

func readClickManifestFromClickDir(clickDir string) (manifest clickManifest, err error) {
	manifestFiles, err := filepath.Glob(path.Join(clickDir, ".click", "info", "*.manifest"))
	if err != nil {
		return manifest, err
	}
	if len(manifestFiles) != 1 {
		return manifest, fmt.Errorf("Error: got %v manifests in %v", len(manifestFiles), clickDir)
	}
	manifestData, err := ioutil.ReadFile(manifestFiles[0])
	manifest, err = readClickManifest([]byte(manifestData))
	return manifest, err
}

func removeClick(clickDir string) (err error) {
	manifest, err := readClickManifestFromClickDir(clickDir)
	if err != nil {
		return err
	}

	if err := removeClickHooks(manifest, false); err != nil {
		return err
	}

	// maybe remove current symlink
	currentSymlink := path.Join(path.Dir(clickDir), "current")
	p, _ := filepath.EvalSymlinks(currentSymlink)
	if clickDir == p {
		if err := unsetActiveClick(p, false); err != nil {
			return err
		}
	}

	return os.RemoveAll(clickDir)
}

func writeHashesFile(snapFile, instDir string) error {
	hashsum, err := helpers.Sha512sum(snapFile)
	if err != nil {
		return err
	}

	s := fmt.Sprintf("sha512: %s", hashsum)
	hashesFile := filepath.Join(instDir, "meta", "hashes")
	return ioutil.WriteFile(hashesFile, []byte(s), 0644)
}

// generate the name
func generateBinaryName(m *packageYaml, binary Binary) string {
	var binName string
	if m.Type == SnapTypeFramework {
		binName = filepath.Base(binary.Name)
	} else {
		binName = fmt.Sprintf("%s.%s", filepath.Base(binary.Name), m.Name)
	}

	return filepath.Join(snapBinariesDir, binName)
}

func binPathForBinary(pkgPath string, binary Binary) string {
	if binary.Exec != "" {
		return filepath.Join(pkgPath, binary.Exec)
	}

	return filepath.Join(pkgPath, binary.Name)
}

func generateSnapBinaryWrapper(binary Binary, pkgPath, aaProfile string, m *packageYaml) string {
	wrapperTemplate := `#!/bin/sh
# !!!never remove this line!!!
##TARGET={{.Target}}

set -e

TMPDIR="/tmp/snaps/{{.Name}}/{{.Version}}/tmp"
if [ ! -d "$TMPDIR" ]; then
    mkdir -p -m1777 "$TMPDIR"
fi
export TMPDIR
export TEMPDIR="$TMPDIR"

# app paths (deprecated)
export SNAPP_APP_PATH="{{.Path}}"
export SNAPP_APP_DATA_PATH="/var/lib/{{.Path}}"
export SNAPP_APP_USER_DATA_PATH="$HOME/{{.Path}}"
export SNAPP_APP_TMPDIR="$TMPDIR"
export SNAPP_OLD_PWD="$(pwd)"

# app paths
export SNAP_APP_PATH="{{.Path}}"
export SNAP_APP_DATA_PATH="/var/lib/{{.Path}}"
export SNAP_APP_USER_DATA_PATH="$HOME/{{.Path}}"
export SNAP_APP_TMPDIR="$TMPDIR"

# FIXME: this will need to become snappy arch or something
export SNAPPY_APP_ARCH="$(dpkg --print-architecture)"

if [ ! -d "$SNAP_APP_USER_DATA_PATH" ]; then
   mkdir -p "$SNAP_APP_USER_DATA_PATH"
fi
export HOME="$SNAP_APP_USER_DATA_PATH"

# export old pwd
export SNAP_OLD_PWD="$(pwd)"
cd {{.Path}}
aa-exec -p {{.AaProfile}} -- {{.Target}} "$@"
`
	actualBinPath := binPathForBinary(pkgPath, binary)

	var templateOut bytes.Buffer
	t := template.Must(template.New("wrapper").Parse(wrapperTemplate))
	wrapperData := struct {
		Name      string
		Version   string
		Target    string
		Path      string
		AaProfile string
	}{
		m.Name, m.Version, actualBinPath, pkgPath, aaProfile,
	}
	t.Execute(&templateOut, wrapperData)

	return templateOut.String()
}

func generateSnapServicesFile(service Service, baseDir string, aaProfile string, m *packageYaml) string {

	serviceTemplate := `[Unit]
Description={{.Description}}
After=apparmor.service
Requires=apparmor.service
X-Snappy=yes

[Service]
ExecStart={{.FullPathStart}}
WorkingDirectory={{.AppPath}}
Environment="SNAPP_APP_PATH={{.AppPath}}" "SNAPP_APP_DATA_PATH=/var/lib{{.AppPath}}" "SNAPP_APP_USER_DATA_PATH=%h{{.AppPath}}" "SNAP_APP_PATH={{.AppPath}}" "SNAP_APP_DATA_PATH=/var/lib{{.AppPath}}" "SNAP_APP_USER_DATA_PATH=%h{{.AppPath}}" "SNAP_APP={{.AppTriple}}"
AppArmorProfile={{.AaProfile}}
{{if .Stop}}ExecStop={{.Stop}}{{end}}
{{if .PostStop}}ExecPostStop={{.PostStop}}{{end}}
{{if .StopTimeout}}TimeoutStopSec={{.StopTimeout}}{{end}}

[Install]
WantedBy=multi-user.target
`
	var templateOut bytes.Buffer
	t := template.Must(template.New("wrapper").Parse(serviceTemplate))
	wrapperData := struct {
		packageYaml
		Service
		AppPath       string
		AaProfile     string
		FullPathStart string
		AppTriple     string
	}{
		*m, service, baseDir, aaProfile, filepath.Join(baseDir, service.Start), fmt.Sprintf("%s_%s_%s", m.Name, service.Name, m.Version),
	}
	if err := t.Execute(&templateOut, wrapperData); err != nil {
		// this can never happen, except we forget a variable
		panic(err)
	}

	return templateOut.String()
}

func generateServiceFileName(m *packageYaml, service Service) string {
	return filepath.Join(snapServicesDir, fmt.Sprintf("%s_%s_%s.service", m.Name, service.Name, m.Version))
}

var runSystemctl = runSystemctlImpl

func runSystemctlImpl(cmd ...string) error {
	args := []string{"systemctl", "--root", globalRootDir}
	args = append(args, cmd...)
	if err := exec.Command(args[0], args[1:]...).Run(); err != nil {
		exitCode, _ := helpers.ExitCode(err)
		return &ErrSystemCtl{cmd: args,
			exitCode: exitCode}
	}

	return nil
}

// takes a directory and removes the global root, this is needed
// when the SetGlobalRoot option is used and we need to generate
// content for the "Services" and "Binaries" section
func stripGlobalRootDir(dir string) string {
	return dir[len(globalRootDir):]
}

func addPackageServices(baseDir string, inhibitHooks bool) error {
	m, err := parsePackageYamlFile(filepath.Join(baseDir, "meta", "package.yaml"))
	if err != nil {
		return err
	}

	for _, service := range m.Services {
		aaProfile := fmt.Sprintf("%s_%s_%s", m.Name, service.Name, m.Version)
		// this will remove the global base dir when generating the
		// service file, this ensures that /apps/foo/1.0/bin/start
		// is in the service file when the SetGlobalRoot() option
		// is used
		realBaseDir := stripGlobalRootDir(baseDir)
		content := generateSnapServicesFile(service, realBaseDir, aaProfile, m)
		serviceFilename := generateServiceFileName(m, service)
		helpers.EnsureDir(filepath.Dir(serviceFilename), 0755)
		if err := ioutil.WriteFile(serviceFilename, []byte(content), 0755); err != nil {
			return err
		}

		// daemon-reload and start only if we are not in the
		// inhibitHooks mode
		//
		// *but* always run enable (which just sets a symlink)
		serviceName := filepath.Base(generateServiceFileName(m, service))
		if !inhibitHooks {
			if err := runSystemctl("daemon-reload"); err != nil {
				return err
			}
		}

		if err := runSystemctl("enable", serviceName); err != nil {
			return err
		}

		if !inhibitHooks {
			if err := runSystemctl("start", serviceName); err != nil {
				return err
			}
		}
	}

	return nil
}

func removePackageServices(baseDir string) error {
	m, err := parsePackageYamlFile(filepath.Join(baseDir, "meta", "package.yaml"))
	if err != nil {
		return err
	}
	for _, service := range m.Services {
		serviceName := filepath.Base(generateServiceFileName(m, service))
		if err := runSystemctl("stop", serviceName); err != nil {
			return err
		}
		if err := runSystemctl("disable", serviceName); err != nil {
			return err
		}
		// FIXME: wait for the service to be really stopped

		os.Remove(generateServiceFileName(m, service))
	}
	if err := runSystemctl("daemon-reload"); err != nil {
		return err
	}

	return nil
}

func getBinaryAaProfile(m *packageYaml, binary Binary) string {
	// check if there is a specific apparmor profile
	if binary.SecurityPolicy != "" {
		return binary.SecurityPolicy
	}
	// ... or apparmor.json
	if binary.SecurityTemplate != "" {
		return binary.SecurityTemplate
	}

	// FIXME: we need to generate a default aa profile here instead
	// of relying on a default one shipped by the package
	return fmt.Sprintf("%s_%s_%s", m.Name, filepath.Base(binary.Name), m.Version)
}

func addPackageBinaries(baseDir string) error {
	m, err := parsePackageYamlFile(filepath.Join(baseDir, "meta", "package.yaml"))
	if err != nil {
		return err
	}

	if err := os.MkdirAll(snapBinariesDir, 0755); err != nil {
		return err
	}

	for _, binary := range m.Binaries {
		aaProfile := getBinaryAaProfile(m, binary)
		// this will remove the global base dir when generating the
		// service file, this ensures that /apps/foo/1.0/bin/start
		// is in the service file when the SetGlobalRoot() option
		// is used
		realBaseDir := stripGlobalRootDir(baseDir)
		content := generateSnapBinaryWrapper(binary, realBaseDir, aaProfile, m)
		if err := ioutil.WriteFile(generateBinaryName(m, binary), []byte(content), 0755); err != nil {
			return err
		}
	}

	return nil
}

func removePackageBinaries(baseDir string) error {
	m, err := parsePackageYamlFile(filepath.Join(baseDir, "meta", "package.yaml"))
	if err != nil {
		return err
	}
	for _, binary := range m.Binaries {
		os.Remove(generateBinaryName(m, binary))
	}

	return nil
}

func installClick(snapFile string, flags InstallFlags) (err error) {
	// FIXME: drop privs to "snap:snap" here
	// like in http://bazaar.launchpad.net/~phablet-team/goget-ubuntu-touch/trunk/view/head:/sysutils/utils.go#L64

	allowUnauthenticated := (flags & AllowUnauthenticated) != 0
	err = auditClick(snapFile, allowUnauthenticated)
	if err != nil {
		return err
		// ?
		//return SnapAuditError
	}

	d := clickdeb.ClickDeb{Path: snapFile}
	manifestData, err := d.ControlMember("manifest")
	if err != nil {
		log.Printf("Snap inspect failed: %s", snapFile)
		return err
	}
	manifest, err := readClickManifest([]byte(manifestData))
	if err != nil {
		return err
	}

	dataDir := filepath.Join(snapDataDir, manifest.Name, manifest.Version)

	targetDir := snapAppsDir
	// the "oem" parts are special
	if manifest.Type == SnapTypeOem {
		targetDir = snapOemDir
	}

	instDir := filepath.Join(targetDir, manifest.Name, manifest.Version)
	if err := helpers.EnsureDir(instDir, 0755); err != nil {
		log.Printf("WARNING: Can not create %s", instDir)
	}

	// if anything goes wrong here we cleanup
	defer func() {
		if err == nil {
			return
		}
		if _, err := os.Stat(instDir); err == nil {
			if err := os.RemoveAll(instDir); err != nil {
				log.Printf("Warning: failed to remove %s: %s", instDir, err)
			}
		}
	}()

	err = d.Unpack(instDir)
	if err != nil {
		return err
	}

	// legacy, the hooks (e.g. apparmor) need this. Once we converted
	// all hooks this can go away
	clickMetaDir := path.Join(instDir, ".click", "info")
	os.MkdirAll(clickMetaDir, 0755)
	err = ioutil.WriteFile(path.Join(clickMetaDir, manifest.Name+".manifest"), manifestData, 0644)
	if err != nil {
		return
	}

	// write the hashes now
	if err := writeHashesFile(snapFile, instDir); err != nil {
		return err
	}

	inhibitHooks := (flags & InhibitHooks) != 0

	currentActiveDir, _ := filepath.EvalSymlinks(filepath.Join(instDir, "..", "current"))
	// deal with the data:
	//
	// if there was a previous version, stop it
	// from being active so that it stops running and can no longer be
	// started then copy the data
	//
	// otherwise just create a empty data dir
	if currentActiveDir != "" {
		oldManifest, err := readClickManifestFromClickDir(currentActiveDir)
		if err != nil {
			return err
		}

		// we need to stop making it active
		if err := unsetActiveClick(currentActiveDir, inhibitHooks); err != nil {
			// if anything goes wrong try to activate the old
			// one again and pass the error on
			setActiveClick(currentActiveDir, inhibitHooks)
			return err
		}

		if err := copySnapData(manifest.Name, oldManifest.Version, manifest.Version); err != nil {
			// FIXME: remove newDir

			// restore the previous version
			setActiveClick(currentActiveDir, inhibitHooks)
			return err
		}
	} else {
		if err := helpers.EnsureDir(dataDir, 0755); err != nil {
			log.Printf("WARNING: Can not create %s", dataDir)
			return err
		}
	}

	// and finally make active
	if err := setActiveClick(instDir, inhibitHooks); err != nil {
		// ensure to revert on install failure
		if currentActiveDir != "" {
			setActiveClick(currentActiveDir, inhibitHooks)
		}
		return err
	}

	return nil
}

// Copy all data for "snapName" from "oldVersion" to "newVersion"
// (but never overwrite)
func copySnapData(snapName, oldVersion, newVersion string) (err error) {
	// collect the directories, homes first
	oldDataDirs, err := filepath.Glob(filepath.Join(snapDataHomeGlob, snapName, oldVersion))
	if err != nil {
		return err
	}
	// then system data
	oldSystemPath := filepath.Join(snapDataDir, snapName, oldVersion)
	oldDataDirs = append(oldDataDirs, oldSystemPath)

	for _, oldDir := range oldDataDirs {
		// replace the trailing "../$old-ver" with the "../$new-ver"
		newDir := filepath.Join(filepath.Dir(oldDir), newVersion)
		if err := copySnapDataDirectory(oldDir, newDir); err != nil {
			return err
		}
	}

	return nil
}

// Lowlevel copy the snap data (but never override existing data)
func copySnapDataDirectory(oldPath, newPath string) (err error) {
	if _, err := os.Stat(oldPath); err == nil {
		if _, err := os.Stat(newPath); err != nil {
			// there is no golang "CopyFile" and we want hardlinks
			// by default to save space
			cmd := exec.Command("cp", "-al", oldPath, newPath)
			if err := cmd.Run(); err != nil {
				if exitCode, err := helpers.ExitCode(err); err != nil {
					return &ErrDataCopyFailed{
						oldPath:  oldPath,
						newPath:  newPath,
						exitCode: exitCode}
				}
				return err
			}
		}
	}
	return nil
}

func unsetActiveClick(clickDir string, inhibitHooks bool) error {
	currentSymlink := filepath.Join(clickDir, "..", "current")

	// sanity check
	currentActiveDir, err := filepath.EvalSymlinks(currentSymlink)
	if err != nil {
		return err
	}
	if clickDir != currentActiveDir {
		return ErrSnapNotActive
	}

	// remove generated services, binaries, clickHooks
	if err := removePackageBinaries(clickDir); err != nil {
		return err
	}

	if err := removePackageServices(clickDir); err != nil {
		return err
	}

	manifest, err := readClickManifestFromClickDir(clickDir)
	if err != nil {
		return err
	}
	if err := removeClickHooks(manifest, inhibitHooks); err != nil {
		return err
	}

	// and finally the current symlink
	if err := os.Remove(currentSymlink); err != nil {
		log.Printf("Warning: failed to remove %s: %s", currentSymlink, err)
	}

	return nil
}

func setActiveClick(baseDir string, inhibitHooks bool) error {
	currentActiveSymlink := filepath.Join(baseDir, "..", "current")
	currentActiveDir, _ := filepath.EvalSymlinks(currentActiveSymlink)

	// already active, nothing to do
	if baseDir == currentActiveDir {
		return nil
	}

	// there is already an active part
	if currentActiveDir != "" {
		unsetActiveClick(currentActiveDir, inhibitHooks)
	}

	// make new part active
	newActiveManifest, err := readClickManifestFromClickDir(baseDir)
	if err != nil {
		return err
	}

	if err := installClickHooks(baseDir, newActiveManifest, inhibitHooks); err != nil {
		// cleanup the failed hooks
		removeClickHooks(newActiveManifest, inhibitHooks)
		return err
	}

	// add the "binaries:" from the package.yaml
	if err := addPackageBinaries(baseDir); err != nil {
		return err
	}
	// add the "services:" from the package.yaml
	if err := addPackageServices(baseDir, inhibitHooks); err != nil {
		return err
	}

	// FIXME: we want to get rid of the current symlink
	if _, err := os.Stat(currentActiveSymlink); err == nil {
		if err := os.Remove(currentActiveSymlink); err != nil {
			log.Printf("Warning: failed to remove %s: %s", currentActiveSymlink, err)
		}
	}

	// symlink is relative to parent dir
	return os.Symlink(filepath.Base(baseDir), currentActiveSymlink)
}
