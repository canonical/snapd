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
endif

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
		-ldflags="-w -B 0x$$(head -c20 /dev/urandom|od -An -tx1|tr -d ' \n')" \
		-mod=vendor \
		$(import_path)/cmd/$(notdir $@)

# Those three need to be built as static binaries. They run on the inside of a
# nearly-arbitrary mount namespace that does not contain anything we can depend
# on (no standard library, for example).
$(builddir)/snap-update-ns $(builddir)/snap-exec $(builddir)/snapctl:
	# Explicit request to use an external linker, otherwise extldflags may not be
	# used
	go build -o $@ -buildmode=default -mod=vendor \
		$(if $(GO_TAGS),-tags "$(GO_TAGS)") \
		-ldflags '-linkmode external -extldflags "-static"' \
		$(import_path)/cmd/$(notdir $@)

# XXX see the note about build ID in rule for building 'snap'
# Snapd can be built with test keys. This is only used by the internal test
# suite to add test assertions. Do not enable this in distribution packages.
$(builddir)/snapd:
	go build -o $@ -buildmode=pie \
		-ldflags="-w -B 0x$$(head -c20 /dev/urandom|od -An -tx1|tr -d ' \n')" \
		-mod=vendor \
		$(if $(GO_TAGS),-tags "$(GO_TAGS)") \
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
	LC_ALL=C.UTF-8 go test -mod=vendor $(import_path)/...

.PHONY: clean
clean:
	rm -f $(go_binaries) 
