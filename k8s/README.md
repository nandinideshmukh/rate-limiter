# RateLimitX on Kubernetes (Kind)

This is a Kubernetes deployment of the existing RateLimitX container image. It does not build an image or change the Go application, Dockerfile, or Compose configuration. The GitHub Actions workflow publishes the image and deploys these manifests to EKS after successful CI checks on pushes to `main`.

## Architecture and resources

The Go container serves the frontend and API on port 8080. A two-replica Deployment uses a ClusterIP Service; the Ingress routes `ratelimitx.local` traffic to that Service. Each pod's `INSTANCE_ID` is its Kubernetes pod name. Both pods use the single Redis Service at `redis:6379`.

| Resource | Purpose |
| --- | --- |
| Namespace `ratelimitx` | Isolates the app, Redis, and monitoring resources. |
| Deployment and Service `ratelimitx` | Runs two Go app pods and provides their stable in-cluster endpoint. |
| Deployment and Service `redis` | Provides one shared, in-memory Redis instance. |
| Ingress `ratelimitx` | Routes the `ratelimitx.local` host to the Go Service through an nginx Ingress Controller. |
| Prometheus ConfigMap and ServiceAccount | Configures five-second pod scraping and provides the identity used by Prometheus. |
| Prometheus Role and RoleBinding | Administrator-managed, namespace-scoped permission for pod discovery; intentionally excluded from the GitHub Actions Kustomize deployment. |
| Prometheus Deployment, Service, and PVC | Runs Prometheus, exposes its web UI inside the cluster, and stores its time series. |
| Grafana provisioning ConfigMap, dashboard ConfigMap, Deployment, Service, and PVC | Provisions the existing Prometheus datasource and RateLimitX dashboard, and stores Grafana data. |

The existing Grafana dashboard JSON is copied into this directory for Kustomize to package. Keep `monitoring/grafana/provisioning/dashboards/ratelimitx-dashboard.json` and `k8s/monitoring/grafana/ratelimitx-dashboard.json` in sync when changing the dashboard.

This is not a production configuration. Redis has one replica, no authentication, and no persistent storage; Redis data is lost when its pod is replaced. Prometheus and Grafana use PVCs, which require a default StorageClass in Kind. The app does not implement graceful HTTP server shutdown, so the Deployment uses Kubernetes' normal termination grace period but cannot guarantee graceful request draining.

## Prerequisites

- Docker Desktop with the existing Kind cluster `ratelimitx` available.
- `kubectl` configured to context `kind-ratelimitx`, and Kustomize support (`kubectl kustomize`).
- The GHCR image `ghcr.io/nandinideshmukh/rate-limiter:main` is public, or the namespace has a suitable image-pull secret if the package is private.
- A default StorageClass for the Prometheus and Grafana claims.
- For EKS, an EKS cluster with worker capacity, an EBS CSI-backed default StorageClass, and namespace `ratelimitx` created before the GitHub Actions deployment role is granted namespace-scoped access.

Check the context and cluster before starting:

```powershell
kubectl config current-context
kubectl get nodes
kubectl get storageclass
```

The expected context is `kind-ratelimitx`. The manifests do not create or recreate the Kind cluster.

## Install an Ingress Controller

An Ingress object does not install a controller. If the cluster does not already have one, install the Kind-compatible ingress-nginx controller and wait for it:

```powershell
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml
kubectl wait --namespace ingress-nginx --for=condition=Ready pod --selector=app.kubernetes.io/component=controller --timeout=180s
```

This command installs the controller in the selected cluster; it is separate from the RateLimitX manifests. Check for an existing controller first to avoid installing a duplicate.

## Apply the manifests

From the repository root:

```powershell
kubectl config use-context kind-ratelimitx
kubectl apply -f .\k8s\namespace.yaml
kubectl kustomize .\k8s\
kubectl apply -k .\k8s\
kubectl rollout status deployment/ratelimitx -n ratelimitx --timeout=180s
kubectl rollout status deployment/redis -n ratelimitx --timeout=180s
kubectl rollout status deployment/prometheus -n ratelimitx --timeout=180s
kubectl rollout status deployment/grafana -n ratelimitx --timeout=180s
```

The root Kustomization intentionally excludes `namespace.yaml`: it must already exist. This lets the EKS deployment role be scoped to `ratelimitx` rather than granting it permission to create or modify cluster-wide namespaces. It also excludes `monitoring/prometheus/rbac.yaml`; only a Kubernetes administrator should install or update the Prometheus Role and RoleBinding. If those resources are not already present, an administrator can apply them once with:

```powershell
kubectl apply -n ratelimitx -f .\k8s\monitoring\prometheus\rbac.yaml
```

The ServiceAccount is a separate manifest and remains included in the normal Kustomize deployment. Do not apply the manifests until the Ingress Controller and image access prerequisites are satisfied.

## Inspect workloads and logs

```powershell
kubectl get deployments,pods,services,ingress,pvc -n ratelimitx -o wide
kubectl describe pods -n ratelimitx
kubectl logs -n ratelimitx -l app.kubernetes.io/name=ratelimitx --all-containers=true --prefix --tail=100
kubectl logs -n ratelimitx deployment/redis --tail=100
kubectl logs -n ratelimitx deployment/prometheus --tail=100
kubectl logs -n ratelimitx deployment/grafana --tail=100
```

## Test Redis connectivity

Run a temporary Redis client pod in the namespace:

```powershell
kubectl run redis-client -n ratelimitx --rm -it --restart=Never --image=redis:7-alpine -- redis-cli -h redis -p 6379 ping
```

The expected response is `PONG`.

## Access the app and Ingress

For clusters without host port mappings, forward a local port to the Ingress Controller in a dedicated PowerShell window:

```powershell
kubectl port-forward -n ingress-nginx service/ingress-nginx-controller 8080:80
```

In another window, verify the host route without editing the Windows hosts file:

```powershell
curl.exe -H "Host: ratelimitx.local" http://127.0.0.1:8080/health
```

To browse the frontend at `http://ratelimitx.local:8080`, open the hosts file as Administrator:

```powershell
Start-Process notepad.exe -Verb RunAs -ArgumentList "$env:WINDIR\System32\drivers\etc\hosts"
```

Add this line, save the file, and then open `http://ratelimitx.local:8080`:

```text
127.0.0.1 ratelimitx.local
```

Alternatively, access the Go Service directly without Ingress:

```powershell
kubectl port-forward -n ratelimitx service/ratelimitx 8080:8080
```

Then browse to `http://localhost:8080`.

If you later create a *new* Kind cluster and want `http://ratelimitx.local` without a port-forward, configure `extraPortMappings` for host ports 80 and 443 to node container ports 80 and 443 in the Kind cluster config before creation, and map `127.0.0.1 ratelimitx.local` in Windows hosts. This is not required for the existing cluster when using the port-forward above. Do not recreate the existing cluster just to add port mappings.

## Access Prometheus and Grafana

In separate PowerShell windows, start port-forwards:

```powershell
kubectl port-forward -n ratelimitx service/prometheus 9090:9090
```

```powershell
kubectl port-forward -n ratelimitx service/grafana 3000:3000
```

- Prometheus: `http://localhost:9090`
- Prometheus scrape targets: `http://localhost:9090/targets`
- Grafana: `http://localhost:3000`

The provisioned RateLimitX dashboard is in the `RateLimitX` folder. Anonymous access is enabled as Viewer, matching the local Compose setup; do not expose Grafana publicly with these settings.

Prometheus discovers ready pods only in namespace `ratelimitx`, keeps only RateLimitX containers exposing port 8080, and scrapes their existing `/metrics` endpoint directly. To confirm both replicas are being scraped, open **Status → Targets** in Prometheus and check for two `ratelimitx` targets.

## Update the app image

Update the image tag in `k8s/app/deployment.yaml` to the desired published tag, then apply and wait for the rollout:

```powershell
kubectl apply -k .\k8s\
kubectl rollout status deployment/ratelimitx -n ratelimitx --timeout=180s
```

For a one-off update without editing the manifest:

```powershell
kubectl set image deployment/ratelimitx ratelimitx=ghcr.io/nandinideshmukh/rate-limiter:<tag> -n ratelimitx
kubectl rollout status deployment/ratelimitx -n ratelimitx --timeout=180s
```

Replace `<tag>` with an actual release tag. The manifest uses `:main` and `imagePullPolicy: Always`.

## Deploy to EKS from GitHub Actions

The `deploy-eks` job in `.github/workflows/ci.yml` runs only for pushes to `main`, after both the `ci` and `publish` jobs succeed. It authenticates to AWS using GitHub's OIDC token (no stored AWS access keys), updates kubeconfig, applies `k8s/`, and waits for all four Deployments to roll out. It does not create an AWS load balancer or use ElastiCache. The app and Redis remain in-cluster; the workflow only deploys Kubernetes resources.

### AWS and EKS setup (one time)

1. Create the EKS cluster and worker nodes. Install the EBS CSI driver and configure a default EBS-backed StorageClass for the Prometheus and Grafana PVCs. Create the namespace with an administrator identity:

   ```powershell
   aws eks update-kubeconfig --name <cluster-name> --region <aws-region>
   kubectl create namespace ratelimitx
   ```

   If the namespace already exists, keep it; do not recreate it.

2. In AWS IAM, add the GitHub Actions OIDC identity provider with URL `https://token.actions.githubusercontent.com` and audience `sts.amazonaws.com` (if it is not already configured in the AWS account).

3. Create an IAM role for GitHub Actions with this trust policy. Replace `<ACCOUNT_ID>` with your AWS account ID. The subject condition limits role assumption to pushes on `main` in this repository:

   ```json
   {
     "Version": "2012-10-17",
     "Statement": [
       {
         "Effect": "Allow",
         "Principal": {
           "Federated": "arn:aws:iam::<ACCOUNT_ID>:oidc-provider/token.actions.githubusercontent.com"
         },
         "Action": "sts:AssumeRoleWithWebIdentity",
         "Condition": {
           "StringEquals": {
             "token.actions.githubusercontent.com:aud": "sts.amazonaws.com",
             "token.actions.githubusercontent.com:sub": "repo:nandinideshmukh@150786311/rate-limiter@1446791164:ref:refs/heads/main"
           }
         }
       }
     ]
   }
   ```

4. Give that IAM role permission to describe only the target EKS cluster (replace the placeholders):

   ```json
   {
     "Version": "2012-10-17",
     "Statement": [
       {
         "Effect": "Allow",
         "Action": "eks:DescribeCluster",
         "Resource": "arn:aws:eks:<AWS_REGION>:<ACCOUNT_ID>:cluster/<CLUSTER_NAME>"
       }
     ]
   }
   ```

5. Add the IAM role to the EKS cluster's **Access entries**, then associate `AmazonEKSAdminPolicy` scoped only to namespace `ratelimitx`. The namespace must exist first. For example, using an AWS CLI identity allowed to administer EKS access:

   ```powershell
   aws eks create-access-entry --cluster-name <cluster-name> --principal-arn <github-actions-role-arn> --type STANDARD
   aws eks associate-access-policy --cluster-name <cluster-name> --principal-arn <github-actions-role-arn> --policy-arn arn:aws:eks::aws:cluster-access-policy/AmazonEKSAdminPolicy --access-scope type=namespace,namespaces=ratelimitx
   ```

   The workflow role needs namespace-scoped permissions for the Deployments, Services, PVCs, ConfigMaps, and ServiceAccounts in the Kustomize resources. It does not need permission to manage Roles or RoleBindings: those are installed by a Kubernetes administrator and are excluded from `kubectl apply -k ./k8s`. It also does not need namespace-creation rights. Use an EKS cluster authentication mode that supports access entries.

6. In the GitHub repository, open **Settings → Secrets and variables → Actions → Variables** and add:

   | Variable | Value |
   | --- | --- |
   | `AWS_ROLE_ARN` | ARN of the GitHub Actions IAM role |
   | `AWS_REGION` | EKS cluster's AWS region, for example `us-west-2` |
   | `EKS_CLUSTER_NAME` | EKS cluster name |

   These are configuration identifiers, not credentials. Do not add AWS access keys. Ensure the GHCR package is public, or separately configure an image-pull secret in namespace `ratelimitx` for a private package.

### Trigger and verify a deployment

Push a change to the repository's `main` branch. In **GitHub → Actions**, open the `CI` run:

- `ci` runs tests, vet, build, and the Docker build.
- `publish` publishes the `main` image to GHCR only after CI passes.
- `deploy-eks` assumes the AWS role through OIDC and applies the Kubernetes manifests only after CI and image publishing succeed.

Check rollout and logs from a machine configured with EKS access:

```powershell
kubectl get deployments,pods,services,pvc -n ratelimitx
kubectl rollout status deployment/ratelimitx -n ratelimitx
kubectl logs -n ratelimitx deployment/ratelimitx --tail=100
```

The manifests include an Ingress object with class `nginx`, but do not install an Ingress Controller or provide a public endpoint. Without an Ingress Controller, access the app using `kubectl port-forward`. Public access without an AWS load balancer requires a separately operated entry point (for example, a self-managed Ingress Controller exposed through NodePort and appropriate network rules).

## Troubleshooting

- **`ImagePullBackOff` / `ErrImagePull`:** Check the image name/tag and GHCR package visibility. For a private package, configure a Kubernetes `imagePullSecret` in namespace `ratelimitx` with read-only package access and reference it from the app pod spec; do not put credentials in these manifests or commit them.
- **`CrashLoopBackOff` for the app:** Check `kubectl logs -n ratelimitx deployment/ratelimitx --previous` and confirm Redis is ready and `REDIS_ADDR` is `redis:6379`. The app exits if it cannot connect to Redis during startup.
- **Failed app probes:** Inspect `kubectl describe pod -n ratelimitx <pod-name>` and its logs. The probes use the app's `/health` endpoint on port 8080.
- **Pending PVCs:** Check `kubectl get storageclass` and `kubectl describe pvc -n ratelimitx`. Configure a default Kind StorageClass before expecting Prometheus or Grafana storage to bind.
- **Ingress has no address or returns 404:** Confirm an ingress-nginx controller is installed and ready, the Ingress class is `nginx`, and the Service has endpoints: `kubectl get endpoints ratelimitx -n ratelimitx`.
- **Grafana URL returns nginx `404 Not Found` (including `/api/health`):** The request host/path did not match the Grafana Ingress rule. The dashboard frontend was requesting `grafana.3.7.37.251.nip.io:32065`, while the Ingress initially only routed host `3.7.37.251.nip.io` on `/grafana`; nginx therefore returned its default 404 before the request reached Grafana. The Ingress and Grafana `GF_SERVER_ROOT_URL` must agree on the same host and whether Grafana is served at `/` or `/grafana`. The current manifests route `grafana.3.7.37.251.nip.io` at `/` to the Grafana Service and configure Grafana for that root URL. Apply the updated manifests and verify the live Ingress and Grafana rollout: `kubectl describe ingress ratelimitx -n ratelimitx` and `kubectl rollout status deployment/grafana -n ratelimitx`.
- **Prometheus has no targets:** Check its pod-discovery RBAC and config, then open `/targets`; the Prometheus config only selects ready app pods on container port 8080.
- **Grafana dashboard or datasource missing:** Check the Grafana logs and verify the provisioning ConfigMaps and projected volume: `kubectl describe pod -n ratelimitx -l app.kubernetes.io/name=grafana`.

## Delete the deployment

```powershell
kubectl delete -k .\k8s\
```

This removes the namespace and its resources, including the Prometheus and Grafana PVCs and their stored data. It does not remove a separately installed Ingress Controller. Only delete the namespace if it contains no resources you need to keep.

## Validation

These commands render or validate resources without applying them:

```powershell
kubectl kustomize .\k8s\
kubectl apply --dry-run=client -k .\k8s\
```
