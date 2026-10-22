if get_option('b_pie')
  gdbserver_shim_link = ['-static-pie']
else
  gdbserver_shim_link = ['-static']
endif

executable(
  'snap-gdbserver-shim',
  'snap-gdbserver-shim.c',
  install : true,
  install_dir : get_option('libexecdir'),
  include_directories : configuration_inc,
  link_depends : [snap_confine_private],
  link_args : [snap_confine_private.full_path()] + gdbserver_shim_link,
)
