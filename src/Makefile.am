libexec_PROGRAMS = snap-confine
noinst_PROGRAMS = snap-confine-unit-tests
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
	cleanup-funcs.h
snap_confine_CFLAGS = -Wall -Werror $(AM_CFLAGS)
snap_confine_LDFLAGS = $(AM_LDFLAGS)
snap_confine_LDADD =

# This is here to help fix rpmlint hardening issue.
# https://en.opensuse.org/openSUSE:Packaging_checks#non-position-independent-executable
snap_confine_CFLAGS += $(SUID_CFLAGS)
snap_confine_LDFLAGS += $(SUID_LDFLAGS)

if STRICT_CONFINEMENT
snap_confine_SOURCES += \
	seccomp-support.c \
	seccomp-support.h \
	udev-support.c \
	udev-support.h
snap_confine_CFLAGS += $(APPARMOR_CFLAGS) $(SECCOMP_CFLAGS) $(LIBUDEV_CFLAGS)
snap_confine_LDADD += $(APPARMOR_LIBS) $(SECCOMP_LIBS) $(LIBUDEV_LIBS)
endif

snap_confine_unit_tests_SOURCES = \
    unit-tests-main.c \
	unit-tests.c \
	unit-tests.h \
	utils-test.c \
	cleanup-funcs-test.c \
	mount-support-test.c
snap_confine_unit_tests_CFLAGS = $(snap_confine_CFLAGS) $(GLIB_CFLAGS)
snap_confine_unit_tests_LDADD = $(snap_confine_LDADD) $(GLIB_LIBS)
snap_confine_unit_tests_LDFLAGS = $(snap_confine_LDFLAGS)

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
	./snap-confine-unit-tests


# Run check-syntax when checking
# TODO: conver those to autotools-style tests later
check: check-syntax check-unit-tests

.PHONY: fmt
fmt:
	for f in $(wildcard *.c) $(wildcard *.h); do \
	       echo "Formatting $$f ... "; \
	       indent -linux "$$f"; \
	done;

EXTRA_DIST = 80-snappy-assign.rules snappy-app-dev

# NOTE: This makes distcheck fail but it is required for udev, so go figure.
# http://www.gnu.org/software/automake/manual/automake.html#Hard_002dCoded-Install-Paths
#
# Install udev rules
install-data-local:
	install -d -m 755 $(DESTDIR)$(shell pkg-config udev --variable=udevdir)/rules.d
	install -m 644 $(srcdir)/80-snappy-assign.rules $(DESTDIR)$(shell pkg-config udev --variable=udevdir)/rules.d

# Install support script for udev rules
install-exec-local:
	install -d -m 755 $(DESTDIR)$(shell pkg-config udev --variable=udevdir)
	install -m 755 $(srcdir)/snappy-app-dev $(DESTDIR)$(shell pkg-config udev --variable=udevdir)

# Ensure that snap-confine is +s (setuid)
install-exec-hook:
	chmod 4755 $(DESTDIR)$(libexecdir)/snap-confine
	install -d -m 755 $(DESTDIR)$(bindir)
	ln -sf $(libexecdir)/snap-confine $(DESTDIR)$(bindir)/ubuntu-core-launcher
