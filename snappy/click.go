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
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/mvo5/goconfigparser"
)

type clickAppHook map[string]string

type clickManifest struct {
	Name    string                  `json:"name"`
	Version string                  `json:"version"`
	Type    string                  `json:"type,omitempty"`
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
	DS_SUCCESS             = 0
	DS_FAIL_NOSIGS         = 10
	DS_FAIL_UNKNOWN_ORIGIN = 11
	DS_FAIL_NOPOLICIES     = 12
	DS_FAIL_BADSIG         = 13
	DS_FAIL_INTERNAL       = 14
)

// This function checks if the given exitCode is "ok" when running with
// --allow-unauthenticated. We allow package with no signature or with
// a unknown policy or with no policies at all. We do not allow overriding
// bad signatures
func allowUnauthenticatedOkExitCode(exitCode int) bool {
	return (exitCode == DS_FAIL_NOSIGS ||
		exitCode == DS_FAIL_UNKNOWN_ORIGIN ||
		exitCode == DS_FAIL_NOPOLICIES)
}

// Tiny wrapper around the debsig-verify commandline
func runDebsigVerifyImpl(clickFile string, allowUnauthenticated bool) (err error) {
	cmd := exec.Command("debsig-verify", clickFile)
	if err := cmd.Run(); err != nil {
		if exitCode, err := exitCode(err); err == nil {
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

func systemClickHooks(hookDir string) (hooks map[string]clickHook, err error) {
	hooks = make(map[string]clickHook)

	hookFiles, err := filepath.Glob(path.Join(hookDir, "*.hook"))
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

func installClickHooks(hooksDir, targetDir string, manifest clickManifest) (err error) {
	systemHooks, err := systemClickHooks(hooksDir)
	if err != nil {
		return err
	}
	for app, hook := range manifest.Hooks {
		for hookName, hookTargetFile := range hook {
			systemHook, ok := systemHooks[hookName]
			if !ok {
				log.Printf("WARNING: Skipping hook %s", hookName)
				continue
			}
			src := path.Join(targetDir, hookTargetFile)
			dst := expandHookPattern(manifest.Name, app, manifest.Version, systemHook.pattern)
			if err := os.Remove(dst); err != nil {
				log.Printf("Warning: failed to remove %s: %s", dst, err)
			}
			if err := os.Symlink(src, dst); err != nil {
				return err
			}
			if systemHook.exec != "" {
				// the spec says this is passed to the shell
				cmd := exec.Command("sh", "-c", systemHook.exec)
				if err = cmd.Run(); err != nil {
					log.Printf("Failed to run hook %s: %s", systemHook.exec, err)
					return err
				}
			}
		}
	}
	return
}

func removeClickHooks(hooksDir string, manifest clickManifest) (err error) {
	systemHooks, err := systemClickHooks(hooksDir)
	if err != nil {
		return err
	}
	for app, hook := range manifest.Hooks {
		for hookName, _ := range hook {
			systemHook, ok := systemHooks[hookName]
			if !ok {
				continue
			}
			dst := expandHookPattern(manifest.Name, app, manifest.Version, systemHook.pattern)
			if err := os.Remove(dst); err != nil {
				log.Printf("Warning: failed to remove %s: %s", dst, err)
			}
			if systemHook.exec != "" {
				cmdStr := strings.Split(systemHook.exec, " ")
				cmd := exec.Command(cmdStr[0], cmdStr...)
				err = cmd.Run()
				if err != nil {
					return err
				}
			}
		}
	}
	return err
}

func removeClick(clickDir string) (err error) {
	manifestFiles, err := filepath.Glob(path.Join(clickDir, ".click", "info", "*.manifest"))
	if err != nil {
		return err
	}
	if len(manifestFiles) != 1 {
		return errors.New(fmt.Sprintf("Error: got %s manifests in %s", len(manifestFiles), clickDir))
	}
	manifestData, err := ioutil.ReadFile(manifestFiles[0])
	manifest, err := readClickManifest([]byte(manifestData))
	if err != nil {
		return err
	}
	err = removeClickHooks("/usr/share/click/hooks", manifest)
	if err != nil {
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

func installClick(snapFile string, allowUnauthenticated bool) (err error) {
	// FIXME: drop privs to "snap:snap" here
	// like in http://bazaar.launchpad.net/~phablet-team/goget-ubuntu-touch/trunk/view/head:/sysutils/utils.go#L64

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
	if err := ensureDir(dataDir, 0755); err != nil {
		log.Printf("WARNING: Can not create %s", dataDir)
	}

	targetDir := snapAppsDir
	// the "oem" parts are special
	if manifest.Type == "oem" {
		targetDir = snapOemDir
	}

	instDir := filepath.Join(targetDir, manifest.Name, manifest.Version)
	if err := ensureDir(instDir, 0755); err != nil {
		log.Printf("WARNING: Can not create %s", instDir)
	}

	// FIXME: replace this with a native extractor to avoid attack
	//        surface
	cmd = exec.Command("dpkg-deb", "--extract", snapFile, instDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// FIXME: make the output part of the SnapExtractError
		log.Printf("Snap install failed with: %s", output)
		if err := os.RemoveAll(instDir); err != nil {
			log.Printf("Warning: failed to remove %s: %s", instDir, err)
		}
		return err
	}

	metaDir := path.Join(instDir, ".click", "info")
	os.MkdirAll(metaDir, 0755)
	err = ioutil.WriteFile(path.Join(metaDir, manifest.Name+".manifest"), manifestData, 0644)
	if err != nil {
		return
	}

	err = installClickHooks("/usr/share/click/hooks", instDir, manifest)
	if err != nil {
		// FIXME: make the output part of the SnapExtractError
		log.Printf("Snap install failed with: %s", output)
		if err := os.RemoveAll(instDir); err != nil {
			log.Printf("Warning: failed to remove %s: %s", instDir, err)
		}
		return err
	}

	// FIXME: we want to get rid of the current symlink
	// update current symlink
	currentSymlink := path.Join(path.Dir(instDir), "current")
	if err := os.Remove(currentSymlink); err != nil {
		log.Printf("Warning: failed to remove %s: %s", currentSymlink, err)
	}
	err = os.Symlink(instDir, currentSymlink)

	return err
}
