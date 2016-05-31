bin_PROGRAMS = snap-run
snap_run_SOURCES = main.c seccomp_utils.c utils.c
snap_run_CFLAGS = \
    $(AM_CFLAGS) \
    $(SECCOMP_CFLAGS) \
    $(APPARMOR_CFLAGS) \
    $(UDEV_CFLAGS)
snap_run_LDADD = \
    $(SECCOMP_LIBS) \
    $(APPARMOR_LIBS) \
    $(UDEV_LIBS)

# Force particular coding style on all source and header files.
.PHONY: check-syntax
check-syntax:
	@d=`mktemp -d`; \
	trap 'rm -rf $d' EXIT; \
	for f in $(wildcard *.c) $(wildcard *.h); do \
	       out="$$d/$$f.out"; \
	       echo "Checking $$f ... "; \
	       indent -linux "$$f" -o "$$out"; \
	       diff -Naur "$$f" "$$out" || exit 1; \
	done;

check: check-syntax