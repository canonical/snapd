desktop_files = [
  'snap-handle-link.desktop',
  'io.snapcraft.SessionAgent.desktop',
]

cfg = configuration_data({
  'bindir': get_option('bindir'),
  'icondir': get_option('datadir') / 'snapd',
})

foreach desktop_file : desktop_files
  configure_file(
    input : desktop_file + '.in',
    output : desktop_file,
    configuration : cfg,
    install : true,
    install_dir : get_option('datadir') / 'applications',
  )
endforeach

configure_file(
  input : 'snap-userd-autostart.desktop.in',
  output : 'snap-userd-autostart.desktop',
  configuration : cfg,
  install : true,
  install_dir : get_option('sysconfdir') / 'xdg/autostart',
)

install_data(
  'snapcraft-logo-bird.svg',
  install_dir: get_option('datadir') / 'snapd',
)
