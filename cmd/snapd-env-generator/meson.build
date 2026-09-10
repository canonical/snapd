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