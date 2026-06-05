 # +-------------------------------------------------------------------+
 # | (C) Copyright IBM Corp. 2025, 2026                                |
 # | SPDX-License-Identifier: Apache-2.0                               |
 # +-------------------------------------------------------------------+
# Enable automatic Go toolchain management
export GOTOOLCHAIN = auto

GOLANG_VERSION		?= $(shell cd $(REPO_ROOT) && go list -f {{.GoVersion}} -m)
BUILDER_IMAGE		?= registry.access.redhat.com/ubi9/go-toolset:9.6-1745588370
GOTOOLCHAIN			?= go$(GOLANG_VERSION)
MAKEFILE_PATH		:= $(abspath $(lastword $(MAKEFILE_LIST)))
REPO_ROOT 			:= $(abspath $(patsubst %/,%,$(dir $(MAKEFILE_PATH))))
CURRENT_DIR			:= $(shell pwd)
VERSION				?= $(shell cat $(REPO_ROOT)/VERSION)
REGISTRY			?= docker.io/spyre-operator
DOCKER				?= $(shell command -v podman 2> /dev/null || echo docker)
DOCKERFILE			= $(REPO_ROOT)/images/Dockerfile.ubi9
DOCKER_BUILD_OPTS	?= --progress=plain
IMAGE_NAME 			:= $(REGISTRY)/spyre-device-plugin
IMAGE_TAG 			?= $(VERSION)
IMAGE 				?= $(IMAGE_NAME):$(IMAGE_TAG)
TEST_IMG			?= $(IMAGE_NAME):dev
CODECOV_PERCENT		?= 51
KUBECTL				?= $(shell command -v oc 2> /dev/null || echo kubectl)
OC					?= $(shell command -v oc)
GOCOVERDIR			?= $(REPO_ROOT)

# Operating system
OS					?= $(shell go env GOOS)
ARCH				?= $(shell go env GOARCH)
LDFLAGS				=

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
ENVTEST			?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT	?= $(LOCALBIN)/golangci-lint
GOVULCHECK		?= $(LOCALBIN)/govulncheck
GINKGO			?= $(LOCALBIN)/ginkgo
YQ				?= $(LOCALBIN)/yq
KIND			?= $(LOCALBIN)/kind
CONTROLLER_GEN	?= $(LOCALBIN)/controller-gen

## Tool Versions
CONTROLLER_TOOLS_VERSION 	?= v0.17.3
ENVTEST_K8S_VERSION			?= 1.31
GOLANGCI_LINT_VERSION		?= 2.11.4
GINKGO_VERSION				?= v2.28.1
YQ_VERSION 					?= v4.29.2
KIND_VERSION				?= 0.20.0
PYTHON                      ?= python3
PIP                         ?= pip3

# detect-secrets
DETECT_SECRETS_GIT ?= "https://github.com/ibm/detect-secrets.git@master\#egg=detect-secrets"

# Shamesly copied from: https://github.com/opendatahub-io/opendatahub-operator/blob/a08c94a226585e43387ad263e2653c0fd43130f1/Makefile#L132C1-L139C1
define go-mod-version
$(shell go mod graph | grep $(1) 2>/dev/null | head -n 1 | cut -d'@' -f 2)
endef

# Using controller-gen to fetch external CRDs and put them in config/crd/external folder
# They're used in tests, as they have to be created for controller to work
define fetch-external-crds
GOFLAGS="-mod=readonly" $(CONTROLLER_GEN) crd \
paths=$(shell go env GOPATH)/pkg/mod/$(1)@$(call go-mod-version,$(1))/$(2)/... \
output:crd:artifacts:config=config/crd/external
endef

DOCKER_GO_BUILD_FLAGS ?= -race

##@ RPM repo configuration

REPO_BASE            ?= na.artifactory.swg-devops.com/artifactory/wcp-ai-foundation-team-spyre-rpm-local/spyre/rhel9
RPM_GPG_CHECK        ?= 1
DOCKERFILE_LOCAL     := $(REPO_ROOT)/images/Dockerfile.local

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: all
all: build ## Build all defined targets


.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-25s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: version
version: ## Display image version
	@echo "Image version: $(VERSION)"

.PHONY: echo-version
echo-version: ## Print (echo) the current version
	$(info $(VERSION))
	@echo > /dev/null

##@ Development tools
.PHONY: ginkgo
ginkgo: $(GINKGO) ## Download and install ginkgo
$(GINKGO):$(LOCALBIN)
	GOBIN=$(LOCALBIN) go install github.com/onsi/ginkgo/v2/ginkgo@$(GINKGO_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download and install setup-envtest
$(ENVTEST):$(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@v0.0.0-20240624150636-162a113134de

GOLANGCI_LINT_INSTALL_SCRIPT ?= 'https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh'
.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ### Download golangci-lint locally if necessary.
$(GOLANGCI_LINT):$(LOCALBIN)
	test -s $(GOLANGCI_LINT) || { curl -sSfL $(GOLANGCI_LINT_INSTALL_SCRIPT) | sh -s -- -b $(LOCALBIN)  v$(GOLANGCI_LINT_VERSION); }

.PHONY: kind
kind: $(KIND) ## Download kind locally if necessary
$(KIND):$(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/kind@v$(KIND_VERSION)

.PHONY: yq
yq: $(YQ) ## Download yq locally if necessary.
$(YQ): $(LOCALBIN)
	test -s $(YQ) || GOBIN=$(LOCALBIN) go install github.com/mikefarah/yq/v4@$(YQ_VERSION)

.PHONY: govulncheck
govulncheck: $(GOVULCHECK) ## Download govulncheck tool if necessary
$(GOVULCHECK): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install golang.org/x/vuln/cmd/govulncheck@latest

.PHONY: controller-gen
controller-gen: $(LOCALBIN) $(CONTROLLER_GEN) ## Download controller-gen if necessary
$(CONTROLLER_GEN): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: venv
venv: ## Setup and activate venv
	$(PYTHON) -m venv venv

PHONY: clean
clean: ## Clean-up intermediate artifacts
	-rm -rf $(LOCALBIN)
	-rm -rf local.mk

##@ Test targets

COVERAGE_FILE := coverage.out
.PHONY: test
test: envtest ginkgo controller-gen vendor fmt vet ## Run unit tests
	$(call fetch-external-crds,github.com/ibm-aiu/spyre-operator,api/v1alpha1)
	CGO_ENABLED=1 GOARCH=$(ARCH) KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" $(GINKGO) -r --label-filter="!e2e" --cover --coverprofile=$(COVERAGE_FILE) --race -v
	go tool cover -func $(COVERAGE_FILE)
	go tool cover -html $(COVERAGE_FILE) -o coverage-report.html
	@percentage=$$(go tool cover -func=$(COVERAGE_FILE) | grep ^total | awk '{print $$3}' | tr -d '%'); \
		if (( $$(echo "$$percentage < $(CODECOV_PERCENT)" | bc -l) )); then \
			echo "----------"; \
			echo "Total test coverage ($${percentage}%) is less than the coverage threshold ($(CODECOV_PERCENT)%)."; \
			exit 1; \
		fi

.PHONY: e2e-test
e2e-test: envtest kind ## Run e2e test
	CGO_ENABLED=1 KUBEBUILDER_ASSETS="$(KUBEBUILDER_ASSETS)" cd test && $(GINKGO) --json-report ./ginkgo.report -focus "openshift-spyre-device-plugin e2e test" -r

.PHONY: test-image
test-image: ginkgo image ## Run unit tests using the test image
	$(DOCKER) tag $(IMAGE) $(TEST_IMAGE)

.PHONY: test-image-push
test-image-push: ## Push the test image
	$(DOCKER) push $(TEST_IMAGE)

##@ Development Targets

.PHONY: fmt
fmt: ## Run the formatter
	go fmt ./...

.PHONY: vet
vet: vendor ## Run the vet command
	CGO_ENABLED=0 go vet -mod vendor ./...

.PHONY: tidy
tidy: ## Run tidy
	go mod tidy

.PHONY: vendor
vendor: tidy ## Run vendor
	go mod vendor

.PHONY: build
build: vendor ## Build local binary
	go build -mod vendor $(LDFLAGS) -race -o $(LOCALBIN)/spyre-device-plugin ./cmd/spyre

.PHONY: lint
lint: golangci-lint vendor  ## Run golangci-lint against code.
	$(GOLANGCI_LINT) run --config $(REPO_ROOT)/.golangci.yaml

.PHONY: lint-fix
lint-fix: golangci-lint vendor ## Run golangci-lint against code.
	$(GOLANGCI_LINT) run --fix --config $(REPO_ROOT)/.golangci.yaml

.PHONY: vulcheck
vulcheck: govulncheck ## Scan for golang vulnerabilities
	CGO_ENABLED=0 $(GOVULCHECK) -show verbose	 ./...

##@ Image operations

.PHONY: docker-build
docker-build: vendor ## Build spyre device plugin image for build host architecture
	$(DOCKER) build $(DOCKER_BUILD_OPTS) --pull \
	--tag $(IMAGE) \
	--build-arg VERSION="$(VERSION)" \
	--build-arg BUILDER_IMAGE="$(BUILDER_IMAGE)" \
	--build-arg BUILD_FLAGS="$(DOCKER_GO_BUILD_FLAGS)" \
	--file $(DOCKERFILE) $(CURDIR)

.PHONY: docker-push
docker-push: ## Push spyre device plugin image for the build host architecture.
	$(DOCKER) push $(IMAGE)

.PHONY: docker-build-push
docker-build-push: docker-build docker-push ## Build and push the spyre device plugin image for the build host

##@ Secret detection targets

.PHONY: detect-secrets-install
detect-secrets-install: venv ## Install detect-secret tool
	. venv/bin/activate; $(PIP) install "git+$(DETECT_SECRETS_GIT)"

.PHONY: secrets-scan
secrets-scan: venv detect-secrets-install ## Scan secrets and create secret-baseline for repo
	. venv/bin/activate; detect-secrets scan --exclude-files go.sum --update .secrets.baseline --no-ghe-scan

.PHONY: secrets-audit
secrets-audit: venv detect-secrets-install ## Audit secrets
	. venv/bin/activate; detect-secrets audit .secrets.baseline

# helper target for viewing the value of makefile variables.
print-%  : ;@echo $* = $($*)
