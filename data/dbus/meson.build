generated_services = [
  'io.snapcraft.Launcher.service',
  'io.snapcraft.SessionAgent.service',
  'io.snapcraft.Settings.service',
]

cfg = configuration_data({
  'bindir': get_option('bindir'),
})

foreach service : generated_services
  configure_file(
    input : service + '.in',
    output : service,
    configuration : cfg,
    install : true,
    install_dir : session_bus_services_dir,
  )
endforeach

install_data(
  'snapd.session-services.conf',
  install_dir : dbusdatadir / 'session.d',
)

install_data(
  'snapd.system-services.conf',
  install_dir : dbusdatadir / 'system.d',
)
