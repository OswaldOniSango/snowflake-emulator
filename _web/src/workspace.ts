/**
 * Where worksheets live between visits.
 *
 * Worksheets are per-browser: the emulator has no notion of a user, and a
 * scratch buffer is not something it should be asked to remember. Storage can
 * be missing or refuse to answer — a private window, blocked site data — so
 * every access is guarded and the console works without it, just without
 * remembering anything.
 */

const STORAGE_KEY = "mallard.workspace";

/** Bumped when the stored shape changes; older versions are discarded. */
const SCHEMA_VERSION = 1;

export interface ExecutionContext {
  database: string;
  schema: string;
}

export interface Worksheet {
  id: string;
  name: string;
  sql: string;
  context: ExecutionContext;
}

export interface Workspace {
  worksheets: Worksheet[];
  activeId: string;
}

interface StoredWorkspace {
  version: number;
  worksheets: Worksheet[];
  activeId: string;
}

export const DEFAULT_CONTEXT: ExecutionContext = { database: "TEST_DB", schema: "PUBLIC" };

const WELCOME_SQL = `SELECT
    IFF(1 > 0, 'yes', 'no')          AS iff_translates,
    NVL(NULL, 'fallback')            AS nvl_translates,
    DATEADD(day, 30, CURRENT_DATE)   AS dateadd_translates;`;

export function newWorksheet(name: string, context = DEFAULT_CONTEXT, sql = ""): Worksheet {
  return { id: newId(), name, sql, context: { ...context } };
}

export function defaultWorkspace(): Workspace {
  const worksheet = newWorksheet("Worksheet 1", DEFAULT_CONTEXT, WELCOME_SQL);
  return { worksheets: [worksheet], activeId: worksheet.id };
}

/**
 * Reads the stored workspace, falling back to a fresh one whenever what comes
 * back cannot be trusted: absent, unreadable, the wrong version, or corrupt.
 */
export function loadWorkspace(storage: Storage | null = safeStorage()): Workspace {
  if (!storage) {
    return defaultWorkspace();
  }

  let raw: string | null;
  try {
    raw = storage.getItem(STORAGE_KEY);
  } catch {
    return defaultWorkspace();
  }
  if (!raw) {
    return defaultWorkspace();
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return defaultWorkspace();
  }

  const stored = parsed as Partial<StoredWorkspace>;
  if (stored.version !== SCHEMA_VERSION || !Array.isArray(stored.worksheets)) {
    return defaultWorkspace();
  }

  const worksheets = stored.worksheets.filter(isWorksheet).map(normalise);
  if (worksheets.length === 0) {
    return defaultWorkspace();
  }

  const activeId = worksheets.some((w) => w.id === stored.activeId)
    ? (stored.activeId as string)
    : (worksheets[0] as Worksheet).id;

  return { worksheets, activeId };
}

export function saveWorkspace(
  workspace: Workspace,
  storage: Storage | null = safeStorage(),
): void {
  if (!storage) {
    return;
  }
  const stored: StoredWorkspace = { version: SCHEMA_VERSION, ...workspace };
  try {
    storage.setItem(STORAGE_KEY, JSON.stringify(stored));
  } catch {
    // Full or refused: the console keeps working, it just forgets.
  }
}

/** A name that does not collide with the ones already open. */
export function nextWorksheetName(existing: Worksheet[]): string {
  for (let n = existing.length + 1; ; n++) {
    const name = `Worksheet ${n}`;
    if (!existing.some((worksheet) => worksheet.name === name)) {
      return name;
    }
  }
}

function isWorksheet(value: unknown): value is Worksheet {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  const candidate = value as Partial<Worksheet>;
  return typeof candidate.id === "string" && candidate.id !== "" && typeof candidate.name === "string";
}

/** Fills in anything a stored worksheet is missing. */
function normalise(worksheet: Worksheet): Worksheet {
  return {
    id: worksheet.id,
    name: worksheet.name,
    sql: typeof worksheet.sql === "string" ? worksheet.sql : "",
    context: {
      database: worksheet.context?.database || DEFAULT_CONTEXT.database,
      schema: worksheet.context?.schema || DEFAULT_CONTEXT.schema,
    },
  };
}

/** Accessing localStorage throws outright in some contexts, not just returns null. */
function safeStorage(): Storage | null {
  try {
    const storage = window.localStorage;
    const probe = "mallard.probe";
    storage.setItem(probe, probe);
    storage.removeItem(probe);
    return storage;
  } catch {
    return null;
  }
}

function newId(): string {
  return `w${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}`;
}
