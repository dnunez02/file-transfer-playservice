# Manual AWS setup (no Terraform)

This guide is written for AWS Academy Learner Lab accounts. It builds by hand the same stack that `terraform/*.tf` creates, mostly in the AWS web console. Commands are only used for what the console cannot do: building and pushing the Docker images, and testing the result.

```
client --HTTP:80--> ALB "file-transfer-gateway" --> gateway tasks (ECS Fargate, port 8000)
                                                       --UDP:8080--> internal NLB "file-transfer-backend"
                                                                       --> backend tasks (ECS Fargate, UDP 8080, health TCP 8081)
                                                                              --> your S3 bucket (prefix tmp/)
```

> These steps follow the Terraform files but were not run against a live account. Console screens change over time, so a button or field may be labelled a little differently. The values to enter are the ones that matter.

Order matters: each step uses something created in an earlier one. Keep a scratch note of the values marked **Record**.

## 0. Before you start

1. **Pick a region** and use it for everything below. Learner Labs only allow `us-east-1` (N. Virginia) and `us-west-2` (Oregon). The Terraform stack uses `us-west-2`; use `us-east-1` instead if that is closer to you. In the console, choose it in the region menu at the top right, and check the menu still shows it at the start of every section. Resources in a different region will not see each other.
2. **Tools on your computer:** Docker, and the AWS CLI v2 (used only for pushing images and testing).
3. **AWS credentials.** Open your course, click *Start Lab*, wait for the green dot, then click *AWS* to open the console. Click *AWS Details*, then *AWS CLI: Show*, and put the three values (access key ID, secret access key and session token) in `~/.aws/credentials` under `[default]`. They expire when the lab session ends, so copy fresh ones each time you start the lab and before you run any `aws` or `docker login` command.
4. **The role.** The tasks run as the `LabRole` that every Learner Lab provides; you do not create any IAM roles. Open **IAM > Roles** and check `LabRole` exists. It already has the S3 read access the backend needs (`s3:GetObject` on your bucket).
5. **Create your own S3 bucket.** The backend serves files from a bucket under a prefix, and this guide uses a bucket you create yourself. Your lab console session creates it, not `LabRole`. If the console refuses to create a bucket, ask your instructor.
   1. Console: **S3 > Create bucket**. Check the region menu shows the region you picked in item 1.
   2. Bucket type *General purpose*. **Bucket name:** 3 to 63 characters, lowercase letters, numbers and hyphens, and unique across all of AWS, for example `file-transfer-<your account ID>-<your initials>`. (Do not use the name in `terraform/variables.tf`; that bucket belongs to someone else.)
   3. Leave *Block all public access* on (nothing is public: the backend reads the files with its task role), leave versioning off and the default encryption, and click *Create bucket*.
   4. Open the bucket and click **Create folder**. Name it `tmp/` and create it. This is the prefix the backend looks under, and it must end with a slash.
   5. **Record** the bucket name, the prefix `tmp/` and the bucket's region (shown in the bucket list). The backend reads the region from the `AWS_REGION` environment variable (set in step 9) and its S3 client cannot follow a redirect to another region, so that variable must be the bucket's region. Because you created the bucket in the region from item 1, that is the same region.
6. **Set shell variables** for the commands below:
   ```bash
   export AWS_REGION=us-west-2            # or us-east-1: the region you picked
   export ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
   export BUCKET=<the bucket name you created in item 5>
   export PREFIX=tmp/
   echo "$ACCOUNT_ID"
   aws s3 ls "s3://$BUCKET"               # prints nothing for an empty bucket; an error means a wrong name or expired credentials
   ```
   **Record** the account ID. You will need it in steps 3 and 9.

> **Learner Lab notes.** The lab has a spending budget. The two load balancers and the Fargate tasks are what cost money while the stack runs, so do the teardown in step 14 when you are done. The lab session also ends after a few hours: the credentials stop working then, so start the lab again and copy new credentials.

## 1. Find the default VPC

Everything runs in the account's default VPC.

1. Console: **VPC > Your VPCs**. Find the row where *Default VPC* is *Yes*.
2. **Record** its **VPC ID** (`vpc-...`) and its **IPv4 CIDR** (usually `172.31.0.0/16`).
3. Open **VPC > Subnets** and check the default VPC has a subnet in each availability zone. The steps below select all of them.

## 2. Image repositories (ECR)

1. Console: **Elastic Container Registry > Private registry > Repositories > Create repository**.
2. Visibility *Private*. Name `file-transfer-gateway`. Leave the other settings and click *Create*.
3. Repeat with the name `file-transfer-backend`.
4. For each repository, open it, then **Lifecycle Policy** (under *Private registry* in the left menu for some console versions) **> Create rule**:
   - Rule priority: `1`
   - Description: `Keep the last 5 images`
   - Image status: *Any*
   - Match criteria: *Image count more than* `5`
   - Rule action: *expire*

   Save it.

## 3. Build and push the images

Run these from the repository root (the folder containing `transfer-server/` and `transfer-frontend/`). The images must be `linux/amd64`, because the task definitions in step 9 use the X86_64 platform, even if you build on an Apple silicon Mac.

```bash
docker build --platform linux/amd64 -f transfer-server/Dockerfile  -t file-transfer-backend:latest .
docker build --platform linux/amd64 -f transfer-frontend/Dockerfile -t file-transfer-gateway:latest .
```

Log in to ECR and push both images. (In the console, opening a repository and clicking **View push commands** shows the same commands.)

```bash
aws ecr get-login-password --region "$AWS_REGION" |
  docker login --username AWS --password-stdin "$ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com"

for name in backend gateway; do
  docker tag "file-transfer-$name:latest" "$ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com/file-transfer-$name:latest"
  docker push "$ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com/file-transfer-$name:latest"
done
```

Check in the console that each repository now shows an image tagged `latest`.

## 4. Security groups

Console: **EC2 > Network & Security > Security Groups > Create security group**. For all three, choose the default VPC you recorded and leave **Outbound rules** at the default (all traffic to `0.0.0.0/0`). Create them in this order, because the second refers to the first.

| Name | Description | Inbound rules |
|---|---|---|
| `file-transfer-alb` | HTTP to the gateway load balancer | Type *HTTP* (TCP 80), source `0.0.0.0/0` |
| `file-transfer-gateway` | Gateway tasks | Type *Custom TCP*, port `8000`, source *Custom* then pick the `file-transfer-alb` group |
| `file-transfer-backend` | Backend tasks | Rule 1: *Custom UDP*, port `8080`, source = the VPC CIDR you recorded. Rule 2: *Custom TCP*, port `8081`, source = the VPC CIDR |

To limit who can reach the gateway, use your own address (for example `203.0.113.7/32`) instead of `0.0.0.0/0` on the first rule.

Why the backend group allows the whole VPC: only the gateway tasks and the NLB, which are inside the VPC, ever talk to the backend, and the NLB health checks come from its private addresses.

## 5. Backend load balancer (internal NLB, UDP)

An ALB cannot balance UDP, so the backend sits behind a network load balancer. Create the target group first, then the load balancer.

### 5a. Target group

Console: **EC2 > Load Balancing > Target Groups > Create target group**.

1. Target type: *IP addresses*.
2. Name: `file-transfer-backend`. Protocol: *UDP*. Port: `8080`. VPC: the default VPC.
3. **Health checks** (UDP groups cannot check UDP, so they check the server's TCP health port):
   - Health check protocol: *HTTP*
   - Health check path: `/health`
   - Expand *Advanced health check settings*: Port *Override* `8081`, Healthy threshold `2`, Unhealthy threshold `2`, Interval `10` seconds.
4. Click *Next*. On the *Register targets* page register nothing (ECS adds the tasks) and click *Create target group*.
5. Open the new group, **Attributes > Edit**:
   - Deregistration delay: `60` seconds
   - Preserve client IP addresses: *On*

   Save.

### 5b. Load balancer

Console: **EC2 > Load Balancing > Load Balancers > Create load balancer > Network Load Balancer**.

1. Name `file-transfer-backend`. Scheme: *Internal*. IP address type: *IPv4*.
2. Network mapping: the default VPC, and tick every availability zone and its subnet.
3. Do not attach a security group to the NLB.
4. Listener: Protocol *UDP*, port `8080`, forward to the `file-transfer-backend` target group.
5. Create it. Then open it, **Attributes > Edit**, and turn **Cross-zone load balancing** *On*. Save.
6. **Record** its **DNS name** (something like `file-transfer-backend-abc123.elb.us-west-2.amazonaws.com`) as `NLB_DNS`.

## 6. Gateway load balancer (public ALB, HTTP)

Same pattern: target group first.

### 6a. Target group

Console: **Target Groups > Create target group**.

1. Target type: *IP addresses*.
2. Name: `file-transfer-gateway`. Protocol *HTTP*, port `8000`, VPC the default VPC. Protocol version *HTTP1*.
3. Health checks: protocol *HTTP*, path `/health`. Under *Advanced*: Success codes `200`, Healthy threshold `2`, Unhealthy threshold `2`, Interval `10` seconds (leave the port as *Traffic port*).
4. Register no targets and create the group.
5. **Attributes > Edit**: Deregistration delay `60` seconds. Save.

### 6b. Load balancer

Console: **Load Balancers > Create load balancer > Application Load Balancer**.

1. Name `file-transfer-gateway`. Scheme: *Internet-facing*. IP address type: *IPv4*.
2. Network mapping: the default VPC, every availability zone and subnet.
3. Security groups: remove `default` and choose `file-transfer-alb`.
4. Listener: *HTTP*, port `80`, forward to the `file-transfer-gateway` target group. (HTTPS would need a domain and a certificate, so this guide uses plain HTTP.)
5. Create it. **Record** its **DNS name** as `ALB_DNS`.

## 7. Log groups

Console: **CloudWatch > Logs > Log management > Create log group**, twice:

- `/ecs/file-transfer-gateway`
- `/ecs/file-transfer-backend`

Set retention to *1 week* on each.

## 8. ECS cluster

Console: **Elastic Container Service > Clusters > Create cluster**. Name `file-transfer`. Infrastructure: *AWS Fargate (serverless)* only. Create it.

## 9. Task definitions

A task definition tells ECS which image to run and with what settings. Create two, one per service. Console: **ECS > Task definitions > Create new task definition** (the form, not *Create new task definition with JSON*). The steps are the same for both; only the values in the tables differ. Console labels can differ slightly from what is written here; the values are what matter.

Values from earlier steps that you need: your account ID and region (step 0), the name of the S3 bucket you created in step 0 and the region it is in, and the NLB DNS name (step 5b).

### Steps for each task definition

1. **Task definition configuration:** enter the *Task definition family* from the table.
2. **Infrastructure requirements:**
   - Launch type: *AWS Fargate*
   - Operating system/Architecture: *Linux/X86_64*
   - Task size: CPU *.25 vCPU*, Memory *.5 GB*
   - Task role: *LabRole* (choose the existing role; do not create one)
   - Task execution role: *LabRole*
3. **Container - 1:**
   - Name: from the table.
   - Image URI: `<ACCOUNT_ID>.dkr.ecr.<REGION>.amazonaws.com/file-transfer-<gateway or backend>:latest`. Open the repository in ECR and use *Copy URI* on the `latest` image to get it right.
   - Essential container: *Yes*
   - Port mappings: the rows from the table (*Add port mapping* for each extra one). Leave the port name and app protocol empty.
   - **Environment variables:** click *Add environment variable* once per row in the table, with *Type* *Value*.
   - **Timeouts > Stop timeout:** `60` seconds. This gives a task time to finish running transfers after it is told to stop.
   - **Logging:** keep *Use log collection* on, choose *awslogs*, and set `awslogs-group`, `awslogs-region` (your region) and `awslogs-stream-prefix` from the table. If the form offers *Auto-configure CloudWatch Logs* or `awslogs-create-group`, turn it off, because the log groups already exist from step 7.
4. Click *Create*. Check the task definition appears as revision `1`.

### Backend

| Setting | Value |
|---|---|
| Family | `file-transfer-backend` |
| Container name | `backend` |
| Port mappings | `8080` / UDP, and `8081` / TCP |
| `awslogs-group` | `/ecs/file-transfer-backend` |
| `awslogs-stream-prefix` | `backend` |

| Environment variable | Value |
|---|---|
| `BUCKET` | the name of the bucket you created in step 0 |
| `PREFIX` | `tmp/` (the folder you created in the bucket; ends with a slash) |
| `PORT` | `8080` |
| `HEALTH_PORT` | `8081` |
| `WORKERS` | `8` |
| `SEND_LOSS` | `0` |
| `RECV_LOSS` | `0` |
| `AWS_REGION` | the region your bucket is in (the region you picked in step 0) |

### Gateway

| Setting | Value |
|---|---|
| Family | `file-transfer-gateway` |
| Container name | `gateway` |
| Port mappings | `8000` / TCP |
| `awslogs-group` | `/ecs/file-transfer-gateway` |
| `awslogs-stream-prefix` | `gateway` |

| Environment variable | Value |
|---|---|
| `LISTEN` | `:8000` |
| `TRANSPORT` | `udp` |
| `UPSTREAM` | the NLB DNS name from step 5b, followed by `:8080` (no `http://`) |
| `MAX_INFLIGHT` | `4` |

What the settings mean: `WORKERS` is how many requests one backend task handles at once, `MAX_INFLIGHT` how many transfers one gateway task runs at once, and `SEND_LOSS` / `RECV_LOSS` are percentages of datagrams the backend deliberately drops to simulate a lossy network (keep them `0` for normal use). The `PREFIX` value must match the folder you created in your bucket and end with a slash.

## 10. ECS services

Create the backend service first, then the gateway. Console: **ECS > Clusters > file-transfer > Services > Create**. In both:

- **Compute configuration:** *Launch type*, *FARGATE*, platform version *LATEST*.
- **Deployment configuration:** Application type *Service*. Family: the task definition from step 9, revision *LATEST*. Service name as below. Desired tasks `2`. Under *Deployment options*, set *Min running tasks* to `100` percent and *Max running tasks* to `200` percent.
- **Networking:** the default VPC, all subnets, *Security group*: *Use an existing security group*, remove `default` and choose the group named below. **Public IP**: *Turned on* (the tasks need it to pull images from ECR without a NAT gateway).
- **Load balancing:** type as below, *Use an existing load balancer*, choose the container, the existing listener and the existing target group. Set **Health check grace period** to `30` seconds.
- Leave Service Connect, Service discovery and Volumes off. (Auto scaling is added in step 11.)

| | Backend | Gateway |
|---|---|---|
| Service name | `backend` | `gateway` |
| Task definition family | `file-transfer-backend` | `file-transfer-gateway` |
| Security group | `file-transfer-backend` | `file-transfer-gateway` |
| Load balancer | `file-transfer-backend` (the NLB) | `file-transfer-gateway` (the ALB) |
| Container to balance | `backend 8080:8080` | `gateway 8000:8000` |
| Listener | `UDP:8080` | `HTTP:80` |
| Target group | `file-transfer-backend` | `file-transfer-gateway` |

Wait for each service to show 2 running tasks (a few minutes).

## 11. Auto scaling

For each service: open it, **Update service**, expand **Service auto scaling**, and choose *Use Service Auto Scaling*:

| | Gateway | Backend |
|---|---|---|
| Minimum tasks | `2` | `2` |
| Maximum tasks | `4` | `4` |
| Policy type | *Target tracking* | *Target tracking* |
| Policy name | `gateway-requests-per-target` | `backend-cpu` |
| ECS service metric | *ALBRequestCountPerTarget* (the gateway target group) | *ECSServiceAverageCPUUtilization* |
| Target value | `100` (requests per minute per task) | `60` (percent CPU) |
| Scale-out cooldown | `60` seconds | `60` seconds |
| Scale-in cooldown | `120` seconds | `120` seconds |

The backend scales on CPU because a network load balancer has no per-target request metric.

If saving fails with an error about a service-linked role, your Learner Lab does not allow Application Auto Scaling. Skip this step: the stack works with a fixed 2 tasks per service.

## 12. Check that it works

In the console:

- **ECS > Clusters > file-transfer > Services**: both services show 2/2 tasks running.
- **EC2 > Target Groups**: open `file-transfer-gateway` and `file-transfer-backend`, tab *Targets*: every target is *healthy* (allow a minute or two after the tasks start).

From your terminal (use the ALB DNS name you recorded):

```bash
export ALB_DNS="<your ALB DNS name>"
curl -i "http://$ALB_DNS/health"          # expect HTTP 200

echo "hello from s3" > my-file.txt
aws s3 cp my-file.txt "s3://$BUCKET/${PREFIX}my-file.txt"
curl -o out.txt "http://$ALB_DNS/files?name=my-file.txt"
cat out.txt                                # expect: hello from s3
```

If a target stays unhealthy or `/health` does not answer: look at **CloudWatch > Log groups > `/ecs/file-transfer-backend`** (or `-gateway`), and check the security group rules in step 4 and that the task definition values in step 9 have no leftover `<...>`.

## 13. Updating the images

1. Build and push the new images as in step 3, with a new tag instead of `latest`, for example `v2` (swap `:latest` for `:v2` in the `docker tag` and `docker push` lines).
2. **ECS > Task definitions**, choose the family, **Create new revision**, change the image tag in the container, create it.
3. **ECS > Clusters > file-transfer > Services**, select the service, **Update service**, choose the new revision, and update. ECS starts new tasks, waits for them to be healthy, then stops the old ones.

(If you keep pushing to `latest`, you can instead tick *Force new deployment* on the service.)

## 14. Tearing it all down

Delete in this order, so nothing is still in use when you delete it. Everything here bills while it exists, mainly the two load balancers and the Fargate tasks.

1. **ECS services.** For each of `backend` and `gateway`: open it, **Delete service**, tick *Force delete* (this scales it to zero and removes its auto scaling settings). Wait until both are gone.
2. **ECS cluster** `file-transfer`: **Delete cluster**.
3. **Load balancers:** `file-transfer-gateway` and `file-transfer-backend`: select each, *Actions > Delete load balancer*. Wait 3 to 5 minutes. Their network interfaces keep the security groups and target groups busy until they are released.
4. **Target groups:** delete `file-transfer-gateway` and `file-transfer-backend`.
5. **Security groups:** delete `file-transfer-gateway` and `file-transfer-backend` first, then `file-transfer-alb` (the gateway group refers to it).
6. **Log groups:** delete `/ecs/file-transfer-gateway` and `/ecs/file-transfer-backend`.
7. **Task definitions:** for each family, select all revisions and *Actions > Deregister* (then *Delete* if you want them gone completely).
8. **ECR:** delete both repositories (the images go with them).
9. **S3:** you created the bucket for this stack, so empty it and delete it. Console: **S3 > Buckets**, select your bucket, **Empty**, then **Delete**. Or with commands:
   ```bash
   aws s3 rb "s3://$BUCKET" --force
   ```
   If you put other data in the bucket and want to keep it, delete only the test files instead: `aws s3 rm "s3://$BUCKET/$PREFIX" --recursive`.

## Reference: every resource and its Terraform file

| Resource | Name | Where in Terraform |
|---|---|---|
| ECR repositories | `file-transfer-gateway`, `file-transfer-backend` (keep last 5) | `ecr.tf` |
| Security groups | `file-transfer-alb`, `file-transfer-gateway`, `file-transfer-backend` | `network.tf` |
| Internal NLB + UDP 8080 listener + target group | `file-transfer-backend` | `nlb.tf` |
| Public ALB + HTTP 80 listener + target group | `file-transfer-gateway` | `alb.tf` |
| Log groups (7 days) | `/ecs/file-transfer-gateway`, `/ecs/file-transfer-backend` | `ecs.tf` |
| ECS cluster | `file-transfer` | `ecs.tf` |
| Task definitions (Fargate, 256 CPU / 512 MB) | `file-transfer-backend`, `file-transfer-gateway` | `ecs.tf` |
| ECS services (2 tasks each) | `backend`, `gateway` | `ecs.tf` |
| Auto scaling (2 to 4 tasks) | `gateway-requests-per-target`, `backend-cpu` | `autoscaling.tf` |
| Already existing, not created | default VPC and subnets, `LabRole` | `network.tf`, `ecs.tf` |
| S3 bucket (you create it in step 0, with a `tmp/` folder) | a name of your choice | not created by Terraform: `main.tf` only looks up an existing bucket |

The Terraform files default to a different bucket name (`files_bucket` in `variables.tf`); this guide does not use it.

One detail to be aware of: a comment in `terraform/network.tf` says the backend target group turns client IP preservation off, but `terraform/nlb.tf` sets `preserve_client_ip = "true"`. This guide follows `nlb.tf` (step 5a). The backend security group rule (the whole VPC CIDR) works either way.
