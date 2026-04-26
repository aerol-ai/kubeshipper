// Minimal JSON → YAML stringifier for Helm values.
// We keep it intentionally simple: scalars, arrays, objects.
// Avoids pulling in js-yaml to keep deps lean — Helm parses our output via sigs.k8s.io/yaml.

export function valuesToYAML(values: Record<string, unknown> | undefined): string {
  if (!values || Object.keys(values).length === 0) return "";
  return stringify(values, 0);
}

function stringify(v: unknown, indent: number): string {
  if (v === null || v === undefined) return "null";
  if (typeof v === "string") return formatString(v);
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  if (Array.isArray(v)) {
    if (v.length === 0) return "[]";
    const pad = "  ".repeat(indent);
    return v
      .map((item) => `\n${pad}- ${stringify(item, indent + 1).replace(/^\n/, "")}`)
      .join("");
  }
  if (typeof v === "object") {
    const entries = Object.entries(v as Record<string, unknown>);
    if (entries.length === 0) return "{}";
    const pad = "  ".repeat(indent);
    return entries
      .map(([k, val]) => {
        const child = stringify(val, indent + 1);
        if (child.startsWith("\n")) return `\n${pad}${k}:${child}`;
        return `\n${pad}${k}: ${child}`;
      })
      .join("");
  }
  return String(v);
}

function formatString(s: string): string {
  // Quote when any of: contains ":", "#", leading/trailing whitespace, special YAML tokens.
  if (
    /[:#\n\t]/.test(s) ||
    /^\s|\s$/.test(s) ||
    /^(true|false|null|~|yes|no|on|off)$/i.test(s) ||
    /^[-+]?[0-9]/.test(s)
  ) {
    return JSON.stringify(s);
  }
  return s;
}
