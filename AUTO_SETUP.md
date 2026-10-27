# Automatic AWS setup (build, deploy and destroy scripts)

This guide is for AWS Academy Learner Lab accounts. It sets up the whole service with three scripts in `scripts/` and tears it down again with one more:

| Script | What it does |
|---|---|
| `scripts/build.sh` | Checks and builds the Go programs and the two Docker images. No AWS needed. |
| `scripts/deploy.sh` | Builds, creates the image repositories, pushes the images, creates everything else with Terraform and waits until the service answers. |
| `scripts/destroy.sh` | Deletes the temporary files and the whole stack, then checks nothing is left. |

The first deploy takes about 15 minutes. The stack bills your lab budget while it runs (two load balancers and four Fargate tasks), so tear it down when you are done.

**One thing you must do by hand: create your own S3 bucket (step 1).** The scripts cannot do it, because the lab's `LabRole` cannot create buckets. The bucket name written in the Terraform files belongs to someone else and will not work for you, so you also tell Terraform your bucket's name (step 2).

> The scripts and commands below follow the repository's code, but this guide was not run against a live Learner Lab. Console screens change, so a button may be labelled a little differently.

Run every command from the repository root (the folder that contains `scripts/` and `terraform/`).

## 0. Before you start

1. **Tools on your computer:** Go, Docker (the Docker app must be running), the AWS CLI v2, Terraform and curl.
2. **Start the lab and get credentials.** Open your course, click *Start Lab*, wait for the green dot, then click *AWS* to open the console. Click *AWS Details*, then *AWS CLI: Show*, and put the three values (access key ID, secret access key and session token) in `~/.aws/credentials` under `[default]`. They expire when the lab session ends, so copy fresh ones each time you start the lab and before running a script. Check them with:
   ```bash
   aws sts get-caller-identity
   ```
3. **Pick a region.** Learner Labs only allow `us-east-1` (N. Virginia) and `us-west-2` (Oregon). The scripts and Terraform files use `us-west-2`, so if you stay there you change nothing. To use `us-east-1` instead:
   - Edit the four places that name the region (list them with `grep -rn us-west-2 terraform/`): the provider in `terraform/main.tf`, and `AWS_REGION` and the two `awslogs-region` lines in `terraform/ecs.tf`. Change `us-west-2` to `us-east-1` in each.
   - Run `export AWS_REGION=us-east-1` in every terminal before you run a script.
   - Create your bucket (step 1) in that region.

## 1. Create your S3 bucket (by hand)

The service serves files from an S3 bucket. Terraform only looks the bucket up; it does not create it.

1. In the console, open **S3 > Create bucket**. Check the region menu (top right) shows the region you picked in step 0.
2. Bucket type *General purpose*. **Bucket name:** 3 to 63 characters, lowercase letters, numbers and hyphens, unique across all of AWS, for example `file-transfer-<your account ID>-<your initials>`.
3. Leave *Block all public access* on (the service reads the files with its own role; nothing is public), leave versioning off and click *Create bucket*.
4. Open the bucket, click **Create folder**, name it `tmp/` and create it. This is the prefix the service looks under, and it must end with a slash.
5. Check that your credentials can see it (an empty bucket prints nothing; an error means a wrong name or expired credentials):
   ```bash
   aws s3 ls "s3://<your bucket name>"
   ```

If the console refuses to create a bucket, ask your instructor.

## 2. Tell Terraform about your bucket

Open `terraform/terraform.tfvars` and add these two lines (use your own bucket name):

```hcl
files_bucket = "<your bucket name>"
files_prefix = "tmp/"
```

Terraform reads this file on every run, so `deploy.sh`, `destroy.sh` and the end-to-end tests all pick it up. Leave the lines in place until `destroy.sh` has finished (step 8): destroying the stack looks the bucket up too, and fails if the name is wrong.

## 3. Build (optional)

```bash
scripts/build.sh
```

This runs the format check, `go vet`, builds the programs, runs the Go tests, checks the Terraform files (`terraform fmt -check` and `validate`) and builds the two Docker images for `linux/amd64`, which is what Fargate runs, even on an Apple silicon Mac. It stops at the first failure. Options:

| Option | Effect |
|---|---|
| `--skip-tests` | skip `go test -race ./...` (faster) |
| `--skip-docker` | skip the Docker images (no Docker needed) |
| `--verify` | also run `scripts/verify-servers.sh` at the end (about 3 minutes) |

`deploy.sh` runs `build.sh --skip-tests` itself, so this step is optional. It is a cheap way to find problems before anything is created on AWS.

## 4. Deploy

```bash
scripts/deploy.sh --yes
```

`--yes` is required because this creates billable resources. In order, the script:

1. builds the images (as in step 3, without the tests);
2. creates the two ECR image repositories;
3. pushes both images, tagged with the current date and time and with `latest`;
4. runs `terraform apply` to create the security groups, the two load balancers, the ECS cluster, services, log groups and the auto scalers;
5. waits until the services are stable, the gateway answers `/health` and every load balancer target is healthy.

At the end it prints the gateway address, your bucket and prefix, and the next steps. Options:

| Option | Effect |
|---|---|
| `--skip-build` | do not build first (the local images must already exist) |
| `--tag TAG` | image tag to deploy (default: the current date and time, so every deploy rolls the tasks) |
| `--var NAME=VALUE` | set a Terraform variable from `terraform/variables.tf`, for example `--var backend_send_loss=10`. May be repeated. |
| `--keep-on-failure` | if the deploy fails, leave what it created in place instead of removing it |

**If the deploy fails.** When the script is creating a new stack and a step fails (or you press Ctrl-C), it runs `scripts/destroy.sh --yes` itself, so the next run starts clean. A stack that already existed is never removed by a failed re-deploy. Use `--keep-on-failure` if you want to look at the half-built stack, and run `scripts/destroy.sh --yes` when you are done.

## 5. Try it

Use the gateway address the script printed:

```bash
curl -i "http://<gateway address>/health"        # expect HTTP 200

echo "hello from s3" > my-file.txt
aws s3 cp my-file.txt "s3://<your bucket name>/tmp/my-file.txt"
curl -o out.txt "http://<gateway address>/files?name=my-file.txt"
cat out.txt                                       # expect: hello from s3
```

The name after `tmp/` is what clients ask for. File names must be plain: no `/`, no spaces, and not starting with `.`.

## 6. Run the end-to-end tests (optional)

Both tests use the stack that is already deployed and write a Markdown report to `e2e-results/`.

```bash
scripts/e2e-aws-udp.sh --yes              # about 5 minutes
scripts/e2e-aws-udp-long.sh --yes         # about an hour
```

The long test changes the backend's simulated loss rates one after another, then puts them back and removes its own test files. Run `scripts/e2e-aws-udp-long.sh --help` for its options.

## 7. Change the code or the settings

After changing code, run the deploy again. Every run uses a new image tag, so ECS rolls the tasks to the new images:

```bash
scripts/deploy.sh --yes
```

Always use `deploy.sh` for changes. A plain `terraform apply` puts the image tag back to `latest` and the loss variables back to 0.

### Tuning auto scaling

Each service has a target tracking policy: ECS adds tasks while the metric is above the target value and removes them when it falls below it. The settings live in `terraform/autoscaling.tf` and `terraform/variables.tf`:

| | Gateway | Backend |
|---|---|---|
| Metric | `ALBRequestCountPerTarget` (requests per minute per task) | `ECSServiceAverageCPUUtilization` (percent) |
| Target value | `100` (`gateway_requests_per_target`) | `60` (`backend_cpu_target`) |
| Fewest / most tasks | `2` / `4` (`gateway_min`, `gateway_max`) | `2` / `4` (`backend_min`, `backend_max`) |
| Scale-out / scale-in cooldown | 60 s / 120 s (written in `autoscaling.tf`) | 60 s / 120 s (written in `autoscaling.tf`) |

**Change the target values or the number of tasks (no file editing).** Pass them as variables. `--skip-build` skips the image build and uses the images you already built in step 3:

```bash
scripts/deploy.sh --yes --skip-build --var backend_cpu_target=40 --var gateway_max=6
```

To make a value permanent, put it in `terraform/terraform.tfvars` instead, for example `backend_cpu_target = 40`. Keep the minimum at or below the maximum.

**Change the cooldowns.** In `terraform/autoscaling.tf`, change `scale_out_cooldown` and `scale_in_cooldown` (seconds) inside the `target_tracking_scaling_policy_configuration` block of each policy. A shorter scale-out cooldown reacts faster; a longer scale-in cooldown avoids removing tasks too early.

**Change the metric.** In `terraform/autoscaling.tf`, the metric of each policy is the `predefined_metric_specification` block. For example, to scale the backend on memory instead of CPU, change

```hcl
    predefined_metric_specification {
      predefined_metric_type = "ECSServiceAverageCPUUtilization"
    }
```

to

```hcl
    predefined_metric_specification {
      predefined_metric_type = "ECSServiceAverageMemoryUtilization"
    }
```

The target value (`backend_cpu_target`, 60 by default) is then a memory percentage. If you want a separate setting, add a `backend_memory_target` variable to `terraform/variables.tf`, copying the `backend_cpu_target` block, and use `var.backend_memory_target` as the policy's `target_value`.

ECS has only the two predefined metrics above (CPU and memory), plus `ALBRequestCountPerTarget` for services behind an ALB. The backend sits behind a network load balancer, which has no per-request metric, which is why it scales on CPU. To scale on any other CloudWatch metric, replace the `predefined_metric_specification` block with a `customized_metric_specification` block, for example:

```hcl
    customized_metric_specification {
      namespace   = "AWS/ECS"
      metric_name = "MemoryUtilization"
      statistic   = "Average"
      dimensions {
        name  = "ClusterName"
        value = aws_ecs_cluster.main.name
      }
      dimensions {
        name  = "ServiceName"
        value = aws_ecs_service.backend.name
      }
    }
```

Use exactly one of the two blocks in a policy. For the gateway, keep the `resource_label` line only while the metric is `ALBRequestCountPerTarget`, and remove it with any other metric. The metric you choose has to move up when the service is busy and down when it is idle, or the policy will not scale correctly.

**Apply the change.** After editing a `.tf` file, format it, then deploy:

```bash
terraform -chdir=terraform fmt
scripts/deploy.sh --yes --skip-build
```

The deploy first runs the Terraform format check and `validate` (inside `build.sh`), so a broken edit stops before anything on AWS is touched. A failed re-deploy of a running stack is not rolled back, so the old settings stay in place.

**Check it.** In the console, open **ECS > Clusters > file-transfer**, select the service and look at its *Service auto scaling* settings, or run:

```bash
aws application-autoscaling describe-scaling-policies --service-namespace ecs --resource-id service/file-transfer/backend
aws application-autoscaling describe-scaling-policies --service-namespace ecs --resource-id service/file-transfer/gateway
```

To watch it scale, put the service under load (for example `scripts/e2e-aws-udp.sh --yes 32`) and check the task counts:

```bash
aws ecs describe-services --cluster file-transfer --services gateway backend --query 'services[].[serviceName,desiredCount,runningCount]' --output table
```

## 8. Tear down

Do these two parts in this order.

1. **Destroy the stack:**
   ```bash
   scripts/destroy.sh --yes
   ```
   It deletes the files under your bucket prefix (`tmp/`) but never the bucket, runs `terraform destroy` (retrying once after a minute if the load balancers are still releasing their network interfaces), and then checks that the Terraform state is empty and the load balancers and image repositories are gone. If there is nothing to destroy it says so and exits 0. It needs the bucket name in `terraform/terraform.tfvars` and the bucket itself to still exist, which is why the bucket is deleted last.
2. **Delete your bucket** once `destroy.sh` has finished. In the console, open **S3 > Buckets**, select the bucket, click **Empty**, then **Delete**. Or:
   ```bash
   aws s3 rb "s3://<your bucket name>" --force
   ```
   Skip this if you want to keep the bucket and what is in it.

`destroy.sh` leaves your local Docker images and `bin/` alone. To remove the images:

```bash
docker rmi file-transfer-backend:latest file-transfer-gateway:latest
```

## Troubleshooting

All three scripts exit 0 on success, 1 when a step fails, and 2 when something they need is missing or the options are wrong.

| Problem | What to do |
|---|---|
| "AWS credentials are not set up or have expired" (exit 2) | Start the lab if it stopped, then copy fresh credentials from *AWS Details* (step 0). |
| Terraform fails looking up the bucket (`NotFound`, `Forbidden`) | Check `files_bucket` in `terraform/terraform.tfvars` is exactly your bucket's name, the bucket exists in the region you are using, and `aws s3 ls "s3://<your bucket name>"` works. |
| `terraform apply` fails on `aws_appautoscaling_target` | Your Learner Lab blocks the Application Auto Scaling service-linked role. The deploy removes what it created. Use [MANUAL_SETUP.md](MANUAL_SETUP.md), which lets you skip the auto scaling step. |
| "already exists" errors | Terraform's record of the stack (`terraform/terraform.tfstate`) is missing, for example after a fresh clone, so `destroy.sh` cannot see what is left. Delete the leftovers by hand: see "If a deploy fails" in [SETUP.md](SETUP.md) or step 14 of [MANUAL_SETUP.md](MANUAL_SETUP.md). |
| `destroy.sh` says resources are left | Run it again after a few minutes; load balancer network interfaces can keep security groups busy. |
| Requests fail with 502 or 504, or targets stay unhealthy | Open **CloudWatch > Log groups** and read `/ecs/file-transfer-backend` and `/ecs/file-transfer-gateway`. A backend `AccessDenied` from S3 means the task role cannot read your bucket or the bucket is in another region than the service. |

For more background, see [SETUP.md](SETUP.md). To build the same stack by hand in the console, see [MANUAL_SETUP.md](MANUAL_SETUP.md).
