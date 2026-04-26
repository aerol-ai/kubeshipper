// gRPC client for the helmd Go sidecar over Unix domain socket.
// We use the grpc-js dynamic loader rather than codegen to keep build simple
// — the proto file at helmd/proto/helmd.proto is the canonical contract.

import * as path from "node:path";
import { promisify } from "node:util";

// Lazy-loaded so the package isn't required for code paths that don't touch helmd.
type GrpcClient = any;

let cached: GrpcClient | null = null;

const SOCKET_PATH = process.env.HELMD_SOCKET ?? "/tmp/helmd.sock";
const PROTO_PATH = process.env.HELMD_PROTO ?? path.resolve("helmd/proto/helmd.proto");

async function getClient(): Promise<GrpcClient> {
  if (cached) return cached;

  // Dynamic require so missing grpc-js at dev time only breaks /charts paths.
  // Add `@grpc/grpc-js` and `@grpc/proto-loader` to package.json before using.
  const grpc = await import("@grpc/grpc-js");
  const protoLoader = await import("@grpc/proto-loader");

  const def = await protoLoader.load(PROTO_PATH, {
    keepCase: false,
    longs: String,
    enums: String,
    defaults: true,
    oneofs: true,
  });
  const pkg = grpc.loadPackageDefinition(def) as any;
  const Service = pkg.helmd.v1.Helmd;

  const target = `unix://${SOCKET_PATH}`;
  cached = new Service(target, grpc.credentials.createInsecure());
  return cached;
}

// Convert TS source schema → proto source object.
export function toProtoSource(s: any): any {
  switch (s.type) {
    case "oci":
      return { oci: { url: s.url, version: s.version, auth: s.auth ?? undefined } };
    case "https":
      return {
        https: { repoUrl: s.repoUrl, chart: s.chart, version: s.version, auth: s.auth ?? undefined },
      };
    case "git":
      return {
        git: {
          repoUrl: s.repoUrl,
          ref: s.ref,
          path: s.path ?? "",
          auth: s.auth ?? undefined,
        },
      };
    case "tgz":
      return { tgz: { tgzBytes: Buffer.from(s.tgzBase64, "base64") } };
    default:
      throw new Error(`unsupported source type: ${s.type}`);
  }
}

export async function helmList(namespace = "", all = false) {
  const c = await getClient();
  return promisify(c.list.bind(c))({ namespace, all });
}

export async function helmGet(release: string, namespace: string) {
  const c = await getClient();
  return promisify(c.get.bind(c))({ release, namespace });
}

export async function helmHistory(release: string, namespace: string, max = 20) {
  const c = await getClient();
  return promisify(c.history.bind(c))({ release, namespace, max });
}

export async function helmUninstall(release: string, namespace: string, deletePvcs: boolean) {
  const c = await getClient();
  return promisify(c.uninstall.bind(c))({ release, namespace, deletePvcs, force: true });
}

export async function helmRollback(release: string, namespace: string, revision: number) {
  const c = await getClient();
  return promisify(c.rollback.bind(c))({ release, namespace, revision });
}

export async function helmRender(req: { release: string; namespace: string; source: any; valuesYaml: string }) {
  const c = await getClient();
  return promisify(c.render.bind(c))({ ...req, source: toProtoSource(req.source) });
}

export async function helmPreflight(req: { release: string; namespace: string; source: any; valuesYaml: string }) {
  const c = await getClient();
  return promisify(c.preflight.bind(c))({ ...req, source: toProtoSource(req.source) });
}

export async function helmDiff(release: string, namespace: string) {
  const c = await getClient();
  return promisify(c.diff.bind(c))({ release, namespace });
}

// Streaming helpers — return an async iterable of events.
export async function* helmInstallStream(req: any): AsyncGenerator<any> {
  const c = await getClient();
  const call = c.install({ ...req, source: toProtoSource(req.source) });
  for await (const ev of call) yield ev;
}

export async function* helmUpgradeStream(req: any): AsyncGenerator<any> {
  const c = await getClient();
  const call = c.upgrade({ ...req, source: toProtoSource(req.source) });
  for await (const ev of call) yield ev;
}

export async function* helmDisableStream(req: any): AsyncGenerator<any> {
  const c = await getClient();
  const call = c.disableResource({ ...req, source: toProtoSource(req.source) });
  for await (const ev of call) yield ev;
}

export async function* helmEnableStream(req: any): AsyncGenerator<any> {
  const c = await getClient();
  const call = c.enableResource({ ...req, source: toProtoSource(req.source) });
  for await (const ev of call) yield ev;
}
