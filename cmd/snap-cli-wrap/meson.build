if get_option('selinux')
  executable(
    'snap',
    'snap-cli-wrap.c',
    install : true,
    install_dir : get_option('bindir'),
    include_directories : configuration_inc,
    c_args : [
      '-DLIBEXECDIR="@0@"'.format(get_option('prefix') / get_option('libexecdir')),
    ],
  )
endif
