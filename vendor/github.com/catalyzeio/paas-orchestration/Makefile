GIT_REV=$(shell git rev-parse --short HEAD)
VERSION=2.6.1-rel-1-${GIT_REV}
MULTI=paas-orchestration
ALIASES=regcli agentcli regserver agentserver schedserver regwatch
RUNTEST=${SKIP_TEST:test}

all: ${RUNTEST} ${MULTI} ${ALIASES}

test:
	mv vendor src
	ln -s ${GOPATH}/src/github.com/catalyzeio/paas-orchestration src/github.com/catalyzeio/paas-orchestration
	cd src/github.com/catalyzeio/paas-orchestration
	GOPATH=${GOPATH}src/github.com/catalyzeio/paas-orchestration go test ${SHORT_TEST} $(go list| grep -v /vendor/)
	cd ${GOPATH}/src/github.com/catalyzeio/paas-orchestration
	unlink src/github.com/catalyzeio/paas-orchestration
	mv src vendor

skip_test:
	echo skipping tests...

paas-orchestration:
	go build -ldflags=${LD_FLAGS}

regcli: ${MULTI}
	ln -sf ${MULTI} $@

regserver: ${MULTI}
	ln -sf ${MULTI} $@

regwatch: ${MULTI}
	ln -sf ${MULTI} $@

agentcli: ${MULTI}
	ln -sf ${MULTI} $@

agentserver: ${MULTI}
	ln -sf ${MULTI} $@

schedserver: ${MULTI}
	ln -sf ${MULTI} $@

deb: all
	# reset build directory
	rm -rf build

	# add binaries
	mkdir -p build/opt/orch/bin
	cp -av ${MULTI} ${ALIASES} build/opt/orch/bin/

	# add other scripts
	cp -av ./scripts/migrate-agent-state build/opt/orch/bin/
	cp -av ./scripts/update-mount-state build/opt/orch/bin/
	cp -av ./scripts/update-jobs-states build/opt/orch/bin/
	cp -av ./scripts/safe-restart-registry build/opt/orch/bin/

	# add templates
	cp -av templates build/opt/orch/

	# add docs
	mkdir -p build/usr/share/doc/orch-council
	cp README.md build/usr/share/doc/orch-council

	# patch up control file
	mkdir -p build/DEBIAN
	sed s/VERSION/${VERSION}/ control.in > build/DEBIAN/control

	# build .deb file
	fakeroot dpkg-deb -b build paas-orchestration_${VERSION}_amd64.deb

clean:
	rm -f *.deb
	rm -rf build
	rm -f ${MULTI} ${ALIASES}

.PHONY: clean all test deb ${MULTI}
