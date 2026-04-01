import * as k8s from "@kubernetes/client-node";
import { k8sCustomObjectsApi } from "./client";

export async function serverSideApply(
  group: string,
  version: string,
  plural: string,
  namespace: string,
  name: string,
  manifest: any,
  fieldManager: string = "kubeshipper"
): Promise<any> {
  const options = {
    headers: {
      "Content-Type": "application/apply-patch+yaml",
    },
  };

  try {
    const result = await (k8sCustomObjectsApi.patchNamespacedCustomObject as any)({
      group: group === "core" ? "" : group,
      version,
      namespace,
      plural,
      name,
      body: manifest,
      fieldManager,
      options,
    });
    return result; // v1 API returns the object directly usually
  } catch (err: any) {
    throw err;
  }
}
