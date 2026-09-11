executable(
  'snap-strace-shim',
  'snap-strace-shim.c',
  install : true,
  install_dir : snapddir,
  include_directories : configuration_inc,
  link_with : [
    snap_confine_private,
  ],
)
