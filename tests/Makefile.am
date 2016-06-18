TESTS =

all_tests = \
    test_bad_seccomp_filter_length \
    test_bad_seccomp_filter_missing_trailing_newline \
    test_complain \
    test_complain_missed \
    test_noprofile \
    test_restrictions \
    test_restrictions_working \
    test_unrestricted \
    test_unrestricted_missed \
    test_whitelist

EXTRA_DIST = $(all_tests) common.sh

if STRICT_CONFINEMENT
TESTS += $(all_tests)
endif

check: ../src/snap-confine

.PHONY: check-syntax
check-syntax:
	shellcheck --format=gcc $(wildcard test_*) common.sh
