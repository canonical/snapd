SUBDIRS = manpages tests

noinst_LIBRARIES = libsnap-confine-private.a

libsnap_confine_private_a_SOURCES = \
	error.h \
	error.c \
	utils.h \
	utils.c \
	secure-getenv.c \
	secure-getenv.h

libexec_PROGRAMS = snap-confine snap-discard-ns system-shutdown
noinst_PROGRAMS = decode-mount-opts
if WITH_UNIT_TESTS
noinst_PROGRAMS += snap-confine-unit-tests
noinst_PROGRAMS += system-shutdown-unit-tests
endif

system-shutdown$(EXEEXT): \
	mountinfo.c \
	mountinfo.h \
	system-shutdown-utils.c \
	system-shutdown-utils.h \
	system-shutdown.c
system-shutdown$(EXEEXT): CFLAGS := $(filter-out -fPIE -pie,$(CFLAGS)) -static
system-shutdown$(EXEEXT): LDFLAGS := $(filter-out -fPIE -pie,$(LDFLAGS)) -static

decode_mount_opts_SOURCES = \
	decode-mount-opts.c \
	mount-opt.c \
	mount-opt.h

snap_discard_ns_SOURCES = \
	ns-support.c \
	ns-support.h \
	apparmor-support.c \
	apparmor-support.h \
	cleanup-funcs.c \
	cleanup-funcs.h \
	mountinfo.c \
	mountinfo.h \
	snap-discard-ns.c
snap_discard_ns_CFLAGS = -Wall -Werror $(AM_CFLAGS)
snap_discard_ns_LDFLAGS = $(AM_LDFLAGS)
snap_discard_ns_LDADD = libsnap-confine-private.a
snap_discard_ns_CFLAGS += $(SECCOMP_CFLAGS)
snap_discard_ns_LDADD += $(SECCOMP_LIBS)

if APPARMOR
snap_discard_ns_CFLAGS += $(APPARMOR_CFLAGS)
snap_discard_ns_LDADD += $(APPARMOR_LIBS)
endif

snap_confine_SOURCES = \
	snap-confine.c \
	snap.c \
	snap.h \
	classic.c \
	classic.h \
	mount-support.c \
	mount-support.h \
	mount-support-nvidia.c \
	mount-support-nvidia.h \
	cleanup-funcs.c \
	cleanup-funcs.h \
	udev-support.c \
	udev-support.h \
	user-support.c \
	user-support.h \
	quirks.c \
	quirks.h \
	mount-opt.c \
	mount-opt.h \
	mountinfo.c \
	mountinfo.h \
	ns-support.c \
	ns-support.h \
	apparmor-support.c \
	apparmor-support.h

snap_confine_CFLAGS = -Wall -Werror $(AM_CFLAGS)
snap_confine_LDFLAGS = $(AM_LDFLAGS)
snap_confine_LDADD = libsnap-confine-private.a
snap_confine_CFLAGS += $(LIBUDEV_CFLAGS)
snap_confine_LDADD += $(LIBUDEV_LIBS)

# This is here to help fix rpmlint hardening issue.
# https://en.opensuse.org/openSUSE:Packaging_checks#non-position-independent-executable
snap_confine_CFLAGS += $(SUID_CFLAGS)
snap_confine_LDFLAGS += $(SUID_LDFLAGS)

if SECCOMP
snap_confine_SOURCES += \
	seccomp-support.c \
	seccomp-support.h
snap_confine_CFLAGS += $(SECCOMP_CFLAGS)
snap_confine_LDADD += $(SECCOMP_LIBS)
endif

if APPARMOR
snap_confine_CFLAGS += $(APPARMOR_CFLAGS)
snap_confine_LDADD += $(APPARMOR_LIBS)
endif

if WITH_UNIT_TESTS
snap_confine_unit_tests_SOURCES = \
	unit-tests-main.c \
	classic.c \
	classic.h \
	quirks.c \
	quirks.h \
	unit-tests.c \
	unit-tests.h \
	utils-test.c \
	cleanup-funcs-test.c \
	mount-support-test.c \
	verify-executable-name-test.c \
	mountinfo-test.c \
	ns-support-test.c \
	apparmor-support.c \
	apparmor-support.h \
	mount-opt-test.c \
	error-test.c
snap_confine_unit_tests_CFLAGS = $(snap_confine_CFLAGS) $(GLIB_CFLAGS)
snap_confine_unit_tests_LDADD = $(snap_confine_LDADD) $(GLIB_LIBS)
snap_confine_unit_tests_LDFLAGS = $(snap_confine_LDFLAGS)

system_shutdown_unit_tests_SOURCES = \
	unit-tests.c \
	unit-tests-main.c \
	mountinfo.c \
	mountinfo.h \
	system-shutdown-utils-test.c
system_shutdown_unit_tests_CFLAGS = $(GLIB_CFLAGS)
system_shutdown_unit_tests_LDADD =  $(GLIB_LIBS)
endif

# Force particular coding style on all source and header files.
.PHONY: check-syntax
check-syntax:
	@d=`mktemp -d`; \
	trap 'rm -rf $d' EXIT; \
	for f in $(wildcard $(srcdir)/*.c) $(wildcard $(srcdir)/*.h); do \
	       out="$$d/`basename $$f.out`"; \
	       echo "Checking $$f ... "; \
	       indent -linux "$$f" -o "$$out"; \
	       diff -Naur "$$f" "$$out" || exit 1; \
	done;

.PHONY: check-unit-tests
check-unit-tests: snap-confine-unit-tests
if WITH_UNIT_TESTS
	./snap-confine-unit-tests
	./system-shutdown-unit-tests
endif


# Run check-syntax when checking
# TODO: conver those to autotools-style tests later
check: check-syntax check-unit-tests

.PHONY: fmt
fmt:
	for f in $(wildcard $(srcdir)/*.c) $(wildcard $(srcdir)/*.h); do \
	       echo "Formatting $$f ... "; \
	       indent -linux "$$f"; \
	done;

EXTRA_DIST = PORTING 80-snappy-assign.rules snappy-app-dev snap-confine.apparmor.in

snap-confine.apparmor: snap-confine.apparmor.in Makefile
	sed -e 's,[@]LIBEXECDIR[@],$(libexecdir),g' -e 's,[@]SNAP_MOUNT_DIR[@],$(SNAP_MOUNT_DIR),' <$< >$@

# NOTE: This makes distcheck fail but it is required for udev, so go figure.
# http://www.gnu.org/software/automake/manual/automake.html#Hard_002dCoded-Install-Paths
#
# Install udev rules and the apparmor profile
#
# NOTE: the funky make functions here just convert /foo/bar/froz into foo.bar.froz
# The inner subst replaces slashes with dots and the outer patsubst strips the leading dot
#
# NOTE: The 'void' directory *has to* be chmod 000
install-data-local: snap-confine.apparmor
	install -d -m 755 $(DESTDIR)$(shell pkg-config udev --variable=udevdir)/rules.d
	install -m 644 $(srcdir)/80-snappy-assign.rules $(DESTDIR)$(shell pkg-config udev --variable=udevdir)/rules.d
	install -d -m 755 $(DESTDIR)/etc/apparmor.d/
	install -m 644 snap-confine.apparmor $(DESTDIR)/etc/apparmor.d/$(patsubst .%,%,$(subst /,.,$(libexecdir))).snap-confine
	install -d -m 000 $(DESTDIR)/var/lib/snapd/void

# Install support script for udev rules
install-exec-local:
	install -d -m 755 $(DESTDIR)$(shell pkg-config udev --variable=udevdir)
	install -m 755 $(srcdir)/snappy-app-dev $(DESTDIR)$(shell pkg-config udev --variable=udevdir)

install-exec-hook:
if CAPS_OVER_SETUID
# Ensure that snap-confine has CAP_SYS_ADMIN capability
	setcap cap_sys_admin=pe $(DESTDIR)$(libexecdir)/snap-confine
else
# Ensure that snap-confine is +s (setuid)
	chmod 4755 $(DESTDIR)$(libexecdir)/snap-confine
endif
	install -d -m 755 $(DESTDIR)$(bindir)
	ln -sf $(libexecdir)/snap-confine $(DESTDIR)$(bindir)/ubuntu-core-launcher

# The hack target helps devlopers work on snap-confine on their live system by
# installing a fresh copy of snap confine and the appropriate apparmor profile.
.PHONY: hack
hack: snap-confine snap-confine.apparmor
	sudo install -D -m 4755 snap-confine $(DESTDIR)$(libexecdir)/snap-confine
	sudo install -m 644 snap-confine.apparmor $(DESTDIR)/etc/apparmor.d/$(patsubst .%,%,$(subst /,.,$(libexecdir))).snap-confine
	sudo apparmor_parser -r snap-confine.apparmor
