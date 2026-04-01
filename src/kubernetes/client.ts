import * as k8s from "@kubernetes/client-node";

export const kc = new k8s.KubeConfig();

// loadFromDefault() tries in-cluster service account first, then falls back to ~/.kube/config
kc.loadFromDefault();

export const k8sApi = kc.makeApiClient(k8s.AppsV1Api);
export const k8sCoreApi = kc.makeApiClient(k8s.CoreV1Api);
export const k8sNetworkingApi = kc.makeApiClient(k8s.NetworkingV1Api);
export const k8sBatchApi = kc.makeApiClient(k8s.BatchV1Api);
export const k8sCustomObjectsApi = kc.makeApiClient(k8s.CustomObjectsApi);
