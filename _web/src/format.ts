import { isDecimalValue, type Cell, type Column } from "./api";

/** How a cell should be rendered and aligned. */
export interface FormattedCell {
  text: string;
  /** NULL is styled apart from a literal "NULL" string. */
  isNull: boolean;
  /** Numbers align right so digits line up down a column. */
  isNumeric: boolean;
}

const NUMERIC_TYPES = new Set(["NUMBER", "FIXED", "REAL", "FLOAT", "DOUBLE"]);

/**
 * Renders one cell. The emulator leaks two DuckDB details that have to be
 * handled here rather than shown raw: DECIMAL values arrive as a struct, and
 * dates and timestamps arrive as ISO 8601 strings.
 */
export function formatCell(value: Cell, column: Column | undefined): FormattedCell {
  if (value === null || value === undefined) {
    return { text: "NULL", isNull: true, isNumeric: false };
  }

  if (isDecimalValue(value)) {
    // A DECIMAL is typed TEXT in rowType, so its own type says nothing; the
    // struct is the only signal that this column holds numbers.
    return { text: formatDecimal(value.Value, value.Scale), isNull: false, isNumeric: true };
  }

  if (typeof value === "number") {
    return { text: String(value), isNull: false, isNumeric: true };
  }

  if (typeof value === "boolean") {
    return { text: value ? "true" : "false", isNull: false, isNumeric: false };
  }

  const type = column?.type ?? "";
  if (type === "DATE" || type.startsWith("TIMESTAMP")) {
    return { text: formatTimestamp(value, type), isNull: false, isNumeric: false };
  }

  return { text: value, isNull: false, isNumeric: NUMERIC_TYPES.has(type) };
}

/** Scales an integer down by 10^scale without floating-point drift. */
export function formatDecimal(value: number, scale: number): string {
  if (scale <= 0) {
    return String(value);
  }
  const negative = value < 0;
  const digits = String(Math.abs(value)).padStart(scale + 1, "0");
  const whole = digits.slice(0, digits.length - scale);
  const fraction = digits.slice(digits.length - scale);
  return `${negative ? "-" : ""}${whole}.${fraction}`;
}

/**
 * Trims an ISO 8601 timestamp to what the column actually carries. The
 * emulator returns a full timestamp even for DATE columns.
 */
export function formatTimestamp(value: string, type: string): string {
  const match = /^(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2}:\d{2})(\.\d+)?Z?$/.exec(value);
  if (!match) {
    return value;
  }
  const [, date, time, fraction] = match;
  if (type === "DATE") {
    return date ?? value;
  }
  return `${date} ${time}${(fraction ?? "").slice(0, 4)}`;
}

/** Right-aligns a column when every value in it reads as a number. */
export function isNumericColumn(rows: Cell[][], index: number, column: Column | undefined): boolean {
  if (NUMERIC_TYPES.has(column?.type ?? "")) {
    return true;
  }
  const values = rows.map((row) => row[index]).filter((value) => value !== null);
  return values.length > 0 && values.every((value) => isDecimalValue(value) || typeof value === "number");
}
