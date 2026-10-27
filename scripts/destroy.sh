#!/usr/bin/env bash
# Destroy everything deploy.sh created: delete the temporary files under the bucket prefix, run terraform destroy, and
# check that nothing is left.
#
# usage: scripts/destroy.sh --yes
#   --yes        required: deletes the temporary files under the bucket prefix and the whole stack
#   -h, --help   show this help
#
# Needs AWS credentials in the shell, the aws CLI and terraform; see SETUP.md, "Running the AWS
# end-to-end test", for where to get them. Local Docker images and bin/ are left alone.
# Exit status: 0 on success (or when there is nothing to destroy), 1 when a step fails, 2 on
# bad usage or a missing prerequisite.

set -u
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

YES=0
for arg in "$@"; do
	case $arg in
	--yes) YES=1 ;;
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

if [ "$YES" != 1 ]; then
	echo "This deletes the stack's temporary files (the bucket prefix only; the bucket stays) and destroys the whole stack."
	echo "Run it again with --yes to go ahead (see --help)."
	exit 2
fi

need_tools aws terraform
need_aws
LOG=$(mktemp "${TMPDIR:-/tmp}/destroy.XXXXXX")
trap 'rm -f "$LOG"' EXIT

step "Looking for the stack"
tf init -input=false >"$LOG" 2>&1 || { tail -20 "$LOG"; die "terraform init failed"; }
resources=$(tf state list 2>/dev/null)
if [ -z "$resources" ]; then
	echo "Terraform has no resources in $TF_DIR: nothing to destroy."
	exit 0
fi
echo "$(printf '%s\n' "$resources" | grep -c .) resources in the Terraform state"

step "Deleting the temporary files under the bucket prefix"
BUCKET=$(tf output -raw files_bucket 2>/dev/null)
PREFIX=$(tf output -raw files_prefix 2>/dev/null)
if [ -n "$BUCKET" ] && [ -n "$PREFIX" ]; then
	# The bucket is shared and not managed by Terraform: only ever remove keys under the prefix.
	aws s3 rm "s3://$BUCKET/$PREFIX" --recursive --only-show-errors || die "deleting s3://$BUCKET/$PREFIX failed"
	echo "deleted everything under s3://$BUCKET/$PREFIX"
else
	echo "no bucket and prefix recorded in the Terraform outputs; leaving the bucket alone"
fi

step "Destroying the stack with Terraform; this takes several minutes"
if ! tf destroy -auto-approve -input=false >"$LOG" 2>&1; then
	echo "terraform destroy failed; load balancer network interfaces can keep security groups busy for a few minutes."
	echo "Waiting 60 seconds and trying once more..."
	sleep 60
	tf destroy -auto-approve -input=false >"$LOG" 2>&1 || { tail -40 "$LOG"; die "terraform destroy failed (output above); fix the problem and run destroy.sh again"; }
fi

step "Checking that nothing is left"
left=0
remaining=$(tf state list 2>/dev/null)
if [ -n "$remaining" ]; then
	echo "still in the Terraform state:"
	printf '%s\n' "$remaining" | sed 's/^/  /'
	left=1
fi
for name in file-transfer-gateway file-transfer-backend; do
	if aws elbv2 describe-load-balancers --names "$name" >/dev/null 2>&1; then
		echo "load balancer still exists: $name"
		left=1
	fi
done
for repo in "$BACKEND_REPO" "$GATEWAY_REPO"; do
	if aws ecr describe-repositories --repository-names "$repo" >/dev/null 2>&1; then
		echo "ECR repository still exists: $repo"
		left=1
	fi
done
[ "$left" = 0 ] || die "some resources are left (listed above); run destroy.sh again or remove them in the AWS console"

echo
echo "Destroyed. Nothing is left in the stack."
