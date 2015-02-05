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
	err = runDebsigVerify(snapFile, allowUnauthenticated)
	if err != nil {
		return err
	}
	// FIXME: check what more we need to do here

	return nil
}

func readClickManifest(data []byte) (manifest clickManifest, err error) {
	r := bytes.NewReader(data)
	dec := json.NewDecoder(r)
	err = dec.Decode(&manifest)
	return
}

func readClickHookFile(hookFile string) (hook clickHook, err error) {
	// FIXME: fugly, write deb822 style parser
	cfg := goconfigparser.New()
	content, err := ioutil.ReadFile(hookFile)
	if err != nil {
		return
	}
	err = cfg.Read(strings.NewReader("[hook]\n" + string(content)))
	if err != nil {
		return
	}
	hook.name, err = cfg.Get("hook", "Hook-Name")
	hook.exec, err = cfg.Get("hook", "Exec")
	hook.user, err = cfg.Get("hook", "User")
	hook.pattern, err = cfg.Get("hook", "Pattern")
	// FIXME: panic if
	//    User-Level: yes
	// as this is not supported
	return
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
				continue
			}
			src := path.Join(targetDir, hookTargetFile)
			dst := expandHookPattern(manifest.Name, app, manifest.Version, systemHook.pattern)
			os.Remove(dst)
			err = os.Symlink(src, dst)
			if err != nil {
				return
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
			os.Remove(dst)
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
	return
}

func removeClick(clickDir string) (err error) {
	manifestFiles, err := filepath.Glob(path.Join(clickDir, ".click", "info", "*.manifest"))
	if err != nil {
		return
	}
	if len(manifestFiles) != 1 {
		return errors.New(fmt.Sprintf("Error: got %s manifests in %s", len(manifestFiles), clickDir))
	}
	manifestData, err := ioutil.ReadFile(manifestFiles[0])
	manifest, err := readClickManifest([]byte(manifestData))
	if err != nil {
		return
	}
	err = removeClickHooks("/usr/share/click/hooks", manifest)
	if err != nil {
		return
	}

	// maybe remove current symlink
	currentSymlink := path.Join(path.Dir(clickDir), "current")
	p, _ := filepath.EvalSymlinks(currentSymlink)
	if clickDir == p {
		os.Remove(currentSymlink)
	}

	return os.RemoveAll(clickDir)
}

func installClick(snapFile, targetDir string, allowUnauthenticated bool) (err error) {
	// FIXME: drop privs to "snap:snap" here

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

	instDir := path.Join(targetDir, manifest.Name, manifest.Version)
	if _, err := os.Stat(instDir); err != nil {
		os.MkdirAll(instDir, 0755)
	}
	// FIXME: replace this with a native extractor to avoid attack
	//        surface
	cmd = exec.Command("dpkg-deb", "--extract", snapFile, instDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// FIXME: make the output part of the SnapExtractError
		log.Printf("Snap install failed with: %s", output)
		os.RemoveAll(instDir)
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
		os.RemoveAll(instDir)
		return err
	}

	// FIXME: we want to get rid of the current symlink
	// update current symlink
	currentSymlink := path.Join(path.Dir(instDir), "current")
	os.Remove(currentSymlink)
	err = os.Symlink(instDir, currentSymlink)

	return err
}
