# This makefile is designed to be invoked from a build tree, not from the
# source tree. This is particularly suited for building RPM packages. The goal
# of this makefile is to *reduce* the duplication, *avoid* unintended
# inconsistency between packaging of snapd across distributions.
#
# -----------------------------------------------------
# Interface between snapd.mk and distribution packaging
# -----------------------------------------------------
#
# Distribution packaging must generate snapd.defines.mk with a set of makefile
# variable definitions that are discussed below. This allows the packaging
# world to define directory layout and build configuration in one place and
# this makefile to simply obey and implement that configuration.

ifeq ($(SNAPD_DEFINES_DIR),)
SNAPD_DEFINES_DIR = $(PWD)
endif
include $(SNAPD_DEFINES_DIR)/snapd.defines.mk

# There are two sets of definitions expected:
# 1) variables defining various directory names
vars += bindir sbindir libexecdir mandir datadir localstatedir sharedstatedir unitdir builddir
# 2) variables defining build options:
#   with_testkeys: set to 1 to build snapd with test key built in
#   with_apparmor: set to 1 to build snapd with apparmor support
#   with_core_bits: set to 1 to build snapd with things needed for the core/snapd snap
#   with_alt_snap_mount_dir: set to 1 to build snapd with alternate snap mount directory
vars += with_testkeys with_apparmor with_core_bits with_alt_snap_mount_dir
# Verify that none of the variables are empty. This may happen if snapd.mk and
# distribution packaging generating snapd.defines.mk get out of sync.

$(foreach var,$(vars),$(if $(value $(var)),,$(error $(var) is empty or unset, check snapd.defines.mk)))

# ------------------------------------------------
# There are no more control knobs after this point
# ------------------------------------------------

# Import path of snapd.
import_path = github.com/snapcore/snapd

# Trusted account keys that should be present in production builds.
# These are used by check-trusted-account-keys target to verify that
# test keys are not accidentally included in production builds.
SNAPD_STORE_KEY_1 = -CvQKAwRQ5h3Ffn10FILJoEZUXOv6km9FwA80-Rcj-f-6jadQ89VRswHNiEB9Lxk
SNAPD_STORE_KEY_2 = d-JcZF9nD9eBw7bwMnH61x-bklnQOhQud1Is6o_cn2wTj8EYDi9musrIT9z2MdAa
SNAPD_REPAIR_ROOT_KEY = nttW6NfBXI_E-00u38W-KH6eiksfQNXuI7IiumoV49_zkbhM0sYTzSnFlwZC-W4t


# This is usually set by %make_install. It is defined here to avoid warnings or
# errors from referencing undefined variables.
DESTDIR? =

# Decide which of the two snap mount directories to use. This is not
# referencing localstatedir because the code copes with only those two values
# explicitly.
ifeq ($(with_alt_snap_mount_dir),1)
snap_mount_dir = /var/lib/snapd/snap
else
snap_mount_dir = /snap
endif

# The list of go binaries we are expected to build.
go_binaries = $(addprefix $(builddir)/, snap snapctl snap-seccomp snap-update-ns snap-exec snapd snapd-apparmor)

GO_TAGS = nosecboot
ifeq ($(with_testkeys),1)
GO_TAGS += withtestkeys
GO_TAGS += structuredlogging
endif

# Any additional tags common to all targets
GO_TAGS += $(EXTRA_GO_BUILD_TAGS)

GO_MOD=-mod=vendor
ifeq ($(with_vendor),0)
GO_MOD=-mod=readonly
endif

GO_STATIC_BUILDMODE = default
GO_STATIC_EXTLDFLAG = -static
ifeq ($(with_static_pie),1)
# override flags for building static binaries in PIE mode if supported
# by the host system
GO_STATIC_BUILDMODE = pie
GO_STATIC_EXTLDFLAG = -static-pie
endif

# Go -ldflags settings for static build. Can be overridden in snapd.defines.mk.
EXTRA_GO_STATIC_LDFLAGS ?= -linkmode external -extldflags="$(GO_STATIC_EXTLDFLAG)" $(EXTRA_GO_LDFLAGS)

# sourcedir is the path to the source directory tree (where the go source files are).
# This is used by prepare-build-tree to remove unnecessary code.
# For Debian/dh-golang, this would be: sourcedir=_build/src/github.com/snapcore/snapd
sourcedir ?= $(CURDIR)

# Prepare the build tree by removing code that is not used in non-embedded builds.
# This removes snap-bootstrap, snap-fde-keymgr, and secboot-related code that
# is only needed for embedded systems and UC20+ builds. This could be somewhat
# avoided if we had all the dependencies in Debian OR if dh-golang supported
# build tags properly.
.PHONY: prepare-build-tree
prepare-build-tree:
	# exclude certain parts that won't be used by debian
	find $(sourcedir)/cmd/snap-bootstrap -name "*.go" 2>/dev/null | xargs rm -f
	find $(sourcedir)/cmd/snap-fde-keymgr -name "*.go" 2>/dev/null | xargs rm -f
	find $(sourcedir)/gadget/install -name "*.go" -not -name "params.go" -not -name "install_placeholder.go" -not -name "kernel.go" 2>/dev/null | xargs rm -f
	# XXX: once dh-golang understands go build tags this would not be needed
	find $(sourcedir)/secboot/ -name "*.go" 2>/dev/null | grep -E '(.*_sb(_test)?\.go|.*_tpm(_test)?\.go|secboot_hooks.go|auth_requestor.go|keymgr/)' | xargs rm -f
	# Rename plainkey files to indicate they are secboot variants
	if [ -f $(sourcedir)/secboot/keys/plainkey.go ]; then mv $(sourcedir)/secboot/keys/plainkey.go $(sourcedir)/secboot/keys/plainkey_sb.go; fi
	if [ -f $(sourcedir)/secboot/keys/plainkey_test.go ]; then mv $(sourcedir)/secboot/keys/plainkey_test.go $(sourcedir)/secboot/keys/plainkey_sb_test.go; fi
	find $(sourcedir)/secboot/keys/ -name "*.go" 2>/dev/null | grep -E '(.*_sb(_test)?\.go)' | xargs rm -f
	find $(sourcedir)/boot/ -name "*.go" 2>/dev/null | grep -E '(.*_sb(_test)?\.go)' | xargs rm -f

# NOTE: This *depends* on building out of tree. Some of the built binaries
# conflict with directory names in the tree.
.PHONY: all
all: $(go_binaries)

# FIXME: not all Go toolchains we build with support '-B gobuildid', replace a
# random GNU build ID with something more predictable, use something similar to
# https://pagure.io/go-rpm-macros/c/1980932bf3a21890a9571effaa23fbe034fd388d
$(builddir)/snap: GO_TAGS += nomanagers
$(builddir)/snap $(builddir)/snap-seccomp $(builddir)/snapd-apparmor:
	go build -o $@ $(if $(GO_TAGS),-tags "$(GO_TAGS)") \
		-buildmode=pie \
		-ldflags="$(EXTRA_GO_LDFLAGS)" \
		$(GO_MOD) \
		$(EXTRA_GO_BUILD_FLAGS) \
		$(import_path)/cmd/$(notdir $@)

# Those three need to be built as static binaries. They run on the inside of a
# nearly-arbitrary mount namespace that does not contain anything we can depend
# on (no standard library, for example).
$(builddir)/snap-update-ns $(builddir)/snap-exec $(builddir)/snapctl:
	go build -o $@ -buildmode=$(GO_STATIC_BUILDMODE) \
		$(GO_MOD) \
		$(if $(GO_TAGS),-tags "$(GO_TAGS)") \
		-ldflags="$(EXTRA_GO_STATIC_LDFLAGS)" \
		$(EXTRA_GO_BUILD_FLAGS) \
		$(import_path)/cmd/$(notdir $@)

# Check that critical binaries are statically linked.
# These binaries execute inside mount namespaces and cannot depend on external libraries.
# builddir: the directory containing the built binaries (e.g., _build/bin)
.PHONY: check-static-binaries
check-static-binaries:
	@echo "Checking that critical binaries are statically linked..."
	@for binary in snap-exec snap-update-ns snapctl; do \
		if [ -f "$(builddir)/$$binary" ]; then \
			if ldd "$(builddir)/$$binary" >/dev/null 2>&1; then \
				echo "ERROR: $$binary is dynamically linked, must be static"; \
				ldd "$(builddir)/$$binary"; \
				exit 1; \
			fi; \
			echo "  $$binary: OK (static)"; \
		fi; \
	done
	@echo "All static binary checks passed."

# XXX see the note about build ID in rule for building 'snap'
# Snapd can be built with test keys. This is only used by the internal test
# suite to add test assertions. Do not enable this in distribution packages.
$(builddir)/snapd:
	go build -o $@ -buildmode=pie \
		-ldflags="$(EXTRA_GO_LDFLAGS)" \
		$(GO_MOD) \
		$(if $(GO_TAGS),-tags "$(GO_TAGS)") \
		$(EXTRA_GO_BUILD_FLAGS) \
		$(import_path)/cmd/$(notdir $@)

# Know how to create certain directories.
$(addprefix $(DESTDIR),$(libexecdir)/snapd $(bindir) $(mandir)/man8 /$(sharedstatedir)/snapd $(localstatedir)/cache/snapd $(snap_mount_dir)):
	install -m 755 -d $@

.PHONY: install

# Install snap into /usr/bin/.
install:: $(builddir)/snap | $(DESTDIR)$(bindir)
	install -m 755 $^ $|

# Install snapctl snapd, snap-{exec,update-ns,seccomp} into /usr/lib/snapd/
install:: $(addprefix $(builddir)/,snapctl snapd snap-exec snap-update-ns snap-seccomp snapd-apparmor) | $(DESTDIR)$(libexecdir)/snapd
	install -m 755 $^ $|

# Ensure /usr/bin/snapctl is a symlink to /usr/lib/snapd/snapctl
install:: | $(DESTDIR)$(bindir)
	ln -s $(libexecdir)/snapd/snapctl $|/snapctl

# Generate and install man page for snap command
install:: $(builddir)/snap | $(DESTDIR)$(mandir)/man8
	$(builddir)/snap help --man > $|/snap.8

# Install the directory structure in /var/lib/snapd
install::
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/apparmor/profiles
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/apparmor/snap-confine
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/assertions
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/cache
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/cgroup
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/cookie
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/dbus-1/services
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/dbus-1/system-services
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/desktop/applications
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/device
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/environment
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/hostfs
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/inhibit
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/lib/gl
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/lib/gl32
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/lib/glvnd
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/lib/vulkan
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/mount
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/seccomp/bpf
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/sequence
	install -m 755 -d $(DESTDIR)/$(sharedstatedir)/snapd/snaps

# Touch files that are ghosted by the package. Those are _NOT_ installed but
# this way the package manager knows about them belonging to the package.
install:: | $(DESTDIR)/$(sharedstatedir)/snapd
	touch $|/state.json
	touch $|/system-key

install:: | $(DESTDIR)$(localstatedir)/cache/snapd
	touch $|/sections
	touch $|/names
	touch $|/commands

install:: | $(DESTDIR)$(snap_mount_dir)
	touch $|/README


# Install the /snap/bin directory
install::
	install -m 755 -d $(DESTDIR)$(snap_mount_dir)/bin

# Install misc directories:
install::
	install -m 755 -d $(DESTDIR)$(localstatedir)/cache/snapd
	install -m 755 -d $(DESTDIR)$(datadir)/polkit-1/actions

# Do not ship snap-preseed. It is currently only useful on ubuntu and tailored
# for preseeding of ubuntu cloud images due to certain assumptions about
# runtime environment of the host and of the preseeded image.
install::
	rm -f $(DESTDIR)$(bindir)/snap-preseed

ifeq ($(with_core_bits),0)
# Remove systemd units that are only used on core devices.
install::
	rm -f $(addprefix $(DESTDIR)$(unitdir)/,snapd.autoimport.service snapd.system-shutdown.service snapd.snap-repair.timer snapd.snap-repair.service snapd.core-fixup.service snapd.recovery-chooser-trigger.service)

# Remove fixup script that is only used on core devices.
install::
	rm -f $(DESTDIR)$(libexecdir)/snapd/snapd.core-fixup.sh

# Remove system-shutdown helper that is only used on core devices.
install::
	rm -f $(DESTDIR)$(libexecdir)/snapd/system-shutdown
endif

ifeq ($(with_apparmor),0)
# Don't ship apparmor helper service when AppArmor is not enabled.
install::
	rm -f $(DESTDIR)$(unitdir)/snapd.apparmor.service
	rm -f $(DESTDIR)$(libexecdir)/snapd/snapd-apparmor
endif

# Tests use C.UTF-8 because some some code depend on this for fancy Unicode
# output that unit tests do not mock.
.PHONY: check
check:
	LC_ALL=C.UTF-8 go test $(GO_MOD) $(if $(GO_TAGS),-tags "$(GO_TAGS)") $(import_path)/...

.PHONY: clean
clean:
	rm -f $(go_binaries)

# Check that production builds contain only the expected trusted account keys.
# This verifies that test keys are not accidentally included in production builds.
# builddir: the directory containing the built binaries (e.g., _build/bin)
.PHONY: check-trusted-account-keys
check-trusted-account-keys:
	@echo "Checking trusted account keys in snapd and related binaries..."
	@# Check snapd binary (2 keys expected)
	@if [ -f "$(builddir)/snapd" ]; then \
		count=$$(strings $(builddir)/snapd | grep -c -E "public-key-sha3-384: [a-zA-Z0-9_-]{64}"); \
		if [ "$$count" -ne 2 ]; then \
			echo "ERROR: Expected 2 public keys in snapd, found $$count"; \
			exit 1; \
		fi; \
		strings $(builddir)/snapd | grep -q "^public-key-sha3-384: $(SNAPD_STORE_KEY_1)$$" || \
			{ echo "ERROR: snapd store key 1 not found"; exit 1; }; \
		strings $(builddir)/snapd | grep -q "^public-key-sha3-384: $(SNAPD_STORE_KEY_2)$$" || \
			{ echo "ERROR: snapd store key 2 not found"; exit 1; }; \
		echo "  snapd: OK (2 keys)"; \
	fi
	@# Check snap-bootstrap if it exists (Ubuntu 16.04+)
	@if [ -f "$(builddir)/snap-bootstrap" ]; then \
		count=$$(strings $(builddir)/snap-bootstrap | grep -c -E "public-key-sha3-384: [a-zA-Z0-9_-]{64}"); \
		if [ "$$count" -ne 2 ]; then \
			echo "ERROR: Expected 2 public keys in snap-bootstrap, found $$count"; \
			exit 1; \
		fi; \
		strings $(builddir)/snap-bootstrap | grep -q "^public-key-sha3-384: $(SNAPD_STORE_KEY_1)$$" || \
			{ echo "ERROR: snap-bootstrap store key 1 not found"; exit 1; }; \
		strings $(builddir)/snap-bootstrap | grep -q "^public-key-sha3-384: $(SNAPD_STORE_KEY_2)$$" || \
			{ echo "ERROR: snap-bootstrap store key 2 not found"; exit 1; }; \
		echo "  snap-bootstrap: OK (2 keys)"; \
	fi
	@# Check snap-preseed if it exists (Ubuntu 16.04+)
	@if [ -f "$(builddir)/snap-preseed" ]; then \
		count=$$(strings $(builddir)/snap-preseed | grep -c -E "public-key-sha3-384: [a-zA-Z0-9_-]{64}"); \
		if [ "$$count" -ne 2 ]; then \
			echo "ERROR: Expected 2 public keys in snap-preseed, found $$count"; \
			exit 1; \
		fi; \
		strings $(builddir)/snap-preseed | grep -q "^public-key-sha3-384: $(SNAPD_STORE_KEY_1)$$" || \
			{ echo "ERROR: snap-preseed store key 1 not found"; exit 1; }; \
		strings $(builddir)/snap-preseed | grep -q "^public-key-sha3-384: $(SNAPD_STORE_KEY_2)$$" || \
			{ echo "ERROR: snap-preseed store key 2 not found"; exit 1; }; \
		echo "  snap-preseed: OK (2 keys)"; \
	fi
	@# Check snap-repair (3 keys expected: 2 common + 1 repair-root)
	@if [ -f "$(builddir)/snap-repair" ]; then \
		count=$$(strings $(builddir)/snap-repair | grep -c -E "public-key-sha3-384: [a-zA-Z0-9_-]{64}"); \
		if [ "$$count" -ne 3 ]; then \
			echo "ERROR: Expected 3 public keys in snap-repair, found $$count"; \
			exit 1; \
		fi; \
		strings $(builddir)/snap-repair | grep -q "^public-key-sha3-384: $(SNAPD_STORE_KEY_1)$$" || \
			{ echo "ERROR: snap-repair store key 1 not found"; exit 1; }; \
		strings $(builddir)/snap-repair | grep -q "^public-key-sha3-384: $(SNAPD_STORE_KEY_2)$$" || \
			{ echo "ERROR: snap-repair store key 2 not found"; exit 1; }; \
		strings $(builddir)/snap-repair | grep -q "^public-key-sha3-384: $(SNAPD_REPAIR_ROOT_KEY)$$" || \
			{ echo "ERROR: snap-repair repair-root key not found"; exit 1; }; \
		echo "  snap-repair: OK (3 keys)"; \
	fi
	@echo "All trusted account key checks passed."
