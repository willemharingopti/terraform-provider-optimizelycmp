PROVIDER   := optimizelycmp
# GoReleaser is downloaded and run by `make check` and `make snapshot`. Pin it to a specific version, for example
# github.com/goreleaser/goreleaser/v2@v2.x.y, once you have chosen one: "latest" runs whatever was published most recently.
GORELEASER ?= github.com/goreleaser/goreleaser/v2@latest
NAMESPACE  ?= willemharingopti
VERSION    ?= 0.1.0
OS_ARCH    := $(shell go env GOOS)_$(shell go env GOARCH)
MIRROR     := $(HOME)/.terraform.d/plugins/registry.terraform.io/$(NAMESPACE)/$(PROVIDER)/$(VERSION)/$(OS_ARCH)

.PHONY: build test testacc testacc-residue fmt vet docs install snapshot check

build:
	go build -o terraform-provider-$(PROVIDER) .

fmt:
	gofmt -w .

vet:
	go vet ./...

test:
	go test ./... -count=1

# Acceptance tests run against a REAL CMP account (use a sandbox). Credentials come from OPTIMIZELY_CMP_CLIENT_ID and
# OPTIMIZELY_CMP_CLIENT_SECRET. testacc leaves nothing behind; testacc-residue also runs the content type and custom field
# tests, which leave a disabled/inactive object behind on every run because the API cannot delete them.
testacc:
	TF_ACC=1 TF_ACC_TERRAFORM_PATH=$$(which terraform) go test ./internal/provider -run TestAcc -v -count=1 -timeout 30m

testacc-residue:
	CMP_ACC_ALLOW_RESIDUE=1 $(MAKE) testacc

# Regenerates docs/ from the schema, examples/ and templates/ (needs terraform on PATH).
docs:
	go generate ./...

# Installs a local build into Terraform's implied local mirror for testing without the registry.
# After rebuilding, delete .terraform.lock.hcl and .terraform/ in the configuration and run `terraform init`.
install:
	mkdir -p $(MIRROR)
	go build -ldflags "-X main.version=$(VERSION)" -o $(MIRROR)/terraform-provider-$(PROVIDER)_v$(VERSION) .

# Validates the release configuration and builds every platform without signing or publishing.
check:
	go run $(GORELEASER) check

snapshot:
	go run $(GORELEASER) release --snapshot --clean --skip=sign,publish
