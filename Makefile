CONTROLLER_GEN ?= go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.16.5

.PHONY: all generate manifests fmt vet build test

all: generate manifests fmt vet build test

generate:
	$(CONTROLLER_GEN) object paths="./api/..."

manifests:
	$(CONTROLLER_GEN) crd rbac:roleName=dispatch-operator paths="./..." \
		output:crd:artifacts:config=config/crd/bases \
		output:rbac:artifacts:config=config/rbac
	mkdir -p charts/dispatch/crds
	cp config/crd/bases/*.yaml charts/dispatch/crds/
	cp config/rbac/role.yaml charts/dispatch/templates/clusterrole.yaml

.PHONY: chart
chart:
	helm lint charts/dispatch
	helm template dispatch charts/dispatch --namespace dispatch >/dev/null

fmt:
	go fmt ./...

vet:
	go vet ./...

build:
	go build ./...

test:
	go test ./...
