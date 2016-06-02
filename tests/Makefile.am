TESTS =

if STRICT_CONFINEMENT
TESTS += \
    test_bad_seccomp_filter_length \
    test_bad_seccomp_filter_missing_trailing_newline \
    test_complain \
    test_complain_missed \
    test_create_user_data \
    test_noprofile \
    test_restrictions_working \
    test_unrestricted \
    test_unrestricted_missed \
    test_whitelist
endif

.PHONY: check-syntax
check-syntax:
	shellcheck --format=gcc $(wildcard test_*) common.sh
