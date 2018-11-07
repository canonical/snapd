# This makefile is designed to be invoked from a build tree, not from the
# source tree. This is particularly suited for building RPM packages. The goal
# of this makefile is to *reduce* the duplication, *avoid* unintended
# inconsistency between packaging of snapd across distributions.
#
# The makefile has three control knobs, some of which are currently unused.
# Those knobs influence the build and installation process. The knobs cannot be
# undefined if they are actually used by the Makefile below.

# Set to 1 to build snapd with test key built in.
with_test_keys?=$(error with_test_keys is unset, must be 1 or 0)
# Set to 1 to build snapd with apparmor support.
with_apparmor?=$(error with_apparmor is unset, must be 1 or 0)
# Set to 1 to build snapd with things needed for the core/snapd snap.
with_core_bits?=$(error with_core_bits is unset, must be 1 or 0)
# Set to 1 to build snapd with alternate snap mount directory.
with_alt_snap_mount_dir=$(error with_alt_snap_mount_dir is unset, must be 1 or 0)

# ------------------------------------------------
# There are no more control knobs after this point
# ------------------------------------------------

# Import path of snapd.
import_path=github.com/snapcore/snapd

# Set a set of variables to some well-known directories. If rpm is installed
# then it is used as canonical reference. In absence of rpm the values are
# built out of Debian-ish defaults.
ifneq ($(shell which rpm),)
prefix:=$(shell rpm -E %{_prefix})
bindir:=$(shell rpm -E %{_bindir})
sbindir:=$(shell rpm -E %{_sbindir})
libexecdir:=$(shell rpm -E %{_libexecdir})
mandir:=$(shell rpm -E %{_mandir})
datadir:=$(shell rpm -E %{_datadir})
localstatedir:=$(shell rpm -E %{_localstatedir})
unitdir=$(shell rpm -E %{_unitdir})
else
$(error expected RPM to be used)
prefix=/usr
bindir=$(prefix)/bin
sbindir=$(prefix)/sbin
libexecdir=$(prefix)/lib
mandir=$(prefix)/
datadir=$(prefix)/share
localstatedir=/var
unitdir=$(prefix)/lib/systemd/system
endif

# Ensure that none of the directory variables are empty.
vars=bindir sbindir libexecdir mandir datadir localstatedir unitdir
$(foreach var,$(vars),$(if $(value $(var)),,$(error $(var) is empty or unset)))

# This is usually set by %make_install. It is defined here to avoid warnings or
# errors from referencing undefined variables.
DESTDIR?=

# Decide which of the two snap mount directories to use. This is not
# referencing localstatedir because the code copes with only those two values
# explicitly.
ifeq ($(with_alt_snap_mount_dir),1)
snap_mount_dir=/var/lib/snapd/snap
else
snap_mount_dir=/snap
endif

# The list of go binaries we are expected to build.
go_binaries=snap snapctl snap-seccomp snap-update-ns snap-exec snapd

# NOTE: This *depends* on building out of tree. Some of the built binaries
# conflict with directory names in the tree.
.PHONY: all
all: $(go_binaries) 

snap snapctl snap-seccomp:
	go build -buildmode=pie $(import_path)/cmd/$@

# Those two need to be built as static binaries. They run on the inside of a
# nearly-arbitrary mount namespace that does not contain anything we can depend
# on (no standard library, for example).
snap-update-ns snap-exec:
	go build -buildmode=default -ldflags '-extldflags "-static"' $(import_path)/cmd/$@

# Snapd can be built with test keys. This is only used by the internal test
# suite to add test assertions. Do not enable this in distribution packages.
snapd:
ifeq ($(with_test_keys),1)
	go build -buildmode=pie -tags withtestkeys $(import_path)/cmd/$@
else
	go build -buildmode=pie $(import_path)/cmd/$@
endif

# Know how to create certain directories.
$(addprefix $(DESTDIR),$(libexecdir)/snapd $(bindir) $(mandir)/man8 $(localstatedir)/lib/snapd $(localstatedir)/cache/snapd $(snap_mount_dir)):
	install -d -m 755 $@

.PHONY: install

# Install snap into /usr/bin/.
install:: snap | $(DESTDIR)$(bindir)
	install -m 755 $^ $|

# Install snapctl snapd, snap-{exec,update-ns,seccomp} into /usr/lib/snapd/
install:: snapctl snapd snap-exec snap-update-ns snap-seccomp | $(DESTDIR)$(libexecdir)/snapd
	install -m 755 $^ $|

# Ensure /usr/bin/snapctl is a symlink to /usr/lib/snapd/snapctl
install:: | $(DESTDIR)$(bindir)
	ln -s $(libexecdir)/snapd/snapctl $|/snapctl

# Generate and install man page for snap command
install:: snap | $(DESTDIR)$(mandir)/man8
	./snap help --man > $|/snap.8

# Install the directory structure in /var/lib/snapd
install::
	install -d $(DESTDIR)$(localstatedir)/lib/snapd/apparmor/profiles
	install -d $(DESTDIR)$(localstatedir)/lib/snapd/apparmor/snap-confine
	install -d $(DESTDIR)$(localstatedir)/lib/snapd/assertions
	install -d $(DESTDIR)$(localstatedir)/lib/snapd/cache
	install -d $(DESTDIR)$(localstatedir)/lib/snapd/cookie
	install -d $(DESTDIR)$(localstatedir)/lib/snapd/desktop/applications
	install -d $(DESTDIR)$(localstatedir)/lib/snapd/device
	install -d $(DESTDIR)$(localstatedir)/lib/snapd/hostfs
	install -d $(DESTDIR)$(localstatedir)/lib/snapd/lib/{gl,gl32,vulkan}
	install -d $(DESTDIR)$(localstatedir)/lib/snapd/mount
	install -d $(DESTDIR)$(localstatedir)/lib/snapd/seccomp/bpf
	install -d $(DESTDIR)$(localstatedir)/lib/snapd/sequence
	install -d $(DESTDIR)$(localstatedir)/lib/snapd/snaps

# Touch files that are ghosted by the package. Those are _NOT_ installed but
# this way the package manager knows about them belonging to the package.
install:: | $(DESTDIR)$(localstatedir)/lib/snapd
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
	install -d $(DESTDIR)$(snap_mount_dir)/bin

# Install misc directories: 
install::
	install -d $(DESTDIR)$(localstatedir)/cache/snapd
	install -d $(DESTDIR)$(datadir)/polkit-1/actions

# Remove traces of ubuntu-core-launcher. It is a phased-out executable that is
# still partially present in the tree but should be removed in the subsequent
# release.
install::
	rm -f $(DESTDIR)$(bindir)/ubuntu-core-launcher

ifeq ($(with_core_bits),0)
# Remove systemd units that are only used on core devices.
install::
	rm -f $(addprefix $(DESTDIR)$(unitdir)/,snapd.autoimport.service snapd.system-shutdown.service snapd.snap-repair.timer snapd.snap-repair.service snapd.core-fixup.service)

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
	LC_ALL=C.UTF-8 go test $(import_path)/...

.PHONY: clean
clean:
	rm -f $(go_binaries) 
