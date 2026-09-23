# Wankarr build shortcuts. `make` alone = vet + test + build.
BINARY := wankarr

.PHONY: all build test vet fmt doctor run clean nas

all: vet test build

build:
	go build -o $(BINARY) .

# Cross-compile for Synology DSM (Linux x86_64, no cgo): copy dist/ to the
# NAS alongside a NAS-specific .env, e.g. /volume2/docker/wankarr/.
nas:
	GOOS=linux GOARCH=amd64 go build -o dist/$(BINARY)-linux-amd64 .

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
