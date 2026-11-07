## Build and publish the Docker image to GitHub Container Registry

Run these commands from the repository root, where the `Dockerfile` is
located. First authenticate to GHCR with a GitHub Personal Access Token that
has permission to publish packages. Docker prompts for the token so it does
not need to be written into the command or shell history.

```powershell
docker login ghcr.io --username nandinideshmukh
docker build --file Dockerfile --tag ghcr.io/nandinideshmukh/rate-limiter:main .
docker push ghcr.io/nandinideshmukh/rate-limiter:main
```

For a versioned release, tag and push the same image with the desired version
tag as well. The GitHub Actions workflow builds and publishes the image on
main-branch and version-tag pushes.

---

configure your hosts file -> run as administrator and add these
3.7.37.251 ratelimitx.local
3.7.37.251 grafana.ratelimitx.local

http://ratelimitx.local:32065/ -> available here
# Cloud Deployment and Troubleshooting

## 1. AWS EKS Deployment

RateLimitX was deployed on AWS EKS in the `ap-south-1` region.

The deployment included:
- Two Go application replicas.
- Redis for shared rate-limiting state.
- NGINX Ingress Controller for incoming traffic.
- Prometheus for metrics collection.
- Grafana for monitoring and dashboards.
- Kubernetes Services, Deployments, ConfigMaps, and Persistent Volume Claims.

### Problems encountered

#### A. GitHub Actions OIDC authentication failure

**Problem:**

GitHub Actions initially failed to assume the AWS IAM role.

**Root cause:**

The IAM role's OIDC trust policy had an incorrect `sub` condition.

The actual GitHub Actions token claims included:

- Issuer: `https://token.actions.githubusercontent.com`
- Audience: `sts.amazonaws.com`
- Subject: `repo:nandinideshmukh@150786311/rate-limiter@1446791164:ref:refs/heads/main`

**Solution:**

Updated the IAM role trust relationship to match the actual token claims.

The workflow uses:

- `id-token: write`
- `contents: read`
- `aws-actions/configure-aws-credentials@v4`

**Learning:**

OIDC authentication depends on matching the issuer, audience, and subject conditions in the IAM trust policy. A mismatch prevents GitHub Actions from assuming the role.

---

#### B. EKS permissions and Kubernetes RBAC failure

**Problem:**

The GitHub Actions deployment failed while creating or updating the Prometheus Kubernetes Role and RoleBinding.

The error indicated insufficient permissions for RBAC resources.

**Root cause:**

The deployment identity had permissions to deploy application resources but did not have sufficient Kubernetes RBAC permissions to manage Roles and RoleBindings.

**Solution:**

Separated cluster-level RBAC configuration from normal application deployment.

The Prometheus ServiceAccount requires permissions to:

- Get Pods.
- List Pods.
- Watch Pods.

These permissions are granted through a Kubernetes Role and RoleBinding.

**Learning:**

AWS IAM authentication and Kubernetes RBAC authorization are separate layers.

Successfully authenticating to EKS does not automatically grant permission to create every Kubernetes resource.

---

#### C. Persistent Volume Claim immutable field error

**Problem:**

Kubernetes rejected updates to existing PVCs:

- `grafana-data`
- `prometheus-data`

**Root cause:**

The existing PVCs used the `gp3` StorageClass, while the updated manifests did not preserve the original StorageClass configuration.

Some PVC fields are immutable after creation.

**Solution:**

Preserved the existing `storageClassName: gp3` configuration in the manifests.

Avoided deleting PVCs because they contain persistent monitoring data.

**Learning:**

Kubernetes persistent storage configuration must be maintained consistently. PVCs should not be deleted casually because doing so can risk losing application data.

---

#### D. Deployment selector immutable error

**Problem:**

Kubernetes rejected updates to existing Deployments because their selectors had changed.

**Root cause:**

Deployment selectors are immutable after creation.

The existing Deployments used specific labels:

| Component | Selector |
|---|---|
| RateLimitX | `app: ratelimitx` |
| Redis | `app: redis` |
| Prometheus | `app: prometheus` |
| Grafana | `app.kubernetes.io/component: monitoring` and `app.kubernetes.io/name: grafana` |

**Solution:**

Preserved the existing selectors and ensured that Pod template labels matched them.

**Learning:**

Deployment selectors are part of Kubernetes resource identity. They should be designed carefully before deployment.

---

#### E. NGINX Ingress returning 404 and 502

**Problem:**

Accessing `grafana.ratelimitx.local` returned an NGINX 404 response.

The application also returned 502 Bad Gateway errors during deployment.

**Root causes:**

1. Initially, the Ingress configuration contained a rule for `ratelimitx.local` but did not contain a Grafana host rule.
2. A 502 response indicated that NGINX could not successfully reach a healthy backend.
3. The RateLimitX Service initially had no ready endpoints.

**Debugging:**

Checked EndpointSlices:

```bash
kubectl get endpointslice -n ratelimitx \
  -l kubernetes.io/service-name=ratelimitx -o wide
```

The output showed no available endpoint addresses, indicating a possible
Service selector mismatch or Pods that were not ready.

**Solution:**

- Verified Service selectors.
- Checked Pod readiness.
- Corrected Ingress host and backend routing.
- Scaled the application Deployment to two replicas.
- Verified that the application Pods became ready.

**Learning:**

An Ingress resource alone does not guarantee that an application is reachable.
The complete request path must work:

`Client → NGINX Ingress → Kubernetes Service → Ready Pods`

---

#### F. Local hostname not resolving on mobile devices

**Problem:**

The application was accessible using `ratelimitx.local` on the development
machine but did not resolve automatically on a phone.

**Root cause:**

The hostname mapping configured on Windows does not automatically apply to
Android or iOS devices.

**Solution:**

Recognized that hostname resolution must be configured for the mobile device
or through a DNS service available on the network.

**Learning:**

Local host-file mappings are machine-specific. They are not automatically
propagated across devices.

---

#### G. CloudShell port-forwarding confusion

**Problem:**

A service accessed through `localhost` in AWS CloudShell was not accessible
through the same localhost address on the Windows machine or phone.

**Root cause:**

`localhost` refers to the machine where the command is running. A
port-forward initiated inside CloudShell exposes the service in the CloudShell
environment, not automatically on the developer's local computer.

**Learning:**

Port-forwarding and localhost networking are environment-specific. The
location where the command runs determines what localhost refers to.

---

#### H. Prometheus health endpoint debugging

**Problem:**

The `/health/prometheus` endpoint initially returned HTTP 404. At another
point, the connection was refused.

**Root cause:**

These errors indicate different problems:

| Error | Meaning |
|---|---|
| HTTP 404 | Server responded, but the requested route was not found. |
| Connection refused | No process was listening on the target port, or the port-forward was unavailable. |
| HTTP 200 | Endpoint responded successfully. |

**Solution:**

Verified the running application version, endpoint route, and port-forward
status. The endpoint subsequently returned HTTP 200.

**Learning:**

HTTP errors and connection errors should be debugged separately. A 404 is an
application-routing issue, while connection refusal is generally a networking
or process-availability issue.

---

#### I. Partial Kubernetes deployment

**Problem:**

A GitHub Actions deployment failed after some Kubernetes resources had
already been applied.

**Root cause:**

Kubernetes resource application is not an all-or-nothing transaction.
Resources successfully applied before a later failure are not automatically
rolled back.

**Solution:**

Inspected the cluster state after failed deployments instead of assuming that
every resource had been reverted. Used rollout and resource-status commands
to identify which components needed attention.

**Learning:**

A failed deployment pipeline does not necessarily mean that the cluster
remained unchanged.

## 2. IAM Policies and Access Management

### GitHub Actions IAM role

GitHub Actions uses OIDC to obtain temporary AWS credentials rather than
storing long-lived AWS access keys in GitHub Secrets.

The workflow assumes the IAM role `RateLimitX-GitHubActions-EKS`. The role
includes permission to describe the EKS cluster:

```json
{
  "Effect": "Allow",
  "Action": [
    "eks:DescribeCluster"
  ],
  "Resource": "*"
}
```

The workflow reads the role ARN from the GitHub repository variable
`AWS_ROLE_ARN`. Other repository variables are:

| Variable | Value |
|---|---|
| `AWS_ROLE_ARN` | `arn:aws:iam::716228811568:role/RateLimitX-GitHubActions-EKS` |
| `AWS_REGION` | `ap-south-1` |
| `EKS_CLUSTER_NAME` | `ratelimitx` |

The role also needs the appropriate EKS access entry and Kubernetes
authorization for the resources managed by the deployment workflow.

### Authentication vs authorization

These are two separate steps:

1. **Authentication:** AWS verifies the identity of the GitHub Actions
   workflow using OIDC.
2. **Authorization:** EKS and Kubernetes determine which operations that
   identity is allowed to perform.

A successful AWS role assumption does not mean that the workflow can
automatically create Deployments, Roles, PVCs, or other Kubernetes resources.

### Kubernetes RBAC for Prometheus

Prometheus needs permission to discover application Pods. The required
permissions include:

```yaml
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch"]
```

Grant these permissions to the Prometheus ServiceAccount through a Role and
RoleBinding in the appropriate namespace.

## 3. Kubernetes Debugging Commands

### Check Pods

```bash
kubectl get pods -n ratelimitx
```

### Check Deployments

```bash
kubectl get deployments -n ratelimitx
```

### Check Services

```bash
kubectl get services -n ratelimitx
```

### Check Ingress

```bash
kubectl get ingress -n ratelimitx
```

### Check EndpointSlices

```bash
kubectl get endpointslice -n ratelimitx
```

### Inspect Pod logs

```bash
kubectl logs -n ratelimitx deployment/ratelimitx
```

### Describe a Pod

```bash
kubectl describe pod <pod-name> -n ratelimitx
```

### Check rollout status

```bash
kubectl rollout status deployment/ratelimitx -n ratelimitx
```

### Inspect PVCs

```bash
kubectl get pvc -n ratelimitx
```

### Check Kubernetes events

```bash
kubectl get events -n ratelimitx --sort-by=.metadata.creationTimestamp
```

## 4. Important Cloud Deployment Lessons

The deployment process provided practical experience with:

- AWS EKS cluster configuration and access management.
- GitHub Actions OIDC-based authentication.
- IAM trust policies and permission policies.
- Kubernetes RBAC and ServiceAccounts.
- Deployment selectors and immutable fields.
- Persistent Volume Claims and StorageClasses.
- NGINX Ingress routing and backend connectivity.
- Kubernetes Services, Pods, and EndpointSlices.
- Debugging HTTP 404, 502, and connection-refused errors.
- Understanding partial deployment failures.
- Diagnosing application health and monitoring endpoints.

The main lesson was that deploying a distributed application involves more
than successfully building a Docker image. Application availability depends
on the interaction between cloud IAM, Kubernetes authorization, networking,
storage, service discovery, and application health.