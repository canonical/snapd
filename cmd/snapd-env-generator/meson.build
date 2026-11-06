executable(
  'snapd-env-generator',
  'main.c',
  install : true,
  install_dir : systemdsystemenvironmentgenertorsdir,
  include_directories : configuration_inc,
  link_with : [
    snap_confine_private,
  ],
)

if rst2man.found()
  custom_target(
    'snapd-env-generator.8',
    output : 'snapd-env-generator.8',
    input : 'snapd-env-generator.rst',
    capture : true,
    install : true,
    install_dir : get_option('mandir') / 'man8',
    command : [
      rst2man, '@INPUT@',
    ]
  )
endif
