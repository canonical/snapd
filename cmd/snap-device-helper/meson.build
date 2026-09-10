executable(
  'snap-device-helper',
  'main.c',
  'snap-device-helper.c',
  'snap-device-helper.h',
  install_dir : snapddir,
  include_directories : configuration_inc,
  link_with : [
    snap_confine_private,
  ],
)

if glib.found()
  unit_tests = executable(
    'unit-tests',
    'snap-device-helper-test.c',
    link_with : [
      snap_confine_private,
      snap_confine_private_test_support,
    ],
    include_directories : configuration_inc,
    dependencies : [
      glib,
    ]
  )

  test('snap-device-helper', unit_tests)
endif
