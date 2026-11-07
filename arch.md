# RateLimitX — Distributed Rate Limiter

RateLimitX is a Go HTTP service that demonstrates five rate-limiting
algorithms using Redis as shared state. It includes HTTP middleware, Docker
Compose configuration, Kubernetes manifests, Prometheus metrics, a Grafana
dashboard, and k6 load-test scenarios.

## Features

- Fixed Window, Sliding Window Log, Sliding Window Counter, Token Bucket,
  and Leaky Bucket implementations.
- Redis-backed state shared by multiple application instances.
- Redis Lua scripts for atomic state transitions.
- Policy selection based on the `X-API-Key` header, with a default policy.
- `429 Too Many Requests` responses and rate-limit headers.
- Prometheus metrics at `/metrics`.
- Docker Compose for a local multi-instance deployment.
- Kubernetes manifests for the application, Redis, Prometheus, and Grafana.
- GitHub Actions CI, image publishing, and EKS deployment workflow.
- k6 basic, comparison, stress, spike, and soak test scripts.

## Architecture

```text
                         +----------------------+
Client ---------------->| NGINX (Docker Compose)|----+
                         +----------------------+    |
                                                     v
                                        +------------------------+
                                        | Go HTTP instances      |
                                        | middleware + policies  |
                                        +------------+-----------+
                                                     |
                                                     v
                                        +------------------------+
                                        | Redis shared state     |
                                        +------------------------+

Go /metrics ---------> Prometheus ---------> Grafana
```

In Kubernetes, an NGINX Ingress routes application and Grafana hostnames to
their Services. The manifests expect an Ingress Controller to be installed in
the cluster.

### Request flow

1. The HTTP middleware reads `X-API-Key` and selects that key's policy, or the
   default policy when the key is unknown.
2. The API key is hashed to form the rate-limit identity. When no API key is
   sent, the remote IP address is used.
3. The selected limiter evaluates the request against shared Redis state.
4. An allowed request reaches the API handler. A rejected request receives
   HTTP 429; a limiter/Redis error receives HTTP 503.

## Algorithms

| Algorithm | State / behavior | Benefits | Trade-off |
|---|---|---|---|
| Fixed Window | Counts requests in a fixed time interval. | Simple and inexpensive. | Requests can bunch at adjacent window boundaries. |
| Sliding Window Log | Records request timestamps and removes those outside the active window. | Accurate rolling-window limit. | Stores per-request entries. |
| Sliding Window Counter | Weights the previous window's count and combines it with the current count. | Lower memory use than a timestamp log with smoother boundaries. | Approximate rather than exact. |
| Token Bucket | Refills tokens over time; each request consumes one token. | Allows controlled bursts while enforcing an average rate. | Requires keeping token and refill state. |
| Leaky Bucket | Schedules requests at a steady interval and limits the pending queue. | Smooths traffic and bounds queued work. | Does not provide the same burst behavior as Token Bucket. |

### Leaky Bucket implementation

The Redis-backed implementation uses a Sorted Set for pending scheduled
requests and a sequence key to make members unique. Its Lua script:

1. Reads Redis server time, so instances share one clock.
2. Removes entries whose scheduled time has passed.
3. Checks the pending count against the bucket capacity.
4. Schedules an accepted request after the previous pending request.
5. Increments the sequence key and applies TTLs to both keys.

The script schedules work; it does not itself execute application requests.
Redis runs the script atomically with respect to other commands. Atomic
execution prevents other clients from interleaving commands, but Lua scripts
do not provide automatic rollback if a script fails after making a write.

## Redis state and TTL

Redis provides shared state across application replicas; per-process counters
would allow a caller to exceed its effective limit by reaching different
instances. The storage implementations use algorithm-appropriate Redis
structures and embedded Lua scripts where atomic multi-command operations are
needed.

TTL (time to live) is the time remaining before a Redis key expires. Key TTL
and Redis persistence are separate concerns: TTL cleans up temporary limiter
state, while RDB/AOF and persistent storage determine whether Redis data can
be recovered after a restart. The Kubernetes Redis manifest currently runs a
single Redis Pod and does not configure a PersistentVolumeClaim; do not assume
rate-limit state survives Pod replacement.

Useful Redis commands when diagnosing state:

```text
TTL <key>
PTTL <key>
EXISTS <key>
GET <key>
ZCARD <key>
ZRANGE <key> 0 -1 WITHSCORES
```

For Sorted Sets, key-level TTL does not replace removing individual expired
members by score.

## Run locally

### Prerequisites

- Go version from [`go.mod`](./go.mod).
- Docker with the Compose plugin.
- Optional: k6 for load testing.

Start the full local stack:

```sh
docker compose up
```

The Compose file runs Redis, two Go server containers, NGINX, Prometheus, and
Grafana using the configured container image. Open:

| Service | Local URL |
|---|---|
| Application through NGINX | <http://localhost:8080/> |
| Server 1 directly | <http://localhost:8081/> |
| Server 2 directly | <http://localhost:8082/> |
| Prometheus | <http://localhost:9090/> |
| Grafana | <http://localhost:3000/> |

The HTTP server defaults to port `8080` and Redis defaults to
`localhost:6379`. The Compose file sets `PORT`, `REDIS_ADDR`, and
`INSTANCE_ID` for its application containers.

The API is mounted at `/api/` and expects a configured API key for a
non-default policy. For example, replace `<API_KEY>` with a key configured by
the application:

```sh
curl -i -H "X-API-Key: <API_KEY>" http://localhost:8080/api/demo
```

An allowed API request reaches the demo handler and identifies the serving
instance. When a request is rejected, the service returns HTTP 429 with
`Retry-After`, `X-RateLimit-Limit`, and `X-RateLimit-Remaining` headers. A
limiter error returns HTTP 503.

Stop the stack with:

```sh
docker compose down
```

### Run the Go service directly

Start Redis separately, then run:

```sh
go run ./cmd/server
```

Environment variables:

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP listen port. |
| `REDIS_ADDR` | `localhost:6379` | Redis host and port. |
| `INSTANCE_ID` | empty | Identifier returned by the demo API handler. |

The service checks Redis connectivity during startup.

## HTTP endpoints

| Path | Purpose |
|---|---|
| `/` and `/index.html` | Serves the embedded project dashboard. |
| `/api/` | Demo API protected by the rate-limit middleware. |
| `/health` | Application health response. |
| `/health/prometheus` | Checks whether the Prometheus service at `prometheus:9090` is healthy. |
| `/metrics` | Prometheus metrics endpoint. |

The middleware exports `X-RateLimit-Limit` and
`X-RateLimit-Remaining`; it adds `X-RateLimit-Reset` when the selected
limiter returns a reset time. Rejected requests also include `Retry-After`.

## Monitoring

Prometheus scrapes the Go application's `/metrics` endpoint. The application
registers these metrics:

| Metric | Type | Labels |
|---|---|---|
| `ratelimitx_requests_total` | Counter | `status`, `algorithm`, `tier` |
| `ratelimitx_request_duration_seconds` | Histogram | `algorithm` |
| `ratelimitx_rejected_requests_total` | Counter | `algorithm`, `tier` |

Grafana is provisioned with the project's dashboard and Prometheus data
source. Review the Grafana authentication and anonymous-access settings
before exposing it outside a trusted local or development environment.

## Load testing

The scripts under [`loadtest/`](./loadtest/) cover basic, algorithm
comparison, stress, spike, and soak traffic patterns. The basic k6 script
uses `BASE_URL` and `API_KEY` environment variables and treats HTTP 429 as an
expected rate-limit response.

Example:

```sh
k6 run loadtest/basic_test.js
```

Use the base URL and API key appropriate for the environment. A 429 is an
expected limiter decision, not necessarily an application error; investigate
unexpected statuses and 5xx responses separately.

## Tests and development

```sh
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

The race detector checks concurrent access to Go memory. It does not test
Redis atomicity; Redis Lua scripts address interleaving of Redis operations.
On Windows, `go test -race` requires CGO and a compatible C compiler. If the
default compiler is unsupported, configure a compatible toolchain such as
MSYS2 UCRT64 for the current PowerShell session:

```powershell
$env:CC = "C:\msys64\ucrt64\bin\gcc.exe"
$env:Path = "C:\msys64\ucrt64\bin;$env:Path"
$env:CGO_ENABLED = "1"
go test -race ./...
```

## Docker and Kubernetes

The Dockerfile uses a Go build stage and a smaller Alpine runtime image. The
runtime process runs as a non-root user.

The Kubernetes manifests are collected in [`k8s/kustomization.yaml`](./k8s/kustomization.yaml).
They define the application, Redis, Prometheus, Grafana, and Ingress.

The manifests target namespace `ratelimitx`; create it before applying the
Kustomization because the namespace manifest is not included in its resource
list:

```sh
kubectl apply -f ./k8s/namespace.yaml
kubectl apply -k ./k8s
kubectl get pods,services,ingress,pvc -n ratelimitx
kubectl rollout status deployment/ratelimitx -n ratelimitx
```

The cluster must already have an NGINX Ingress Controller and the required
storage class available. The configured Ingress hosts are `ratelimitx.local` and
`grafana.ratelimitx.local`; local hostname mappings must point to the
appropriate Ingress address on every client device that needs to resolve
them. Do not copy a development machine's hosts-file entry as a portable
public address.

The app Deployment has startup, readiness, and liveness probes. Redis is
configured as a single replica. Review resource sizing, Redis availability,
persistence, Grafana access, and secret management before using this setup
for production.

## CI/CD

The workflow in [`.github/workflows/ci.yml`](./.github/workflows/ci.yml):

1. Runs tests, race tests, `go vet`, and `go build`.
2. Builds the Docker image.
3. Publishes the image to GitHub Container Registry for main-branch and
   version-tag pushes.
4. On pushes to `main`, uses GitHub OIDC to assume an AWS role, configures
   `kubectl` for EKS, applies `k8s/`, and waits for rollouts.

The deployment requires repository variables for the AWS role ARN, AWS
region, and EKS cluster name, plus a correctly scoped IAM trust policy.
OIDC avoids long-lived AWS access keys, but the role and Kubernetes
authorization must still grant only the permissions the workflow needs.

## Troubleshooting notes

- **Race detector / compiler errors on Windows:** the race detector needs
  CGO and a compatible compiler. Check `CC`, `CGO_ENABLED`, and the selected
  GCC toolchain.
- **OIDC assume-role failure:** verify the workflow's OIDC audience and
  subject match the IAM trust policy, and that `id-token: write` is enabled.
  Keep account IDs, role ARNs, and token claims in deployment configuration,
  not copied into public documentation.
- **Kubernetes RBAC `forbidden`:** the deployment identity lacks permission
  for the requested resource. Have an authorized administrator provision
  narrowly scoped RBAC rather than granting broad cluster-admin access.
- **PVC or Deployment selector immutable errors:** Kubernetes does not allow
  changing certain fields on existing resources. Preserve the existing
  StorageClass and Deployment selectors; inspect resources before attempting
  replacement, especially where persistent data is involved.
- **Ingress 404:** confirm the request hostname matches an Ingress rule and
  the Ingress Controller has observed that resource.
- **Ingress 502 / no endpoints:** check Pod readiness, Service selectors,
  Pod labels, container ports, and EndpointSlices:

  ```sh
  kubectl get pods,services,endpointslices -n ratelimitx -o wide
  kubectl describe deployment ratelimitx -n ratelimitx
  ```

- **Hostname works on one device only:** hosts-file entries are local to
  that device. Configure DNS or a matching hosts entry on each client, and
  ensure the client can reach the cluster's Ingress address.
- **Localhost confusion:** `localhost` refers to the machine where the
  process or port-forward is running. A port-forward started in a remote
  shell is not automatically exposed on a developer workstation or phone.
- **Health endpoint results:** HTTP 404 means the server responded but the
  route was not found; connection refusal means no process is accepting
  connections at that address and port; HTTP 200 means the endpoint
  responded successfully.
- **Partial `kubectl apply`:** earlier resources may have been updated before
  a later resource failed. Inspect actual cluster state; an apply failure is
  not an automatic rollback.
- **AWS costs:** scaling Pods to zero does not remove EKS control-plane,
  worker-node, or persistent-volume costs. Verify the AWS resources and
  billing state before considering a deployment fully stopped.

## Interview questions and answers

### Rate limiting

**1. What is rate limiting?**  
It controls how many operations a client can perform during a period or at a
given rate. It protects service capacity and provides predictable access.

**2. Why is rate limiting useful?**  
It helps prevent accidental overload and abusive traffic, protects
downstream dependencies, and can enforce different service tiers.

**3. How do rate limiting and throttling differ?**  
The terms are sometimes used interchangeably. Rate limiting defines an
allowed request rate; throttling commonly describes delaying or reducing
traffic when a limit or capacity threshold is reached.

**4. How does Fixed Window work?**  
It increments a counter for a fixed time bucket and rejects requests once
the configured count is reached. The bucket resets at the next boundary.

**5. What is the Fixed Window boundary-burst problem?**  
A client can use its full quota just before a boundary and another full quota
just after it, producing a short burst larger than the nominal rate.

**6. How does Sliding Window Log work?**  
It stores request timestamps, removes entries older than the rolling
window, and compares the remaining count with the limit. It is accurate but
stores per-request state.

**7. How does Sliding Window Counter work?**  
It combines the current fixed-window count with a weighted portion of the
previous window's count. It reduces boundary effects with less state than a
full request log, at the cost of an estimate.

**8. How does Token Bucket work?**  
Tokens are replenished at a configured rate up to a capacity. Each request
consumes a token; requests can burst while tokens are available.

**9. How does Leaky Bucket work in this project?**  
The Redis implementation schedules pending requests at regular intervals
in a Sorted Set and rejects new work when the pending queue reaches its
capacity.

**10. How do Token Bucket and Leaky Bucket differ?**  
Token Bucket allows bursts up to its token capacity. Leaky Bucket smooths
accepted work into a more regular schedule and bounds pending work.

**11. Which algorithms allow bursts?**  
Token Bucket explicitly permits bursts up to its capacity. Fixed Window can
also allow boundary bursts; the size and behavior depend on its window and
limit.

**12. Which algorithm gives an exact rolling-window count?**  
Sliding Window Log, because it tracks individual request timestamps.

**13. What is the accuracy/memory trade-off?**  
Tracking each request gives precise rolling-window decisions but uses more
memory. Counters use less memory but approximate or have fixed boundaries.

### Redis and Lua

**14. Why use Redis?**  
Redis is a shared, low-latency state store that lets multiple Go instances
make decisions using the same limiter state.

**15. Why not keep counters only in Go memory?**  
Each process would see only its local requests. A caller could exceed a
global limit by sending traffic to different replicas, and local state is
lost when a process restarts.

**16. How is state shared across replicas?**  
All replicas connect to the same Redis service and use the same key
derivation for a caller and limiter.

**17. What is a Redis key?**  
A key is the name Redis uses to identify a stored value, such as a counter,
hash, or Sorted Set.

**18. What is TTL? How is it different from persistence?**  
TTL is the time until a key expires. Persistence is how Redis saves data for
recovery after a restart. A TTL does not itself make data durable.

**19. What is a Redis Sorted Set?**  
It stores unique members ordered by numeric scores. The Leaky Bucket uses
scheduled timestamps as scores so it can find and remove due requests.

**20. What do `ZADD`, `ZCARD`, `ZRANGE`, and `ZREVRANGE` do?**  
`ZADD` inserts or updates members and scores; `ZCARD` counts members;
`ZRANGE` reads members in ascending score order; `ZREVRANGE` reads them in
descending order.

**21. What does `ZREMRANGEBYSCORE` do?**  
It removes all Sorted Set members whose scores fall within a specified
range. The Leaky Bucket removes scheduled entries that are already due.

**22. What do `INCR` and `PEXPIRE` do?**  
`INCR` atomically increments an integer key. `PEXPIRE` sets a key's
expiration in milliseconds.

**23. Why use Redis server time?**  
Using Redis `TIME` gives all application instances one clock for scheduling
and expiry calculations, avoiding differences between host clocks.

**24. Why use Lua scripts?**  
They keep related Redis reads and writes together as one atomic execution,
preventing another client from interleaving commands during a
check-and-update operation. They also reduce client/server round trips.

**25. Does Lua provide rollback?**  
No. Redis runs a script atomically relative to other commands, but it does
not automatically undo earlier writes if a later command in the script
errors.

**26. What happens if Redis restarts or becomes unavailable?**  
In-memory state may be lost unless Redis persistence is configured. While
Redis is unavailable, limiter operations return errors; this service responds
with HTTP 503 rather than silently allowing an unmetered request.

**27. What is connection pooling?**  
The Redis client reuses a bounded pool of connections rather than opening a
new network connection for every operation. This reduces connection setup
overhead.

### Go and distributed systems

**28. Why use Go for this service?**  
Go has a small deployment footprint, built-in concurrency support, a strong
standard HTTP library, and a straightforward toolchain for building a
containerized service.

**29. What is a goroutine?**  
A goroutine is a lightweight concurrent execution unit managed by the Go
runtime.

**30. What is a mutex?**  
A mutex coordinates access to shared Go memory so only one goroutine at a
time can enter a protected critical section. Redis Lua provides atomicity
for Redis operations; a Go mutex does not coordinate separate processes.

**31. What is `context.Context` used for?**  
It carries cancellation, deadlines, and request-scoped values across API
boundaries. Passing the HTTP request context to Redis lets cancellation
propagate.

**32. Why use a timeout?**  
A timeout bounds how long an operation may wait, so a slow dependency does
not consume resources indefinitely. The Redis client startup health check
uses a bounded context.

**33. What is CGO and why does `go test -race` need it?**  
CGO lets Go interoperate with C code. The race detector relies on a
compatible C toolchain on supported platforms, so compiler setup may be
required.

**34. What does `go test -race` detect?**  
It detects data races in Go memory accesses exercised by the tests. It does
not verify distributed Redis behavior or prove that untested code is race
free.

**35. What is a race condition?**  
It is a correctness problem where the result depends on the timing or
interleaving of concurrent operations.

**36. What is horizontal scaling?**  
It is adding service instances to handle more work. For a stateful limiter,
instances need shared state or a partitioning strategy to preserve the
intended limit.

**37. How do availability and consistency differ?**  
Availability concerns whether the system responds; consistency concerns
whether replicas observe compatible state. A distributed design must choose
how to behave when the shared state service is unhealthy.

**38. What are fail-open and fail-closed?**  
Fail-open allows requests when the limiter dependency fails, prioritizing
availability but risking unbounded traffic. Fail-closed rejects or errors
requests, preserving enforcement but reducing availability. This service
returns 503 when a limiter operation errors.

**39. What is the trade-off of centralized Redis state?**  
It gives replicas a common source of limiter state, but adds a dependency,
network latency, and an availability/scaling concern that must be monitored
and operated.

### Docker and Kubernetes

**40. What is containerization?**  
It packages an application and its runtime dependencies into an image so it
can run consistently across compatible environments.

**41. Why use a multi-stage Docker build?**  
The build stage compiles the Go binary; the runtime stage contains the
binary and runtime base without the Go compiler, reducing the shipped image.

**42. Why run the container as non-root?**  
It limits the impact of a compromised process by reducing its permissions
inside the container.

**43. What is Docker Compose?**  
It describes and runs a set of related containers and their networks,
ports, volumes, and dependencies for local development.

**44. What is a Kubernetes Pod and Deployment?**  
A Pod is the smallest schedulable unit that runs container(s). A Deployment
manages ReplicaSets and keeps the desired number of Pods running.

**45. What is a Kubernetes Service?**  
A Service provides a stable network endpoint and routes traffic to matching
Pods, whose IP addresses can change.

**46. What are ClusterIP, NodePort, and Ingress?**  
ClusterIP provides an internal Service address. NodePort exposes a Service
on node ports. Ingress defines HTTP(S) host/path routing; an Ingress
Controller implements those rules.

**47. What is a PersistentVolumeClaim?**  
A PVC is a request for persistent storage. Whether data survives Pod or
cluster changes also depends on the provisioned volume and reclaim policy.

**48. Why are Deployment selectors immutable?**  
The selector defines which Pods a Deployment manages. Changing it after
creation could change ownership unexpectedly, so Kubernetes disallows
updates to that field.

**49. What is Kubernetes RBAC?**  
Role-Based Access Control grants identities specific permissions on
Kubernetes resources through Roles/ClusterRoles and their bindings.

**50. How do readiness and liveness probes differ?**  
Readiness determines whether a Pod should receive Service traffic. Liveness
determines whether Kubernetes should restart a container. A startup probe
gives slow-starting containers time before the other probes begin.

### Monitoring, testing, CI/CD, and AWS

**51. What are Prometheus and Grafana?**  
Prometheus scrapes and stores time-series metrics. Grafana queries data
sources such as Prometheus and presents dashboards and visualizations.

**52. What is a time-series metric and what is scraping?**  
A time series is a sequence of measurements indexed by time and labels.
Scraping is Prometheus periodically requesting a configured metrics
endpoint.

**53. What is k6 and what is a virtual user?**  
k6 is a load-testing tool. A virtual user is a simulated concurrent
execution of a test scenario.

**54. How do you measure latency and identify a bottleneck?**  
Use request-duration distributions such as median and tail percentiles,
along with throughput, error rates, resource use, and dependency metrics.
Compare these under controlled load to identify where latency or failures
increase.

**55. How do you distinguish expected 429s from failures in load tests?**  
Treat HTTP 429 as an expected limiter outcome and track it separately.
Unexpected HTTP statuses, 5xx responses, and transport failures should be
reported as errors.

**56. What is CI/CD?**  
Continuous Integration automatically validates changes. Continuous
Delivery/Deployment automates packaging and release steps after required
checks pass.

**57. What does this project's GitHub Actions workflow do?**  
It runs Go tests, race tests, vet, and builds; builds and publishes the
container image; then uses OIDC and `kubectl` to deploy to EKS on main
branch pushes.

**58. What is GitHub OIDC and why use it with AWS?**  
GitHub Actions exchanges a short-lived OIDC identity token for temporary
AWS credentials through a trusted role. This avoids storing long-lived AWS
access keys in GitHub secrets.

**59. What is AWS STS?**  
AWS Security Token Service issues temporary credentials for an authorized
identity or role session.

**60. How do authentication and authorization differ?**  
Authentication establishes who an identity is. Authorization determines
what that identity is allowed to do.

**61. What is Amazon EKS?**  
Amazon Elastic Kubernetes Service is AWS's managed Kubernetes service. AWS
manages the Kubernetes control plane; worker capacity and cluster add-ons
still need configuration and operation.

**62. What happens if `kubectl apply` partially fails?**  
Earlier resources may already have been created or updated. Inspect the
cluster and rollout state, fix the failing resource, and reapply; do not
assume the operation rolled back.

### AWS networking and infrastructure

The documented cloud setup used Amazon EKS in `ap-south-1`, an EKS worker
node group, Kubernetes Ingress/Services, an AWS security-group ingress path,
GitHub Actions OIDC, and `gp3`-backed PVCs for monitoring data. The project
notes record application access at `3.7.37.251:32065`. Confirm the live AWS
console/configuration before quoting exact security-group IDs, source CIDRs,
node-group sizing, or subnet layout; those values are not all stored in this
repository.

**63. What AWS components did you use for this deployment?**  
I deployed the Kubernetes workloads to Amazon EKS in `ap-south-1`. EKS worker
nodes provided compute capacity, IAM/OIDC and STS provided deployment
authentication, security groups controlled network access, and `gp3` storage
backed monitoring PVCs. GitHub Container Registry (GHCR), not Amazon ECR, is
the image registry configured by this project.

**64. What is a VPC?**  
A Virtual Private Cloud is an isolated virtual network in AWS. It contains
subnets, route tables, and network controls used by resources such as EKS
worker nodes.

**65. What is the difference between a public and a private subnet?**  
A public subnet has a route to an Internet Gateway; a private subnet does
not provide direct inbound internet routing. Private workloads commonly use
a NAT Gateway for outbound internet access. The exact subnet arrangement
for this cluster should be confirmed in the AWS configuration.

**66. What is an EKS node group?**  
An EKS node group is a group of EC2 worker nodes registered with an EKS
cluster. Kubernetes schedules application Pods onto the available nodes.
The node group's desired/minimum/maximum capacity controls worker capacity;
it is separate from the number of application Pod replicas.

**67. How is a node group different from a Kubernetes Deployment?**  
A node group provides the EC2 machines that run workloads. A Deployment
declares and maintains the desired number of application Pods on the
cluster. For example, two RateLimitX replicas do not imply that the node
group has exactly two EC2 instances.

**68. What is an AWS security group?**  
A security group is a stateful virtual firewall associated with a resource
or network interface. Inbound rules allow specified protocols, ports, and
sources; response traffic for allowed connections is automatically tracked.
Security groups are distinct from Kubernetes NetworkPolicies.

**69. How did you add an inbound security-group rule for application access?**  
I identified the security group attached to the worker node or network
interface receiving the traffic, opened **EC2 → Network & Security →
Security Groups → Inbound rules → Edit inbound rules**, and added a
**Custom TCP** rule for the externally accessed NodePort. The deployment
notes document access on TCP port `32065` at `3.7.37.251`, so that port is
the one to verify against the active Ingress Controller Service and AWS
security group. I would set **Source** to the required client IP/CIDR (or
an approved security-group source), save the rule, and test connectivity.
I would not expose the app's internal ClusterIP port `8080` directly to the
internet.

**70. Why restrict the security-group source instead of allowing `0.0.0.0/0`?**  
Restricting the source to trusted client addresses reduces exposure. A
public source may be needed for a deliberately public service, but it should
be an explicit decision with only the required ports open and suitable
application protections in place.

**71. Which ports should be opened for this request path?**  
The recorded client URL uses TCP `32065`, the documented NodePort. The
Ingress then routes by hostname to the internal application Service on
port `8080` or Grafana Service on port `3000`. Those backend ports are
cluster-internal in the manifests. Open ports `80` or `443` only if the
Ingress Controller is actually configured to receive client traffic on
those ports; do not assume they replace the configured NodePort.

**72. What is the request path from a browser to a RateLimitX Pod?**  
The client resolves the hostname to the configured reachable address and
connects to the exposed NodePort (`3.7.37.251:32065` in the deployment
notes). The Ingress Controller receives the request, matches the host rule,
and forwards it to the `ratelimitx` ClusterIP Service on port `8080`. The
Service sends it to a ready application Pod. The security group must allow
the network connection to the node port; Kubernetes Ingress and Service
rules handle routing inside the cluster.

**73. Does a security group route traffic to an Ingress backend?**  
No. A security group filters network traffic at the AWS network interface
or resource boundary. The Ingress Controller routes HTTP requests based on
host/path rules, and a Kubernetes Service selects Pod endpoints.

**74. What is the difference between a NodePort and a ClusterIP Service?**  
ClusterIP provides an internal cluster address. NodePort also exposes a
port on cluster nodes so traffic can enter from outside the cluster when
network rules allow it. In this deployment, the RateLimitX Service itself
is ClusterIP; the external NodePort is part of the Ingress access path.

**75. What is the difference between an Ingress and an Ingress Controller?**  
An Ingress is a Kubernetes resource describing host/path routing rules. An
Ingress Controller is the running software that watches those resources and
implements the routing. Creating an Ingress alone does not install a
controller or open an AWS network path.

**76. What is the difference between a security group and a network ACL?**  
A security group is stateful and is associated with a resource or network
interface. A network ACL is stateless and applies at the subnet boundary,
so inbound and outbound rules must both allow the traffic.

**77. How does DNS or a hosts-file entry relate to security-group rules?**  
DNS or a hosts-file entry maps a hostname to an IP address; it does not
open a port or create a route. The client must resolve the host to a
reachable address, and network controls must allow the destination port.
A Windows hosts-file entry is local to that computer and does not configure
DNS on a phone.

**78. What is an Internet Gateway?**  
An Internet Gateway enables internet routing for resources in public
subnets when route tables and public addressing are configured accordingly.
It does not by itself permit traffic through a security group.

**79. What is a route table?**  
A route table determines where network traffic from a subnet is directed,
such as to local VPC addresses, an Internet Gateway, or a NAT Gateway. A
valid route is required in addition to appropriate security-group rules.

**80. What is a NAT Gateway used for?**  
A NAT Gateway allows resources in private subnets to initiate outbound
internet connections without accepting unsolicited inbound internet
connections. Whether this cluster used a NAT Gateway should be verified in
its VPC configuration.

**81. What is an Availability Zone, and how does it relate to a node group?**  
An Availability Zone is an isolated location within an AWS Region. Worker
nodes can be distributed across multiple zones to improve resilience, if
the node-group subnet configuration and available capacity support it.

**82. What does `gp3` mean in the deployment notes?**  
`gp3` is an AWS EBS General Purpose SSD volume type. The monitoring
PersistentVolumeClaims used the `gp3` StorageClass; PVCs request storage,
while the CSI/provisioning configuration determines the backing volume.

**83. Did this project use Amazon ECR?**  
The checked-in Compose, Kubernetes, and GitHub Actions configuration points
to GitHub Container Registry (`ghcr.io/nandinideshmukh/rate-limiter`), not
Amazon ECR. I would describe GHCR as the registry used and EKS as the AWS
deployment target.

**84. How did GitHub Actions authenticate to AWS without stored access keys?**  
The workflow requests a GitHub OIDC token and assumes an IAM role through
`aws-actions/configure-aws-credentials`. AWS STS returns temporary
credentials after validating the role trust policy. The workflow then
configures `kubectl` for the EKS cluster.

**85. What does `aws eks update-kubeconfig` do?**  
It adds or updates a kubeconfig context for the named EKS cluster and
region, allowing tools such as `kubectl` to connect using AWS
authentication. It does not grant Kubernetes permissions by itself.

**86. How do you debug a connection timeout to the cluster?**  
Check the hostname-to-IP resolution, destination IP and port, client
network, security-group inbound rule and source, subnet route table, and
whether the node or Ingress Controller is listening on that port. Then
check Kubernetes Ingress rules, Service endpoints, and Pod readiness.
An allowed security-group rule alone does not prove that the route or
backend is healthy.

**87. What are the main security-group troubleshooting checks?**  
Confirm the rule is on the security group attached to the receiving node or
network interface; confirm protocol, destination port, source CIDR, and
whether another network control blocks the path. Test from an allowed
client and keep the rule limited to the intended traffic.

## Two-minute project explanation

RateLimitX is a distributed rate-limiting service built with Go and Redis.
It implements Fixed Window, Sliding Window Log, Sliding Window Counter,
Token Bucket, and Leaky Bucket algorithms behind HTTP middleware. The
middleware selects a policy using an API key, then asks a Redis-backed
limiter to decide whether to allow the request.

The main distributed-systems challenge is keeping decisions consistent
across multiple Go instances. Redis provides shared state, and Lua scripts
keep related Redis operations atomic. The Leaky Bucket implementation uses
a Sorted Set to schedule pending requests, Redis server time for a shared
clock, and a sequence key for unique members.

The project also includes a Docker Compose stack, Kubernetes manifests,
Prometheus metrics, Grafana dashboards, and k6 scenarios. GitHub Actions
runs tests and static checks, publishes a container image, and deploys to
EKS using GitHub OIDC. This project has been a practical way to explore
concurrency, shared state, container orchestration, observability, and
automated deployment.

## Further improvements

- Use highly available Redis and define failover behavior.
- Configure and validate Redis persistence if state must survive restarts.
- Add distributed tracing and richer dependency-level metrics.
- Benchmark each algorithm with consistent workloads.
- Add policy/configuration management rather than relying only on
  application-defined policies.
- Review production security, access controls, and secret handling.
