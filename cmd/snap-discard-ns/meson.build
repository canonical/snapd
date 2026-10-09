executable(
  'snap-discard-ns',
  'snap-discard-ns.c',
  install : true,
  install_dir : get_option('libexecdir'),
  include_directories : configuration_inc,
  link_with : [
    snap_confine_private,
  ],
)

if rst2man.found()
  custom_target(
    'snap-discard-ns.8',
    output : 'snap-discard-ns.8',
    input : 'snap-discard-ns.rst',
    capture : true,
    install : true,
    install_dir : get_option('mandir') / 'man8',
    command : [
      rst2man, '@INPUT@',
    ]
  )
endif
