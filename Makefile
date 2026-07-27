NAME := harmos
PACKAGE := github.com/pottom/harmos
CMD := cmd/harmos

# harmos stamps its version from git so a locally built binary never claims to be
# "dev": git describe reports the last vX.Y.Z tag, how many commits sit past it, and
# the short commit (e.g. v0.1.0, or v0.1.0-3-gabc1234). --dirty appends "-dirty" when
# the working tree has uncommitted changes, so a build from a modified tree can never
# masquerade as a clean release. An untagged tree (before any tag, or a shallow clone
# without tags) falls through to the -dev string below.
VERSION := $(shell git describe --tags --dirty 2>/dev/null)
ifeq ($(VERSION),)
VERSION := v0.0.0-dev
endif

COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE := $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
GOBIN := go

# CGO stays on: the darwin concealed-clipboard path (spec §4a) needs it, and a native
# build defaults to CGO on anyway. Release cross-builds are GoReleaser's job (see
# .goreleaser.yaml), not this file's — the Makefile keeps the development targets.
GOFLAGS ?= -trimpath -mod=readonly
LDFLAGS := -s -w \
	-X '$(PACKAGE)/internal/version.Version=$(VERSION)' \
	-X '$(PACKAGE)/internal/version.GitCommit=$(COMMIT)' \
	-X '$(PACKAGE)/internal/version.BuildDate=$(DATE)'

# build puts the runnable, stripped binary at the repo root — where harmos is run from
# during development, never dist/ or a temp dir.
build:
	@echo "Version: $(VERSION)"
	GOFLAGS="$(GOFLAGS)" $(GOBIN) build -ldflags="$(LDFLAGS)" -o ./$(NAME) ./$(CMD)

# install stamps the same version onto the binary in $GOBIN/$GOPATH/bin.
install:
	@echo "Version: $(VERSION)"
	GOFLAGS="$(GOFLAGS)" $(GOBIN) install -ldflags="$(LDFLAGS)" ./$(CMD)

run:
	$(GOBIN) run ./$(CMD)

test:
	$(GOBIN) test ./...

lint:
	golangci-lint run -c .golangci.yml

clean:
	$(GOBIN) mod tidy
	-rm -f ./$(NAME)
	-rm -r dist

.PHONY: build install run test lint clean
