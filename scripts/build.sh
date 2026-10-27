#!/usr/bin/env bash
# Build and check everything that does not need AWS: the Go programs, their tests, the
# Terraform files and the two Docker images.
#
# usage: scripts/build.sh [options]
#   --skip-tests   do not run `go test -race ./...`
#   --skip-docker  do not build the Docker images (no Docker needed)
#   --verify       also run scripts/verify-servers.sh at the end (about 3 minutes)
#   -h, --help     show this help
#
# Produces: bin/transfer-server, bin/transfer-client, bin/transfer-frontend, and the local
# Docker images file-transfer-backend:latest and file-transfer-gateway:latest (built for
# linux/amd64, which is what Fargate runs, even on an Apple silicon Mac).
# Exit status: 0 on success, 1 when a step fails, 2 when a needed tool is missing.

set -u
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

SKIP_TESTS=0
SKIP_DOCKER=0
VERIFY=0
for arg in "$@"; do
	case $arg in
	--skip-tests) SKIP_TESTS=1 ;;
	--skip-docker) SKIP_DOCKER=1 ;;
	--verify) VERIFY=1 ;;
	-h | --help)
		usage "$0"
		exit 0
		;;
	*)
		echo "unknown option: $arg" >&2
		usage "$0" >&2
		exit 2
		;;
	esac
done

need_tools go
[ "$SKIP_DOCKER" = 1 ] || { need_tools docker; need_docker; }
cd "$ROOT" || exit 1

step "Checking Go formatting and vet"
unformatted=$(gofmt -l .)
[ -z "$unformatted" ] || die "these files are not gofmt-formatted (run gofmt -w): $unformatted"
go vet ./... || die "go vet failed"

step "Building the Go programs into bin/"
mkdir -p bin
for program in transfer-server transfer-client transfer-frontend; do
	go build -o "bin/$program" "./$program" || die "building $program failed"
	echo "bin/$program"
done

if [ "$SKIP_TESTS" = 1 ]; then
	step "Go tests skipped (--skip-tests)"
else
	step "Running the Go tests (with the race detector)"
	go test -race -count=1 ./... 2>&1 | grep -v "no test files"
	[ "${PIPESTATUS[0]}" = 0 ] || die "go tests failed"
fi

step "Checking the Terraform files"
if command -v terraform >/dev/null 2>&1; then
	tf fmt -check -recursive || die "terraform fmt found unformatted files (run: terraform -chdir=terraform fmt -recursive)"
	tf init -backend=false -input=false >/dev/null || die "terraform init failed"
	tf validate || die "terraform validate failed"
else
	echo "terraform is not installed; skipping (see SETUP.md for where to get it)"
fi

if [ "$SKIP_DOCKER" = 1 ]; then
	step "Docker images skipped (--skip-docker)"
else
	step "Building the Docker images (linux/amd64)"
	docker build --platform linux/amd64 -f transfer-server/Dockerfile -t "$BACKEND_REPO:latest" . || die "building the backend image failed"
	docker build --platform linux/amd64 -f transfer-frontend/Dockerfile -t "$GATEWAY_REPO:latest" . || die "building the gateway image failed"
fi

if [ "$VERIFY" = 1 ]; then
	step "Running scripts/verify-servers.sh"
	scripts/verify-servers.sh || die "verify-servers.sh reported failures"
fi

echo
echo "Build finished."
echo "  programs: bin/transfer-server bin/transfer-client bin/transfer-frontend"
[ "$SKIP_DOCKER" = 1 ] || echo "  images:   $BACKEND_REPO:latest $GATEWAY_REPO:latest"
