Snappy config
=============

The snappy config command is a mechanism for package to provide a way
to set and get its configuration.  A standard yaml based protocol is
used for the interaction. The application is repsonsible to provide a
configuration handler that can transform yaml into the apps native
configuration format.

We plan to support a schema file for the yaml as well to make e.g. web
based editing of the config simpler.  The format of the configuration
is as follows:

	config:
	  packagename:
	    key: value
	  another-pkg:
	    key: value

The application provides a configuration handler in
meta/hooks/config. This configuration handler must provide reading new
configuration from stdin and output the current configuration (after
the new configuration has been applied) to stdout.

The package config hook must return exitcode 0 and return valid yaml
of the form:

	config:
	  packagename:
	    key: value

on stdout.

In addition to the "config" toplevel yaml key there is a optional
"status" key per packagename that contains details about the
success/failure of get/set the configuration. The current key/value
pairs are suppprted right now:

 - error: optional error string
 - warning: optional warning string

Some key/value pairs in the configuration are fixed and all
applications. E.g. all applications that listen to a port must support
the "ports" config option.

The current list of values that must be supported (if the feature is used:

 - ports: the listen ports (if the application listens to the network)

When the configuratin is applied the service will be restarted by
snappy automatically(?).

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


