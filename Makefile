.PHONY: build clean install install-local deb deb-docker test test-integration test-verbose apt-repo apt-upload release deploy stamp-install demo-frames docgen

VERSION := $(shell cat VERSION)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

build:
	go build $(LDFLAGS) -o bin/bewitchd ./cmd/bewitchd
	go build $(LDFLAGS) -o bin/bewitch ./cmd/bewitch

clean:
	rm -rf bin/ dist/
	rm -f ../bewitch_*.deb ../bewitch_*.buildinfo ../bewitch_*.changes

install: build
	install -m 755 bin/bewitchd /usr/bin/bewitchd
	install -m 755 bin/bewitch /usr/bin/bewitch

install-local: build
	install -m 755 bin/bewitchd /usr/local/bin/bewitchd
	install -m 755 bin/bewitch /usr/local/bin/bewitch

deb: build
	scripts/gen-changelog.sh
	dpkg-buildpackage -us -uc -b

deb-docker:
	docker build --platform linux/amd64 -f Dockerfile.build -o dist/amd64 .
	docker build --platform linux/arm64 -f Dockerfile.build -o dist/arm64 .

test:
	go test ./...

test-integration:
	go test -tags integration -count=1 ./...

test-verbose:
	go test -v ./...

GPG_KEY_FILE ?= $(HOME)/.config/bewitch/signing.key

apt-repo:
	docker build -f Dockerfile.repo -t bewitch-repo .
	docker run --rm \
		-e SITE_PUBLIC=/work/site/public \
		-e GPG_KEY_FILE=/work/signing.key \
		-v $(CURDIR)/dist:/work/dist \
		-v $(CURDIR)/site/public:/work/site/public \
		-v $(GPG_KEY_FILE):/work/signing.key:ro \
		bewitch-repo dist/amd64/bewitch_*.deb dist/arm64/bewitch_*.deb

apt-upload:
	scripts/upload-pool.sh dist/amd64/bewitch_*.deb dist/arm64/bewitch_*.deb \
		dist/amd64/bewitch-*.tar.gz dist/arm64/bewitch-*.tar.gz

stamp-install:
	@V=$$(cat VERSION) && \
	sed 's/^VERSION="[^"]*"/VERSION="'"$$V"'"/' site/public/install.sh > site/public/install.sh.tmp && \
	mv site/public/install.sh.tmp site/public/install.sh

demo-frames: build
	@echo "Starting mock daemon..."
	@bin/bewitchd -config data/bewitch.toml & DAEMON_PID=$$!; \
	sleep 3; \
	bin/bewitch -config data/bewitch.toml capture-frames \
		--cols 120 --rows 32 --frames 5 --delay 400ms \
		site/public/demo-frames.json; \
	kill $$DAEMON_PID 2>/dev/null; \
	wait $$DAEMON_PID 2>/dev/null || true

docgen:
	go run cmd/docgen/main.go . > site/src/generated/api-schema.json

deploy:
	cd site && bun run build
	@V=$$(cat VERSION) && \
	sed -e 's/^VERSION="[^"]*"/VERSION="'"$$V"'"/' \
	    -e 's/BEWITCH_CHANNEL:-stable/BEWITCH_CHANNEL:-dev/' \
	    site/public/install.sh > site/dist/install-dev.sh
	cd site && wrangler pages deploy dist --project-name=bewitch --commit-dirty=true

release: stamp-install deb-docker apt-upload apt-repo deploy
