# Shared helpers for build.sh, deploy.sh and destroy.sh. Source this file; do not run it.

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
TF_DIR=$ROOT/terraform
export AWS_REGION=${AWS_REGION:-us-west-2}
export AWS_DEFAULT_REGION=$AWS_REGION

# These names are fixed in the Terraform files.
CLUSTER=file-transfer
BACKEND_REPO=file-transfer-backend
GATEWAY_REPO=file-transfer-gateway

STEP=0

# step message: a progress line
step() {
	STEP=$((STEP + 1))
	echo
	echo "==> [$STEP] $*"
}

# die message: print to stderr and exit 1
die() {
	echo "error: $*" >&2
	exit 1
}

# usage file: print the comment block at the top of a script (from its second line to the first blank line)
usage() {
	sed -n '2,/^$/p' "$1" | sed 's/^# \{0,1\}//' | sed '$d'
}

# need_tools tool...: exit 2 when a tool is not installed
need_tools() {
	local tool
	for tool in "$@"; do
		command -v "$tool" >/dev/null 2>&1 || {
			echo "Missing tool: $tool (see SETUP.md, \"Running the AWS end-to-end test\", for where to get it)" >&2
			exit 2
		}
	done
}

# need_docker: exit 2 when the Docker daemon is not running
need_docker() {
	docker info >/dev/null 2>&1 || {
		echo "The Docker daemon is not running. Start Docker Desktop and try again." >&2
		exit 2
	}
}

# need_aws: exit 2 when there are no valid AWS credentials
need_aws() {
	aws sts get-caller-identity >/dev/null 2>&1 || {
		echo "AWS credentials are not set up or have expired (see SETUP.md, \"Running the AWS end-to-end test\")." >&2
		exit 2
	}
}

# tf args...: run terraform in the terraform folder
tf() { terraform -chdir="$TF_DIR" "$@"; }

target_group_arn() { # name
	aws elbv2 describe-target-groups --names "$1" --query 'TargetGroups[0].TargetGroupArn' --output text 2>/dev/null
}

# target_counts arn -> "healthy total"
target_counts() {
	local states total healthy
	states=$(aws elbv2 describe-target-health --target-group-arn "$1" \
		--query 'TargetHealthDescriptions[].TargetHealth.State' --output text 2>/dev/null | tr '\t' '\n')
	total=$(printf '%s\n' "$states" | grep -c .)
	healthy=$(printf '%s\n' "$states" | grep -c '^healthy$')
	echo "$healthy $total"
}
