FROM ubuntu

RUN apt-get update \
    && env DEBIAN_FRONTEND=noninteractive apt-get install -y djbdns inotify-tools \
    && apt-get clean \
    && rm -rf /var/lib/apt

ADD prep.sh /tmp/
RUN (bash -e /tmp/prep.sh && rm -f /tmp/prep.sh)

CMD ["/usr/bin/svscan", "/service"]
