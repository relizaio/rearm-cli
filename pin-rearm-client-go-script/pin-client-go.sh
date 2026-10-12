#!/bin/sh
# Pins github.com/relizaio/rearm-client-go in the Go module of the current
# directory to one commit, so the pin is never edited by hand.
#
#   make pin-client-go REF=<commit or branch>   rewrite go.mod and go.sum
#   make pin-client-go-check                    fail if the committed pin is not
#                                               what the target writes
#
# REF is resolved on the client-go remote to a commit (a branch or tag name is
# looked up with git ls-remote; a hex string is taken as a commit), then
#   go get github.com/relizaio/rearm-client-go@<commit> && go mod tidy
# rewrites the requirement to that commit's pseudo-version. The same REF twice
# changes nothing. It prints the old and the new version.
#
# It refuses to run while go.mod or go.sum has unstaged changes or an
# unresolved conflict. To settle a conflict on the pin, take either side of
# both files (git checkout --theirs go.mod go.sum && git add go.mod go.sum, or
# git checkout origin/<base> -- go.mod go.sum) and run the target again.
#
# --check re-runs the target for the version go.mod already requires and fails
# if go.mod or go.sum would change: a pin edited by hand, or a go.sum out of
# step with it. It leaves both files as they were. It also runs outside a git
# work tree (a docker build context), fetching through the module proxy, which
# serves the same files for a pinned version.
#
# POSIX sh, so it runs in the golang:alpine build stage too.
set -eu

MODULE=github.com/relizaio/rearm-client-go
REMOTE=https://$MODULE

die() { echo "pin-client-go: $*" >&2; exit 1; }

check=0
ref=
case "${1:-}" in
	--check) check=1 ;;
	'' | -*) die "usage: $0 <commit or branch> | --check" ;;
	*) ref=$1 ;;
esac

gomod=$(go env GOMOD)
[ -n "$gomod" ] && [ "$gomod" != /dev/null ] || die "not inside a Go module"
cd "$(dirname "$gomod")"

export GOFLAGS="-mod=mod${GOFLAGS:+ $GOFLAGS}"

if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
	if [ -n "$(git ls-files -u -- go.mod go.sum)" ]; then
		die "go.mod or go.sum has an unresolved conflict: take either side of both files, git add them, and run it again"
	fi
	if ! git diff --quiet -- go.mod go.sum; then
		die "go.mod or go.sum has unstaged changes: commit, stage or discard them first; the pin is never edited by hand"
	fi
elif [ "$check" = 0 ]; then
	die "not inside a git work tree"
fi

pinned() {
	awk -v m="$MODULE" '$1 == m { print $2; exit } $1 == "require" && $2 == m { print $3; exit }' go.mod
}

old=$(pinned)

if [ "$check" = 1 ]; then
	[ -n "$old" ] || die "go.mod does not require $MODULE"
	saved=$(mktemp -d)
	trap 'cp "$saved/go.mod" go.mod; cp "$saved/go.sum" go.sum; rm -rf "$saved"' EXIT
	cp go.mod go.sum "$saved/"
	go get "$MODULE@$old"
	go mod tidy
	if cmp -s go.mod "$saved/go.mod" && cmp -s go.sum "$saved/go.sum"; then
		echo "rearm-client-go: $old is what make pin-client-go writes"
		exit 0
	fi
	diff -u "$saved/go.mod" go.mod >&2 || true
	diff -u "$saved/go.sum" go.sum >&2 || true
	die "the committed pin $old is not what make pin-client-go writes: re-run it with REF=<the commit you want> and commit go.mod and go.sum"
fi

# A branch head is not on the module proxy yet: fetch client-go straight from
# git, and take its checksum from the repository rather than the sum database.
export GOPRIVATE="$MODULE${GOPRIVATE:+,$GOPRIVATE}"

refs=$(git ls-remote "$REMOTE" "refs/heads/$ref" "refs/tags/$ref")
commit=$(printf '%s\n' "$refs" | awk -v r="refs/heads/$ref" '$2 == r { print $1 }')
if [ -z "$commit" ]; then
	if printf '%s\n' "$refs" | awk -v r="refs/tags/$ref" '$2 == r { f = 1 } END { exit !f }'; then
		commit=$ref # go resolves a tag to its commit
	elif printf '%s\n' "$ref" | grep -Eqx '[0-9a-fA-F]{7,40}'; then
		commit=$ref
	else
		die "$ref is neither a branch or tag on $REMOTE nor a commit"
	fi
fi

go get "$MODULE@$commit"
go mod tidy

new=$(pinned)
if [ "$old" = "$new" ]; then
	echo "rearm-client-go: $new (unchanged)"
else
	echo "rearm-client-go: ${old:-none} -> $new"
fi
