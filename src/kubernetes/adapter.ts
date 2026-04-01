import * as k8s from "@kubernetes/client-node";
import { k8sApi, k8sCoreApi, k8sNetworkingApi } from "./client";
import type { ServiceSpec } from "../api/validation";

const NAMESPACE = "default";
const FIELD_MANAGER = "kubeshipper";

// initOverrides (second arg) passed to patch calls so the HTTP client actually sends the header
const SSA_INIT = { headers: { "Content-Type": "application/apply-patch+yaml" } };
const SMP_INIT = { headers: { "Content-Type": "application/strategic-merge-patch+json" } };

export async function deployService(spec: ServiceSpec): Promise<void> {
  // 1. Apply Deployment
  await applyDeployment(spec);

  // 2. Apply Service if port is provided
  if (spec.port) {
    await applyService(spec);
    
    // 3. Apply Ingress if public is true
    if (spec.public) {
      await applyIngress(spec);
    }
  }
}

export async function updateService(spec: ServiceSpec): Promise<void> {
  await deployService(spec);
}

export async function deleteService(id: string): Promise<void> {
  try {
    await k8sApi.deleteNamespacedDeployment({ name: id, namespace: NAMESPACE });
  } catch (e) { /* ignore 404 */ }
  
  try {
    await k8sCoreApi.deleteNamespacedService({ name: id, namespace: NAMESPACE });
  } catch (e) { /* ignore 404 */ }
  
  try {
    await k8sNetworkingApi.deleteNamespacedIngress({ name: id, namespace: NAMESPACE });
  } catch (e) { /* ignore 404 */ }
}

export async function restartService(id: string): Promise<void> {
  // Strategic merge patch safely creates the annotation whether or not it already exists
  const patch = {
    spec: {
      template: {
        metadata: {
          annotations: {
            "kubeshipper.io/restartedAt": new Date().toISOString(),
          },
        },
      },
    },
  };

  await (k8sApi.patchNamespacedDeployment as any)(
    { name: id, namespace: NAMESPACE, body: patch },
    SMP_INIT
  );
}

export async function getServiceStatus(id: string): Promise<any> {
  try {
    const deployment = await k8sApi.readNamespacedDeploymentStatus({ name: id, namespace: NAMESPACE });
    const status = deployment.status;
    const desired = status?.replicas ?? 0;
    const readyReplicas = status?.readyReplicas ?? 0;

    return {
      // Scale-to-zero (desired === 0) is intentionally ready; otherwise all replicas must be up
      ready: desired === 0 || (readyReplicas > 0 && readyReplicas === desired),
      readyReplicas,
      totalReplicas: desired,
      conditions: status?.conditions ?? [],
    };
  } catch (e) {
    return { ready: false, reason: "Deployment not found" };
  }
}

// ---- Internal Manifest Builders ----

async function applyDeployment(spec: ServiceSpec) {
  const envVars = spec.env
    ? Object.entries(spec.env).map(([name, value]) => ({ name, value: String(value) }))
    : undefined;

  const container: k8s.V1Container = {
    name: "app",
    image: spec.image,
    env: envVars,
    ports: spec.port ? [{ containerPort: spec.port }] : undefined,
    resources: spec.resources ? {
      requests: spec.resources.requests as { [key: string]: string },
      limits: spec.resources.limits as { [key: string]: string },
    } : undefined,
  };

  const deployment: k8s.V1Deployment = {
    apiVersion: "apps/v1",
    kind: "Deployment",
    metadata: {
      name: spec.name,
      namespace: NAMESPACE,
    },
    spec: {
      replicas: spec.replicas,
      selector: {
        matchLabels: { app: spec.name },
      },
      template: {
        metadata: {
          labels: { app: spec.name },
        },
        spec: {
          containers: [container],
          imagePullSecrets: spec.imagePullSecret
            ? [{ name: spec.imagePullSecret }]
            : undefined,
        },
      },
    },
  };

  await (k8sApi.patchNamespacedDeployment as any)(
    { name: spec.name, namespace: NAMESPACE, body: deployment, fieldManager: FIELD_MANAGER, force: true },
    SSA_INIT
  );
}

async function applyService(spec: ServiceSpec) {
  const service: k8s.V1Service = {
    apiVersion: "v1",
    kind: "Service",
    metadata: {
      name: spec.name,
      namespace: NAMESPACE,
    },
    spec: {
      selector: { app: spec.name },
      ports: [
        {
          port: spec.port as number,
          targetPort: spec.port as number,
          protocol: "TCP",
        },
      ],
      type: "ClusterIP",
    },
  };

  await (k8sCoreApi.patchNamespacedService as any)(
    { name: spec.name, namespace: NAMESPACE, body: service, fieldManager: FIELD_MANAGER, force: true },
    SSA_INIT
  );
}

async function applyIngress(spec: ServiceSpec) {
  const rule: k8s.V1IngressRule = {
    http: {
      paths: [
        {
          path: "/",
          pathType: "Prefix",
          backend: {
            service: {
              name: spec.name,
              port: { number: spec.port as number },
            },
          },
        },
      ],
    },
  };
  if (spec.hostname) rule.host = spec.hostname;

  const ingress: k8s.V1Ingress = {
    apiVersion: "networking.k8s.io/v1",
    kind: "Ingress",
    metadata: {
      name: spec.name,
      namespace: NAMESPACE,
    },
    spec: {
      rules: [rule],
    },
  };

  await (k8sNetworkingApi.patchNamespacedIngress as any)(
    { name: spec.name, namespace: NAMESPACE, body: ingress, fieldManager: FIELD_MANAGER, force: true },
    SSA_INIT
  );
}
