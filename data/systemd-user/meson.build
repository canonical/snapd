cfg = configuration_data({
  'bindir': get_option('bindir'),
})

systemd_units_generated = [
  'snapd.session-agent.service'
]

foreach generated_unit : systemd_units_generated
  configure_file(
    input : generated_unit + '.in',
    output : generated_unit,
    configuration : cfg,
    install : true,
    install_dir : systemduserunitdir,
  )
endforeach

install_data(
  'snapd.session-agent.socket',
  install_dir : systemduserunitdir,
)
