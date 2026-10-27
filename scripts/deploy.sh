#!/usr/bin/env bash
# Deploy the whole project to AWS: build, create the image repositories, push the images,
# create everything else with Terraform, and wait until the gateway answers.
#
# usage: scripts/deploy.sh --yes [options]
#   --yes              required: creates billable resources (two load balancers, Fargate
#                      tasks, ECR repositories; the S3 bucket already exists)
#   --skip-build       do not run scripts/build.sh first (the local images must already exist)
#   --keep-on-failure  when the deploy fails, leave what it created in place instead of running
#                      scripts/destroy.sh (a stack that already existed is never removed)
#   --tag TAG          image tag to deploy (default: the current date and time, so every
#                      deploy rolls the tasks)
#   --var NAME=VALUE   pass a Terraform variable, for example --var backend_send_loss=10
#                      (may be repeated; see terraform/variables.tf)
#   -h, --help         show this help
#
# Needs AWS credentials in the shell, the aws CLI, terraform and a running Docker; see
# SETUP.md, "Running the AWS end-to-end test", for where to get them. Takes about 15 minutes
# the first time.
# Exit status: 0 on success, 1 when a step fails, 2 on bad usage or a missing prerequisite.

set -u
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

YES=0
SKIP_BUILD=0
KEEP=0
TAG=$(date '+%Y%m%d%H%M%S')
TF_VARS=()
while [ $# -gt 0 ]; do
	case $1 in
	--yes) YES=1 ;;
	--skip-build) SKIP_BUILD=1 ;;
	--keep-on-failure) KEEP=1 ;;
	--tag)
		[ $# -ge 2 ] || { echo "--tag needs a value" >&2; exit 2; }
		TAG=$2
		shift
		;;
	--var)
		[ $# -ge 2 ] && [[ $2 == *=* ]] || { echo "--var needs NAME=VALUE" >&2; exit 2; }
		TF_VARS+=(-var "$2")
		shift
		;;
	-h | --help)
		usage "$0"
		exit 0
		;;
	*)
		echo "unknown option: $1" >&2
		usage "$0" >&2
		exit 2
		;;
	esac
	shift
done

if [ "$YES" != 1 ]; then
	echo "This creates billable AWS resources (two load balancers, Fargate tasks, ECR, S3)."
	echo "Run it again with --yes to go ahead (see --help)."
	exit 2
fi

need_tools aws terraform docker curl
need_docker
need_aws
LOG=$(mktemp "${TMPDIR:-/tmp}/deploy.XXXXXX")
ROLLBACK=0
cleanup() {
	status=$?
	rm -f "$LOG"
	if [ "$status" != 0 ] && [ "$ROLLBACK" = 1 ] && [ "$KEEP" != 1 ]; then
		step "Deploy failed: removing what was created (scripts/destroy.sh)"
		"$ROOT/scripts/destroy.sh" --yes || echo "cleanup failed; run scripts/destroy.sh --yes" >&2
	fi
}
trap cleanup EXIT
trap 'exit 130' INT TERM
cd "$ROOT" || exit 1

if [ "$SKIP_BUILD" = 1 ]; then
	step "Build skipped (--skip-build)"
	docker image inspect "$BACKEND_REPO:latest" "$GATEWAY_REPO:latest" >/dev/null 2>&1 ||
		die "the local images $BACKEND_REPO:latest and $GATEWAY_REPO:latest do not exist; run scripts/build.sh first"
else
	step "Building (scripts/build.sh --skip-tests)"
	scripts/build.sh --skip-tests || die "the build failed"
fi

step "Creating the image repositories"
tf init -input=false >"$LOG" 2>&1 || { tail -20 "$LOG"; die "terraform init failed"; }
# Only a stack this run creates is removed when the deploy fails; a working one is left alone.
[ -z "$(tf state list 2>/dev/null)" ] && ROLLBACK=1
tf apply -auto-approve -input=false -target=aws_ecr_repository.gateway -target=aws_ecr_repository.backend \
	"${TF_VARS[@]+"${TF_VARS[@]}"}" >"$LOG" 2>&1 || { tail -30 "$LOG"; die "creating the image repositories failed"; }

step "Pushing the images as $TAG and latest"
backend_uri=$(aws ecr describe-repositories --repository-names "$BACKEND_REPO" --query 'repositories[0].repositoryUri' --output text) ||
	die "could not find the $BACKEND_REPO repository"
gateway_uri=$(aws ecr describe-repositories --repository-names "$GATEWAY_REPO" --query 'repositories[0].repositoryUri' --output text) ||
	die "could not find the $GATEWAY_REPO repository"
aws ecr get-login-password | docker login --username AWS --password-stdin "${backend_uri%%/*}" >/dev/null ||
	die "logging in to ECR failed"
for pair in "$BACKEND_REPO:$backend_uri" "$GATEWAY_REPO:$gateway_uri"; do
	local_name=${pair%%:*}
	uri=${pair#*:}
	for tag in "$TAG" latest; do
		docker tag "$local_name:latest" "$uri:$tag" || die "tagging $local_name failed"
		docker push "$uri:$tag" >"$LOG" 2>&1 || { tail -20 "$LOG"; die "pushing $uri:$tag failed"; }
	done
	echo "pushed $uri:$TAG"
done

step "Creating the stack with Terraform (load balancers, services, scalers); this takes several minutes"
tf apply -auto-approve -input=false -var "image_tag=$TAG" "${TF_VARS[@]+"${TF_VARS[@]}"}" >"$LOG" 2>&1 ||
	{ tail -40 "$LOG"; die "terraform apply failed (full output above; fix the problem and run deploy.sh again)"; }
ALB=$(tf output -raw alb_dns_name) || die "could not read the gateway address"
BUCKET=$(tf output -raw files_bucket) || die "could not read the bucket name"
PREFIX=$(tf output -raw files_prefix) || die "could not read the bucket prefix"

step "Waiting for the services to settle"
aws ecs wait services-stable --cluster "$CLUSTER" --services gateway backend ||
	die "the ECS services did not become stable; check: aws ecs describe-services --cluster $CLUSTER --services gateway backend"

step "Waiting for the gateway to answer and every target to be healthy"
ok=0
for attempt in $(seq 1 60); do
	code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "http://$ALB/health" 2>/dev/null)
	gw=$(target_counts "$(target_group_arn file-transfer-gateway)")
	be=$(target_counts "$(target_group_arn file-transfer-backend)")
	if [ "$code" = 200 ] && [ "${gw% *}" = "${gw#* }" ] && [ "${gw% *}" -gt 0 ] && [ "${be% *}" = "${be#* }" ] && [ "${be% *}" -gt 0 ]; then
		ok=1
		break
	fi
	sleep 5
done
[ "$ok" = 1 ] || die "the gateway did not become healthy within 5 minutes (health $code, gateway targets healthy/total: ${gw% *}/${gw#* }, backend: ${be% *}/${be#* })"

echo
echo "Deployed."
echo "  gateway:  http://$ALB"
echo "  bucket:   s3://$BUCKET/$PREFIX"
echo "  cluster:  $CLUSTER (region $AWS_REGION)"
echo "  images:   tag $TAG"
echo
echo "Next steps:"
echo "  upload a file:   aws s3 cp ./my-file.txt s3://$BUCKET/${PREFIX}my-file.txt"
echo "  fetch it:        curl -o out.txt \"http://$ALB/files?name=my-file.txt\""
echo "  run the test:    scripts/e2e-aws-udp.sh --yes"
echo "  tear it down:    scripts/destroy.sh --yes"
