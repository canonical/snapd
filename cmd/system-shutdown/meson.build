if get_option('core')
  executable(
    'system-shutdown',
    'system-shutdown-utils.c',
    'system-shutdown-utils.h',
    'system-shutdown.c',
    install : true,
    install_dir : snapddir,
    include_directories : configuration_inc,
    link_with : [
      snap_confine_private,
    ],
  )
endif

if glib.found()
  unit_tests = executable(
    'unit-tests',
    'system-shutdown-utils-test.c',
    link_with : [
      snap_confine_private,
      snap_confine_private_test_support,
    ],
    include_directories : configuration_inc,
    dependencies : [
      glib,
    ]
  )

  test('system-shutdown', unit_tests)
endif
