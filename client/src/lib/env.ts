export const VALID_ENV_KEY = /^[A-Za-z_][A-Za-z0-9_]*$/;

export interface EnvRow {
  id: string,
  key: string,
  value: string
}

let nextId = 0;

export function newEnvRow(): EnvRow {
  return { id: String(nextId++), key: "", value: ""};
}

export interface EnvValidation {
  message: string | null;
  invalidIds: Set<string>;
}

export function validateEnvRows(rows: EnvRow[]): EnvValidation {
  const invalidIds = new Set<string>();
  const firstIdByKey = new Map<string, string>();
  let message: string | null = null;

  for (const row of rows) {
    const key = row.key.trim();
    let error: string | null = null;

    if (key === "" && row.value === "") continue;
    if (key === "") {
      error = "Add a key for every value.";
    } else if (!VALID_ENV_KEY.test(key)) {
      error = `Invalid key "${key}". Key can contain letters, digits, and underscores, and should begin with a letter or underscore.`;
    } else if (key === "PORT") {
      error = "Set the port with the port field, not as an env var.";
    } else if (firstIdByKey.has(key)) {
      error = `Duplicate key "${key}".`;
      invalidIds.add(firstIdByKey.get(key)!);
    } else {
      firstIdByKey.set(key, row.id);
    }

    if (error) {
      invalidIds.add(row.id);
      message ??= error;
    }
  }

  return { message, invalidIds };
}

export function toEnvObject(rows: EnvRow[]): Record<string, string> | undefined {
  const env: Record<string, string> = {};
  for (const row of rows) {
    const key = row.key.trim();
    if (key) env[key] = row.value;
  }
  return Object.keys(env).length > 0 ? env : undefined;
}
