EXTRA_DIST = VERSION snap-wrap/PORTING
CLEANFILES =
TESTS =
libexec_PROGRAMS =
dist_man_MANS =
noinst_PROGRAMS =
noinst_LIBRARIES =

subdirs = snap-wrap snap-discard-ns system-shutdown libsnap-wrap-private snap-update-ns

# Run check-syntax when checking
# TODO: conver those to autotools-style tests later
check: check-syntax-c check-syntax-sh check-unit-tests

# Force particular coding style on all source and header files.
.PHONY: check-syntax-c
check-syntax-c:
	@d=`mktemp -d`; \
	trap 'rm -rf $d' EXIT; \
	for f in $(foreach dir,$(subdirs),$(wildcard $(srcdir)/$(dir)/*.[ch])) ; do \
	       out="$$d/`basename $$f.out`"; \
	       echo "Checking $$f ... "; \
	       HOME=$(srcdir) indent "$$f" -o "$$out"; \
	       diff -Naur "$$f" "$$out" || exit 1; \
	done;

# Check for common shell errors
.PHONY: check-syntax-sh
check-syntax-sh: $(filter-out %.log %.trs,$(wildcard $(srcdir)/snap-wrap/tests/test_*)) snap-wrap/tests/common.sh
if HAVE_SHELLCHECK
	$(HAVE_SHELLCHECK) -x --format=gcc $^
else
	echo "shellcheck is not installed, skipping (non-critical) syntax test"
endif

.PHONY: check-unit-tests
if WITH_UNIT_TESTS
check-unit-tests: snap-wrap/unit-tests system-shutdown/unit-tests libsnap-wrap-private/unit-tests snap-update-ns/unit-tests
	$(HAVE_VALGRIND) ./libsnap-wrap-private/unit-tests
	$(HAVE_VALGRIND) ./snap-wrap/unit-tests
	$(HAVE_VALGRIND) ./snap-update-ns/unit-tests
	$(HAVE_VALGRIND) ./system-shutdown/unit-tests
else
check-unit-tests:
	echo "unit tests are disabled (rebuild with --enable-unit-tests)"
endif

.PHONY: fmt
fmt: $(foreach dir,$(subdirs),$(wildcard $(srcdir)/$(dir)/*.[ch]))
	HOME=$(srcdir) indent $^

# The hack target helps devlopers work on snap-wrap on their live system by
# installing a fresh copy of snap wrap and the appropriate apparmor profile.
.PHONY: hack
hack: snap-wrap/snap-wrap snap-wrap/snap-wrap.apparmor
	sudo install -D -m 4755 snap-wrap/snap-wrap $(DESTDIR)$(libexecdir)/snap-wrap
	sudo install -m 644 snap-wrap/snap-wrap.apparmor $(DESTDIR)/etc/apparmor.d/$(patsubst .%,%,$(subst /,.,$(libexecdir))).snap-wrap
	sudo apparmor_parser -r snap-wrap/snap-wrap.apparmor

##
## libsnap-wrap-private.a
##

noinst_LIBRARIES += libsnap-wrap-private.a

libsnap_wrap_private_a_SOURCES = \
	libsnap-wrap-private/classic.c \
	libsnap-wrap-private/classic.h \
	libsnap-wrap-private/cleanup-funcs.c \
	libsnap-wrap-private/cleanup-funcs.h \
	libsnap-wrap-private/error.c \
	libsnap-wrap-private/error.h \
	libsnap-wrap-private/fault-injection.c \
	libsnap-wrap-private/fault-injection.h \
	libsnap-wrap-private/mount-opt.c \
	libsnap-wrap-private/mount-opt.h \
	libsnap-wrap-private/mountinfo.c \
	libsnap-wrap-private/mountinfo.h \
	libsnap-wrap-private/privs.c \
	libsnap-wrap-private/privs.h \
	libsnap-wrap-private/secure-getenv.c \
	libsnap-wrap-private/secure-getenv.h \
	libsnap-wrap-private/snap.c \
	libsnap-wrap-private/snap.h \
	libsnap-wrap-private/string-utils.c \
	libsnap-wrap-private/string-utils.h \
	libsnap-wrap-private/utils.c \
	libsnap-wrap-private/utils.h

if WITH_UNIT_TESTS
noinst_PROGRAMS += libsnap-wrap-private/unit-tests
libsnap_wrap_private_unit_tests_SOURCES = \
	libsnap-wrap-private/classic-test.c \
	libsnap-wrap-private/cleanup-funcs-test.c \
	libsnap-wrap-private/error-test.c \
	libsnap-wrap-private/fault-injection-test.c \
	libsnap-wrap-private/mount-opt-test.c \
	libsnap-wrap-private/mountinfo-test.c \
	libsnap-wrap-private/privs-test.c \
	libsnap-wrap-private/secure-getenv-test.c \
	libsnap-wrap-private/snap-test.c \
	libsnap-wrap-private/string-utils-test.c \
	libsnap-wrap-private/unit-tests-main.c \
	libsnap-wrap-private/unit-tests.c \
	libsnap-wrap-private/unit-tests.h \
	libsnap-wrap-private/utils-test.c
libsnap_wrap_private_unit_tests_CFLAGS = $(GLIB_CFLAGS)
libsnap_wrap_private_unit_tests_LDADD = $(GLIB_LIBS)
libsnap_wrap_private_unit_tests_CFLAGS += -D_ENABLE_FAULT_INJECTION

# XXX: This injects a dependency on libcap in a way that makes automake happy
# and allows us to link libcap statically. We need to link in libcap statically
# as at this time adding runtime dependencies to snap-wrap is tricky.

libsnap-wrap-private/unit-tests$(EXEEXT): $(libsnap_wrap_private_unit_tests_OBJECTS) $(libsnap_wrap_private_unit_tests_DEPENDENCIES) $(EXTRA_libsnap_wrap_private_unit_tests_DEPENDENCIES) libsnap-wrap-private/$(am__dirstamp)
	@rm -f libsnap-wrap-private/unit-tests$(EXEEXT)
	$(AM_V_CCLD)$(libsnap_wrap_private_unit_tests_LINK) $(libsnap_wrap_private_unit_tests_OBJECTS) $(libsnap_wrap_private_unit_tests_LDADD) $(LIBS)

libsnap-wrap-private/unit-tests$(EXEEXT): LIBS += -Wl,-Bstatic -lcap -Wl,-Bdynamic

endif

##
## decode-mount-opts
##

noinst_PROGRAMS += decode-mount-opts/decode-mount-opts

decode_mount_opts_decode_mount_opts_SOURCES = \
	decode-mount-opts/decode-mount-opts.c
decode_mount_opts_decode_mount_opts_LDADD = libsnap-wrap-private.a
# XXX: this makes automake generate decode_mount_opts_decode_mount_opts_LINK
decode_mount_opts_decode_mount_opts_CFLAGS = -D_fake

decode-mount-opts/decode-mount-opts$(EXEEXT): $(decode_mount_opts_decode_mount_opts_OBJECTS) $(decode_mount_opts_decode_mount_opts_DEPENDENCIES) $(EXTRA_decode_mount_opts_decode_mount_opts_DEPENDENCIES) libsnap-wrap-private/$(am__dirstamp)
	@rm -f decode-mount-opts/decode-mount-opts$(EXEEXT)
	$(AM_V_CCLD)$(decode_mount_opts_decode_mount_opts_LINK) $(decode_mount_opts_decode_mount_opts_OBJECTS) $(decode_mount_opts_decode_mount_opts_LDADD) $(LIBS)

decode-mount-opts/decode-mount-opts$(EXEEXT): LIBS += -Wl,-Bstatic -lcap -Wl,-Bdynamic

##
## snap-wrap
##

all_tests = \
    snap-wrap/tests/test_bad_seccomp_filter_args \
    snap-wrap/tests/test_bad_seccomp_filter_args_clone \
    snap-wrap/tests/test_bad_seccomp_filter_args_null \
    snap-wrap/tests/test_bad_seccomp_filter_args_prctl \
    snap-wrap/tests/test_bad_seccomp_filter_args_prio \
    snap-wrap/tests/test_bad_seccomp_filter_args_quotactl \
    snap-wrap/tests/test_bad_seccomp_filter_args_socket \
    snap-wrap/tests/test_bad_seccomp_filter_args_termios \
    snap-wrap/tests/test_bad_seccomp_filter_length \
    snap-wrap/tests/test_bad_seccomp_filter_missing_trailing_newline \
    snap-wrap/tests/test_complain \
    snap-wrap/tests/test_complain_missed \
    snap-wrap/tests/test_noprofile \
    snap-wrap/tests/test_restrictions \
    snap-wrap/tests/test_restrictions_working \
    snap-wrap/tests/test_restrictions_working_args \
    snap-wrap/tests/test_restrictions_working_args_clone \
    snap-wrap/tests/test_restrictions_working_args_prctl \
    snap-wrap/tests/test_restrictions_working_args_prio \
    snap-wrap/tests/test_restrictions_working_args_quotactl \
    snap-wrap/tests/test_restrictions_working_args_socket \
    snap-wrap/tests/test_restrictions_working_args_termios \
    snap-wrap/tests/test_unrestricted \
    snap-wrap/tests/test_unrestricted_missed \
    snap-wrap/tests/test_whitelist

libexec_PROGRAMS += snap-wrap/snap-wrap
dist_man_MANS += snap-wrap/snap-wrap.5
CLEANFILES += snap-wrap/snap-wrap.5
EXTRA_DIST += snap-wrap/snap-wrap.rst
EXTRA_DIST += snap-wrap/snap-wrap.apparmor.in
EXTRA_DIST += $(all_tests) snap-wrap/tests/common.sh
if SECCOMP
if CONFINEMENT_TESTS
TESTS += $(all_tests)
endif
endif

snap_wrap_snap_wrap_SOURCES = \
	snap-wrap/apparmor-support.c \
	snap-wrap/apparmor-support.h \
	snap-wrap/mount-support-nvidia.c \
	snap-wrap/mount-support-nvidia.h \
	snap-wrap/mount-support.c \
	snap-wrap/mount-support.h \
	snap-wrap/ns-support.c \
	snap-wrap/ns-support.h \
	snap-wrap/quirks.c \
	snap-wrap/quirks.h \
	snap-wrap/snap-wrap-args.c \
	snap-wrap/snap-wrap-args.h \
	snap-wrap/snap-wrap.c \
	snap-wrap/udev-support.c \
	snap-wrap/udev-support.h \
	snap-wrap/user-support.c \
	snap-wrap/user-support.h

snap_wrap_snap_wrap_CFLAGS = -Wall -Werror $(AM_CFLAGS)
snap_wrap_snap_wrap_LDFLAGS = $(AM_LDFLAGS)
snap_wrap_snap_wrap_LDADD = libsnap-wrap-private.a
snap_wrap_snap_wrap_CFLAGS += $(LIBUDEV_CFLAGS)
snap_wrap_snap_wrap_LDADD += $(LIBUDEV_LIBS)

snap-wrap/snap-wrap$(EXEEXT): $(snap_wrap_snap_wrap_OBJECTS) $(snap_wrap_snap_wrap_DEPENDENCIES) $(EXTRA_snap_wrap_snap_wrap_DEPENDENCIES) libsnap-wrap-private/$(am__dirstamp)
	@rm -f snap-wrap/snap-wrap$(EXEEXT)
	$(AM_V_CCLD)$(snap_wrap_snap_wrap_LINK) $(snap_wrap_snap_wrap_OBJECTS) $(snap_wrap_snap_wrap_LDADD) $(LIBS)

snap-wrap/snap-wrap$(EXEEXT): LIBS += -Wl,-Bstatic -lcap -Wl,-Bdynamic

# This is here to help fix rpmlint hardening issue.
# https://en.opensuse.org/openSUSE:Packaging_checks#non-position-independent-executable
snap_wrap_snap_wrap_CFLAGS += $(SUID_CFLAGS)
snap_wrap_snap_wrap_LDFLAGS += $(SUID_LDFLAGS)

if SECCOMP
snap_wrap_snap_wrap_SOURCES += \
	snap-wrap/seccomp-support.c \
	snap-wrap/seccomp-support.h
snap_wrap_snap_wrap_CFLAGS += $(SECCOMP_CFLAGS)
snap_wrap_snap_wrap_LDADD += $(SECCOMP_LIBS)
endif

if APPARMOR
snap_wrap_snap_wrap_CFLAGS += $(APPARMOR_CFLAGS)
snap_wrap_snap_wrap_LDADD += $(APPARMOR_LIBS)
endif

# an extra build that has additional debugging enabled at compile time

noinst_PROGRAMS += snap-wrap/snap-wrap-debug
snap_wrap_snap_wrap_debug_SOURCES = $(snap_wrap_snap_wrap_SOURCES)
snap_wrap_snap_wrap_debug_CFLAGS = $(snap_wrap_snap_wrap_CFLAGS)
snap_wrap_snap_wrap_debug_LDFLAGS = $(snap_wrap_snap_wrap_LDFLAGS)
snap_wrap_snap_wrap_debug_LDADD = $(snap_wrap_snap_wrap_LDADD)
snap_wrap_snap_wrap_debug_CFLAGS += -DSNAP_WRAP_DEBUG_BUILD=1

snap-wrap/snap-wrap-debug$(EXEEXT): $(snap_wrap_snap_wrap_debug_OBJECTS) $(snap_wrap_snap_wrap_debug_DEPENDENCIES) $(EXTRA_snap_wrap_snap_wrap_debug_DEPENDENCIES) libsnap-wrap-private/$(am__dirstamp)
	@rm -f snap-wrap/snap-wrap-debug$(EXEEXT)
	$(AM_V_CCLD)$(snap_wrap_snap_wrap_debug_LINK) $(snap_wrap_snap_wrap_debug_OBJECTS) $(snap_wrap_snap_wrap_debug_LDADD) $(LIBS)

snap-wrap/snap-wrap-debug$(EXEEXT): LIBS += -Wl,-Bstatic -lcap -Wl,-Bdynamic

if WITH_UNIT_TESTS
noinst_PROGRAMS += snap-wrap/unit-tests
snap_wrap_unit_tests_SOURCES = \
	libsnap-wrap-private/unit-tests-main.c \
	libsnap-wrap-private/unit-tests.c \
	libsnap-wrap-private/unit-tests.h \
	snap-wrap/apparmor-support.c \
	snap-wrap/apparmor-support.h \
	snap-wrap/mount-support-test.c \
	snap-wrap/ns-support-test.c \
	snap-wrap/quirks.c \
	snap-wrap/quirks.h \
	snap-wrap/snap-wrap-args-test.c
snap_wrap_unit_tests_CFLAGS = $(snap_wrap_snap_wrap_CFLAGS) $(GLIB_CFLAGS)
snap_wrap_unit_tests_LDADD = $(snap_wrap_snap_wrap_LDADD) $(GLIB_LIBS)
snap_wrap_unit_tests_LDFLAGS = $(snap_wrap_snap_wrap_LDFLAGS)

snap-wrap/unit-tests$(EXEEXT): $(snap_wrap_unit_tests_OBJECTS) $(snap_wrap_unit_tests_DEPENDENCIES) $(EXTRA_snap_wrap_unit_tests_DEPENDENCIES) libsnap-wrap-private/$(am__dirstamp)
	@rm -f snap-wrap/unit-tests$(EXEEXT)
	$(AM_V_CCLD)$(snap_wrap_unit_tests_LINK) $(snap_wrap_unit_tests_OBJECTS) $(snap_wrap_unit_tests_LDADD) $(LIBS)

snap-wrap/unit-tests$(EXEEXT): LIBS += -Wl,-Bstatic -lcap -Wl,-Bdynamic

endif

snap-wrap/%.5: snap-wrap/%.rst
	mkdir -p snap-wrap
	$(HAVE_RST2MAN) $^ > $@

snap-wrap/snap-wrap.apparmor: snap-wrap/snap-wrap.apparmor.in Makefile
	sed -e 's,[@]LIBEXECDIR[@],$(libexecdir),g' -e 's,[@]SNAP_MOUNT_DIR[@],$(SNAP_MOUNT_DIR),' <$< >$@

# Install the apparmor profile
#
# NOTE: the funky make functions here just convert /foo/bar/froz into
# foo.bar.froz The inner subst replaces slashes with dots and the outer
# patsubst strips the leading dot
install-data-local:: snap-wrap/snap-wrap.apparmor
if APPARMOR
	install -d -m 755 $(DESTDIR)/etc/apparmor.d/
	install -m 644 snap-wrap/snap-wrap.apparmor $(DESTDIR)/etc/apparmor.d/$(patsubst .%,%,$(subst /,.,$(libexecdir))).snap-wrap
endif

# NOTE: The 'void' directory *has to* be chmod 000
install-data-local::
	install -d -m 000 $(DESTDIR)/var/lib/snapd/void

install-exec-hook::
if CAPS_OVER_SETUID
# Ensure that snap-wrap has CAP_SYS_ADMIN capability
	setcap cap_sys_admin=pe $(DESTDIR)$(libexecdir)/snap-wrap
else
# Ensure that snap-wrap is +s (setuid)
	chmod 4755 $(DESTDIR)$(libexecdir)/snap-wrap
endif

##
## ubuntu-core-launcher
##

install-exec-hook::
	install -d -m 755 $(DESTDIR)$(bindir)
	ln -sf $(libexecdir)/snap-wrap $(DESTDIR)$(bindir)/ubuntu-core-launcher

##
## snappy-app-dev
##

EXTRA_DIST += \
	snap-wrap/80-snappy-assign.rules \
	snap-wrap/snappy-app-dev

# NOTE: This makes distcheck fail but it is required for udev, so go figure.
# http://www.gnu.org/software/automake/manual/automake.html#Hard_002dCoded-Install-Paths
#
# Install udev rules
install-data-local::
	install -d -m 755 $(DESTDIR)$(shell pkg-config udev --variable=udevdir)/rules.d
	install -m 644 $(srcdir)/snap-wrap/80-snappy-assign.rules $(DESTDIR)$(shell pkg-config udev --variable=udevdir)/rules.d

# Install support script for udev rules
install-exec-local::
	install -d -m 755 $(DESTDIR)$(shell pkg-config udev --variable=udevdir)
	install -m 755 $(srcdir)/snap-wrap/snappy-app-dev $(DESTDIR)$(shell pkg-config udev --variable=udevdir)

##
## snap-discard-ns
##

libexec_PROGRAMS += snap-discard-ns/snap-discard-ns
dist_man_MANS += snap-discard-ns/snap-discard-ns.5
CLEANFILES += snap-discard-ns/snap-discard-ns.5
EXTRA_DIST += snap-discard-ns/snap-discard-ns.rst

snap_discard_ns_snap_discard_ns_SOURCES = \
	snap-wrap/ns-support.c \
	snap-wrap/ns-support.h \
	snap-wrap/apparmor-support.c \
	snap-wrap/apparmor-support.h \
	snap-discard-ns/snap-discard-ns.c
snap_discard_ns_snap_discard_ns_CFLAGS = -Wall -Werror $(AM_CFLAGS)
snap_discard_ns_snap_discard_ns_LDFLAGS = $(AM_LDFLAGS)
snap_discard_ns_snap_discard_ns_LDADD = libsnap-wrap-private.a

if APPARMOR
snap_discard_ns_snap_discard_ns_CFLAGS += $(APPARMOR_CFLAGS)
snap_discard_ns_snap_discard_ns_LDADD += $(APPARMOR_LIBS)
endif

snap-discard-ns/%.5: snap-discard-ns/%.rst
	mkdir -p snap-discard-ns
	$(HAVE_RST2MAN) $^ > $@

##
## system-shutdown
##

libexec_PROGRAMS += system-shutdown/system-shutdown

system_shutdown_system_shutdown_SOURCES = \
	system-shutdown/system-shutdown-utils.c \
	system-shutdown/system-shutdown-utils.h \
	system-shutdown/system-shutdown.c
system_shutdown_system_shutdown_LDADD = libsnap-wrap-private.a
system_shutdown_system_shutdown_CFLAGS = $(filter-out -fPIE -pie,$(CFLAGS)) -static
system_shutdown_system_shutdown_LDFLAGS = $(filter-out -fPIE -pie,$(LDFLAGS)) -static

if WITH_UNIT_TESTS
noinst_PROGRAMS += system-shutdown/unit-tests
system_shutdown_unit_tests_SOURCES = \
	libsnap-wrap-private/unit-tests-main.c \
	libsnap-wrap-private/unit-tests.c \
	system-shutdown/system-shutdown-utils-test.c
system_shutdown_unit_tests_LDADD = libsnap-wrap-private.a
system_shutdown_unit_tests_CFLAGS = $(GLIB_CFLAGS)
system_shutdown_unit_tests_LDADD +=  $(GLIB_LIBS)
endif

##
## snap-update-ns
##

libexec_PROGRAMS += snap-update-ns/snap-update-ns
dist_man_MANS += snap-update-ns/snap-update-ns.5
CLEANFILES += snap-update-ns/snap-update-ns.5
EXTRA_DIST += snap-update-ns/snap-update-ns.rst

snap_update_ns_snap_update_ns_LDADD = libsnap-wrap-private.a

snap_update_ns_snap_update_ns_SOURCES = \
	snap-update-ns/snap-update-ns.c \
	snap-update-ns/mount-entry.h \
	snap-update-ns/mount-entry.c

snap-update-ns/%.5: snap-update-ns/%.rst
	mkdir -p snap-update-ns
	$(HAVE_RST2MAN) $^ > $@

if WITH_UNIT_TESTS
noinst_PROGRAMS += snap-update-ns/unit-tests
snap_update_ns_unit_tests_SOURCES = \
	libsnap-wrap-private/unit-tests-main.c \
	libsnap-wrap-private/unit-tests.c \
	snap-update-ns/mount-entry-test.c \
	snap-update-ns/test-data.c \
	snap-update-ns/test-data.h \
	snap-update-ns/test-utils.c \
	snap-update-ns/test-utils.h
snap_update_ns_unit_tests_LDADD = libsnap-wrap-private.a
snap_update_ns_unit_tests_CFLAGS = $(GLIB_CFLAGS)
snap_update_ns_unit_tests_LDADD +=  $(GLIB_LIBS)
endif
