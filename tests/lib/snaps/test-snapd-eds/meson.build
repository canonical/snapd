project('test-snapd-eds', 'c', version: '0.1')

ebook_deps = dependency('libebook-1.2')
ecal_deps = dependency('libecal-1.2')

executable('contacts',
  'contacts.c',
  dependencies: ebook_deps,
  install: true)

executable('calendar',
  'calendar.c',
  dependencies: ecal_deps,
  install: true)
