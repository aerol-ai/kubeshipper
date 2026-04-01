# KubeShipper Detailed Use Cases (V2)

Below is the comprehensive breakdown of both the supported capabilities (catered) and explicitly excluded/pending capabilities (not catered) of the current KubeShipper API implementation following the V2 Orchestration rewrite. 

## 🟢 45 Use Cases Catered To

These are scenarios and patterns that the orchestration API abstraction is designed to handle flawlessly:

1. **Initial Stateless App Deployment**: Deploying a brand new web service from scratch easily without knowing K8s YAML.
2. **Rolling Updates**: Performing zero-downtime rolling updates of applications.
3. **Internal Microservices**: Deploying backend APIs that only other cluster services can talk to (`public: false`).
4. **Public Web Applications**: Exposing a containerized frontend directly to the internet via Ingress (`public: true`).
5. **Manual Replicas Scaling**: Scaling a service up seamlessly to handle anticipated traffic spikes using `PATCH`.
6. **Scale to Zero**: Scaling `replicas: 0` for staging environments during weekends to save cloud costs.
7. **Environment Variable Configuration**: Injecting required environment variables securely into the container runtime.
8. **Live Pod Log Streaming**: Debugging live traffic issues by streaming logs dynamically via `GET /services/:id/logs`.
9. **Service Status Monitoring**: Programmatically querying if a deployment rollout has reached a fully `ready` state.
10. **Environment Teardown**: Deleting temporary pull-request (PR) environments via a simple `DELETE /services/:id` call.
11. **CI/CD Integration**: Enabling GitHub Actions to trigger deployments by just submitting a generic JSON payload.
12. **One-Shot Jobs**: Executing a database migration that runs once and terminates (via future `type: "job"` support).
13. **Periodic Maintenance**: Triggering recurring cleanup tasks (via future `type: "cronjob"` support).
14. **Generic Ingress Abstraction**: Creating complex ingress routing rules using a simple boolean network flag.
15. **Container CPU/Memory Requests**: Guaranteeing minimum reserved compute power for mission-critical services.
16. **Noisy Neighbor Protection**: Enforcing hard CPU and Memory limits to stop memory leaks from crashing the node.
17. **Graceful Restarts**: Triggering a fresh rollout (`POST /services/restart`) of replicas when an app hangs without altering the image.
18. **Quick Rollbacks**: Reverting a broken release rapidly by re-submitting an older image hash via the `PATCH` endpoint.
19. **Idempotent Application**: Submitting the same JSON payload multiple times safely without generating duplicate K8s resources.
20. **Self-Service Previews**: Providing a low-friction interface for developers to test unmerged feature branches.
21. **Background Workers**: Running queue listeners requiring no network ports or Ingress footprints.
22. **Declarative Updates**: Leveraging K8s Server-Side Apply to only update changed fields without managing client-side `kubectl` diffs.
23. **Conflict Resolution**: Preventing standard 3-way merge deployment collisions that often plague pipeline automations.
24. **Port Mapping**: Automatically mapping the specified target port through the Deployment down to the ClusterIP Service.
25. **Unified Lifecycle Management**: Tying Deployments, Services, and Ingress to a single canonical "Service" ID.
26. **Standardized Labeling**: Automatically labeling resulting K8s deployments with standard `app: {name}` labels.
27. **Custom Domains**: Future-proofing the architecture to allow specific host routing in the API specification.
28. **Regex Constraint Validation**: Using strict `Zod` endpoints to block non-compliant DNS-1035 K8s strings upfront.
29. **Type Checking Pipeline**: Strictly guarding inputs from bad CI integrations.
30. **Fast Local Developer Experience**: Rapidly provisioning and mapping external services securely locally.
31. **Centralized Desired State**: Storing the single-source-of-truth configuration entirely in the API's memory layer.
32. **Automated Name Synchronization**: Propagating the service ID accurately across multiple isolated K8s entities.
33. **Simple Partial Updates**: Replacing just the environment variables without needing to specify the `image` string again.
34. **Abstracting Kubernetes Complexity**: Shielding feature engineers from understanding Service selector mappings.
35. **Multi-Environment Multi-Tenancy**: Deploying the same codebase differently against a `prod` vs `staging` KubeShipper URL.
36. **Auditable API Footprint**: Acting as a middleware gatekeeper that can theoretically store all deployment histories.
37. **Headless Execution**: Functioning reliably as a purely REST-driven engine suitable for UI dashboard wrappers.
38. **Zero CLI Dependency**: Executing deployments securely via official TypeScript native Kubernetes GRPC/REST drivers.
39. **In-Cluster Authentication**: Executing deployments implicitly using native ServiceAccount mounted pods without secret files.
40. **Predictable Teardown**: Guaranteeing that hitting `DELETE` wipes the `Deployment`, the `Service`, and the `Ingress` concurrently.
41. **Asynchronous Orchestration**: API queuing with background workers ensuring long K8s deployments do not block HTTP requests.
42. **Persistent Desired State**: Native `bun:sqlite` backed state tracking meaning unhandled deployment tasks survive API restarts effortlessly.
43. **Drift Reconciliation**: Actively monitoring and healing manually deleted Kubernetes components natively via the worker loop.
44. **Health-based Auto-Rollbacks**: Natively checking `ProgressDeadlineExceeded` statuses and reverting bad image payloads via SQLite historical tracking.
45. **Audit Eventing**: A persistent timeline (`/services/:id/events`) tracking exactly when and why deployments succeeded or failed dynamically.

---

## 🔴 40 Pending / Unhandled Use Cases

These are complex, niche, or specific capabilities that remain completely out of scope or pending future upgrades. KubeShipper explicitly avoids supporting these right now to maintain a clean abstraction:

1. **Multi-Container Pods**: Injecting sidecar containers (like Envoy for Service Meshes) inside the same pod.
2. **InitContainers**: Running prerequisite setup scripts or pulling external data before the main application boots.
3. **Custom Resource Definitions (CRDs)**: Expanding the Kubernetes API with specialized third-party operators.
4. **StatefulSets**: Managing stateful databases requiring persistent network identities (PostgreSQL, Kafka, Redis).
5. **Persistent Volume Claims (PVC)**: Mounting permanent block storage disks to preserve data across pod restarts.
6. **DaemonSets**: Guaranteeing a pod runs unconditionally across every single physical Node in a cluster (e.g., Datadog agent).
7. **Horizontal Pod Autoscaling (HPA)**: Dynamically increasing replica counts automatically based on CPU/Memory pressure thresholds.
8. **Vertical Pod Autoscaling (VPA)**: Dynamically resizing requested container limits without manual restarts.
9. **NetworkPolicies**: Writing micro-segmentation firewall rules describing which pods can talk to which other pods.
10. **Role-Based Access Control (RBAC)**: Generating specific Kubernetes Roles and RoleBindings for workload identities.
11. **PodDisruptionBudgets (PDB)**: Shielding services from aggressive cluster-wide administrative node drains.
12. **Custom ServiceAccounts**: Mapping specific AWS IAM/GCP workload identities implicitly per service instead of using the default.
13. **Advanced Ingress Rules**: Managing complex rewrites (`rewrite-target`), sticky sessions, or custom NGINX rate-limiting annotations.
14. **Custom Health Probes**: Explicitly defining granular `Liveness`, `Readiness`, and `Startup` probe TCP/HTTP execution checks.
15. **Node Affinity**: Mandating that certain workloads absolutely must (or must not) run on specific labeled Nodes.
16. **Tolerations & Taints**: Letting workloads run on specialized Hardware nodes (e.g. GPU machines) specifically flagged for isolation.
17. **Cert-Manager TLS Secrets**: Automatically issuing Let’s Encrypt SSL certificates or managing TLS SNI wildcard routing.
18. **ConfigMap File Volume Mounts**: Mounting large configuration text files explicitly into a standard container Linux file path.
19. **Secret File Volume Mounts**: Mounting raw Docker registry `.dockerconfigjson` pulling credentials.
20. **External Secret Vaulting**: Auto-injecting secrets from HashiCorp Vault or AWS Secrets Manager dynamically at runtime.
21. **Multi-Cluster Federation**: Deploying the identical traffic-split workload to multiple distinct K8s clusters geographically.
22. **Blue-Green Deployments**: Managing 100% split routing where testing is done on inactive clusters before shifting the router natively.
23. **Canary Releases**: Natively funneling 5% of web traffic to a new deployment version while observing metric failures.
24. **GitOps Sync Algorithms**: Continuously polling a Git repository directly within the cluster similar to ArgoCD or FluxCD.
25. **Helm Chart Templating**: Interpreting deeply nested loops, global variables, and `values.yaml` Helm ecosystem inputs.
26. **Kustomize Overlays**: Layering multi-tiered YAML patch modifications natively.
27. **Pod Security Contexts**: Modifying `RunAsUser`, dropping root capabilities statically, or elevating privilege escalation limits.
28. **Docker Image Building**: KubeShipper only deploys images; it cannot run Kaniko or Buildx workflows.
29. **Ephemeral Storage Quotas**: Constraining the local disk space limits of a Pod’s writeable temporary layer.
30. **Custom Metrics Server Integrations**: Binding deployment scales to arbitrary events like Prometheus Queue Depths.
31. **Advanced Kubernetes Jobs**: Implementing job parallelisms, strictly defining completions, or controlling active deadline seconds.
32. **Service Mesh Overlays**: Exerting control over Linkerd or Istio VirtualServices for internal mTLS.
33. **HostNetwork Allocations**: Attaching a container directly to the physical Node’s underlying IP and bypassing Kube-Proxy.
34. **Custom DNS Configurations**: Rewriting `/etc/resolv.conf` logic (`dnsPolicy`) specifically for complex routing loops.
35. **Admission Webhook Integrations**: Intercepting and mutating API payloads cryptographically before the K8s API server processes them.
36. **Dynamic Namespace Creation**: It currently hardcodes deployments to the `default` namespace; lacking a multi-namespace dynamic allocation engine.
37. **PriorityClasses Configuration**: Flagging specific pods as absolute high-priority, forcing the eviction of lower-priority pods during resource starvation.
38. **Arbitrary Exec Access**: Acting as a proxy proxy to execute bash commands `kubectl exec -it` natively into container shells.
39. **Non-HTTP Protocol Ingress**: Creating network rules for UDP, raw TCP streams, or gRPC endpoints through standard ingress controllers without NodePorts.
40. **Operator Framework Generation**: Interpreting Custom Resources (like `KafkaTopic.yaml`) and mapping them into native control loops.
