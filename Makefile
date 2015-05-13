GIT_REV=$(shell git rev-parse --short HEAD)

VERSION=0.4.1-dev-0-${GIT_REV}

deb: clean build
	mkdir -p build/usr/sbin
	cp -av l2link/l2link build/usr/sbin/
	cp -av l3bridge/l3bridge build/usr/sbin/
	cp -av l3direct/l3direct build/usr/sbin/
	cp -av shadowfax build/usr/sbin/
	cp -av dnetns build/usr/sbin/

	mkdir -p build/etc
	cp -av conf build/etc/shadowfax

	mkdir -p build/var/run/shadowfax

	mkdir -p build/DEBIAN/
	sed s/VERSION/$(VERSION)/ control.in > build/DEBIAN/control

	find conf -type f | sed s-conf-/etc/shadowfax- > build/DEBIAN/conffiles

	fakeroot dpkg-deb -b build shadowfax-${VERSION}.deb

build:
	go build
	cd l2link && go build
	cd l3bridge && go build
	cd l3direct && go build

clean:
	go clean
	cd l2link && go clean
	cd l3bridge && go clean
	cd l3direct && go clean
	rm -f *.deb
	rm -rf build

.PHONY: clean build
