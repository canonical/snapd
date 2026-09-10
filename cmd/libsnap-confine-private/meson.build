if glib.found()
  snap_confine_private_test_support = static_library(
    'snap-confine-private-test-support',
    'test-utils.c',
    'test-utils.h',
    'unit-tests-main.c',
    'unit-tests.c',
    'unit-tests.h',
    include_directories : configuration_inc,
    dependencies : [
      glib,
   ]
  )
endif

snap_confine_private_sources = [
  'apparmor-support.c',
  'apparmor-support.h',
  'bpf/bpf-insn.h',
  'cgroup-freezer-support.c',
  'cgroup-freezer-support.h',
  'cgroup-support.c',
  'cgroup-support.h',
  'device-cgroup-support.c',
  'device-cgroup-support.h',
  'classic.c',
  'classic.h',
  'cleanup-funcs.c',
  'cleanup-funcs.h',
  'error.c',
  'error.h',
  'fault-injection.c',
  'fault-injection.h',
  'feature.c',
  'feature.h',
  'infofile.c',
  'infofile.h',
  'locking.c',
  'locking.h',
  'mount-opt.c',
  'mount-opt.h',
  'mountinfo.c',
  'mountinfo.h',
  'panic.c',
  'panic.h',
  'privs.c',
  'privs.h',
  'secure-getenv.c',
  'secure-getenv.h',
  'snap-dir.c',
  'snap-dir.h',
  'snap.c',
  'snap.h',
  'string-utils.c',
  'string-utils.h',
  'tool.c',
  'tool.h',
  'utils.c',
  'utils.h',
]

if bpf
  snap_confine_private_sources += [
    'bpf-support.c',
    'bpf-support.h',
  ]
endif

snap_confine_private = static_library(
  'snap-confine-private',
  snap_confine_private_sources,
  include_directories : configuration_inc,
  dependencies : [
    libcap,
  ],
)

if glib.found()
  unit_tests = executable(
    'unit-tests',
    'cgroup-support-test.c',
    'classic-test.c',
    'cleanup-funcs-test.c',
    'error-test.c',
    'fault-injection-test.c',
    'feature-test.c',
    'infofile-test.c',
    'locking-test.c',
    'mount-opt-test.c',
    'mountinfo-test.c',
    'panic-test.c',
    'privs-test.c',
    'secure-getenv-test.c',
    'snap-dir-test.c',
    'snap-test.c',
    'string-utils-test.c',
    'test-utils-test.c',
    'utils-test.c',
    link_with : [
      snap_confine_private,
      snap_confine_private_test_support,
    ],
    c_args : [
      '-D_ENABLE_FAULT_INJECTION',
    ],
    include_directories : configuration_inc,
    dependencies : [
      glib,
    ]
  )

  test('lib-snap-confine-private', unit_tests)
endif
