runit Configuration Files
=========================
These are example configuration files for use with
[runit](http://smarden.org/runit/). These configuration files should be placed
in `/data/docker/orch`, then enabled via

    $ cd /etc/services
    $ ln -s /data/docker/orch/* .

The `runsvdir` daemon should take care of the rest.

To get the status of all services, do

    $ sv status /etc/services/*

Logs will end up in a subdirectory underneath `/data/docker/logs`. Under some
circumstances, these logs may contain sensitive information and thus should not
be placed in the normal root filesystem under `/var/log`. To ship them to a
central location consider using something like
[logstash-forwarder](https://github.com/elastic/logstash-forwarder).
