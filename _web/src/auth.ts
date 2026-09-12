/** Browser-scoped authentication state for the local emulator. */

const STORAGE_KEY = "mallard.session";

export interface AuthSession {
  token: string;
  masterToken: string;
  username: string;
  role: string;
  database: string;
  schema: string;
  warehouse: string;
  expiresAt: number;
}

export interface LoginInput {
  username: string;
  password: string;
  database?: string;
  schema?: string;
  warehouse?: string;
  role?: string;
}

interface LoginResponse {
  success?: boolean;
  message?: string;
  data?: {
    token: string;
    masterToken: string;
    validityInSeconds: number;
    sessionInfo: {
      databaseName: string;
      schemaName: string;
      warehouseName: string;
      roleName: string;
    };
  };
}

let current = read();
const listeners = new Set<(session: AuthSession | null) => void>();

export function session(): AuthSession | null {
  return current;
}

export function subscribe(listener: (value: AuthSession | null) => void): () => void {
  listeners.add(listener);
  listener(current);
  return () => listeners.delete(listener);
}

export async function login(input: LoginInput, fetchFn: typeof fetch = fetch): Promise<AuthSession> {
  const response = await fetchFn("/session/v1/login-request", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      data: {
        LOGIN_NAME: input.username,
        PASSWORD: input.password,
        databaseName: input.database,
        schemaName: input.schema,
        warehouseName: input.warehouse,
        roleName: input.role,
        CLIENT_APP_ID: "mallard-console",
      },
    }),
  });
  const body = (await response.json().catch(() => ({}))) as LoginResponse;
  if (!response.ok || !body.success || !body.data?.token) {
    throw new Error(body.message ?? `Login failed with HTTP ${response.status}.`);
  }
  const info = body.data.sessionInfo;
  current = {
    token: body.data.token,
    masterToken: body.data.masterToken,
    username: input.username,
    role: info.roleName,
    database: info.databaseName,
    schema: info.schemaName,
    warehouse: info.warehouseName,
    expiresAt: Date.now() + body.data.validityInSeconds * 1000,
  };
  write(current);
  notify();
  return current;
}

export async function logout(fetchFn: typeof fetch = fetch): Promise<void> {
  if (current) {
    await fetchFn("/session/logout", {
      method: "POST",
      headers: authHeaders(),
      body: JSON.stringify({ token: current.token }),
    }).catch(() => undefined);
  }
  current = null;
  remove();
  notify();
}

/** Changes the active role without creating a second browser session. */
export async function useRole(role: string, fetchFn: typeof fetch = fetch): Promise<AuthSession> {
  if (!current) throw new Error("Sign in before changing roles.");
  const response = await fetchFn("/queries/v1/query-request", {
    method: "POST",
    headers: { ...authHeaders(), "Content-Type": "application/json" },
    body: JSON.stringify({ sqlText: `USE ROLE ${quoteIdentifier(role)}` }),
  });
  const body = (await response.json().catch(() => ({}))) as { success?: boolean; message?: string };
  if (!response.ok || !body.success) {
    throw new Error(body.message ?? `Role change failed with HTTP ${response.status}.`);
  }
  return rememberRole(role);
}

/** Keeps the browser context aligned after a successful worksheet USE ROLE. */
export function rememberRole(role: string): AuthSession {
  if (!current) throw new Error("Sign in before changing roles.");
  current = { ...current, role };
  write(current);
  notify();
  return current;
}

/** Selects the warehouse used by subsequent REST worksheet statements. */
export function useWarehouse(warehouse: string): AuthSession {
  if (!current) throw new Error("Sign in before selecting a warehouse.");
  current = { ...current, warehouse };
  write(current);
  notify();
  return current;
}

export function authHeaders(): Record<string, string> {
  return current ? { Authorization: `Bearer ${current.token}` } : {};
}

function quoteIdentifier(value: string): string {
  return `"${value.replaceAll('"', '""')}"`;
}

function notify(): void {
  for (const listener of listeners) listener(current);
}

function read(): AuthSession | null {
  try {
    const value = JSON.parse(sessionStorage.getItem(STORAGE_KEY) ?? "null") as Partial<AuthSession> | null;
    if (!value?.token || !value.username || (value.expiresAt && value.expiresAt <= Date.now())) return null;
    return value as AuthSession;
  } catch {
    return null;
  }
}

function write(value: AuthSession): void {
  try { sessionStorage.setItem(STORAGE_KEY, JSON.stringify(value)); } catch { /* private browsing */ }
}

function remove(): void {
  try { sessionStorage.removeItem(STORAGE_KEY); } catch { /* private browsing */ }
}
