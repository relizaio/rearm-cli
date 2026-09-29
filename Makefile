# Moves the rearm-client-go pin in go.mod and go.sum. Never edit that line by
# hand: a conflict on it is resolved by taking either side and re-running the
# target. See hack/pin-client-go.sh.

.PHONY: pin-client-go pin-client-go-check

pin-client-go:
	@test -n "$(REF)" || { echo "usage: make pin-client-go REF=<commit or branch>" >&2; exit 2; }
	sh hack/pin-client-go.sh "$(REF)"

pin-client-go-check:
	sh hack/pin-client-go.sh --check
