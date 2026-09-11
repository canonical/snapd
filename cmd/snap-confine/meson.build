executable(
  'snap-confine',
  'cookie-support.c',
  'cookie-support.h',
  'mount-support-nvidia.c',
  'mount-support-nvidia.h',
  'mount-support.c',
  'mount-support.h',
  'ns-support.c',
  'ns-support.h',
  'group-policy.c',
  'group-policy.h',
  'seccomp-support-ext.c',
  'seccomp-support-ext.h',
  'seccomp-support.c',
  'seccomp-support.h',
  'snap-confine-args.c',
  'snap-confine-args.h',
  'snap-confine-invocation.c',
  'snap-confine-invocation.h',
  'snap-confine.c',
  'udev-support.c',
  'udev-support.h',
  'user-support.c',
  'user-support.h',
  install : true,
  install_dir : snapddir,
  c_args : [
    '-DLIBEXECDIR="@0@"'.format(get_option('prefix') / get_option('libexecdir')),
    '-DNATIVE_LIBDIR="@0@"'.format(get_option('prefix') / get_option('libdir')),
  ],
  link_with : [
    snap_confine_private,
  ],
  include_directories : configuration_inc,
  dependencies : [
    libcap,
    libudev,
  ],
)

if glib.found()
  unit_tests = executable(
    'unit-tests',
    'cookie-support-test.c',
    'mount-support-test.c',
    'ns-support-test.c',
    'seccomp-support-test.c',
    'snap-confine-args-test.c',
    'snap-confine-invocation-test.c',
    'group-policy-test.c',
    link_with : [
      snap_confine_private,
      snap_confine_private_test_support,
    ],
    include_directories : configuration_inc,
    dependencies : [
      glib,
    ]
  )

  test('snap-confine', unit_tests)
endif

install_emptydir(
  snapdstatedir / 'void',
  install_mode : '--x--x--x',
)

install_data(
  'snap-confine.caps',
  'snap-confine.v2-only.caps',
  install_dir : snapddir,
)

if get_option('apparmor')
  cfg = configuration_data({
    'LIBEXECDIR': snapddir,
  })
  configure_file(
    input : 'snap-confine.apparmor.in',
    output : '@0@.snap-confine'.format(snapddir.substring(1).replace('/', '.')),
    configuration : cfg,
    install : true,
    install_dir : get_option('apparmorconfigdir'),
  )
endif
