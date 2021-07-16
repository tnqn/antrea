SHELL              := /bin/bash
# go options
GO                 ?= go
LDFLAGS            :=
GOFLAGS            :=
IPSEC              ?= n
BINDIR             ?= $(CURDIR)/bin
GO_FILES           := $(shell find . -type d -name '.cache' -prune -o -type f -name '*.go' -print)
GOPATH             ?= $$($(GO) env GOPATH)
DOCKER_CACHE       := $(CURDIR)/.cache
ANTCTL_BINARY_NAME ?= antctl
OVS_VERSION        := $(shell head -n 1 build/images/deps/ovs-version)
GO_VERSION         := $(shell head -n 1 build/images/deps/go-version)
CNI_BINARIES_VERSION := $(shell head -n 1 build/images/deps/cni-binaries-version)
NANOSERVER_VERSION := $(shell head -n 1 build/images/deps/nanoserver-version)
BUILD_TAG          := $(shell build/images/build-tag.sh)
ifneq ($(IPSEC), n)
BUILD_TAG          := $(BUILD_TAG)-ipsec
endif
WIN_BUILD_TAG      := $(shell echo $(GO_VERSION) $(CNI_BINARIES_VERSION) $(NANOSERVER_VERSION)|md5sum|head -c 10)
GIT_HOOKS          := $(shell find hack/git_client_side_hooks -type f -print)
DOCKER_NETWORK     ?= default
TRIVY_TARGET_IMAGE ?=

GOLANGCI_LINT_VERSION := v1.50.1
GOLANGCI_LINT_BINDIR  := $(CURDIR)/.golangci-bin
GOLANGCI_LINT_BIN     := $(GOLANGCI_LINT_BINDIR)/$(GOLANGCI_LINT_VERSION)/golangci-lint

DOCKER_BUILD_ARGS := --build-arg OVS_VERSION=$(OVS_VERSION)
DOCKER_BUILD_ARGS += --build-arg GO_VERSION=$(GO_VERSION)
DOCKER_BUILD_ARGS += --build-arg BUILD_TAG=$(BUILD_TAG)
WIN_BUILD_ARGS := --build-arg GO_VERSION=$(GO_VERSION)
WIN_BUILD_ARGS += --build-arg CNI_BINARIES_VERSION=$(CNI_BINARIES_VERSION)
WIN_BUILD_ARGS += --build-arg NANOSERVER_VERSION=$(NANOSERVER_VERSION)
WIN_BUILD_ARGS += --build-arg WIN_BUILD_TAG=$(WIN_BUILD_TAG)

.PHONY: all
all: build

include versioning.mk

LDFLAGS += $(VERSION_LDFLAGS)

UNAME_S := $(shell uname -s)
USERID  := $(shell id -u)
GRPID   := $(shell id -g)

.PHONY: install-hooks
install-hooks:
	@echo "===> Copying Antrea git hooks to local <==="
	@mkdir -p .git/hooks
	install $(GIT_HOOKS) .git/hooks/

.PHONY: uninstall-hooks
uninstall-hooks:
	@echo "===> Removing Antrea git hooks from local <==="
	rm $(addprefix .git/hooks/,$(notdir $(GIT_HOOKS)))

.PHONY: bin
bin:
	@mkdir -p $(BINDIR)
	GOOS=linux $(GO) build -o $(BINDIR) $(GOFLAGS) -ldflags '$(LDFLAGS)' antrea.io/antrea/cmd/...

.trivy-bin:
	curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b $@ v0.34.0

check-%:
	@: $(if $(value $*),,$(error $* is undefined))

.PHONY: trivy-scan
trivy-scan: .trivy-bin check-TRIVY_TARGET_IMAGE
	$(CURDIR)/.trivy-bin/trivy image -c .trivy.yml $(TRIVY_TARGET_IMAGE)

.PHONY: antrea-agent
antrea-agent:
	@mkdir -p $(BINDIR)
	GOOS=linux $(GO) build -o $(BINDIR) $(GOFLAGS) -ldflags '$(LDFLAGS)' antrea.io/antrea/cmd/antrea-agent

.PHONY: antrea-agent-release
antrea-agent-release:
	@mkdir -p $(BINDIR)
	@CGO_ENABLED=0 $(GO) build -o $(BINDIR)/$(ANTREA_AGENT_BINARY_NAME) $(GOFLAGS) -ldflags '$(LDFLAGS)' antrea.io/antrea/cmd/antrea-agent

.PHONY: antrea-agent-simulator
antrea-agent-simulator:
	@mkdir -p $(BINDIR)
	GOOS=linux $(GO) build -o $(BINDIR) $(GOFLAGS) -ldflags '$(LDFLAGS)' antrea.io/antrea/cmd/antrea-agent-simulator

.PHONY: antrea-agent-instr-binary
antrea-agent-instr-binary:
	@mkdir -p $(BINDIR)
	GOOS=linux $(GO) test -tags testbincover -covermode count -coverpkg=antrea.io/antrea/pkg/... -c -o $(BINDIR)/antrea-agent-coverage $(GOFLAGS) -ldflags '$(LDFLAGS)' antrea.io/antrea/cmd/antrea-agent


.PHONY: antrea-agent-tweaker
antrea-agent-tweaker:
	@mkdir -p $(BINDIR)
	GOOS=linux $(GO) build -o $(BINDIR) $(GOFLAGS) -ldflags '$(LDFLAGS)' antrea.io/antrea/cmd/antrea-agent-tweaker

.PHONY: antrea-controller
antrea-controller:
	@mkdir -p $(BINDIR)
	GOOS=linux $(GO) build -o $(BINDIR) $(GOFLAGS) -ldflags '$(LDFLAGS)' antrea.io/antrea/cmd/antrea-controller

.PHONY: .coverage
.coverage:
	mkdir -p $(CURDIR)/.coverage

.PHONY: antrea-controller-instr-binary
antrea-controller-instr-binary:
	@mkdir -p $(BINDIR)
	GOOS=linux $(GO) test -tags testbincover -covermode count -coverpkg=antrea.io/antrea/pkg/... -c -o $(BINDIR)/antrea-controller-coverage $(GOFLAGS) -ldflags '$(LDFLAGS)' antrea.io/antrea/cmd/antrea-controller

# diable cgo for antrea-cni since it can be installed on some systems with
# incompatible or missing system libraries.
.PHONY: antrea-cni
antrea-cni:
	@mkdir -p $(BINDIR)
	GOOS=linux CGO_ENABLED=0 $(GO) build -o $(BINDIR) $(GOFLAGS) -ldflags '$(LDFLAGS)' antrea.io/antrea/cmd/antrea-cni

.PHONY: antrea-cni
antrea-cni-release:
	@mkdir -p $(BINDIR)
	@CGO_ENABLED=0 $(GO) build -o $(BINDIR)/$(ANTREA_CNI_BINARY_NAME) $(GOFLAGS) -ldflags '$(LDFLAGS)' antrea.io/antrea/cmd/antrea-cni

.PHONY: antrea-resource-init
antrea-resource-init:
	@mkdir -p $(BINDIR)
	GOOS=linux $(GO) build -o $(BINDIR) $(GOFLAGS) -ldflags '$(LDFLAGS)' antrea.io/antrea/cmd/antrea-resource-init

.PHONY: antctl-instr-binary
antctl-instr-binary:
	@mkdir -p $(BINDIR)
	GOOS=linux $(GO) test -tags testbincover -covermode count -coverpkg=antrea.io/antrea/pkg/... -c -o $(BINDIR)/antctl-coverage $(GOFLAGS) -ldflags '$(LDFLAGS)' antrea.io/antrea/cmd/antctl

# diable cgo for antrea-cni and antrea-agent: antrea-cni is meant to be
# installed on the host and the antrea-agent is run as a process on Windows.
.PHONY: windows-bin
windows-bin:
	@mkdir -p $(BINDIR)
	GOOS=windows CGO_ENABLED=0 $(GO) build -o $(BINDIR) $(GOFLAGS) -ldflags '$(LDFLAGS)' antrea.io/antrea/cmd/antrea-cni antrea.io/antrea/cmd/antrea-agent antrea.io/antrea/cmd/antctl

.PHONY: flow-aggregator
flow-aggregator:
	@mkdir -p $(BINDIR)
	GOOS=linux $(GO) build -o $(BINDIR) $(GOFLAGS) -ldflags '$(LDFLAGS)' antrea.io/antrea/cmd/flow-aggregator

.PHONY: flow-aggregator-instr-binary
flow-aggregator-instr-binary:
	@mkdir -p $(BINDIR)
	GOOS=linux $(GO) test -tags testbincover -covermode count -coverpkg=antrea.io/antrea/pkg/... -c -o $(BINDIR)/flow-aggregator-coverage $(GOFLAGS) -ldflags '$(LDFLAGS)' antrea.io/antrea/cmd/flow-aggregator

.PHONY: test-unit test-integration
ifeq ($(UNAME_S),Linux)
test-unit: .linux-test-unit
test-integration: .linux-test-integration
else ifneq (,$(findstring MSYS_NT-,$(UNAME_S)))
test-unit: .windows-test-unit
test-integration:
	$(error Cannot use target 'test-integration' on Windows, but you can run integration tests with 'docker-test-integration')
else
test-unit:
	$(error Cannot use target 'test-unit' on OS $(UNAME_S), but you can run unit tests with 'docker-test-unit')
test-integration:
	$(error Cannot use target 'test-integration' on a non-Linux OS, but you can run integration tests with 'docker-test-integration')
endif

.PHONY: build
build: build-ubuntu

.PHONY: test
test: golangci
test: build
test: docker-test-unit
test: docker-test-integration

$(DOCKER_CACHE):
	@mkdir -p $@/gopath
	@mkdir -p $@/gocache

# Since the WORKDIR is mounted from host, the $(id -u):$(id -g) user can access it.
# Inside the docker, the user is nameless and does not have a home directory. This is ok for our use case.
DOCKER_ENV := \
	@docker run --rm -u $$(id -u):$$(id -g) \
		-e "GOCACHE=/tmp/gocache" \
		-e "GOPATH=/tmp/gopath" \
		-w /usr/src/antrea.io/antrea \
		-v $(DOCKER_CACHE)/gopath:/tmp/gopath \
		-v $(DOCKER_CACHE)/gocache:/tmp/gocache \
		-v $(CURDIR):/usr/src/antrea.io/antrea \
		golang:$(GO_VERSION)

.PHONY: docker-bin
docker-bin: $(DOCKER_CACHE)
	$(DOCKER_ENV) make bin
	@chmod -R 0755 $<

.PHONY: docker-windows-bin
docker-windows-bin: $(DOCKER_CACHE)
	$(DOCKER_ENV) make windows-bin

.PHONY: docker-test-unit
docker-test-unit: $(DOCKER_CACHE)
	@$(DOCKER_ENV) make test-unit
	@chmod -R 0755 $<

.PHONY: docker-test-integration
docker-test-integration: .coverage
	@echo "===> Building Antrea Integration Test Docker image <==="
ifneq ($(NO_PULL),)
	docker build -t antrea/test -f build/images/test/Dockerfile $(DOCKER_BUILD_ARGS) .
else
	docker build --pull -t antrea/test -f build/images/test/Dockerfile $(DOCKER_BUILD_ARGS) .
endif
	@docker run --privileged --rm \
		-e "GOCACHE=/tmp/gocache" \
		-e "GOPATH=/tmp/gopath" \
		-e "INCONTAINER=true" \
		-w /usr/src/antrea.io/antrea \
		-v $(DOCKER_CACHE)/gopath:/tmp/gopath \
		-v $(DOCKER_CACHE)/gocache:/tmp/gocache \
		-v $(CURDIR)/.coverage:/usr/src/antrea.io/antrea/.coverage \
		-v $(CURDIR):/usr/src/antrea.io/antrea:ro \
		-v /lib/modules:/lib/modules \
		--sysctl net.ipv6.conf.all.disable_ipv6=0 \
		antrea/test test-integration $(USERID) $(GRPID)

.PHONY: docker-tidy
docker-tidy: $(DOCKER_CACHE)
	@rm -f go.sum
	@$(DOCKER_ENV) $(GO) mod tidy
	@rm -f plugins/octant/go.sum
	@$(DOCKER_ENV) bash -c "cd plugins/octant && $(GO) mod tidy"
	@chmod -R 0755 $<
	@chmod 0644 go.sum plugins/octant/go.sum

ANTCTL_BINARIES := antctl-darwin antctl-linux antctl-windows
$(ANTCTL_BINARIES): antctl-%:
	@GOOS=$* $(GO) build -o $(BINDIR)/$@ $(GOFLAGS) -ldflags '$(LDFLAGS)' antrea.io/antrea/cmd/antctl
	@if [[ $@ != *windows ]]; then \
	  chmod 0755 $(BINDIR)/$@; \
	else \
	  mv $(BINDIR)/$@ $(BINDIR)/$@.exe; \
	fi

.PHONY: antctl
antctl: $(ANTCTL_BINARIES)

.PHONY: antctl-release
antctl-release:
	@CGO_ENABLED=0 $(GO) build -o $(BINDIR)/$(ANTCTL_BINARY_NAME) $(GOFLAGS) -ldflags '-s -w $(LDFLAGS)' antrea.io/antrea/cmd/antctl

.PHONY: check-copyright
check-copyright: 
	@GO=$(GO) $(CURDIR)/hack/add-license.sh

.PHONY: add-copyright
add-copyright: 
	@GO=$(GO) $(CURDIR)/hack/add-license.sh --add

.PHONY: .linux-test-unit
.linux-test-unit: .coverage
	@echo
	@echo "==> Running unit tests <=="
	$(GO) test -race -coverpkg=antrea.io/antrea/cmd/...,antrea.io/antrea/pkg/...,antrea.io/antrea/multicluster/cmd/...,antrea.io/antrea/multicluster/controllers/... \
	  -coverprofile=.coverage/coverage-unit.txt -covermode=atomic \
	  antrea.io/antrea/cmd/... antrea.io/antrea/pkg/... antrea.io/antrea/multicluster/cmd/... antrea.io/antrea/multicluster/controllers/...

.PHONY: .windows-test-unit
.windows-test-unit: .coverage
	@echo
	@echo "==> Running unit tests <=="
	$(GO) test -race -coverpkg=antrea.io/antrea/cmd/...,antrea.io/antrea/pkg/... \
	  -coverprofile=.coverage/coverage-unit.txt -covermode=atomic \
	  antrea.io/antrea/cmd/... antrea.io/antrea/pkg/...

.PHONY: tidy
tidy:
	@rm -f go.sum
	@$(GO) mod tidy
	@rm -f plugins/octant/go.sum
	@cd plugins/octant && $(GO) mod tidy

.PHONY: .linux-test-integration
.linux-test-integration: .coverage
	@echo
	@echo "==> Running integration tests <=="
	@echo "SOME TESTS WILL FAIL IF NOT RUN AS ROOT!"
	$(GO) test -v -coverpkg=antrea.io/antrea/pkg/... -coverprofile=.coverage/coverage-integration.txt -covermode=atomic antrea.io/antrea/test/integration/...

test-tidy:
	@echo
	@echo "===> Checking go.mod tidiness <==="
	@GO=$(GO) $(CURDIR)/hack/tidy-check.sh
	@echo "===> Checking octant plugins go.mod tidiness <==="
	@GO=$(GO) $(CURDIR)/hack/tidy-check.sh plugins/octant

.PHONY: fmt
fmt:
	@echo
	@echo "===> Formatting Go files <==="
	@gofmt -s -l -w $(GO_FILES)

$(GOLANGCI_LINT_BIN):
	@echo "===> Installing Golangci-lint <==="
	@rm -rf $(GOLANGCI_LINT_BINDIR)/* # remove old versions
	@curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOLANGCI_LINT_BINDIR)/$(GOLANGCI_LINT_VERSION) $(GOLANGCI_LINT_VERSION)

.PHONY: golangci
golangci: $(GOLANGCI_LINT_BIN)
	@echo "===> Running golangci (linux) <==="
	@GOOS=linux $(GOLANGCI_LINT_BIN) run -c $(CURDIR)/.golangci.yml
	@echo "===> Running golangci (windows) <==="
	@GOOS=windows $(GOLANGCI_LINT_BIN) run -c $(CURDIR)/.golangci.yml
	@echo "===> Running golangci for Octant plugin (linux) <==="
	@cd plugins/octant && GOOS=linux CGO_ENABLED=0 $(GOLANGCI_LINT_BIN) run -c $(CURDIR)/.golangci.yml
	@echo "===> Running golangci for Octant plugin (windows) <==="
	@cd plugins/octant && GOOS=windows CGO_ENABLED=0 $(GOLANGCI_LINT_BIN) run -c $(CURDIR)/.golangci.yml

.PHONY: golangci-fix
golangci-fix: $(GOLANGCI_LINT_BIN)
	@echo "===> Running golangci (linux) <==="
	@GOOS=linux $(GOLANGCI_LINT_BIN) run -c $(CURDIR)/.golangci.yml --fix
	@echo "===> Running golangci (windows) <==="
	@GOOS=windows $(GOLANGCI_LINT_BIN) run -c $(CURDIR)/.golangci.yml --fix
	@echo "===> Running golangci for Octant plugin (linux) <==="
	@cd plugins/octant && GOOS=linux CGO_ENABLED=0 $(GOLANGCI_LINT_BIN) run -c $(CURDIR)/.golangci.yml --fix
	@echo "===> Running golangci for Octant plugin (windows) <==="
	@cd plugins/octant && GOOS=windows CGO_ENABLED=0 $(GOLANGCI_LINT_BIN) run -c $(CURDIR)/.golangci.yml --fix

.PHONY: clean
clean:
	@rm -rf $(BINDIR)
	@rm -rf $(DOCKER_CACHE)
	@rm -rf $(GOLANGCI_LINT_BINDIR)
	@rm -rf .coverage
	@rm -rf .trivy-bin

.PHONY: codegen
codegen:
	@echo "===> Updating generated code <==="
	$(CURDIR)/hack/update-codegen.sh

.PHONY: mockgen
mockgen:
	@echo "===> Updating generated mock code <==="
	$(CURDIR)/hack/update-codegen.sh mockgen

### Docker images ###
.PHONY: debian
debian:
	@echo "===> Building antrea/antrea-debian Docker image <==="
ifneq ($(NO_PULL),)
	docker build -t antrea/antrea-debian:$(DOCKER_IMG_VERSION) -f build/images/Dockerfile.debian $(DOCKER_BUILD_ARGS) .
else
	docker build --pull -t antrea/antrea-debian:$(DOCKER_IMG_VERSION) -f build/images/Dockerfile.debian $(DOCKER_BUILD_ARGS) .
endif
	docker tag antrea/antrea-debian:$(DOCKER_IMG_VERSION) antrea/antrea-debian

.PHONY: ubuntu
ubuntu:
	@echo "===> Building antrea/antrea-ubuntu Docker image <==="
ifneq ($(NO_PULL),)
	docker build -t antrea/antrea-ubuntu:$(DOCKER_IMG_VERSION) -f build/images/Dockerfile.ubuntu $(DOCKER_BUILD_ARGS) .
else
	docker build --pull -t antrea/antrea-ubuntu:$(DOCKER_IMG_VERSION) -f build/images/Dockerfile.ubuntu $(DOCKER_BUILD_ARGS) .
endif
	docker tag antrea/antrea-ubuntu:$(DOCKER_IMG_VERSION) antrea/antrea-ubuntu

# Build bins in a golang container, and build the antrea-ubuntu Docker image.
.PHONY: build-ubuntu
build-ubuntu:
	@echo "===> Building Antrea bins and antrea/antrea-ubuntu Docker image <==="
ifneq ($(NO_PULL),)
	docker build -t antrea/antrea-ubuntu:$(DOCKER_IMG_VERSION) -f build/images/Dockerfile.build.ubuntu $(DOCKER_BUILD_ARGS) .
else
	docker build --pull -t antrea/antrea-ubuntu:$(DOCKER_IMG_VERSION) -f build/images/Dockerfile.build.ubuntu $(DOCKER_BUILD_ARGS) .
endif
	docker tag antrea/antrea-ubuntu:$(DOCKER_IMG_VERSION) antrea/antrea-ubuntu

# Build bins in a golang container, and build the antrea-ubi Docker image.
.PHONY: build-ubi
build-ubi:
	@echo "===> Building Antrea bins and antrea/antrea-ubi Docker image <==="
ifneq ($(NO_PULL),"")
	docker build -t antrea/antrea-ubi:$(DOCKER_IMG_VERSION) -f build/images/Dockerfile.build.ubi $(DOCKER_BUILD_ARGS) .
else
	docker build --pull -t antrea/antrea-ubi:$(DOCKER_IMG_VERSION) -f build/images/Dockerfile.build.ubi $(DOCKER_BUILD_ARGS) .
endif
	docker tag antrea/antrea-ubi:$(DOCKER_IMG_VERSION) antrea/antrea-ubi

.PHONY: build-windows
build-windows:
	@echo "===> Building Antrea bins and antrea/antrea-windows Docker image <==="
ifneq ($(NO_PULL),)
	docker build -t antrea/antrea-windows:$(DOCKER_IMG_VERSION) -f build/images/Dockerfile.build.windows --network $(DOCKER_NETWORK) $(WIN_BUILD_ARGS) .
else
	docker build --pull -t antrea/antrea-windows:$(DOCKER_IMG_VERSION) -f build/images/Dockerfile.build.windows --network $(DOCKER_NETWORK) $(WIN_BUILD_ARGS) .
endif
	docker tag antrea/antrea-windows:$(DOCKER_IMG_VERSION) antrea/antrea-windows

.PHONY: build-ubuntu-coverage
build-ubuntu-coverage:
	@echo "===> Building Antrea bins and antrea/antrea-ubuntu-coverage Docker image <==="
ifneq ($(NO_PULL),)
	docker build -t antrea/antrea-ubuntu-coverage:$(DOCKER_IMG_VERSION) -f build/images/Dockerfile.build.coverage $(DOCKER_BUILD_ARGS) .
else
	docker build --pull -t antrea/antrea-ubuntu-coverage:$(DOCKER_IMG_VERSION) -f build/images/Dockerfile.build.coverage $(DOCKER_BUILD_ARGS) .
endif
	docker tag antrea/antrea-ubuntu-coverage:$(DOCKER_IMG_VERSION) antrea/antrea-ubuntu-coverage

.PHONY: build-scale-simulator
build-scale-simulator:
	@echo "===> Building simulator bin and antrea-ubuntu-simulator image"
	docker build -t antrea/antrea-ubuntu-simulator:$(DOCKER_IMG_VERSION) -f build/images/Dockerfile.simulator.build.ubuntu $(DOCKER_BUILD_ARGS) .
	docker tag antrea/antrea-ubuntu-simulator:$(DOCKER_IMG_VERSION) antrea/antrea-ubuntu-simulator

.PHONY: manifest
manifest:
	@echo "===> Generating dev manifest for Antrea <==="
	$(CURDIR)/hack/generate-standard-manifests.sh --mode dev --out build/yamls
	$(CURDIR)/hack/generate-manifest-octant.sh --mode dev > build/yamls/antrea-octant.yml
	$(CURDIR)/hack/generate-manifest-windows.sh --mode dev > build/yamls/antrea-windows.yml
	$(CURDIR)/hack/generate-manifest-flow-aggregator.sh --mode dev > build/yamls/flow-aggregator.yml

.PHONY: manifest-scale
manifest-scale:
	@echo "===> Generating simulator manifest for Antrea <==="
	$(CURDIR)/hack/generate-manifest.sh --mode dev --simulator > build/yamls/antrea-scale.yml

.PHONY: manifest-coverage
manifest-coverage:
	$(CURDIR)/hack/generate-manifest.sh --mode dev --coverage > build/yamls/antrea-coverage.yml
	$(CURDIR)/hack/generate-manifest.sh --mode dev --ipsec --coverage > build/yamls/antrea-ipsec-coverage.yml
	$(CURDIR)/hack/generate-manifest-flow-aggregator.sh --mode dev --coverage > build/yamls/flow-aggregator-coverage.yml

.PHONY: octant-antrea-ubuntu
octant-antrea-ubuntu:
	@echo "===> Building antrea/octant-antrea-ubuntu Docker image <==="
	docker build --pull -t antrea/octant-antrea-ubuntu:$(DOCKER_IMG_VERSION) -f build/images/Dockerfile.octant.ubuntu $(DOCKER_BUILD_ARGS) .
	docker tag antrea/octant-antrea-ubuntu:$(DOCKER_IMG_VERSION) antrea/octant-antrea-ubuntu

.PHONY: antrea-mc-controller
antrea-mc-controller:
	@echo "===> Building antrea/antrea-mc-controller Docker image <==="
ifneq ($(NO_PULL),)
	docker build -t antrea/antrea-mc-controller:$(DOCKER_IMG_VERSION) -f multicluster/build/images/Dockerfile.build $(DOCKER_BUILD_ARGS) .
else
	docker build --pull -t antrea/antrea-mc-controller:$(DOCKER_IMG_VERSION) -f multicluster/build/images/Dockerfile.build $(DOCKER_BUILD_ARGS) .
endif
	docker tag antrea/antrea-mc-controller:$(DOCKER_IMG_VERSION) antrea/antrea-mc-controller

.PHONY: antrea-mc-controller-coverage
antrea-mc-controller-coverage:
	@echo "===> Building antrea/antrea-mc-controller-coverage Docker image <==="
ifneq ($(NO_PULL),)
	docker build -t antrea/antrea-mc-controller-coverage:$(DOCKER_IMG_VERSION) -f multicluster/build/images/Dockerfile.build.coverage $(DOCKER_BUILD_ARGS) .
else
	docker build --pull -t antrea/antrea-mc-controller-coverage:$(DOCKER_IMG_VERSION) -f multicluster/build/images/Dockerfile.build.coverage $(DOCKER_BUILD_ARGS) .
endif
	docker tag antrea/antrea-mc-controller-coverage:$(DOCKER_IMG_VERSION) antrea/antrea-mc-controller-coverage

.PHONY: flow-aggregator-image
flow-aggregator-image:
	@echo "===> Building antrea/flow-aggregator Docker image <==="
ifneq ($(NO_PULL),)
	docker build -t antrea/flow-aggregator:$(DOCKER_IMG_VERSION) -f build/images/flow-aggregator/Dockerfile $(DOCKER_BUILD_ARGS) .
else
	docker build --pull -t antrea/flow-aggregator:$(DOCKER_IMG_VERSION) -f build/images/flow-aggregator/Dockerfile $(DOCKER_BUILD_ARGS) .
endif
	docker tag antrea/flow-aggregator:$(DOCKER_IMG_VERSION) antrea/flow-aggregator

.PHONY: flow-aggregator-image-debian
flow-aggregator-image-debian:
	@echo "===> Building antrea/flow-aggregator Docker image <==="
ifneq ($(NO_PULL),)
	docker build -t antrea/flow-aggregator-debian:$(DOCKER_IMG_VERSION) -f build/images/flow-aggregator/Dockerfile.debian $(DOCKER_BUILD_ARGS) .
else
	docker build --pull -t antrea/flow-aggregator-debian:$(DOCKER_IMG_VERSION) -f build/images/flow-aggregator/Dockerfile.debian $(DOCKER_BUILD_ARGS) .
endif
	docker tag antrea/flow-aggregator-debian:$(DOCKER_IMG_VERSION) antrea/flow-aggregator-debian
	docker tag antrea/flow-aggregator-debian:$(DOCKER_IMG_VERSION) projects.registry.vmware.com/antrea/flow-aggregator-debian
	docker tag antrea/flow-aggregator-debian:$(DOCKER_IMG_VERSION) projects.registry.vmware.com/antrea/flow-aggregator-debian:$(DOCKER_IMG_VERSION)

.PHONY: flow-aggregator-ubuntu-coverage
flow-aggregator-ubuntu-coverage:
	@echo "===> Building antrea/flow-aggregator-coverage Docker image <==="
ifneq ($(NO_PULL),)
	docker build -t antrea/flow-aggregator-coverage:$(DOCKER_IMG_VERSION) -f build/images/flow-aggregator/Dockerfile.coverage $(DOCKER_BUILD_ARGS) .
else
	docker build --pull -t antrea/flow-aggregator-coverage:$(DOCKER_IMG_VERSION) -f build/images/flow-aggregator/Dockerfile.coverage $(DOCKER_BUILD_ARGS) .
endif
	docker tag antrea/flow-aggregator-coverage:$(DOCKER_IMG_VERSION) antrea/flow-aggregator-coverage

.PHONY: verify
verify:
	@echo "===> Verifying spellings <==="
	GO=$(GO) $(CURDIR)/hack/verify-spelling.sh
	@echo "===> Verifying Table of Contents <==="
	GO=$(GO) $(CURDIR)/hack/verify-toc.sh
	@echo "===> Verifying documentation formatting for website <==="
	$(CURDIR)/hack/verify-docs-for-website.sh

.PHONY: toc
toc:
	@echo "===> Generating Table of Contents for Antrea docs <==="
	GO=$(GO) $(CURDIR)/hack/update-toc.sh

.PHONE: markdownlint
markdownlint:
	@echo "===> Running markdownlint <==="
	markdownlint -c hack/.markdownlint-config.yml -p hack/.markdownlint-ignore .

.PHONE: markdownlint-fix
markdownlint-fix:
	@echo "===> Running markdownlint <==="
	markdownlint --fix -c hack/.markdownlint-config.yml -p hack/.markdownlint-ignore .

.PHONY: spelling-fix
spelling-fix:
	@echo "===> Updating incorrect spellings <==="
	$(CURDIR)/hack/update-spelling.sh
