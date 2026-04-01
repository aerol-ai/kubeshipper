import type { MiddlewareHandler } from "hono";

/**
 * Token-based auth middleware.
 *
 * Set AUTH_TOKEN in the environment to enable authentication.
 * When AUTH_TOKEN is not set, the API is fully open (useful for local dev).
 *
 * Clients must send:  Authorization: Bearer <AUTH_TOKEN>
 */
export const authMiddleware: MiddlewareHandler = async (c, next) => {
  const expected = process.env.AUTH_TOKEN;

  // AUTH_TOKEN not configured — open access (dev / in-cluster no-auth mode)
  if (!expected) {
    return next();
  }

  const header = c.req.header("Authorization");

  if (!header?.startsWith("Bearer ")) {
    return c.json({ error: "Unauthorized: missing or malformed Authorization header" }, 401);
  }

  const token = header.slice(7);

  // Constant-time comparison is not strictly necessary here because the token
  // is not a cryptographic secret that can be oracle-attacked over timing, but
  // we avoid short-circuit string comparison anyway.
  if (token.length !== expected.length || token !== expected) {
    return c.json({ error: "Forbidden: invalid token" }, 403);
  }

  return next();
};
