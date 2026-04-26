import { z } from "zod";

const BasicAuth = z.object({
  username: z.string().min(1),
  password: z.string().min(1),
});

const OCISource = z.object({
  type: z.literal("oci"),
  url: z.string().regex(/^oci:\/\//),
  version: z.string().min(1),
  auth: BasicAuth.optional(),
});

const HTTPSSource = z.object({
  type: z.literal("https"),
  repoUrl: z.string().url(),
  chart: z.string().min(1),
  version: z.string().min(1),
  auth: BasicAuth.optional(),
});

const GitSource = z.object({
  type: z.literal("git"),
  repoUrl: z.string().min(1),
  ref: z.string().min(1),
  path: z.string().optional(),
  auth: z
    .object({
      sshKeyPem: z.string().optional(),
      token: z.string().optional(),
    })
    .refine((v) => !!(v.sshKeyPem || v.token), { message: "git auth requires sshKeyPem or token" })
    .optional(),
});

const TgzSource = z.object({
  type: z.literal("tgz"),
  // base64-encoded tgz bytes
  tgzBase64: z.string().min(1),
});

export const ChartSourceSchema = z.discriminatedUnion("type", [
  OCISource,
  HTTPSSource,
  GitSource,
  TgzSource,
]);
export type ChartSource = z.infer<typeof ChartSourceSchema>;

const PrereqSecretSchema = z.object({
  namespace: z.string().min(1),
  name: z.string().min(1),
  type: z.string().optional(),
  stringData: z.record(z.string(), z.string()),
});

export const InstallSchema = z.object({
  release: z.string().min(1).regex(/^[a-z0-9-]+$/),
  namespace: z.string().min(1).regex(/^[a-z0-9-]+$/),
  source: ChartSourceSchema,
  values: z.record(z.string(), z.unknown()).optional(),
  atomic: z.boolean().default(true),
  wait: z.boolean().default(true),
  timeoutSeconds: z.number().int().positive().max(3600).default(600),
  createNamespace: z.boolean().default(true),
  prerequisites: z
    .object({
      secrets: z.array(PrereqSecretSchema).default([]),
    })
    .optional(),
});

export const UpgradeSchema = z.object({
  source: ChartSourceSchema,
  values: z.record(z.string(), z.unknown()).optional(),
  atomic: z.boolean().default(true),
  wait: z.boolean().default(true),
  timeoutSeconds: z.number().int().positive().max(3600).default(600),
  reuseValues: z.boolean().default(false),
  resetValues: z.boolean().default(false),
});

export const RollbackSchema = z.object({
  revision: z.number().int().nonnegative().default(0),
  wait: z.boolean().default(true),
  timeoutSeconds: z.number().int().positive().max(3600).default(300),
});

export const DisableResourceSchema = z.object({
  source: ChartSourceSchema,
  values: z.record(z.string(), z.unknown()).optional(),
  resourceNamespace: z.string().optional(),
  deletePvcs: z.boolean().default(true),
  timeoutSeconds: z.number().int().positive().max(3600).default(600),
});

export const PreflightSchema = z.object({
  release: z.string().min(1),
  namespace: z.string().min(1),
  source: ChartSourceSchema,
  values: z.record(z.string(), z.unknown()).optional(),
});

export type InstallReq = z.infer<typeof InstallSchema>;
export type UpgradeReq = z.infer<typeof UpgradeSchema>;
export type RollbackReq = z.infer<typeof RollbackSchema>;
export type DisableResourceReq = z.infer<typeof DisableResourceSchema>;
export type PreflightReq = z.infer<typeof PreflightSchema>;
