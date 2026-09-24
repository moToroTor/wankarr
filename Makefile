# Wankarr build shortcuts. `make` alone = vet + test + build.
BINARY := wankarr

.PHONY: all build test vet fmt doctor run clean nas

all: vet test build

build:
	go build -o $(BINARY) .

# Cross-compile for Synology DSM (Linux x86_64, static): copy dist/ to
# the NAS alongside a NAS-specific .env, e.g. /volume2/docker/wankarr/.
# CGO_ENABLED=0 is required: DSM 7's glibc (2.26 on this NAS line) is far
# older than the CI builders', and a cgo binary dies at startup with
# "GLIBC_2.3x not found".
nas:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/$(BINARY)-linux-amd64 .

# Assemble a DSM 7 .spk for one arch (needs a matching linux binary first):
#   make spk ARCH=avoton VERSION=1.0.0
spk:
	go run ./tools/mkspk --binary dist/$(BINARY)-linux-amd64 \
		--arch $(ARCH) --version $(VERSION) --rev 1 --out dist

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l .

doctor: build
	./$(BINARY) doctor

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY)
