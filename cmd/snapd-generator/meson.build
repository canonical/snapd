executable(
  'snapd-generator',
  'main.c',
  install : true,
  install_dir : systemdsystemgeneratordir,
  include_directories : configuration_inc,
  link_with : [
    snap_confine_private,
  ],
)