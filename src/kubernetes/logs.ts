import { kc } from "./client";
import * as k8s from "@kubernetes/client-node";
import stream from "stream";

export async function streamLogs(
  namespace: string,
  appName: string,
  outputStream: stream.Writable
): Promise<void> {
  const log = new k8s.Log(kc);
  
  // Find pods matching label app=appName
  const coreApi = kc.makeApiClient(k8s.CoreV1Api);
  const podsRes = await coreApi.listNamespacedPod({
    namespace,
    labelSelector: `app=${appName}`,
  });

  const pods = podsRes.items;
  if (!pods || pods.length === 0) {
    outputStream.write(`No pods found for ${appName}\n`);
    outputStream.end();
    return;
  }

  // Stream logs from the first matching pod
  const podName = pods[0]?.metadata?.name;
  if (!podName) return;

  try {
    await log.log(namespace, podName, "app", outputStream, { follow: true, tailLines: 50 });
  } catch (err) {
    outputStream.write(`Error fetching logs: ${err}\n`);
    outputStream.end();
  }
}
