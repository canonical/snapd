
EXTRA_DIST = VERSION snap-confine/PORTING
CLEANFILES =
TESTS =
libexec_PROGRAMS =
dist_man_MANS =
noinst_PROGRAMS =
noinst_LIBRARIES =

# FIXME: get this via something like pkgconf once it is defined there
SYSTEMD_SYSTEM_ENV_GENERATOR_DIR=/usr/lib/systemd/system-environment-generators

CHECK_CFLAGS = -Wall -Wextra -Wmissing-prototypes -Wstrict-prototypes \
	-Wno-missing-field-initializers -Wno-unused-parameter

# Make all warnings errors when building for unit tests
if WITH_UNIT_TESTS
CHECK_CFLAGS += -Werror
endif

subdirs = snap-confine snap-discard-ns system-shutdown libsnap-confine-private snap-gdb-shim snapd-generator snapd-env-generator

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
if WITH_UNIT_TESTS
check-unit-tests: snap-confine/unit-tests system-shutdown/unit-tests libsnap-confine-private/unit-tests
	$(HAVE_VALGRIND) ./libsnap-confine-private/unit-tests
	SNAP_DEVICE_HELPER=$(srcdir)/snap-confine/snap-device-helper $(HAVE_VALGRIND) ./snap-confine/unit-tests
	$(HAVE_VALGRIND) ./system-shutdown/unit-tests
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
	sudo install -D -m 6755 snap-confine/snap-confine $(DESTDIR)$(libexecdir)/snap-confine
	sudo install -m 644 snap-confine/snap-confine.apparmor $(DESTDIR)/etc/apparmor.d/$(patsubst .%,%,$(subst /,.,$(libexecdir))).snap-confine.real
	sudo install -d -m 755 $(DESTDIR)/var/lib/snapd/apparmor/snap-confine/
	sudo apparmor_parser -r snap-confine/snap-confine.apparmor
	sudo install -m 755 snap-update-ns/snap-update-ns $(DESTDIR)$(libexecdir)/snap-update-ns
	sudo install -m 755 snap-seccomp/snap-seccomp $(DESTDIR)$(libexecdir)/snap-seccomp

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
libsnap_confine_private_a_CFLAGS = $(CHECK_CFLAGS)

noinst_LIBRARIES += libsnap-confine-private-debug.a
libsnap_confine_private_debug_a_SOURCES = $(libsnap_confine_private_a_SOURCES)
libsnap_confine_private_debug_a_CFLAGS = $(CHECK_CFLAGS) -DSNAP_CONFINE_DEBUG_BUILD=1

if WITH_UNIT_TESTS
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
libsnap_confine_private_unit_tests_CFLAGS = $(CHECK_CFLAGS) $(GLIB_CFLAGS)
libsnap_confine_private_unit_tests_LDADD = $(GLIB_LIBS)
libsnap_confine_private_unit_tests_CFLAGS += -D_ENABLE_FAULT_INJECTION
libsnap_confine_private_unit_tests_STATIC =

if STATIC_LIBCAP
libsnap_confine_private_unit_tests_STATIC += -lcap
else
libsnap_confine_private_unit_tests_LDADD += -lcap
endif  # STATIC_LIBCAP

# Use a hacked rule if we're doing static build. This allows us to inject the LIBS += .. rule below.
libsnap-confine-private/unit-tests$(EXEEXT): $(libsnap_confine_private_unit_tests_OBJECTS) $(libsnap_confine_private_unit_tests_DEPENDENCIES) $(EXTRA_libsnap_confine_private_unit_tests_DEPENDENCIES) libsnap-confine-private/$(am__dirstamp)
	@rm -f libsnap-confine-private/unit-tests$(EXEEXT)
	$(AM_V_CCLD)$(libsnap_confine_private_unit_tests_LINK) $(libsnap_confine_private_unit_tests_OBJECTS) $(libsnap_confine_private_unit_tests_LDADD) $(LIBS)

libsnap-confine-private/unit-tests$(EXEEXT): LIBS += -Wl,-Bstatic $(libsnap_confine_private_unit_tests_STATIC) -Wl,-Bdynamic
endif  # WITH_UNIT_TESTS

##
## decode-mount-opts
##

noinst_PROGRAMS += decode-mount-opts/decode-mount-opts

decode_mount_opts_decode_mount_opts_SOURCES = \
	decode-mount-opts/decode-mount-opts.c
decode_mount_opts_decode_mount_opts_LDADD = libsnap-confine-private.a
decode_mount_opts_decode_mount_opts_STATIC =

if STATIC_LIBCAP
decode_mount_opts_decode_mount_opts_STATIC += -lcap
else
decode_mount_opts_decode_mount_opts_LDADD += -lcap
endif  # STATIC_LIBCAP

# XXX: this makes automake generate decode_mount_opts_decode_mount_opts_LINK
decode_mount_opts_decode_mount_opts_CFLAGS = -D_fake

# Use a hacked rule if we're doing static build. This allows us to inject the LIBS += .. rule below.
decode-mount-opts/decode-mount-opts$(EXEEXT): $(decode_mount_opts_decode_mount_opts_OBJECTS) $(decode_mount_opts_decode_mount_opts_DEPENDENCIES) $(EXTRA_decode_mount_opts_decode_mount_opts_DEPENDENCIES) libsnap-confine-private/$(am__dirstamp)
	@rm -f decode-mount-opts/decode-mount-opts$(EXEEXT)
	$(AM_V_CCLD)$(decode_mount_opts_decode_mount_opts_LINK) $(decode_mount_opts_decode_mount_opts_OBJECTS) $(decode_mount_opts_decode_mount_opts_LDADD) $(LIBS)

decode-mount-opts/decode-mount-opts$(EXEEXT): LIBS += -Wl,-Bstatic $(decode_mount_opts_decode_mount_opts_STATIC) -Wl,-Bdynamic

##
## snap-confine
##

libexec_PROGRAMS += snap-confine/snap-confine
if HAVE_RST2MAN
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

snap_confine_snap_confine_CFLAGS = $(CHECK_CFLAGS) $(AM_CFLAGS) -DLIBEXECDIR=\"$(libexecdir)\" -DNATIVE_LIBDIR=\"$(libdir)\"
snap_confine_snap_confine_LDFLAGS = $(AM_LDFLAGS)
snap_confine_snap_confine_LDADD = libsnap-confine-private.a
snap_confine_snap_confine_CFLAGS += $(LIBUDEV_CFLAGS)
snap_confine_snap_confine_LDADD += $(snap_confine_snap_confine_extra_libs)
# _STATIC is where we collect statically linked in libraries
snap_confine_snap_confine_STATIC =
# use a separate variable instead of snap_confine_snap_confine_LDADD to collect
# all external libraries, this way it can be reused in
# snap_confine_snap_confine_debug_LDADD withouth applying any text
# transformations
snap_confine_snap_confine_extra_libs = $(LIBUDEV_LIBS)

if STATIC_LIBCAP
snap_confine_snap_confine_STATIC += -lcap
else
snap_confine_snap_confine_extra_libs += -lcap
endif  # STATIC_LIBCAP

# Use a hacked rule if we're doing static build. This allows us to inject the LIBS += .. rule below.
snap-confine/snap-confine$(EXEEXT): $(snap_confine_snap_confine_OBJECTS) $(snap_confine_snap_confine_DEPENDENCIES) $(EXTRA_snap_confine_snap_confine_DEPENDENCIES) libsnap-confine-private/$(am__dirstamp)
	@rm -f snap-confine/snap-confine$(EXEEXT)
	$(AM_V_CCLD)$(snap_confine_snap_confine_LINK) $(snap_confine_snap_confine_OBJECTS) $(snap_confine_snap_confine_LDADD) $(LIBS)

snap-confine/snap-confine$(EXEEXT): LIBS += -Wl,-Bstatic $(snap_confine_snap_confine_STATIC) -Wl,-Bdynamic -pthread

# This is here to help fix rpmlint hardening issue.
# https://en.opensuse.org/openSUSE:Packaging_checks#non-position-independent-executable
snap_confine_snap_confine_CFLAGS += $(SUID_CFLAGS)
snap_confine_snap_confine_LDFLAGS += $(SUID_LDFLAGS)

if SECCOMP
snap_confine_snap_confine_SOURCES += \
	snap-confine/seccomp-support.c \
	snap-confine/seccomp-support.h
snap_confine_snap_confine_CFLAGS += $(SECCOMP_CFLAGS)
if STATIC_LIBSECCOMP
snap_confine_snap_confine_STATIC += $(shell pkg-config --static --libs libseccomp)
else
snap_confine_snap_confine_extra_libs += $(SECCOMP_LIBS)
endif  # STATIC_LIBSECCOMP
endif  # SECCOMP

if APPARMOR
snap_confine_snap_confine_CFLAGS += $(APPARMOR_CFLAGS)
if STATIC_LIBAPPARMOR
snap_confine_snap_confine_STATIC += $(shell pkg-config --static --libs libapparmor)
else
snap_confine_snap_confine_extra_libs += $(APPARMOR_LIBS)
endif  # STATIC_LIBAPPARMOR
endif  # APPARMOR

# an extra build that has additional debugging enabled at compile time

noinst_PROGRAMS += snap-confine/snap-confine-debug
snap_confine_snap_confine_debug_SOURCES = $(snap_confine_snap_confine_SOURCES)
snap_confine_snap_confine_debug_CFLAGS = $(snap_confine_snap_confine_CFLAGS)
snap_confine_snap_confine_debug_LDFLAGS = $(snap_confine_snap_confine_LDFLAGS)
snap_confine_snap_confine_debug_LDADD = libsnap-confine-private-debug.a $(snap_confine_snap_confine_extra_libs)
snap_confine_snap_confine_debug_CFLAGS += -DSNAP_CONFINE_DEBUG_BUILD=1
snap_confine_snap_confine_debug_STATIC = $(snap_confine_snap_confine_STATIC)

# Use a hacked rule if we're doing static build. This allows us to inject the LIBS += .. rule below.
snap-confine/snap-confine-debug$(EXEEXT): $(snap_confine_snap_confine_debug_OBJECTS) $(snap_confine_snap_confine_debug_DEPENDENCIES) $(EXTRA_snap_confine_snap_confine_debug_DEPENDENCIES) libsnap-confine-private/$(am__dirstamp)
	@rm -f snap-confine/snap-confine-debug$(EXEEXT)
	$(AM_V_CCLD)$(snap_confine_snap_confine_debug_LINK) $(snap_confine_snap_confine_debug_OBJECTS) $(snap_confine_snap_confine_debug_LDADD) $(LIBS)

snap-confine/snap-confine-debug$(EXEEXT): LIBS += -Wl,-Bstatic $(snap_confine_snap_confine_debug_STATIC) -Wl,-Bdynamic -pthread

if WITH_UNIT_TESTS
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
	snap-confine/snap-confine-args-test.c \
	snap-confine/snap-device-helper-test.c
snap_confine_unit_tests_CFLAGS = $(snap_confine_snap_confine_CFLAGS) $(GLIB_CFLAGS)
snap_confine_unit_tests_LDADD = $(snap_confine_snap_confine_LDADD) $(GLIB_LIBS)
snap_confine_unit_tests_LDFLAGS = $(snap_confine_snap_confine_LDFLAGS)
snap_confine_unit_tests_STATIC = $(snap_confine_snap_confine_STATIC)

# Use a hacked rule if we're doing static build. This allows us to inject the LIBS += .. rule below.
snap-confine/unit-tests$(EXEEXT): $(snap_confine_unit_tests_OBJECTS) $(snap_confine_unit_tests_DEPENDENCIES) $(EXTRA_snap_confine_unit_tests_DEPENDENCIES) libsnap-confine-private/$(am__dirstamp)
	@rm -f snap-confine/unit-tests$(EXEEXT)
	$(AM_V_CCLD)$(snap_confine_unit_tests_LINK) $(snap_confine_unit_tests_OBJECTS) $(snap_confine_unit_tests_LDADD) $(LIBS)

snap-confine/unit-tests$(EXEEXT): LIBS += -Wl,-Bstatic $(snap_confine_unit_tests_STATIC) -Wl,-Bdynamic -pthread
endif  # WITH_UNIT_TESTS

if HAVE_RST2MAN
snap-confine/%.1: snap-confine/%.rst
	mkdir -p snap-confine
	$(HAVE_RST2MAN) $^ > $@
endif

snap-confine/snap-confine.apparmor: snap-confine/snap-confine.apparmor.in Makefile
	sed -e 's,[@]LIBEXECDIR[@],$(libexecdir),g' -e 's,[@]SNAP_MOUNT_DIR[@],$(SNAP_MOUNT_DIR),' <$< >$@

# Install the apparmor profile
#
# NOTE: the funky make functions here just convert /foo/bar/froz into
# foo.bar.froz The inner subst replaces slashes with dots and the outer
# patsubst strips the leading dot
install-data-local:: snap-confine/snap-confine.apparmor
if APPARMOR
	install -d -m 755 $(DESTDIR)/etc/apparmor.d/
	install -m 644 snap-confine/snap-confine.apparmor $(DESTDIR)/etc/apparmor.d/$(patsubst .%,%,$(subst /,.,$(libexecdir))).snap-confine
endif
	install -d -m 755 $(DESTDIR)/var/lib/snapd/apparmor/snap-confine/

# NOTE: The 'void' directory *has to* be chmod 000
install-data-local::
	install -d -m 000 $(DESTDIR)/var/lib/snapd/void

install-exec-hook::
if CAPS_OVER_SETUID
# Ensure that snap-confine has CAP_SYS_ADMIN capability
	setcap cap_sys_admin=pe $(DESTDIR)$(libexecdir)/snap-confine
else
# Ensure that snap-confine is u+s,g+s (setuid and setgid)
	chmod 6755 $(DESTDIR)$(libexecdir)/snap-confine
endif

##
## snap-mgmt
##

libexec_PROGRAMS += snap-mgmt/snap-mgmt

snap-mgmt/snap-mgmt: snap-mgmt/snap-mgmt.sh.in Makefile snap-mgmt/$(am__dirstamp)
	sed -e 's,[@]SNAP_MOUNT_DIR[@],$(SNAP_MOUNT_DIR),' <$< >$@


##
## ubuntu-core-launcher
##

install-exec-hook::
	install -d -m 755 $(DESTDIR)$(bindir)
	ln -sf $(libexecdir)/snap-confine $(DESTDIR)$(bindir)/ubuntu-core-launcher

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
	install -d -m 755 $(DESTDIR)$(libexecdir)
	install -m 755 $(srcdir)/snap-confine/snap-device-helper $(DESTDIR)$(libexecdir)

##
## snap-discard-ns
##

libexec_PROGRAMS += snap-discard-ns/snap-discard-ns
if HAVE_RST2MAN
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
snap_discard_ns_snap_discard_ns_CFLAGS = $(CHECK_CFLAGS) $(AM_CFLAGS)
snap_discard_ns_snap_discard_ns_LDFLAGS = $(AM_LDFLAGS)
snap_discard_ns_snap_discard_ns_LDADD = libsnap-confine-private.a
snap_discard_ns_snap_discard_ns_STATIC =

if APPARMOR
snap_discard_ns_snap_discard_ns_CFLAGS += $(APPARMOR_CFLAGS)
if STATIC_LIBAPPARMOR
snap_discard_ns_snap_discard_ns_STATIC += $(shell pkg-config --static --libs libapparmor)
else
snap_discard_ns_snap_discard_ns_LDADD += $(APPARMOR_LIBS)
endif  # STATIC_LIBAPPARMOR
endif  # APPARMOR

if STATIC_LIBCAP
snap_discard_ns_snap_discard_ns_STATIC += -lcap
else
snap_discard_ns_snap_discard_ns_LDADD += -lcap
endif  # STATIC_LIBCAP

# Use a hacked rule if we're doing static build. This allows us to inject the LIBS += .. rule below.
snap-discard-ns/snap-discard-ns$(EXEEXT): $(snap_discard_ns_snap_discard_ns_OBJECTS) $(snap_discard_ns_snap_discard_ns_DEPENDENCIES) $(EXTRA_snap_discard_ns_snap_discard_ns_DEPENDENCIES) snap-discard-ns/$(am__dirstamp)
	@rm -f snap-discard-ns/snap-discard-ns$(EXEEXT)
	$(AM_V_CCLD)$(snap_discard_ns_snap_discard_ns_LINK) $(snap_discard_ns_snap_discard_ns_OBJECTS) $(snap_discard_ns_snap_discard_ns_LDADD) $(LIBS)

snap-discard-ns/snap-discard-ns$(EXEEXT): LIBS += -Wl,-Bstatic $(snap_discard_ns_snap_discard_ns_STATIC) -Wl,-Bdynamic -pthread

if HAVE_RST2MAN
snap-discard-ns/%.5: snap-discard-ns/%.rst
	mkdir -p snap-discard-ns
	$(HAVE_RST2MAN) $^ > $@
endif

##
## system-shutdown
##

libexec_PROGRAMS += system-shutdown/system-shutdown

system_shutdown_system_shutdown_SOURCES = \
	system-shutdown/system-shutdown-utils.c \
	system-shutdown/system-shutdown-utils.h \
	system-shutdown/system-shutdown.c
system_shutdown_system_shutdown_LDADD = libsnap-confine-private.a
system_shutdown_system_shutdown_CFLAGS = $(CHECK_CFLAGS) $(filter-out -fPIE -pie,$(CFLAGS)) -static
system_shutdown_system_shutdown_LDFLAGS = $(filter-out -fPIE -pie,$(LDFLAGS)) -static

if WITH_UNIT_TESTS
noinst_PROGRAMS += system-shutdown/unit-tests
system_shutdown_unit_tests_SOURCES = \
	libsnap-confine-private/unit-tests-main.c \
	libsnap-confine-private/unit-tests.c \
	system-shutdown/system-shutdown-utils-test.c
system_shutdown_unit_tests_LDADD = libsnap-confine-private.a
system_shutdown_unit_tests_CFLAGS = $(GLIB_CFLAGS)
system_shutdown_unit_tests_LDADD +=  $(GLIB_LIBS)
endif

##
## snap-gdb-shim
##

libexec_PROGRAMS += snap-gdb-shim/snap-gdb-shim

snap_gdb_shim_snap_gdb_shim_SOURCES = \
	snap-gdb-shim/snap-gdb-shim.c

snap_gdb_shim_snap_gdb_shim_LDADD = libsnap-confine-private.a

##
## snapd-generator
##

libexec_PROGRAMS += snapd-generator/snapd-generator

snapd_generator_snapd_generator_SOURCES = snapd-generator/main.c
snapd_generator_snapd_generator_LDADD = libsnap-confine-private.a

##
## snapd-env-generator
##

systemdsystemenvgeneratordir=$(SYSTEMD_SYSTEM_ENV_GENERATOR_DIR)
systemdsystemenvgenerator_PROGRAMS = snapd-env-generator/snapd-env-generator

snapd_env_generator_snapd_env_generator_SOURCES = snapd-env-generator/main.c
snapd_env_generator_snapd_env_generator_LDADD = libsnap-confine-private.a
EXTRA_DIST += snapd-env-generator/snapd-env-generator.rst

if HAVE_RST2MAN
snapd-env-generator/%.7: snapd-env-generator/%.rst
	mkdir -p snapd-env-generator
	$(HAVE_RST2MAN) $^ > $@

dist_man_MANS += snapd-env-generator/snapd-env-generator.7
CLEANFILES += snapd-env-generator/snapd-env-generator.7
endif

##
## snapd-apparmor
##

EXTRA_DIST += snapd-apparmor/snapd-apparmor

install-exec-local::
	install -d -m 755 $(DESTDIR)$(libexecdir)
if APPARMOR
	install -m 755 $(srcdir)/snapd-apparmor/snapd-apparmor $(DESTDIR)$(libexecdir)
endif
