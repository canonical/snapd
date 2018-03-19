include ../build-config

srcdir = $(realpath .)

BUILDDIR = build
EXTRA_DIST = VERSION snap-confine/PORTING
CLEANFILES =
CLEAN =
TESTS =
libexecsnapd_PROGRAMS =
dist_man_MANS =
noinst_PROGRAMS =
noinst_LIBRARIES =

RST2MAN = $(shell which rst2man)
VALGRIND = $(shell which valgrind)

LIBUDEV_LIBS = $(shell pkg-config --libs libudev)
LIBUDEV_CFLAGS = $(shell pkg-config --cflags libudev)

LIBCAP_LIBS = $(shell pkg-config --libs libcap)
LIBCAP_CFLAGS = $(shell pkg-config --cflags libcap)
STATIC_LIBCAP ?= 0

ifeq ($(SECCOMP),1)
SECCOMP_LIBS = $(shell pkg-config --libs libseccomp)
SECCOMP_CFLAGS = $(shell pkg-config --cflags libseccomp)
STATIC_LIBSECCOMP ?= 0
endif

ifeq ($(APPARMOR),1)
APPARMOR_LIBS = $(shell pkg-config --libs libapparmor)
APPARMOR_CFLAGS = $(shell pkg-config --cflags libapparmor)
STATIC_LIBAPPARMOR ?= 0
endif

GLIB_LIBS = $(shell pkg-config --libs glib-2.0)
GLIB_CFLAGS = $(shell pkg-config --cflags glib-2.0)

CHECK_CFLAGS = -Wall -Wextra -Wmissing-prototypes -Wstrict-prototypes \
	-Wno-missing-field-initializers -Wno-unused-parameter
# -std=gnu11 is the default for recent versions of gcc
BASE_CFLAGS = $(CFLAGS) -D_GNU_SOURCE -std=gnu11

# generate compilation template
# 1 - variables grouping prefix (eg. libsnap_confine_private_a)
define COMPILE_template =
$(1)_OBJECTS = $$(patsubst %.c, $$(BUILDDIR)/$(1)/%.o, $$(filter %.c, $$($(1)_SOURCES)))

$$(BUILDDIR)/$(1)_dirstamp:
	mkdir -p $$(sort $$(dir $$($(1)_OBJECTS)))
	touch $$(BUILDDIR)/$(1)_dirstamp

$$(BUILDDIR)/$(1)/%.o: %.c config.h $$(BUILDDIR)/$(1)_dirstamp
	$$(CC) -c -o $$@ \
		-MD -MP -MT $$@ $$< \
		$$($(1)_CFLAGS) \
		-I. -DHAVE_CONFIG_H

.PHONY: $(1)_CLEAN
$(1)_CLEAN:
	rm -rf $$(sort $$(dir $$($(1)_OBJECTS)))
	rm -f $$(BUILDDIR)/$(1)_dirstamp

CLEAN += $(1)_CLEAN

-include $$(patsubst %.o, %.d, $$($(1)_OBJECTS))
endef

# Make all warnings errors when building for unit tests
ifeq ($(WITH_UNIT_TESTS),1)
CHECK_CFLAGS += -Werror
endif

.PHONY: all
all: build-all

VERSION:
	cd .. && ./mkversion.sh

CLEANFILES += config.h VERSION
config.h: config.h.in Makefile VERSION ../build-config
	sed < $< > $@ \
		-e s:[@]SNAP_MOUNT_DIR[@]:$(SNAP_MOUNT_DIR):g \
		-e s:[@]PACKAGE_VERSION[@]:$(shell cat VERSION):g \
		-e s:[@]LIB32_DIR[@]:$(LIB32_DIR):g \
		-e s:[@]HOST_ARCH_TRIPLET[@]:$(HOST_ARCH_TRIPLET):g \
		-e s:[@]HOST_ARCH32_TRIPLET[@]:$(HOST_ARCH32_TRIPLET):g

	if [ "$(APPARMOR)" = "1" ]; then \
		echo "#define HAVE_APPARMOR" >> $@; \
	fi
	if [ "$(SECCOMP)" = "1" ]; then \
		echo "#define HAVE_SECCOMP" >> $@; \
	fi
	if [ "$(MERGED_USR)" = "1" ]; then \
		echo "#define MERGED_USR" >> $@; \
	fi
	if [ "$(NVIDIA_BIARCH)" = "1" ]; then \
		echo "#define NVIDIA_BIARCH" >> $@; \
	fi
	if [ "$(NVIDIA_MULTIARCH)" = "1" ]; then \
		echo "#define NVIDIA_MULTIARCH" >> $@; \
	fi
	if [ "$(CAPS_OVER_SETUID)" = "1" ]; then \
		echo "#define CAPS_OVER_SETUID" >> $@; \
	fi

subdirs = snap-confine snap-discard-ns system-shutdown libsnap-confine-private snap-gdb-shim snapd-generator

# Run check-syntax when checking
# TODO: conver those to autotools-style tests later
check: check-unit-tests

# Force particular coding style on all source and header files.
.PHONY: check-syntax-c
check-syntax-c:
	echo "WARNING: check-syntax-c produces different results for different version of indent"
	echo "Your version of indent: `indent --version`"
	@d=`mktemp -d`; \
	trap 'rm -rf $d' EXIT; \
	for f in $(foreach dir,$(subdirs),$(wildcard $(srcdir)/$(dir)/*.[ch])) ; do \
	       out="$$d/`basename $$f.out`"; \
	       echo "Checking $$f ... "; \
	       HOME=$(srcdir) indent "$$f" -o "$$out"; \
	       diff -Naur "$$f" "$$out" || exit 1; \
	done;

.PHONY: check-unit-tests
ifeq ($(WITH_UNIT_TESTS),1)
check-unit-tests: snap-confine/unit-tests system-shutdown/unit-tests libsnap-confine-private/unit-tests
	$(VALGRIND) ./libsnap-confine-private/unit-tests
	$(VALGRIND) ./snap-confine/unit-tests
	$(VALGRIND) ./system-shutdown/unit-tests
else
check-unit-tests:
	echo "unit tests are disabled (rebuild with --enable-unit-tests)"
endif

.PHONY: fmt
fmt: $(foreach dir,$(subdirs),$(wildcard $(srcdir)/$(dir)/*.[ch]))
	HOME=$(srcdir) indent $^

# The hack target helps devlopers work on snap-confine on their live system by
# installing a fresh copy of snap confine and the appropriate apparmor profile.
.PHONY: hack
hack: snap-confine/snap-confine snap-confine/snap-confine.apparmor snap-update-ns/snap-update-ns snap-seccomp/snap-seccomp
	sudo install -D -m 6755 snap-confine/snap-confine $(DESTDIR)$(libexecsnapddir)/snap-confine
	sudo install -m 644 snap-confine/snap-confine.apparmor $(DESTDIR)/etc/apparmor.d/$(patsubst .%,%,$(subst /,.,$(libexecsnapddir))).snap-confine.real
	sudo install -d -m 755 $(DESTDIR)/var/lib/snapd/apparmor/snap-confine/
	sudo apparmor_parser -r snap-confine/snap-confine.apparmor
	sudo install -m 755 snap-update-ns/snap-update-ns $(DESTDIR)$(libexecsnapddir)/snap-update-ns
	sudo install -m 755 snap-seccomp/snap-seccomp $(DESTDIR)$(libexecsnapddir)/snap-seccomp

# for the hack target also:
snap-update-ns/snap-update-ns: snap-update-ns/*.go snap-update-ns/*.[ch]
	cd snap-update-ns && GOPATH=$(or $(GOPATH),$(realpath $(srcdir)/../../../../..)) go build -i -v
snap-seccomp/snap-seccomp: snap-seccomp/*.go
	cd snap-seccomp && GOPATH=$(or $(GOPATH),$(realpath $(srcdir)/../../../../..)) go build -i -v

##
## libsnap-confine-private.a
##

noinst_LIBRARIES += libsnap-confine-private.a

libsnap_confine_private_a_SOURCES = \
	libsnap-confine-private/cgroup-freezer-support.c \
	libsnap-confine-private/cgroup-freezer-support.h \
	libsnap-confine-private/classic.c \
	libsnap-confine-private/classic.h \
	libsnap-confine-private/cleanup-funcs.c \
	libsnap-confine-private/cleanup-funcs.h \
	libsnap-confine-private/error.c \
	libsnap-confine-private/error.h \
	libsnap-confine-private/fault-injection.c \
	libsnap-confine-private/fault-injection.h \
	libsnap-confine-private/locking.c \
	libsnap-confine-private/locking.h \
	libsnap-confine-private/mount-opt.c \
	libsnap-confine-private/mount-opt.h \
	libsnap-confine-private/mountinfo.c \
	libsnap-confine-private/mountinfo.h \
	libsnap-confine-private/privs.c \
	libsnap-confine-private/privs.h \
	libsnap-confine-private/secure-getenv.c \
	libsnap-confine-private/secure-getenv.h \
	libsnap-confine-private/snap.c \
	libsnap-confine-private/snap.h \
	libsnap-confine-private/string-utils.c \
	libsnap-confine-private/string-utils.h \
	libsnap-confine-private/utils.c \
	libsnap-confine-private/utils.h
libsnap_confine_private_a_CFLAGS = $(BASE_CFLAGS) $(CHECK_CFLAGS)

libsnap_confine_private_a_OBJECTS = $(patsubst %.c, $(BUILDDIR)/libsnap_confine_private_a/%.o, $(filter %.c, $(libsnap_confine_private_a_SOURCES)))
libsnap-confine-private.a: $(libsnap_confine_private_a_OBJECTS)
	$(AR) cr $@ $^

$(eval $(call COMPILE_template,libsnap_confine_private_a))

noinst_LIBRARIES += libsnap-confine-private-debug.a
libsnap_confine_private_debug_a_SOURCES = $(libsnap_confine_private_a_SOURCES)
libsnap_confine_private_debug_a_CFLAGS = $(BASE_CFLAGS) $(CHECK_CFLAGS) -DSNAP_CONFINE_DEBUG_BUILD=1

libsnap_confine_private_debug_a_OBJECTS = $(patsubst %.c, $(BUILDDIR)/libsnap_confine_private_debug_a/%.o, $(filter %.c, $(libsnap_confine_private_debug_a_SOURCES)))
libsnap-confine-private-debug.a: $(libsnap_confine_private_debug_a_OBJECTS)
	$(AR) cr $@ $^

$(eval $(call COMPILE_template,libsnap_confine_private_debug_a))

ifeq ($(WITH_UNIT_TESTS),1)
noinst_PROGRAMS += libsnap-confine-private/unit-tests
libsnap_confine_private_unit_tests_SOURCES = \
	libsnap-confine-private/classic-test.c \
	libsnap-confine-private/cleanup-funcs-test.c \
	libsnap-confine-private/error-test.c \
	libsnap-confine-private/fault-injection-test.c \
	libsnap-confine-private/locking-test.c \
	libsnap-confine-private/mount-opt-test.c \
	libsnap-confine-private/mountinfo-test.c \
	libsnap-confine-private/privs-test.c \
	libsnap-confine-private/secure-getenv-test.c \
	libsnap-confine-private/snap-test.c \
	libsnap-confine-private/string-utils-test.c \
	libsnap-confine-private/test-utils.c \
	libsnap-confine-private/test-utils-test.c \
	libsnap-confine-private/unit-tests-main.c \
	libsnap-confine-private/unit-tests.c \
	libsnap-confine-private/unit-tests.h \
	libsnap-confine-private/utils-test.c
libsnap_confine_private_unit_tests_CFLAGS = $(BASE_CFLAGS) $(CHECK_CFLAGS) $(GLIB_CFLAGS)
libsnap_confine_private_unit_tests_LDADD = $(GLIB_LIBS)
libsnap_confine_private_unit_tests_CFLAGS += -D_ENABLE_FAULT_INJECTION
libsnap_confine_private_unit_tests_STATIC =

ifeq ($(STATIC_LIBCAP),1)
libsnap_confine_private_unit_tests_STATIC += -lcap
else
libsnap_confine_private_unit_tests_LDADD += -lcap
endif  # STATIC_LIBCAP

# libsnap_confine_private_unit_tests_OBJECTS = $(patsubst %.c, $(BUILDDIR)/libsnap_confine_private_unit_tests/%.o, $(filter %.c, $(libsnap_confine_private_unit_tests_SOURCES)))

$(eval $(call COMPILE_template,libsnap_confine_private_unit_tests))

libsnap-confine-private/unit-tests: $(libsnap_confine_private_unit_tests_OBJECTS)
	$(CC) -o $@ \
		$(libsnap_confine_private_unit_tests_OBJECTS) \
		$(libsnap_confine_private_unit_tests_LDADD) \
		-Wl,-Bstatic $(libsnap_confine_private_unit_tests_STATIC) -Wl,-Bdynamic

endif  # WITH_UNIT_TESTS

##
## decode-mount-opts
##

noinst_PROGRAMS += decode-mount-opts/decode-mount-opts

decode_mount_opts_decode_mount_opts_SOURCES = \
	decode-mount-opts/decode-mount-opts.c
decode_mount_opts_decode_mount_opts_LDADD = libsnap-confine-private.a
decode_mount_opts_decode_mount_opts_STATIC =

decode_mount_opts_decode_mount_opts_OBJECTS = $(patsubst %.c, %.o, $(filter %.c, $(decode_mount_opts_decode_mount_opts_SOURCES)))

ifeq ($(STATIC_LIBCAP),1)
decode_mount_opts_decode_mount_opts_STATIC += -lcap
else
decode_mount_opts_decode_mount_opts_LDADD += -lcap
endif  # STATIC_LIBCAP

decode_mount_opts_decode_mount_opts_CFLAGS = $(BASE_CFLAGS) -D_fake

$(eval $(call COMPILE_template,decode_mount_opts_decode_mount_opts))

decode-mount-opts/decode-mount-opts: $(decode_mount_opts_decode_mount_opts_OBJECTS) libsnap-confine-private.a Makefile
	$(CC) -o $@ \
		$(decode_mount_opts_decode_mount_opts_OBJECTS) \
		$(decode_mount_opts_decode_mount_opts_LDADD) \
		-Wl,-Bstatic $(decode_mount_opts_decode_mount_opts_STATIC) -Wl,-Bdynamic


##
## snap-confine
##

libexecsnapd_PROGRAMS += snap-confine/snap-confine
ifneq ($(RST2MAN),)
dist_man_MANS += snap-confine/snap-confine.1
CLEANFILES += snap-confine/snap-confine.1
endif
EXTRA_DIST += snap-confine/snap-confine.rst
EXTRA_DIST += snap-confine/snap-confine.apparmor.in

snap_confine_snap_confine_SOURCES = \
	snap-confine/apparmor-support.c \
	snap-confine/apparmor-support.h \
	snap-confine/cookie-support.c \
	snap-confine/cookie-support.h \
	snap-confine/mount-support-nvidia.c \
	snap-confine/mount-support-nvidia.h \
	snap-confine/mount-support.c \
	snap-confine/mount-support.h \
	snap-confine/ns-support.c \
	snap-confine/ns-support.h \
	snap-confine/quirks.c \
	snap-confine/quirks.h \
	snap-confine/snap-confine-args.c \
	snap-confine/snap-confine-args.h \
	snap-confine/snap-confine.c \
	snap-confine/udev-support.c \
	snap-confine/udev-support.h \
	snap-confine/user-support.c \
	snap-confine/user-support.h

ifeq ($(SECCOMP),1)
snap_confine_snap_confine_SOURCES += \
	snap-confine/seccomp-support.c \
	snap-confine/seccomp-support.h
endif

snap_confine_snap_confine_OBJECTS = $(patsubst %.c, %.o, $(filter %.c, $(snap_confine_snap_confine_SOURCES)))

snap_confine_snap_confine_CFLAGS = $(BASE_CFLAGS) $(CHECK_CFLAGS) $(AM_CFLAGS) -DLIBEXECDIR=\"$(libexecdir)\" -DNATIVE_LIBDIR=\"$(libdir)\"
snap_confine_snap_confine_LDFLAGS = $(LDFLAGS)
snap_confine_snap_confine_LDADD = libsnap-confine-private.a
snap_confine_snap_confine_CFLAGS += $(LIBUDEV_CFLAGS)
# _STATIC is where we collect statically linked in libraries
snap_confine_snap_confine_STATIC =
# use a separate variable instead of snap_confine_snap_confine_LDADD to collect
# all external libraries, this way it can be reused in
# snap_confine_snap_confine_debug_LDADD withouth applying any text
# transformations
snap_confine_snap_confine_extra_libs =
snap_confine_snap_confine_LDADD += $(LIBUDEV_LIBS)

ifeq ($(STATIC_LIBCAP),1)
snap_confine_snap_confine_STATIC += -lcap
else
snap_confine_snap_confine_extra_libs += -lcap
endif  # STATIC_LIBCAP

ifeq ($(SECCOMP),1)
snap_confine_snap_confine_CFLAGS += $(SECCOMP_CFLAGS)
ifeq ($(STATIC_LIBSECCOMP),1)
snap_confine_snap_confine_STATIC += $(shell pkg-config --static --libs libseccomp)
else
snap_confine_snap_confine_extra_libs += $(SECCOMP_LIBS)
endif  # STATIC_LIBSECCOMP
endif  # SECCOMP

ifeq ($(APPARMOR),1)
snap_confine_snap_confine_CFLAGS += $(APPARMOR_CFLAGS)
ifeq ($(STATIC_LIBAPPARMOR),1)
snap_confine_snap_confine_STATIC += $(shell pkg-config --static --libs libapparmor)
else
snap_confine_snap_confine_extra_libs += $(APPARMOR_LIBS)
endif  # STATIC_LIBAPPARMOR
endif  # APPARMOR

snap_confine_snap_confine_LDADD += $(snap_confine_snap_confine_extra_libs)

$(eval $(call COMPILE_template,snap_confine_snap_confine))

# Use a hacked rule if we're doing static build. This allows us to inject the LIBS += .. rule below.
snap-confine/snap-confine: $(snap_confine_snap_confine_OBJECTS) libsnap-confine-private.a
	$(CC) -o $@ $(snap_confine_snap_confine_OBJECTS) \
		libsnap-confine-private.a \
		$(snap_confine_snap_confine_LDFLAGS) \
		$(snap_confine_snap_confine_LDADD) \
		-Wl,-Bstatic $(snap_confine_snap_confine_STATIC) -Wl,-Bdynamic -pthread

# This is here to help fix rpmlint hardening issue.
# https://en.opensuse.org/openSUSE:Packaging_checks#non-position-independent-executable
snap_confine_snap_confine_CFLAGS += $(SUID_CFLAGS)
snap_confine_snap_confine_LDFLAGS += $(SUID_LDFLAGS)


# an extra build that has additional debugging enabled at compile time

noinst_PROGRAMS += snap-confine/snap-confine-debug
snap_confine_snap_confine_debug_SOURCES = $(snap_confine_snap_confine_SOURCES)
snap_confine_snap_confine_debug_CFLAGS = $(snap_confine_snap_confine_CFLAGS)
snap_confine_snap_confine_debug_LDFLAGS = $(snap_confine_snap_confine_LDFLAGS)
snap_confine_snap_confine_debug_LDADD = libsnap-confine-private-debug.a $(snap_confine_snap_confine_extra_libs)
snap_confine_snap_confine_debug_CFLAGS += -DSNAP_CONFINE_DEBUG_BUILD=1
snap_confine_snap_confine_debug_STATIC = $(snap_confine_snap_confine_STATIC)

$(eval $(call COMPILE_template,snap_confine_snap_confine_debug))

# Use a hacked rule if we're doing static build. This allows us to inject the LIBS += .. rule below.
snap-confine/snap-confine-debug: $(snap_confine_snap_confine_debug_OBJECTS) libsnap-confine-private-debug.a
	$(CC) -o $@ $(snap_confine_snap_confine_OBJECTS) \
		$(snap_confine_snap_confine_LDFLAGS) \
		$(snap_confine_snap_confine_LDADD) \
		-Wl,-Bstatic $(snap_confine_snap_confine_STATIC) -Wl,-Bdynamic -pthread

ifeq ($(WITH_UNIT_TESTS),1)
noinst_PROGRAMS += snap-confine/unit-tests
snap_confine_unit_tests_SOURCES = \
	libsnap-confine-private/test-utils.c \
	libsnap-confine-private/unit-tests-main.c \
	libsnap-confine-private/unit-tests.c \
	libsnap-confine-private/unit-tests.h \
	snap-confine/apparmor-support.c \
	snap-confine/apparmor-support.h \
	snap-confine/cookie-support-test.c \
	snap-confine/mount-support-test.c \
	snap-confine/ns-support-test.c \
	snap-confine/quirks.c \
	snap-confine/quirks.h \
	snap-confine/snap-confine-args-test.c
snap_confine_unit_tests_CFLAGS = $(snap_confine_snap_confine_CFLAGS) $(GLIB_CFLAGS)
snap_confine_unit_tests_LDADD = $(snap_confine_snap_confine_LDADD) $(GLIB_LIBS)
snap_confine_unit_tests_LDFLAGS = $(snap_confine_snap_confine_LDFLAGS)
snap_confine_unit_tests_STATIC = $(snap_confine_snap_confine_STATIC)

$(eval $(call COMPILE_template,snap_confine_unit_tests))

snap-confine/unit-tests: $(snap_confine_unit_tests_OBJECTS) libsnap-confine-private.a
	$(CC) -o $@ $(snap_confine_unit_tests_OBJECTS) \
		libsnap-confine-private.a \
		$(snap_confine_unit_tests_LDFLAGS) \
		$(snap_confine_unit_tests_LDADD) \
		-Wl,-Bstatic $(snap_confine_unit_tests_STATIC) -Wl,-Bdynamic -pthread
endif

ifneq ($(RST2MAN),)
snap-confine/%.1: snap-confine/%.rst
	mkdir -p snap-confine
	$(RST2MAN) $^ > $@
endif

snap-confine/snap-confine.apparmor: snap-confine/snap-confine.apparmor.in Makefile
	sed -e 's,[@]LIBEXECSNAPDDIR[@],$(libexecsnapddir),g' -e 's,[@]SNAP_MOUNT_DIR[@],$(SNAP_MOUNT_DIR),' <$< >$@

# Install the apparmor profile
#
# NOTE: the funky make functions here just convert /foo/bar/froz into
# foo.bar.froz The inner subst replaces slashes with dots and the outer
# patsubst strips the leading dot
install-data-local:: snap-confine/snap-confine.apparmor
ifeq ($(APPARMOR),1)
	install -d -m 755 $(DESTDIR)/etc/apparmor.d/
	install -m 644 snap-confine/snap-confine.apparmor $(DESTDIR)/etc/apparmor.d/$(patsubst .%,%,$(subst /,.,$(libexecsnapddir))).snap-confine
endif
	install -d -m 755 $(DESTDIR)/var/lib/snapd/apparmor/snap-confine/

# NOTE: The 'void' directory *has to* be chmod 000
install-data-local::
	install -d -m 000 $(DESTDIR)/var/lib/snapd/void

install-exec-hook::
ifeq ($(CAPS_OVER_SETUID),1)
# Ensure that snap-confine has CAP_SYS_ADMIN capability
	setcap cap_sys_admin=pe $(DESTDIR)$(libexecsnapddir)/snap-confine
else
# Ensure that snap-confine is u+s,g+s (setuid and setgid)
	chmod 6755 $(DESTDIR)$(libexecsnapddir)/snap-confine
endif

##
## snap-mgmt
##

libexecsnapd_PROGRAMS += snap-mgmt/snap-mgmt

snap-mgmt/snap-mgmt: snap-mgmt/snap-mgmt.sh.in Makefile snap-mgmt/$(am__dirstamp)
	sed -e 's,[@]SNAP_MOUNT_DIR[@],$(SNAP_MOUNT_DIR),' <$< >$@


##
## ubuntu-core-launcher
##

install-exec-hook::
	install -d -m 755 $(DESTDIR)$(bindir)
	ln -sf $(libexecsnapddir)/snap-confine $(DESTDIR)$(bindir)/ubuntu-core-launcher

##
## snap-device-helper
##

EXTRA_DIST += \
	snap-confine/snap-device-helper

# NOTE: This makes distcheck fail but it is required for udev, so go figure.
# http://www.gnu.org/software/automake/manual/automake.html#Hard_002dCoded-Install-Paths
#
# Install support script for udev rules
install-exec-local::
	install -d -m 755 $(DESTDIR)$(libexecsnapddir)
	install -m 755 $(srcdir)/snap-confine/snap-device-helper $(DESTDIR)$(libexecsnapddir)

##
## snap-discard-ns
##

libexecsnapd_PROGRAMS += snap-discard-ns/snap-discard-ns
ifneq ($(RST2MAN),)
dist_man_MANS += snap-discard-ns/snap-discard-ns.5
CLEANFILES += snap-discard-ns/snap-discard-ns.5
endif
EXTRA_DIST += snap-discard-ns/snap-discard-ns.rst

snap_discard_ns_snap_discard_ns_SOURCES = \
	snap-confine/ns-support.c \
	snap-confine/ns-support.h \
	snap-confine/apparmor-support.c \
	snap-confine/apparmor-support.h \
	snap-discard-ns/snap-discard-ns.c
snap_discard_ns_snap_discard_ns_CFLAGS = $(BASE_CFLAGS) $(CHECK_CFLAGS) $(AM_CFLAGS)
snap_discard_ns_snap_discard_ns_LDFLAGS = $(LDFLAGS)
snap_discard_ns_snap_discard_ns_LDADD = libsnap-confine-private.a
snap_discard_ns_snap_discard_ns_STATIC =

ifeq ($(APPARMOR),1)
snap_discard_ns_snap_discard_ns_CFLAGS += $(APPARMOR_CFLAGS)
ifeq ($(STATIC_LIBAPPARMOR),1)
snap_discard_ns_snap_discard_ns_STATIC += $(shell pkg-config --static --libs libapparmor)
else
snap_discard_ns_snap_discard_ns_LDADD += $(APPARMOR_LIBS)
endif  # STATIC_LIBAPPARMOR
endif  # APPARMOR

ifeq ($(STATIC_LIBCAP),1)
snap_discard_ns_snap_discard_ns_STATIC += -lcap
else
snap_discard_ns_snap_discard_ns_LDADD += -lcap
endif  # STATIC_LIBCAP

$(eval $(call COMPILE_template,snap_discard_ns_snap_discard_ns))

snap-discard-ns/snap-discard-ns: $(snap_discard_ns_snap_discard_ns_OBJECTS) libsnap-confine-private.a
	$(CC) -o $@ $(snap_discard_ns_snap_discard_ns_OBJECTS) \
		$(snap_discard_ns_snap_discard_ns_LDFLAGS) \
		$(snap_discard_ns_snap_discard_ns_LDADD) \
		-Wl,-Bstatic $(snap_discard_ns_snap_discard_ns_STATIC) -Wl,-Bdynamic -pthread

ifneq ($(RST2MAN),)
snap-discard-ns/%.5: snap-discard-ns/%.rst
	mkdir -p snap-discard-ns
	$(RST2MAN) $^ > $@
endif

##
## system-shutdown
##

libexecsnapd_PROGRAMS += system-shutdown/system-shutdown

system_shutdown_system_shutdown_SOURCES = \
	system-shutdown/system-shutdown-utils.c \
	system-shutdown/system-shutdown-utils.h \
	system-shutdown/system-shutdown.c
system_shutdown_system_shutdown_LDADD = libsnap-confine-private.a
system_shutdown_system_shutdown_CFLAGS = $(BASE_CFLAGS) $(CHECK_CFLAGS) $(filter-out -fPIE -pie,$(CFLAGS)) -static
system_shutdown_system_shutdown_LDFLAGS = $(filter-out -fPIE -pie,$(LDFLAGS)) -static

$(eval $(call COMPILE_template,system_shutdown_system_shutdown))

system-shutdown/system-shutdown: $(system_shutdown_system_shutdown_OBJECTS) libsnap-confine-private.a
	$(CC) -o $@ $(system_shutdown_system_shutdown_OBJECTS) \
		$(system_shutdown_system_shutdown_LDFLAGS) \
		$(system_shutdown_system_shutdown_LDADD) \
		-Wl,-Bstatic $(system_shutdown_system_shutdown_STATIC) -Wl,-Bdynamic -pthread

ifeq ($(WITH_UNIT_TESTS),1)
noinst_PROGRAMS += system-shutdown/unit-tests
system_shutdown_unit_tests_SOURCES = \
	libsnap-confine-private/unit-tests-main.c \
	libsnap-confine-private/unit-tests.c \
	system-shutdown/system-shutdown-utils-test.c
system_shutdown_unit_tests_LDADD = libsnap-confine-private.a
system_shutdown_unit_tests_CFLAGS = $(GLIB_CFLAGS)
system_shutdown_unit_tests_LDADD +=  $(GLIB_LIBS)

$(eval $(call COMPILE_template,system_shutdown_unit_tests))

system-shutdown/unit-tests: $(system_shutdown_unit_tests_OBJECTS) libsnap-confine-private.a
	$(CC) -o $@ $(system_shutdown_unit_tests_OBJECTS) \
		$(system_shutdown_unit_tests_LDFLAGS) \
		$(system_shutdown_unit_tests_LDADD) \
		-Wl,-Bstatic $(system_shutdown_unit_tests_STATIC) -Wl,-Bdynamic -pthread
endif

##
## snap-gdb-shim
##

libexecsnapd_PROGRAMS += snap-gdb-shim/snap-gdb-shim

snap_gdb_shim_snap_gdb_shim_SOURCES = \
	snap-gdb-shim/snap-gdb-shim.c

snap_gdb_shim_snap_gdb_shim_CFLAGS = $(BASE_CFLAGS) $(CHECK_CFLAGS)
snap_gdb_shim_snap_gdb_shim_LDADD = libsnap-confine-private.a

$(eval $(call COMPILE_template,snap_gdb_shim_snap_gdb_shim))

snap-gdb-shim/snap-gdb-shim: $(snap_gdb_shim_snap_gdb_shim_OBJECTS) libsnap-confine-private.a
	$(CC) -o $@ $(snap_gdb_shim_snap_gdb_shim_OBJECTS) \
		$(snap_gdb_shim_snap_gdb_shim_LDFLAGS) \
		$(snap_gdb_shim_snap_gdb_shim_LDADD) \
		-Wl,-Bstatic $(snap_gdb_shim_snap_gdb_shim_STATIC) -Wl,-Bdynamic -pthread

##
## snapd-generator
##

libexecsnapd_PROGRAMS += snapd-generator/snapd-generator

snapd_generator_snapd_generator_SOURCES = snapd-generator/main.c
snapd_generator_snapd_generator_LDADD = libsnap-confine-private.a

$(eval $(call COMPILE_template,snapd_generator_snapd_generator))

snapd-generator/snapd-generator: $(snapd_generator_snapd_generator_OBJECTS) libsnap-confine-private.a
	$(CC) -o $@ $(snapd_generator_snapd_generator_OBJECTS) \
		$(snapd_generator_snapd_generator_LDFLAGS) \
		$(snapd_generator_snapd_generator_LDADD) \
		-Wl,-Bstatic $(snapd_generator_snapd_generator_STATIC) -Wl,-Bdynamic -pthread

.PHONY: build-all
build-all: $(libexecsnapd_PROGRAMS) $(dist_man_MANS) $(noinst_PROGRAMS) $(noinst_LIBRARIES)

.PHONY: clean
clean: $(CLEAN)
	rm -f $(libexecsnapd_PROGRAMS) $(dist_man_MANS) $(noinst_PROGRAMS) $(noinst_LIBRARIES)
	rm -f $(CLEANFILES)

.PHONY: install-programs
install-programs: $(libexecsnapd_PROGRAMS)
	mkdir -p $(DESTDIR)$(libexecsnapddir)
	install -t $(DESTDIR)$(libexecsnapddir) -m0755 $(libexecsnapd_PROGRAMS)

.PHONY: install-manpages
install-manpages: $(dist_man_MANS)
	for mp in $(dist_man_MANS); do \
		mandir=$$(echo $$mp | cut -f2 -d.); \
		mkdir -p $(DESTDIR)$(datadir)/man/man$${mandir} && \
		install -t $(DESTDIR)$(datadir)/man/man$${mandir} -m 0644 $$mp; \
	done

install-exec: install-programs install-exec-hook install-exec-local

install-data: install-manpages install-data-local

.PHONY: install install-exec install-programs install-data install-manpages
install: build-all install-exec install-data
