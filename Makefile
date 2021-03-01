# Current Operator version
VERSION := $(shell git describe --tags --dirty --match 'v*' 2> /dev/null || git describe --always --dirty)
BUILDDATE := $(shell date -u '+%Y-%m-%dT%H:%M:%S.%NZ')
# Default bundle image tag
BUNDLE_IMG ?= quay.io/backube/scribe-bundle:$(VERSION)
# Options for 'bundle-build'
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

TEST_ARGS ?= -progress -randomizeAllSpecs -randomizeSuites -slowSpecThreshold 30 -p -cover -coverprofile cover.out -outputdir .

GOLANGCI_VERSION := v1.31.0
HELM_VERSION := v3.5.0
OPERATOR_SDK_VERSION := v1.0.1
KUTTL_VERSION := 0.7.2
export SHELL := /bin/bash

# Image URL to use all building/pushing image targets
IMAGE ?= quay.io/backube/scribe:latest
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true,crdVersions=v1"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN := $(shell go env GOPATH)/bin
else
GOBIN := $(shell go env GOBIN)
endif
export PATH := $(PATH):$(GOBIN)

all: manager manifests

# Run tests
.PHONY: test
ENVTEST_ASSETS_DIR=$(shell pwd)/testbin
test: generate manifests golangci-lint ginkgo helm-lint
	$(GOLANGCILINT) run ./...
	mkdir -p ${ENVTEST_ASSETS_DIR}
	test -f ${ENVTEST_ASSETS_DIR}/setup-envtest.sh || curl -sSLo ${ENVTEST_ASSETS_DIR}/setup-envtest.sh https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/master/hack/setup-envtest.sh
	source ${ENVTEST_ASSETS_DIR}/setup-envtest.sh; fetch_envtest_tools $(ENVTEST_ASSETS_DIR); setup_envtest_env $(ENVTEST_ASSETS_DIR); ginkgo $(TEST_ARGS) ./...

.PHONY: helm-lint
helm-lint: helm
	cd helm && $(HELM) lint scribe

# Build manager binary
.PHONY: manager
manager: generate
	go build -o bin/manager -ldflags -X=main.scribeVersion=$(VERSION) main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
.PHONY: run
run: generate manifests
	go run -ldflags -X=main.scribeVersion=$(VERSION) ./main.go

# Install CRDs into a cluster
.PHONY: install
install: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

# Uninstall CRDs from a cluster
.PHONY: uninstall
uninstall: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
.PHONY: deploy
deploy: manifests kustomize
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMAGE}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

# Deploy controller in the configured OpenShift cluster in ~/.kube/config
.PHONY: deploy-openshift
deploy-openshift: manifests kustomize
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMAGE}
	$(KUSTOMIZE) build config/openshift | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
.PHONY: manifests
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	cp config/crd/bases/* helm/scribe/crds

# Generate code
.PHONY: generate
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

# Build the docker image
.PHONY: docker-build
docker-build:
	docker build --build-arg "VERSION=$(VERSION)" . -t ${IMAGE}

# Push the docker image
.PHONY: docker-push
docker-push:
	docker push ${IMAGE}

# find or download controller-gen
# download controller-gen if necessary
.PHONY: controller-gen
controller-gen:
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.3.0 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

.PHONY: kustomize
kustomize:
ifeq (, $(shell which kustomize))
	@{ \
	set -e ;\
	KUSTOMIZE_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$KUSTOMIZE_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/kustomize/kustomize/v3@v3.5.4 ;\
	rm -rf $$KUSTOMIZE_GEN_TMP_DIR ;\
	}
KUSTOMIZE=$(GOBIN)/kustomize
else
KUSTOMIZE=$(shell which kustomize)
endif

# Generate bundle manifests and metadata, then validate generated files.
.PHONY: bundle
bundle: manifests
	$(OPERATOR_SDK) generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMAGE)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
	$(OPERATOR_SDK) bundle validate ./bundle

# Build the bundle image.
.PHONY: bundle-build
bundle-build:
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: golangci-lint
GOLANGCI_URL := https://install.goreleaser.com/github.com/golangci/golangci-lint.sh
golangci-lint:
ifeq (, $(shell which golangci-lint))
	curl -fL ${GOLANGCI_URL} | sh -s -- -b ${GOBIN} ${GOLANGCI_VERSION}
GOLANGCILINT=$(GOBIN)/golangci-lint
else
GOLANGCILINT=$(shell which golangci-lint)
endif

.PHONY: operator-sdk
OPERATOR_SDK_URL := https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk-$(OPERATOR_SDK_VERSION)-x86_64-linux-gnu
operator-sdk:
ifeq (, $(shell which operator-sdk))
	mkdir -p ${GOBIN}
	curl -fL "${OPERATOR_SDK_URL}" > "${GOBIN}/operator-sdk"
	chmod a+x "${GOBIN}/operator-sdk"
OPERATOR_SDK=$(GOBIN)/operator-sdk
else
OPERATOR_SDK=$(shell which operator-sdk)
endif

.PHONY: ginkgo
ginkgo:
ifeq (, $(shell which ginkgo))
	go get github.com/onsi/ginkgo/ginkgo
GINKGO=$(GOBIN)/ginkgo
else
GINKGO=$(shell which ginkgo)
endif

.PHONY: kuttl
KUTTL_URL := https://github.com/kudobuilder/kuttl/releases/download/v$(KUTTL_VERSION)/kubectl-kuttl_$(KUTTL_VERSION)_linux_x86_64
kuttl:
ifeq (, $(shell which kubectl-kuttl))
	mkdir -p ${GOBIN}
	curl -fL "${KUTTL_URL}" > "${GOBIN}/kubectl-kuttl"
	chmod a+x "${GOBIN}/kubectl-kuttl"
endif

# Prior to running these tests, you should have a cluster available and Scribe
# should be running
.PHONY: test-e2e
test-e2e: kuttl
	cd test-kuttl && kubectl kuttl test
	rm -f test-kuttl/kubeconfig

.PHONY: helm
HELM_URL := https://get.helm.sh/helm-$(HELM_VERSION)-linux-amd64.tar.gz
helm:
ifeq (, $(shell which helm))
	mkdir -p ${GOBIN}
	curl -fL "${HELM_URL}" > "${GOBIN}/helm"
	chmod a+x "${GOBIN}/helm"
HELM=$(GOBIN)/helm
else
HELM=$(shell which helm)
endif

# Generate package manifests.
.PHONY: packagemanifests
packagemanifests: kustomize manifests
	operator-sdk generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMAGE)
	$(KUSTOMIZE) build config/manifests | operator-sdk generate packagemanifests -q --version 0.1.0 --verbose

.PHONY: cleanup-operator
cleanup-operator:
	operator-sdk cleanup scribe

.PHONY: run-operator
run-operator:
	operator-sdk run packagemanifests --version ${OPERATOR_VERSION}
