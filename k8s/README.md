# RateLimitX on Kubernetes (Kind)

This is a local-learning deployment of the existing RateLimitX container image. It does not build an image or change the Go application, Dockerfile, Compose configuration, or CI workflow.

## Architecture and resources

The Go container serves the frontend and API on port 8080. A two-replica Deployment uses a ClusterIP Service; the Ingress routes `ratelimitx.local` traffic to that Service. Each pod's `INSTANCE_ID` is its Kubernetes pod name. Both pods use the single Redis Service at `redis:6379`.

| Resource | Purpose |
| --- | --- |
| Namespace `ratelimitx` | Isolates the app, Redis, and monitoring resources. |
| Deployment and Service `ratelimitx` | Runs two Go app pods and provides their stable in-cluster endpoint. |
| Deployment and Service `redis` | Provides one shared, in-memory Redis instance. |
| Ingress `ratelimitx` | Routes the `ratelimitx.local` host to the Go Service through an nginx Ingress Controller. |
| Prometheus ConfigMap, ServiceAccount, Role, and RoleBinding | Configures five-second pod scraping and grants read-only access to pod discovery in this namespace. |
| Prometheus Deployment, Service, and PVC | Runs Prometheus, exposes its web UI inside the cluster, and stores its time series. |
| Grafana provisioning ConfigMap, dashboard ConfigMap, Deployment, Service, and PVC | Provisions the existing Prometheus datasource and RateLimitX dashboard, and stores Grafana data. |

The existing Grafana dashboard JSON is copied into this directory for Kustomize to package. Keep `monitoring/grafana/provisioning/dashboards/ratelimitx-dashboard.json` and `k8s/monitoring/grafana/ratelimitx-dashboard.json` in sync when changing the dashboard.

This is not a production configuration. Redis has one replica, no authentication, and no persistent storage; Redis data is lost when its pod is replaced. Prometheus and Grafana use PVCs, which require a default StorageClass in Kind. The app does not implement graceful HTTP server shutdown, so the Deployment uses Kubernetes' normal termination grace period but cannot guarantee graceful request draining.

## Prerequisites

- Docker Desktop with the existing Kind cluster `ratelimitx` available.
- `kubectl` configured to context `kind-ratelimitx`, and Kustomize support (`kubectl kustomize`).
- The GHCR image `ghcr.io/nandinideshmukh/rate-limiter:main` is public, or the namespace has a suitable image-pull secret if the package is private.
- A default StorageClass for the Prometheus and Grafana claims.

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
kubectl kustomize .\k8s\
kubectl apply -k .\k8s\
kubectl rollout status deployment/ratelimitx -n ratelimitx --timeout=180s
kubectl rollout status deployment/redis -n ratelimitx --timeout=180s
kubectl rollout status deployment/prometheus -n ratelimitx --timeout=180s
kubectl rollout status deployment/grafana -n ratelimitx --timeout=180s
```

`kubectl apply -k` creates the namespace if needed, then all app and monitoring resources. Do not apply the manifests until the Ingress Controller and image access prerequisites are satisfied.

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

## Troubleshooting

- **`ImagePullBackOff` / `ErrImagePull`:** Check the image name/tag and GHCR package visibility. For a private package, configure a Kubernetes `imagePullSecret` in namespace `ratelimitx` with read-only package access and reference it from the app pod spec; do not put credentials in these manifests or commit them.
- **`CrashLoopBackOff` for the app:** Check `kubectl logs -n ratelimitx deployment/ratelimitx --previous` and confirm Redis is ready and `REDIS_ADDR` is `redis:6379`. The app exits if it cannot connect to Redis during startup.
- **Failed app probes:** Inspect `kubectl describe pod -n ratelimitx <pod-name>` and its logs. The probes use the app's `/health` endpoint on port 8080.
- **Pending PVCs:** Check `kubectl get storageclass` and `kubectl describe pvc -n ratelimitx`. Configure a default Kind StorageClass before expecting Prometheus or Grafana storage to bind.
- **Ingress has no address or returns 404:** Confirm an ingress-nginx controller is installed and ready, the Ingress class is `nginx`, and the Service has endpoints: `kubectl get endpoints ratelimitx -n ratelimitx`.
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
