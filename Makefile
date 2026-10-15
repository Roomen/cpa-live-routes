PLUGIN_ID := live-routes
GOOS_HOST := $(shell go env GOOS)
GOARCH_HOST := $(shell go env GOARCH)
EXT_darwin := dylib
EXT_linux := so
EXT_windows := dll
EXT_HOST := $(EXT_$(GOOS_HOST))
LDFLAGS := -trimpath -ldflags "-s -w"
VERSION ?=
PLUGINS_DIR ?=

.PHONY: test build darwin dist install-local e2e clean

test:
	go vet ./...
	go test -race ./...

build:
	@mkdir -p bin
	CGO_ENABLED=1 go build -buildmode=c-shared $(LDFLAGS) -o bin/$(PLUGIN_ID).$(EXT_HOST) .
	@rm -f bin/$(PLUGIN_ID).h

# darwin/amd64 must be built with the TLS-slot toolchain (scripts/darwin-amd64-go.sh); stock Go crashes CPA there.
MACOS_MIN := 12.0
DARWIN_ENV := CGO_ENABLED=1 GOOS=darwin MACOSX_DEPLOYMENT_TARGET=$(MACOS_MIN) CGO_CFLAGS="-O2 -g -mmacosx-version-min=$(MACOS_MIN)" CGO_LDFLAGS="-mmacosx-version-min=$(MACOS_MIN)"
TLS_GO_DIR ?= $(HOME)/Library/Caches/live-routes-tlsgo

darwin:
	@mkdir -p bin/darwin_arm64 bin/darwin_amd64
	$(DARWIN_ENV) GOARCH=arm64 CC="clang -arch arm64" go build -buildmode=c-shared $(LDFLAGS) -o bin/darwin_arm64/$(PLUGIN_ID).dylib .
	goroot=$$(scripts/darwin-amd64-go.sh "$(TLS_GO_DIR)") && \
	  GOROOT="$$goroot" PATH="$$goroot/bin:$$PATH" GOTOOLCHAIN=local $(DARWIN_ENV) GOARCH=amd64 CC="clang -arch x86_64" \
	  go build -buildmode=c-shared $(LDFLAGS) -o bin/darwin_amd64/$(PLUGIN_ID).dylib .
	@rm -f bin/darwin_arm64/$(PLUGIN_ID).h bin/darwin_amd64/$(PLUGIN_ID).h
	scripts/check-darwin.sh bin/darwin_arm64/$(PLUGIN_ID).dylib arm64 $(MACOS_MIN)
	scripts/check-darwin.sh bin/darwin_amd64/$(PLUGIN_ID).dylib amd64 $(MACOS_MIN)

# Zips every bin/<goos>_<goarch>/ directory present. The library sits at the zip root.
dist:
	@test -n "$(VERSION)" || { echo "usage: make dist VERSION=x.y.z"; exit 2; }
	@rm -rf dist && mkdir -p dist
	@found=0; for d in bin/*_*/; do \
	  [ -d "$$d" ] || continue; \
	  t=$$(basename "$$d"); os=$${t%%_*}; arch=$${t#*_}; \
	  case "$$os" in darwin) ext=dylib;; linux) ext=so;; windows) ext=dll;; *) echo "skip $$t"; continue;; esac; \
	  lib="$${d}$(PLUGIN_ID).$$ext"; \
	  [ -f "$$lib" ] || { echo "skip $$t: $$lib missing"; continue; }; \
	  zipname="$(PLUGIN_ID)_$(VERSION)_$${os}_$${arch}.zip"; \
	  (cd "$$d" && zip -q -j "$(CURDIR)/dist/$$zipname" "$(PLUGIN_ID).$$ext") || exit 1; \
	  echo "packed dist/$$zipname"; found=1; \
	done; \
	[ "$$found" = 1 ] || { echo "no bin/<goos>_<goarch>/ builds found"; exit 1; }
	@cd dist && { command -v sha256sum >/dev/null 2>&1 && sha256sum *.zip || shasum -a 256 *.zip; } > checksums.txt
	@cat dist/checksums.txt

install-local: build
	@test -n "$(PLUGINS_DIR)" || { echo "usage: make install-local PLUGINS_DIR=/path/to/plugins"; exit 2; }
	mkdir -p "$(PLUGINS_DIR)/$(GOOS_HOST)/$(GOARCH_HOST)"
	cp bin/$(PLUGIN_ID).$(EXT_HOST) "$(PLUGINS_DIR)/$(GOOS_HOST)/$(GOARCH_HOST)/$(PLUGIN_ID).$(EXT_HOST)"
	@echo "installed to $(PLUGINS_DIR)/$(GOOS_HOST)/$(GOARCH_HOST)/"

e2e:
	bash scripts/e2e/run.sh

clean:
	rm -rf bin dist
