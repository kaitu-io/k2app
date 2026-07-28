# The fork lives under .fork/ and is not committed; patches/ is. Run `make fork`
# after a fresh clone, or any time the patch changes.
#
# -checklinkname=0: quic-go's own hkdf_test.go reaches into crypto/tls internals
# with //go:linkname. Naming the TLS engine as an interface (the seam patch) stops
# the linker from keeping one of those symbols alive, so the check trips while
# building the TEST binary. Production builds are unaffected -- `make build` runs
# without the flag on purpose, so a real breakage would still show up.
GOTEST := go test -ldflags=-checklinkname=0 -count=1

.PHONY: all fork test test-fork build vet patch clean

all: test

fork:
	@./scripts/fork-quic-go.sh

test: | .fork/quic-go
	$(GOTEST) ./...

# Regression check on the patch itself: quic-go's own suite must still pass.
test-fork: | .fork/quic-go
	cd .fork/quic-go && $(GOTEST) ./...

build: | .fork/quic-go
	go build ./...

vet: | .fork/quic-go
	go vet ./...

# Regenerate the patch from the working fork. scripts/fork-quic-go.sh keeps a
# pristine copy next to it for exactly this.
patch: | .fork/quic-go.orig
	cd .fork && diff -ruN quic-go.orig quic-go > ../patches/quic-go-utls-seam.patch || true
	@echo "wrote patches/quic-go-utls-seam.patch"

.fork/quic-go .fork/quic-go.orig:
	@./scripts/fork-quic-go.sh

clean:
	rm -rf .fork
