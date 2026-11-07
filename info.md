# Kubernetes, EKS, and AWS basics for RateLimitX

This note explains the main terms and objects visible in the RateLimitX EKS
cluster. “EKS” is likely what “ek8” meant: Amazon Elastic Kubernetes Service,
AWS's managed Kubernetes service.

## Kubernetes in brief

Kubernetes runs containerized workloads and continuously compares the live
cluster with the desired state declared in manifests. For example, a
Deployment asks Kubernetes to keep two RateLimitX pods available; if a pod
stops, Kubernetes can create a replacement.

### Kubernetes objects

Each entry gives the object's basic purpose and how it relates to this
deployment.

| Object | Two-line explanation |
|---|---|
| **Cluster** | A Kubernetes cluster is the control plane plus the worker capacity where workloads run. EKS operates the managed control plane for this application. |
| **Node** | A node is a worker machine that runs pods; EKS worker nodes are commonly EC2 instances. Kubernetes schedules pods onto nodes that have sufficient resources. |
| **Namespace** | A namespace groups and scopes Kubernetes resources within a cluster. RateLimitX application and monitoring resources use `ratelimitx`, while cluster services use namespaces such as `kube-system`. |
| **Pod** | A pod is Kubernetes's smallest schedulable unit, usually containing one application container. Pods can be replaced, so their names and IP addresses should not be treated as permanent. |
| **Deployment** | A Deployment declares the desired number and template of interchangeable pods. It manages application workloads such as `ratelimitx`, `redis`, Prometheus, and Grafana. |
| **ReplicaSet** | A ReplicaSet maintains the number of matching pods requested by a Deployment. Deployment-managed ReplicaSets also support rolling updates and rollbacks. |
| **DaemonSet** | A DaemonSet schedules a copy of a pod on each eligible node. EKS node services such as `aws-node` and node-level CSI agents commonly use this pattern. |
| **ControllerRevision** | A ControllerRevision stores versioned configuration history for certain Kubernetes controllers. It is rollout metadata, not a pod or a running service. |
| **Service** | A Service provides a stable virtual address and DNS name for a set of selected pods. RateLimitX uses Services so traffic can reach app replicas and Redis without tracking pod IPs. |
| **ClusterIP** | ClusterIP is the default Service type and exposes a stable endpoint only inside the cluster. The app and Redis Services use in-cluster communication. |
| **Ingress** | An Ingress is a set of HTTP routing rules, such as routing `ratelimitx.local` to the app Service. It requires a separate Ingress Controller to implement those rules. |
| **Ingress Controller** | An Ingress Controller watches Ingress rules and handles incoming traffic accordingly. This cluster uses NGINX; creating an Ingress object alone does not install or run the controller. |
| **ConfigMap** | A ConfigMap stores non-secret configuration separately from container images. Prometheus and Grafana use ConfigMaps for scrape settings, data sources, and dashboard provisioning. |
| **PersistentVolumeClaim (PVC)** | A PVC is a workload's request for persistent storage with a requested size and access mode. Prometheus and Grafana use PVCs so their data can survive pod replacement. |
| **PersistentVolume (PV)** | A PV is the cluster resource that provides storage requested by a PVC. In EKS, the EBS CSI driver can provision an EBS-backed PV for a claim. |
| **StorageClass** | A StorageClass describes how Kubernetes should provision a type of storage. The `gp3` class in this deployment requests AWS General Purpose SSD EBS volumes. |
| **CSI driver** | A Container Storage Interface driver connects Kubernetes storage operations to a storage provider. The EBS CSI controller and node pods let Kubernetes provision, attach, and mount EBS volumes. |
| **ServiceAccount** | A ServiceAccount gives a pod a Kubernetes identity for API access. Prometheus uses one to discover pods, with permissions granted by its Role and RoleBinding. |
| **Role** | A Role defines a set of Kubernetes API permissions within one namespace. The Prometheus pod-discovery Role allows reading pod information needed for scraping discovery. |
| **RoleBinding** | A RoleBinding grants a Role's permissions to a user, group, or ServiceAccount. The Prometheus binding associates the pod-discovery permissions with its ServiceAccount. |
| **EndpointSlice** | An EndpointSlice lists the current network endpoints behind a Service. An empty or missing ready endpoint can explain why an Ingress or client gets a 502 response. |
| **Readiness probe** | A readiness probe checks whether a container should receive Service traffic. If it fails, Kubernetes removes that pod from the Service's ready endpoints until it recovers. |
| **Liveness probe** | A liveness probe checks whether a container should be restarted. It is different from readiness: a live container may still not be ready to serve requests. |
| **Resource request and limit** | A request is the CPU or memory Kubernetes uses when scheduling a pod; a limit caps the container's use. The app manifest requests `50m` CPU and `64Mi` memory and sets higher limits. |
| **Kustomization** | Kustomize combines and optionally transforms a set of Kubernetes manifests. The `k8s/kustomization.yaml` file groups the app, Redis, ingress, and monitoring resources for deployment. |
| **Metrics Server** | Metrics Server gathers recent resource usage from Kubernetes nodes and pods. It supports commands such as `kubectl top` and can supply metrics for autoscaling, but it is not the Prometheus metrics store. |
| **CoreDNS** | CoreDNS provides DNS resolution inside the Kubernetes cluster. It lets pods locate Services by names such as `redis` instead of hard-coded IP addresses. |
| **kube-proxy** | `kube-proxy` helps implement Kubernetes Service networking on nodes. It routes traffic addressed to a Service toward the Service's available pod endpoints. |

## AWS and EKS objects

EKS runs Kubernetes in AWS and integrates it with AWS networking, identity,
compute, and storage. The following AWS resources and services explain the
infrastructure used or referenced by this deployment.

| AWS object or service | Two-line explanation |
|---|---|
| **Amazon EKS cluster** | EKS is AWS's managed Kubernetes control plane service. The `ratelimitx` cluster is in the `ap-south-1` AWS region. |
| **AWS Region** | A Region is a geographic area containing AWS infrastructure. The cluster and its regional resources are configured in `ap-south-1` (Mumbai). |
| **Amazon VPC** | A VPC is a logically isolated network in AWS that contains cluster networking resources. EKS worker nodes and pod networking need VPC subnets and routes to communicate. |
| **Subnet** | A subnet is an IP range within a VPC, associated with a specific Availability Zone. EKS uses subnets for its control-plane connectivity and worker-node network interfaces. |
| **EC2 instance** | Amazon EC2 provides virtual machines that can serve as EKS worker nodes. The Kubernetes scheduler places pods on available node capacity, not directly on the control plane. |
| **AWS VPC CNI (`aws-node`)** | The VPC Container Network Interface plugin connects Kubernetes pod networking to the VPC. Its `aws-node` pods run on worker nodes and manage pod network interfaces and addresses. |
| **Amazon EBS** | Elastic Block Store provides block storage volumes for EC2 and EKS workloads. Prometheus and Grafana PVCs can be backed by EBS so data persists beyond a pod's lifetime. |
| **EBS `gp3` volume** | `gp3` is a general-purpose SSD EBS volume type. The project's `gp3` StorageClass is used when provisioning persistent monitoring storage. |
| **EBS CSI add-on** | The EBS Container Storage Interface add-on integrates EBS volume lifecycle operations with Kubernetes. Its controller and node pods provision, attach, and mount volumes for PVCs. |
| **IAM role** | An AWS Identity and Access Management role grants a set of AWS permissions to an assumed identity. GitHub Actions assumes the deployment role to obtain temporary credentials and access EKS. |
| **OIDC identity provider** | OpenID Connect lets GitHub Actions present a signed workflow identity to AWS without storing long-lived AWS access keys. The IAM role trust policy must match the workflow's issuer, audience, and subject claims. |
| **EKS access entry / Kubernetes authorization** | EKS access configuration maps an AWS identity to permissions for a cluster. Kubernetes RBAC or an EKS access policy then determines which Kubernetes resources that identity may operate on. |
| **EKS Pod Identity Agent** | The Pod Identity Agent supports associating Kubernetes workloads with AWS IAM roles. It runs as cluster infrastructure; only workloads configured for Pod Identity use the resulting AWS permissions. |
| **Security group** | An AWS security group is a stateful network firewall attached to network interfaces or resources. Its inbound and outbound rules must allow the required node, control-plane, and application traffic. |
| **AWS Load Balancer** | An AWS load balancer distributes network or HTTP traffic to registered targets. This deployment's manifest specifies NGINX Ingress; do not assume it creates an AWS load balancer unless the cluster's controller and Service configuration do so. |

## How a request and shared state flow

1. A client sends HTTP traffic to the NGINX Ingress Controller.
2. The matching Ingress rule sends traffic to the RateLimitX Kubernetes Service.
3. The Service forwards traffic to one of the ready RateLimitX pods.
4. The app pod uses the Redis Service to read or update shared rate-limit state.
5. Prometheus collects metrics, and Grafana queries Prometheus to display dashboards.

Kubernetes and AWS have distinct responsibilities: Kubernetes manages desired
workload state and in-cluster discovery, while AWS provides the managed EKS
control plane, compute, networking, identity integrations, and persistent
volumes.
