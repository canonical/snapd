EXTRA_DIST = VERSION snap-confine/PORTING
CLEANFILES =
TESTS =
libexec_PROGRAMS =
dist_man_MANS =
noinst_PROGRAMS =
noinst_LIBRARIES =

subdirs = snap-confine snap-discard-ns system-shutdown libsnap-confine-private snap-update-ns

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
	       indent -linux "$$f" -o "$$out"; \
	       diff -Naur "$$f" "$$out" || exit 1; \
	done;

# Check for common shell errors
.PHONY: check-syntax-sh
check-syntax-sh: $(filter-out %.log %.trs,$(wildcard $(srcdir)/snap-confine/tests/test_*)) snap-confine/tests/common.sh
if HAVE_SHELLCHECK
	$(HAVE_SHELLCHECK) --format=gcc $^
else
	echo "shellcheck is not installed, skipping (non-critical) syntax test"
endif

.PHONY: check-unit-tests
if WITH_UNIT_TESTS
check-unit-tests: snap-confine/snap-confine-unit-tests system-shutdown/system-shutdown-unit-tests libsnap-confine-private/unit-tests
	$(HAVE_VALGRIND) ./snap-confine/snap-confine-unit-tests
	$(HAVE_VALGRIND) ./system-shutdown/system-shutdown-unit-tests
	$(HAVE_VALGRIND) ./libsnap-confine-private/unit-tests
else
check-unit-tests:
	echo "unit tests are disabled (rebuild with --enable-unit-tests)"
endif

.PHONY: fmt
fmt: $(foreach dir,$(subdirs),$(wildcard $(srcdir)/$(dir)/*.[ch]))
	indent -linux $^

# The hack target helps devlopers work on snap-confine on their live system by
# installing a fresh copy of snap confine and the appropriate apparmor profile.
.PHONY: hack
hack: snap-confine/snap-confine snap-confine/snap-confine.apparmor
	sudo install -D -m 4755 snap-confine/snap-confine $(DESTDIR)$(libexecdir)/snap-confine
	sudo install -m 644 snap-confine/snap-confine.apparmor $(DESTDIR)/etc/apparmor.d/$(patsubst .%,%,$(subst /,.,$(libexecdir))).snap-confine
	sudo apparmor_parser -r snap-confine/snap-confine.apparmor

##
## libsnap-confine-private.a
##

noinst_LIBRARIES += libsnap-confine-private.a

libsnap_confine_private_a_SOURCES = \
	libsnap-confine-private/classic.c \
	libsnap-confine-private/classic.h \
	libsnap-confine-private/cleanup-funcs.c \
	libsnap-confine-private/cleanup-funcs.h \
	libsnap-confine-private/error.c \
	libsnap-confine-private/error.h \
	libsnap-confine-private/mount-opt.c \
	libsnap-confine-private/mount-opt.h \
	libsnap-confine-private/mountinfo.c \
	libsnap-confine-private/mountinfo.h \
	libsnap-confine-private/secure-getenv.c \
	libsnap-confine-private/secure-getenv.h \
	libsnap-confine-private/snap.c \
	libsnap-confine-private/snap.h \
	libsnap-confine-private/string-utils.c \
	libsnap-confine-private/string-utils.h \
	libsnap-confine-private/utils.c \
	libsnap-confine-private/utils.h

if WITH_UNIT_TESTS
noinst_PROGRAMS += libsnap-confine-private/unit-tests
libsnap_confine_private_unit_tests_SOURCES = \
	libsnap-confine-private/classic-test.c \
	libsnap-confine-private/cleanup-funcs-test.c \
	libsnap-confine-private/error-test.c \
	libsnap-confine-private/mount-opt-test.c \
	libsnap-confine-private/mountinfo-test.c \
	libsnap-confine-private/fault-injection-test.c \
	libsnap-confine-private/secure-getenv-test.c \
	libsnap-confine-private/snap-test.c \
	libsnap-confine-private/string-utils-test.c \
	libsnap-confine-private/unit-tests-main.c \
	libsnap-confine-private/unit-tests.c \
	libsnap-confine-private/unit-tests.h \
	libsnap-confine-private/utils-test.c
libsnap_confine_private_unit_tests_CFLAGS = $(GLIB_CFLAGS)
libsnap_confine_private_unit_tests_LDADD = $(GLIB_LIBS)
libsnap_confine_private_unit_tests_CFLAGS += -D_ENABLE_FAULT_INJECTION
endif

##
## decode-mount-opts
##

noinst_PROGRAMS += decode-mount-opts/decode-mount-opts

decode_mount_opts_decode_mount_opts_SOURCES = \
	decode-mount-opts/decode-mount-opts.c
decode_mount_opts_decode_mount_opts_LDADD = libsnap-confine-private.a

##
## snap-confine
##

all_tests = \
    snap-confine/tests/test_bad_seccomp_filter_args \
    snap-confine/tests/test_bad_seccomp_filter_args_clone \
    snap-confine/tests/test_bad_seccomp_filter_args_mknod \
    snap-confine/tests/test_bad_seccomp_filter_args_null \
    snap-confine/tests/test_bad_seccomp_filter_args_prctl \
    snap-confine/tests/test_bad_seccomp_filter_args_prio \
    snap-confine/tests/test_bad_seccomp_filter_args_quotactl \
    snap-confine/tests/test_bad_seccomp_filter_args_socket \
    snap-confine/tests/test_bad_seccomp_filter_args_termios \
    snap-confine/tests/test_bad_seccomp_filter_length \
    snap-confine/tests/test_bad_seccomp_filter_missing_trailing_newline \
    snap-confine/tests/test_complain \
    snap-confine/tests/test_complain_missed \
    snap-confine/tests/test_noprofile \
    snap-confine/tests/test_restrictions \
    snap-confine/tests/test_restrictions_working \
    snap-confine/tests/test_restrictions_working_args \
    snap-confine/tests/test_restrictions_working_args_clone \
    snap-confine/tests/test_restrictions_working_args_mknod \
    snap-confine/tests/test_restrictions_working_args_prctl \
    snap-confine/tests/test_restrictions_working_args_prio \
    snap-confine/tests/test_restrictions_working_args_quotactl \
    snap-confine/tests/test_restrictions_working_args_socket \
    snap-confine/tests/test_restrictions_working_args_termios \
    snap-confine/tests/test_unrestricted \
    snap-confine/tests/test_unrestricted_missed \
    snap-confine/tests/test_whitelist

libexec_PROGRAMS += snap-confine/snap-confine
dist_man_MANS += snap-confine/snap-confine.5
CLEANFILES += snap-confine/snap-confine.5
EXTRA_DIST += snap-confine/snap-confine.rst
EXTRA_DIST += snap-confine/snap-confine.apparmor.in
EXTRA_DIST += $(all_tests) snap-confine/tests/common.sh
if SECCOMP
if CONFINEMENT_TESTS
TESTS += $(all_tests)
endif
endif

snap_confine_snap_confine_SOURCES = \
	snap-confine/apparmor-support.c \
	snap-confine/apparmor-support.h \
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

snap_confine_snap_confine_CFLAGS = -Wall -Werror $(AM_CFLAGS)
snap_confine_snap_confine_LDFLAGS = $(AM_LDFLAGS)
snap_confine_snap_confine_LDADD = libsnap-confine-private.a
snap_confine_snap_confine_CFLAGS += $(LIBUDEV_CFLAGS)
snap_confine_snap_confine_LDADD += $(LIBUDEV_LIBS)

# This is here to help fix rpmlint hardening issue.
# https://en.opensuse.org/openSUSE:Packaging_checks#non-position-independent-executable
snap_confine_snap_confine_CFLAGS += $(SUID_CFLAGS)
snap_confine_snap_confine_LDFLAGS += $(SUID_LDFLAGS)

if SECCOMP
snap_confine_snap_confine_SOURCES += \
	snap-confine/seccomp-support.c \
	snap-confine/seccomp-support.h
snap_confine_snap_confine_CFLAGS += $(SECCOMP_CFLAGS)
snap_confine_snap_confine_LDADD += $(SECCOMP_LIBS)
endif

if APPARMOR
snap_confine_snap_confine_CFLAGS += $(APPARMOR_CFLAGS)
snap_confine_snap_confine_LDADD += $(APPARMOR_LIBS)
endif

if WITH_UNIT_TESTS
noinst_PROGRAMS += snap-confine/snap-confine-unit-tests
snap_confine_snap_confine_unit_tests_SOURCES = \
	libsnap-confine-private/unit-tests-main.c \
	libsnap-confine-private/unit-tests.c \
	libsnap-confine-private/unit-tests.h \
	snap-confine/apparmor-support.c \
	snap-confine/apparmor-support.h \
	snap-confine/mount-support-test.c \
	snap-confine/ns-support-test.c \
	snap-confine/quirks.c \
	snap-confine/quirks.h \
	snap-confine/snap-confine-args-test.c
snap_confine_snap_confine_unit_tests_CFLAGS = $(snap_confine_snap_confine_CFLAGS) $(GLIB_CFLAGS)
snap_confine_snap_confine_unit_tests_LDADD = $(snap_confine_snap_confine_LDADD) $(GLIB_LIBS)
snap_confine_snap_confine_unit_tests_LDFLAGS = $(snap_confine_snap_confine_LDFLAGS)
endif

snap-confine/%.5: snap-confine/%.rst
	mkdir -p snap-confine
	$(HAVE_RST2MAN) $^ > $@

snap-confine/snap-confine.apparmor: snap-confine/snap-confine.apparmor.in Makefile
	sed -e 's,[@]LIBEXECDIR[@],$(libexecdir),g' -e 's,[@]SNAP_MOUNT_DIR[@],$(SNAP_MOUNT_DIR),' <$< >$@

# Install the apparmor profile
#
# NOTE: the funky make functions here just convert /foo/bar/froz into
# foo.bar.froz The inner subst replaces slashes with dots and the outer
# patsubst strips the leading dot
install-data-local:: snap-confine/snap-confine.apparmor
	install -d -m 755 $(DESTDIR)/etc/apparmor.d/
	install -m 644 snap-confine/snap-confine.apparmor $(DESTDIR)/etc/apparmor.d/$(patsubst .%,%,$(subst /,.,$(libexecdir))).snap-confine

# NOTE: The 'void' directory *has to* be chmod 000
install-data-local::
	install -d -m 000 $(DESTDIR)/var/lib/snapd/void

install-exec-hook::
if CAPS_OVER_SETUID
# Ensure that snap-confine has CAP_SYS_ADMIN capability
	setcap cap_sys_admin=pe $(DESTDIR)$(libexecdir)/snap-confine
else
# Ensure that snap-confine is +s (setuid)
	chmod 4755 $(DESTDIR)$(libexecdir)/snap-confine
endif

##
## ubuntu-core-launcher
##

install-exec-hook::
	install -d -m 755 $(DESTDIR)$(bindir)
	ln -sf $(libexecdir)/snap-confine $(DESTDIR)$(bindir)/ubuntu-core-launcher

##
## snappy-app-dev
##

EXTRA_DIST += \
	snap-confine/80-snappy-assign.rules \
	snap-confine/snappy-app-dev

# NOTE: This makes distcheck fail but it is required for udev, so go figure.
# http://www.gnu.org/software/automake/manual/automake.html#Hard_002dCoded-Install-Paths
#
# Install udev rules
install-data-local:: snap-confine/snap-confine.apparmor
	install -d -m 755 $(DESTDIR)$(shell pkg-config udev --variable=udevdir)/rules.d
	install -m 644 $(srcdir)/snap-confine/80-snappy-assign.rules $(DESTDIR)$(shell pkg-config udev --variable=udevdir)/rules.d

# Install support script for udev rules
install-exec-local::
	install -d -m 755 $(DESTDIR)$(shell pkg-config udev --variable=udevdir)
	install -m 755 $(srcdir)/snap-confine/snappy-app-dev $(DESTDIR)$(shell pkg-config udev --variable=udevdir)

##
## snap-discard-ns
##

libexec_PROGRAMS += snap-discard-ns/snap-discard-ns
dist_man_MANS += snap-discard-ns/snap-discard-ns.5
CLEANFILES += snap-discard-ns/snap-discard-ns.5
EXTRA_DIST += snap-discard-ns/snap-discard-ns.rst

snap_discard_ns_snap_discard_ns_SOURCES = \
	snap-confine/ns-support.c \
	snap-confine/ns-support.h \
	snap-confine/apparmor-support.c \
	snap-confine/apparmor-support.h \
	snap-discard-ns/snap-discard-ns.c
snap_discard_ns_snap_discard_ns_CFLAGS = -Wall -Werror $(AM_CFLAGS)
snap_discard_ns_snap_discard_ns_LDFLAGS = $(AM_LDFLAGS)
snap_discard_ns_snap_discard_ns_LDADD = libsnap-confine-private.a

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
system_shutdown_system_shutdown_LDADD = libsnap-confine-private.a
system_shutdown_system_shutdown_CFLAGS = $(filter-out -fPIE -pie,$(CFLAGS)) -static
system_shutdown_system_shutdown_LDFLAGS = $(filter-out -fPIE -pie,$(LDFLAGS)) -static

if WITH_UNIT_TESTS
noinst_PROGRAMS += system-shutdown/system-shutdown-unit-tests
system_shutdown_system_shutdown_unit_tests_SOURCES = \
	libsnap-confine-private/unit-tests-main.c \
	libsnap-confine-private/unit-tests.c \
	system-shutdown/system-shutdown-utils-test.c
system_shutdown_system_shutdown_unit_tests_LDADD = libsnap-confine-private.a
system_shutdown_system_shutdown_unit_tests_CFLAGS = $(GLIB_CFLAGS)
system_shutdown_system_shutdown_unit_tests_LDADD +=  $(GLIB_LIBS)
endif

##
## snap-update-ns
##

libexec_PROGRAMS += snap-update-ns/snap-update-ns
dist_man_MANS += snap-update-ns/snap-update-ns.5
CLEANFILES += snap-update-ns/snap-update-ns.5
EXTRA_DIST += snap-update-ns/snap-update-ns.rst

snap_update_ns_snap_update_ns_LDADD = libsnap-confine-private.a

snap-update-ns/%.5: snap-update-ns/%.rst
	mkdir -p snap-update-ns
	$(HAVE_RST2MAN) $^ > $@
