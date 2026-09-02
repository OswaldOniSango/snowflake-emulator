// Typed client for the emulator's REST API v2.
//
// Shapes here describe what the emulator actually returns, which is narrower
// than server/types/rest_api_v2.go declares: rowType carries only name, type
// and nullable — never precision, scale, length or byteLength.

/** SQLSTATE reported by a statement that succeeded. */
const SQL_STATE_SUCCESS = "00000";

/** A column in a result set. */
export interface Column {
  name: string;
  type: string;
  nullable: boolean;
}

/**
 * A cell value. DECIMAL columns arrive as a DuckDB struct rather than a
 * number, and are typed TEXT in rowType, so the raw shape has to survive as
 * far as the formatter.
 */
export type Cell = string | number | boolean | null | DecimalValue;

/** DuckDB's DECIMAL representation: Value scaled down by 10^Scale. */
export interface DecimalValue {
  Width: number;
  Scale: number;
  Value: number;
}

export function isDecimalValue(value: unknown): value is DecimalValue {
  return (
    typeof value === "object" &&
    value !== null &&
    "Value" in value &&
    "Scale" in value &&
    typeof (value as DecimalValue).Value === "number" &&
    typeof (value as DecimalValue).Scale === "number"
  );
}

export interface Statement {
  columns: Column[];
  rows: Cell[][];
  handle: string;
  /** Milliseconds measured client-side; the API reports no duration. */
  elapsedMs: number;
  /**
   * Set for DDL and DML, which the emulator answers with a single column
   * named "number of rows affected" rather than a result set.
   */
  rowsAffected: number | null;
}

/** A statement the emulator rejected. Carries no result set. */
export class StatementError extends Error {
  constructor(
    readonly code: string,
    readonly sqlState: string,
    message: string,
    readonly handle: string,
  ) {
    super(message);
    this.name = "StatementError";
  }
}

interface RawResponse {
  resultSetMetaData?: { numRows: number; format: string; rowType: Column[] };
  data?: Cell[][];
  code?: string;
  sqlState?: string;
  message?: string;
  statementHandle?: string;
}

const ROWS_AFFECTED_COLUMN = "number of rows affected";

/**
 * Submits a statement. The emulator answers HTTP 200 even for a failure and
 * reports it in the body, so the status alone says nothing about whether the
 * SQL ran — sqlState is what decides.
 */
export async function runStatement(
  statement: string,
  context: { database: string; schema: string },
  fetchFn: typeof fetch = fetch,
): Promise<Statement> {
  const startedAt = performance.now();

  const response = await fetchFn("/api/v2/statements", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ statement, ...context }),
  });

  const body = (await response.json()) as RawResponse;
  const elapsedMs = Math.round(performance.now() - startedAt);
  const handle = body.statementHandle ?? "";

  if (body.sqlState && body.sqlState !== SQL_STATE_SUCCESS) {
    throw new StatementError(
      body.code ?? "unknown",
      body.sqlState,
      body.message ?? "Statement failed with no message.",
      handle,
    );
  }

  if (!response.ok) {
    throw new StatementError(
      body.code ?? String(response.status),
      body.sqlState ?? "",
      body.message ?? `Request failed with HTTP ${response.status}.`,
      handle,
    );
  }

  const columns = body.resultSetMetaData?.rowType ?? [];
  const rows = body.data ?? [];

  return {
    columns,
    rows,
    handle,
    elapsedMs,
    rowsAffected: readRowsAffected(columns, rows),
  };
}

/**
 * DDL and DML come back as a one-column, one-row result set holding the count.
 * Reporting that as a grid would be noise, so it is lifted out here.
 */
function readRowsAffected(columns: Column[], rows: Cell[][]): number | null {
  if (columns.length !== 1 || columns[0]?.name !== ROWS_AFFECTED_COLUMN) {
    return null;
  }
  const value = rows[0]?.[0];
  return typeof value === "number" ? value : null;
}

/** What a statement becomes on its way to DuckDB. */
export interface Translation {
  statement: string;
  translated: string;
  /** The component that executes it: "translator", "copy_processor", … */
  handledBy: string;
  /** False when a processor builds the final SQL elsewhere. */
  complete: boolean;
  note?: string;
}

/**
 * Asks what a statement translates to without running it. This is the view no
 * Snowflake console can offer, because nothing is being translated there.
 */
export async function translateStatement(
  statement: string,
  context: { database: string; schema: string },
  fetchFn: typeof fetch = fetch,
): Promise<Translation> {
  const response = await fetchFn("/api/v2/translate", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ statement, ...context }),
  });

  const body = (await response.json()) as Partial<Translation> & { message?: string };

  if (!response.ok) {
    throw new StatementError(
      "translate",
      "",
      body.message ?? `Translation failed with HTTP ${response.status}.`,
      "",
    );
  }

  return {
    statement: body.statement ?? statement,
    translated: body.translated ?? "",
    handledBy: body.handledBy ?? "translator",
    complete: body.complete ?? true,
    ...(body.note ? { note: body.note } : {}),
  };
}

/** A database in the catalog. */
export interface Database {
  name: string;
}

/** A schema inside a database. */
export interface Schema {
  name: string;
}

/** One entry in a schema's contents. */
export interface SchemaObject {
  name: string;
  /** "table", "stream", "procedure", "task" or "stage". */
  kind: string;
  detail?: string;
}

async function getJSON<T>(path: string, fetchFn: typeof fetch): Promise<T> {
  const response = await fetchFn(path);
  if (!response.ok) {
    const body = (await response.json().catch(() => ({}))) as { message?: string };
    throw new StatementError(
      "catalog",
      "",
      body.message ?? `Request to ${path} failed with HTTP ${response.status}.`,
      "",
    );
  }
  return (await response.json()) as T;
}

export function listDatabases(fetchFn: typeof fetch = fetch): Promise<Database[]> {
  return getJSON<Database[]>("/api/v2/databases", fetchFn);
}

export function listSchemas(database: string, fetchFn: typeof fetch = fetch): Promise<Schema[]> {
  return getJSON<Schema[]>(`/api/v2/databases/${encodeURIComponent(database)}/schemas`, fetchFn);
}

/**
 * Lists everything a schema contains. Tables come from DuckDB rather than the
 * catalog, so tables created with SQL are included.
 */
export async function listSchemaObjects(
  database: string,
  schema: string,
  fetchFn: typeof fetch = fetch,
): Promise<SchemaObject[]> {
  const body = await getJSON<{ objects?: SchemaObject[] }>(
    `/api/v2/databases/${encodeURIComponent(database)}/schemas/${encodeURIComponent(schema)}/objects`,
    fetchFn,
  );
  return body.objects ?? [];
}
