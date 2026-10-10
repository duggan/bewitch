.PHONY: build clean install install-local deb deb-docker test test-integration test-verbose apt-repo apt-upload release deploy stamp-install demo-video docgen site site-serve og

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
	sed 's/^VERSION="[^"]*"/VERSION="'"$$V"'"/' site/static/install.sh > site/static/install.sh.tmp && \
	mv site/static/install.sh.tmp site/static/install.sh

# Record the homepage highlight reel (site/demo.tape) against a fresh mock
# daemon running the "incident" scenario, then encode the web version and
# poster into site/static/demo/ (committed). Needs vhs (with ttyd) and ffmpeg.
# The raw recording lands in site/demo-video/out/ (not committed).
DEMO_DIR := /tmp/bewitch-demo
demo-video: build
	@rm -rf $(DEMO_DIR) && mkdir -p $(DEMO_DIR) site/demo-video/out
	@echo "Starting mock daemon (log: $(DEMO_DIR)/daemon.log)..."
	@bin/bewitchd -config scripts/demo-video/bewitch.toml > $(DEMO_DIR)/daemon.log 2>&1 & DAEMON_PID=$$!; \
	vhs site/demo.tape; STATUS=$$?; \
	kill $$DAEMON_PID 2>/dev/null; wait $$DAEMON_PID 2>/dev/null; \
	grep -E 'ALERT|RESOLVED|ERRO' $(DEMO_DIR)/daemon.log || true; \
	[ $$STATUS -eq 0 ] || exit $$STATUS; \
	scripts/demo-video/encode.sh site/demo-video/out/raw.mp4 site/static/demo

docgen:
	go run cmd/docgen/main.go . > site/data/api-schema.json

# Generate site/data/versions.json from VERSION + LATEST_STABLE (drives the docs
# version switcher/banner). Committed too, so a bare `zola build` still works.
site-versions:
	@scripts/gen-site-versions.sh

# Build the static site with Zola (output: site/dist/).
site: site-versions
	cd site && zola build

# Serve the site locally with live reload at http://127.0.0.1:1111.
site-serve: site-versions
	cd site && zola serve

# Regenerate the social/OG card (site/static/og.png). Needs rsvg-convert.
og:
	python3 scripts/gen-og.py

deploy: docgen
	@V=$$(cat VERSION) && \
	sed -i.bak \
	    -e "s|bewitch-[0-9][0-9.]*-linux|bewitch-$$V-linux|g" \
	    -e "s|bewitch_[0-9][0-9.]*-1_|bewitch_$$V-1_|g" \
	    site/content/docs/installation.md
	cd site && zola build
	@V=$$(cat VERSION) && \
	sed 's/^VERSION="[^"]*"/VERSION="'"$$V"'"/' site/static/install.sh > site/dist/install.sh && \
	sed -e 's/^VERSION="[^"]*"/VERSION="'"$$V"'"/' \
	    -e 's/BEWITCH_CHANNEL:-stable/BEWITCH_CHANNEL:-dev/' \
	    site/static/install.sh > site/dist/install-dev.sh
	@mv site/content/docs/installation.md.bak site/content/docs/installation.md
	# The .bak existed in content/ during the build, so Zola copied it into dist —
	# drop the stray before it gets deployed/relocated.
	@rm -f site/dist/docs/installation.md.bak
	# Relocate the main-built docs under /docs/dev/ (mirrors deploy-site.yml); the
	# canonical /docs/ is served from R2 docs/stable/ by functions/docs/[[path]].js.
	cd site/dist/docs && mkdir -p dev && \
	    find . -mindepth 1 -maxdepth 1 ! -name dev -exec mv {} dev/ \;
	cd site && wrangler pages deploy dist --project-name=bewitch --commit-dirty=true

release: stamp-install deb-docker apt-upload apt-repo deploy
