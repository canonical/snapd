libexec_PROGRAMS = snap-confine
if WITH_UNIT_TESTS
noinst_PROGRAMS = snap-confine-unit-tests
endif
snap_confine_SOURCES = \
	main.c \
	sc-main.c \
	sc-main.h \
	utils.c \
	utils.h \
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
	user-support.h

snap_confine_CFLAGS = -Wall -Werror $(AM_CFLAGS)
snap_confine_LDFLAGS = $(AM_LDFLAGS)
snap_confine_LDADD =
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
	unit-tests.c \
	unit-tests.h \
	utils-test.c \
	cleanup-funcs-test.c \
	mount-support-test.c \
	verify-executable-name-test.c
snap_confine_unit_tests_CFLAGS = $(snap_confine_CFLAGS) $(GLIB_CFLAGS)
snap_confine_unit_tests_LDADD = $(snap_confine_LDADD) $(GLIB_LIBS)
snap_confine_unit_tests_LDFLAGS = $(snap_confine_LDFLAGS)
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
endif


# Run check-syntax when checking
# TODO: conver those to autotools-style tests later
check: check-syntax check-unit-tests

.PHONY: fmt
fmt:
	for f in $(wildcard *.c) $(wildcard *.h); do \
	       echo "Formatting $$f ... "; \
	       indent -linux "$$f"; \
	done;

EXTRA_DIST = 80-snappy-assign.rules snappy-app-dev snap-confine.apparmor.in

snap-confine.apparmor: snap-confine.apparmor.in Makefile
	sed -e 's,[@]LIBEXECDIR[@],$(libexecdir),g' <$< >$@

# NOTE: This makes distcheck fail but it is required for udev, so go figure.
# http://www.gnu.org/software/automake/manual/automake.html#Hard_002dCoded-Install-Paths
#
# Install udev rules and the apparmor profile
#
# NOTE: the funky make functions here just convert /foo/bar/froz into foo.bar.froz
# The inner subst replaces slashes with dots and the outer patsubst strips the leading dot
install-data-local: snap-confine.apparmor
	install -d -m 755 $(DESTDIR)$(shell pkg-config udev --variable=udevdir)/rules.d
	install -m 644 $(srcdir)/80-snappy-assign.rules $(DESTDIR)$(shell pkg-config udev --variable=udevdir)/rules.d
	install -d -m 755 $(DESTDIR)/etc/apparmor.d/
	install -m 644 snap-confine.apparmor $(DESTDIR)/etc/apparmor.d/$(patsubst .%,%,$(subst /,.,$(libexecdir))).snap-confine

# Install support script for udev rules
install-exec-local:
	install -d -m 755 $(DESTDIR)$(shell pkg-config udev --variable=udevdir)
	install -m 755 $(srcdir)/snappy-app-dev $(DESTDIR)$(shell pkg-config udev --variable=udevdir)

# Ensure that snap-confine is +s (setuid)
install-exec-hook:
	chmod 4755 $(DESTDIR)$(libexecdir)/snap-confine
	install -d -m 755 $(DESTDIR)$(bindir)
	ln -sf $(libexecdir)/snap-confine $(DESTDIR)$(bindir)/ubuntu-core-launcher
