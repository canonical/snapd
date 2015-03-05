package snappy

/* This part of the code implements enough of the click file format
   to install a "snap" package
   Limitations:
   - no per-user registration
   - no user-level hooks
   - dpkg-deb --unpack is used to "install" instead of "dpkg -i"
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

	"launchpad.net/snappy/helpers"

	"github.com/mvo5/goconfigparser"
)

type clickAppHook map[string]string

type clickManifest struct {
	Name    string                  `json:"name"`
	Version string                  `json:"version"`
	Type    SnapType                `json:"type,omitempty"`
	Hooks   map[string]clickAppHook `json:"hooks,omitempty"`
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
	"bin-path": true,
}

// var to make it testable
var clickSystemHooksDir = "/usr/share/click/hooks"

// InstallFlags can be used to pass additional flags to the install of a
// snap
type InstallFlags uint

const (
	// AllowUnauthenticated allows to install a snap even if it can not be authenticated
	AllowUnauthenticated InstallFlags = 1 << iota
)

// Execute the hook.Exec command
func (s *clickHook) execHook() (err error) {
	// the spec says this is passed to the shell
	cmd := exec.Command("sh", "-c", s.exec)
	if err = cmd.Run(); err != nil {
		log.Printf("Failed to run hook %s: %s", s.exec, err)
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
		if exitCode, err := helpers.ExitCode(err); err == nil {
			if allowUnauthenticated && allowUnauthenticatedOkExitCode(exitCode) {
				log.Println("Signature check failed, but installing anyway as requested")
				return nil
			}
		}
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
	expanded = strings.Replace(pattern, "${id}", id, -1)

	return
}

func installClickHooks(targetDir string, manifest clickManifest) (err error) {
	systemHooks, err := systemClickHooks()
	if err != nil {
		return err
	}
	for app, hook := range manifest.Hooks {
		for hookName, hookTargetFile := range hook {
			// ignore hooks that only exist for compatibility
			// with the old snappy-python (like bin-path)
			if ignoreHooks[hookName] {
				continue
			}

			systemHook, ok := systemHooks[hookName]
			if !ok {
				log.Printf("WARNING: Skipping hook %s", hookName)
				continue
			}
			src := path.Join(targetDir, hookTargetFile)
			dst := expandHookPattern(manifest.Name, app, manifest.Version, systemHook.pattern)
			if _, err := os.Stat(dst); err == nil {
				if err := os.Remove(dst); err != nil {
					log.Printf("Warning: failed to remove %s: %s", dst, err)
				}
			}
			if err := os.Symlink(src, dst); err != nil {
				return err
			}
			if systemHook.exec != "" {
				if err := systemHook.execHook(); err != nil {
					return err
				}
			}
		}
	}
	return
}

func removeClickHooks(manifest clickManifest) (err error) {
	systemHooks, err := systemClickHooks()
	if err != nil {
		return err
	}
	for app, hook := range manifest.Hooks {
		for hookName := range hook {
			systemHook, ok := systemHooks[hookName]
			if !ok {
				continue
			}
			dst := expandHookPattern(manifest.Name, app, manifest.Version, systemHook.pattern)
			if _, err := os.Stat(dst); err == nil {
				if err := os.Remove(dst); err != nil {
					log.Printf("Warning: failed to remove %s: %s", dst, err)
				}
			}
			if systemHook.exec != "" {
				if err := systemHook.execHook(); err != nil {
					return err
				}
			}
		}
	}
	return err
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

	if err := removePackageYamlBinaries(clickDir); err != nil {
		return err
	}

	if err := removeClickHooks(manifest); err != nil {
		return err
	}

	// maybe remove current symlink
	currentSymlink := path.Join(path.Dir(clickDir), "current")
	p, _ := filepath.EvalSymlinks(currentSymlink)
	if clickDir == p {
		if err := os.Remove(currentSymlink); err != nil {
			log.Printf("Warning: failed to remove %s: %s", currentSymlink, err)
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

func generateSnapBinaryWrapper(binary Binary, pkgPath, aaProfile string, m *packageYaml) string {
	wrapperTemplate := `#!/bin/sh
# !!!never remove this line!!!
##TARGET={{.Target}}

set -e

TMPDIR="/tmp/snapps/{{.Name}}/{{.Version}}/tmp"
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
	actualBinPath := filepath.Join(pkgPath, binary.Name)

	var templateOut bytes.Buffer
	t := template.Must(template.New("wrapper").Parse(wrapperTemplate))
	wrapperData := struct {
		packageYaml
		Target    string
		Path      string
		AaProfile string
	}{
		*m, actualBinPath, pkgPath, aaProfile,
	}
	t.Execute(&templateOut, wrapperData)

	return templateOut.String()
}

func getAaProfile(m *packageYaml, binary Binary) string {
	// FIXME: we need to generate a default aa profile here instead
	// once we have click-apparmor in snappy itself
	clickhookPath := fmt.Sprintf("/var/lib/apparmor/clicks/%s_%s_%s.json", m.Name, filepath.Dir(binary.Name), m.Version)
	if helpers.FileExists(clickhookPath) {
		return clickhookPath
	}

	customProfilePath := fmt.Sprintf("/var/lib/apparmor/profiles/profile_%s_%s_%s", m.Name, filepath.Dir(binary.Name), m.Version)

	if helpers.FileExists(customProfilePath) {
		return customProfilePath
	}

	return "unconfined"
}

func addPackageYamlBinaries(baseDir string) error {
	m, err := parsePackageYamlFile(filepath.Join(baseDir, "meta", "package.yaml"))
	if err != nil {
		return err
	}

	if err := os.MkdirAll(snapBinariesDir, 0755); err != nil {
		return err
	}

	for _, binary := range m.Binaries {
		aaProfile := getAaProfile(m, binary)
		content := generateSnapBinaryWrapper(binary, baseDir, aaProfile, m)
		if err := ioutil.WriteFile(generateBinaryName(m, binary), []byte(content), 0755); err != nil {
			return err
		}
	}

	return nil
}

func removePackageYamlBinaries(baseDir string) error {
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

	cmd := exec.Command("dpkg-deb", "-I", snapFile, "manifest")
	manifestData, err := cmd.Output()
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

	// FIXME: replace this with a native extractor to avoid attack
	//        surface
	cmd = exec.Command("dpkg-deb", "--extract", snapFile, instDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// FIXME: make the output part of the SnapExtractError
		log.Printf("Snap install failed with: %s", output)
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

	currentActiveDir, _ := filepath.EvalSymlinks(filepath.Join(instDir, "..", "current"))
	// deal with the data, if there was a previous version, copy the data
	// otherwise just create a empty data dir
	if currentActiveDir != "" {
		oldManifest, err := readClickManifestFromClickDir(currentActiveDir)
		if err != nil {
			return err
		}
		if err := copySnapData(manifest.Name, oldManifest.Version, manifest.Version); err != nil {
			return err
		}
	} else {
		if err := helpers.EnsureDir(dataDir, 0755); err != nil {
			log.Printf("WARNING: Can not create %s", dataDir)
			return err
		}
	}

	// and finally make active
	err = setActiveClick(instDir)
	if err != nil {
		// ensure to revert on install failure
		if currentActiveDir != "" {
			setActiveClick(currentActiveDir)
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
				return err
			}
		}
	}
	return nil
}

func setActiveClick(baseDir string) (err error) {
	currentActiveSymlink := filepath.Join(baseDir, "..", "current")
	currentActiveDir, err := filepath.EvalSymlinks(currentActiveSymlink)

	// already active, nothing to do
	if baseDir == currentActiveDir {
		return nil
	}

	// there is already an active part
	if currentActiveDir != "" {
		currentActiveManifest, err := readClickManifestFromClickDir(currentActiveDir)
		if err != nil {
			return err
		}

		if err := removePackageYamlBinaries(currentActiveDir); err != nil {
			return err
		}

		if err := removeClickHooks(currentActiveManifest); err != nil {
			return err
		}
	}

	// make new part active
	newActiveManifest, err := readClickManifestFromClickDir(baseDir)
	if err != nil {
		return err
	}

	// add the "binaries:" from the package.yaml
	if err := addPackageYamlBinaries(baseDir); err != nil {
		return err
	}

	// and now the cick hooks
	err = installClickHooks(baseDir, newActiveManifest)
	if err != nil {
		return err
	}

	// FIXME: we want to get rid of the current symlink
	if _, err := os.Stat(currentActiveSymlink); err == nil {
		if err := os.Remove(currentActiveSymlink); err != nil {
			log.Printf("Warning: failed to remove %s: %s", currentActiveSymlink, err)
		}
	}
	err = os.Symlink(baseDir, currentActiveSymlink)
	return err
}
