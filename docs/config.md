Snappy config
=============

The snappy config command is a mechanism for package to provide a way
to set and get its configuration. A standard yaml based protocol is
used for the interaction. The application is responsible for providing
a configuration handler that can transform yaml into the app's native
configuration format.

We plan also to support a schema file for the yaml to make editing
simpler, for example web based editing of config. The format of the
configuration is as follows:

	config:
	  packagename:
	    key: value
	  another-pkg:
	    key: value

The application provides a configuration handler in
`meta/hooks/config`. This configuration handler must provide for reading
new configuration from stdin and output the current configuration (after
the new configuration has been applied) to stdout.

The package config hook must return exit code 0 and return valid yaml
of the form:

	config:
	  packagename:
	    key: value

on stdout.

In addition to the "config" top-level yaml key there is a optional
"status" key per packagename that contains details about the
success/failure of get/set the configuration. The current key/value
pairs are supported right now:

 - error: optional error string
 - warning: optional warning string

Some key/value pairs in the configuration are fixed, for example all
applications that listen to a port must support the "ports" config option.

The current list of values that must be supported (if the feature is used):

 - ports: the listen ports (if the application listens to the network)

When the configuration is applied the service will be restarted by
snappy automatically.

Examples:
---------

Example to set config.

snappy calls meta/hooks/config and sends the following over stdin:

	config:
	  ubuntu-core:
	    timezone: Europe/Berlin

    The meta/hooks/config sends the following back:

	config:
	  ubuntu-core:
	    timezone: Europe/Berlin

Example to get a config:

snappy calls meta/hooks/config with empty input. The meta/hooks/config sends
the following back:

	config:
	  ubuntu-core:
	    timezone: Europe/Berlin
	status:
	  ubuntu-core:
	    ok: true

Example to set a non-existing config:

snappy -> meta/hooks/config

	config:
	  ubuntu-core:
	    tea-in-the-morning: false

meta/hooks/config exit with status 10 and message:

	status:
	  ubuntu-core:
	    error: Unknown config option "tea-in-the-morning"

[snappy fails the install of the app]

### Example: list items and changing config data locally

Running `snappy config SNAPNAME` command will print the current config of a
snap:

	$ snappy config SNAPNAME
	SNAPNAME:
	  config1: value
	  config2:
	    - listitem1
	    - listitem2

To change an existing configuration you would save this output to a local
YAML file, edit it and use the snappy config command to pass the new YAML
syntax to the snap:

	$ snappy config SNAPNAME > my.yaml
	$ sed -i 's/listitem1/listitem1-changed/'
	$ snappy config SNAPNAME my.yaml

This command will fail with a non-zero exit code if the YAML syntax is not
valid or the config handler of the snap signals an error. 

If it succeeds, the new config is applied:

	$ snappy config SNAPNAME
	SNAPNAME:
	  config1: value
	  config2:
	    - listitem1-changed
	    - listitem2

