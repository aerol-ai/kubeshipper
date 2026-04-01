import { z } from "zod";

export const ResourcesSchema = z.object({
  requests: z.object({
    cpu: z.string().optional(),
    memory: z.string().optional(),
  }).optional(),
  limits: z.object({
    cpu: z.string().optional(),
    memory: z.string().optional(),
  }).optional(),
});

export const ServiceSpecSchema = z.object({
  name: z.string().min(1).max(63).regex(/^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/, "Must be a valid DNS-1035 label"),
  image: z.string().min(1),
  port: z.number().int().min(1).max(65535).optional(),
  env: z.record(z.string(), z.string()).optional(),
  replicas: z.number().int().min(0).default(1),
  public: z.boolean().default(false),
  resources: ResourcesSchema.optional(),
  type: z.enum(["service", "job", "cronjob"]).default("service"),
  schedule: z.string().optional(), // Used if type is cronjob
});

export const PartialServiceSpecSchema = ServiceSpecSchema.partial();

export type ServiceSpec = z.infer<typeof ServiceSpecSchema>;
export type PartialServiceSpec = z.infer<typeof PartialServiceSpecSchema>;
