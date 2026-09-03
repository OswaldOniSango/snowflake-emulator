import type { Cell, Column, Statement } from "./api";
import { formatCell } from "./format";

/**
 * Saving a result set to a file.
 *
 * Values go out as the grid shows them — decimals unpacked from the struct the
 * emulator returns, timestamps trimmed to what the column carries — so that
 * what is exported is what was read on screen. The one departure is NULL,
 * which the grid spells out and both formats leave empty or null, because a
 * literal "NULL" in a CSV is indistinguishable from the string.
 */

/** Escapes one field per RFC 4180. */
function csvField(text: string): string {
  // Quote when the value carries a delimiter, a quote or a newline. A leading
  // separator character is also quoted, so a spreadsheet cannot read the field
  // as a formula.
  if (/[",\r\n]/.test(text) || /^[=+\-@]/.test(text)) {
    return `"${text.replace(/"/g, '""')}"`;
  }
  return text;
}

/** The result set as CSV, with a header row of column names. */
export function toCSV(statement: Statement): string {
  const header = statement.columns.map((column) => csvField(column.name));
  const rows = statement.rows.map((row) =>
    statement.columns.map((column, index) => {
      const formatted = formatCell(row[index] ?? null, column);
      return formatted.isNull ? "" : csvField(formatted.text);
    }),
  );
  // CRLF is what RFC 4180 specifies, and what Excel expects.
  return [header, ...rows].map((fields) => fields.join(",")).join("\r\n");
}

/**
 * Column names for JSON keys, with duplicates disambiguated.
 *
 * SELECT 1 AS x, 2 AS x is legal and would otherwise lose a column silently
 * when the second key overwrote the first.
 */
export function jsonKeys(columns: Column[]): string[] {
  const seen = new Map<string, number>();
  return columns.map((column) => {
    const count = (seen.get(column.name) ?? 0) + 1;
    seen.set(column.name, count);
    return count === 1 ? column.name : `${column.name}_${count}`;
  });
}

/** One cell as JSON, keeping the types JSON can carry losslessly. */
function jsonValue(value: Cell, column: Column | undefined): unknown {
  if (value === null || value === undefined) {
    return null;
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return value;
  }
  // Everything else leaves as text. A DECIMAL in particular can hold more
  // digits than a double, so turning it back into a JSON number would lose
  // precision that the emulator took care to keep.
  return formatCell(value, column).text;
}

/** The result set as an array of objects, one per row. */
export function toJSON(statement: Statement): string {
  const keys = jsonKeys(statement.columns);
  const rows = statement.rows.map((row) => {
    const object: Record<string, unknown> = {};
    keys.forEach((key, index) => {
      object[key] = jsonValue(row[index] ?? null, statement.columns[index]);
    });
    return object;
  });
  return JSON.stringify(rows, null, 2);
}

/** A filename stamped to the second, so repeated exports do not collide. */
export function exportFilename(extension: string, now = new Date()): string {
  const pad = (value: number) => String(value).padStart(2, "0");
  const stamp =
    `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}` +
    `-${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`;
  return `results-${stamp}.${extension}`;
}

/** Hands the browser a file to save. */
export function download(filename: string, mime: string, content: string): void {
  const url = URL.createObjectURL(new Blob([content], { type: `${mime};charset=utf-8` }));
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  // Revoking immediately can cut the save short in some browsers; a turn of
  // the event loop is enough for the click to have been handled.
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

/**
 * The export controls for the results dock.
 *
 * `current` is read at click time rather than captured, so the buttons keep
 * exporting whatever is on screen without being rebuilt after every run.
 */
export function createExportControls(current: () => Statement | null): HTMLElement {
  const bar = document.createElement("div");
  bar.className = "dock-actions";

  const button = (label: string, extension: string, mime: string, render: (s: Statement) => string) => {
    const element = document.createElement("button");
    element.className = "ghost";
    element.type = "button";
    element.textContent = label;
    element.addEventListener("click", () => {
      const statement = current();
      if (statement) {
        download(exportFilename(extension), mime, render(statement));
      }
    });
    return element;
  };

  bar.append(
    button("Export CSV", "csv", "text/csv", toCSV),
    button("Export JSON", "json", "application/json", toJSON),
  );
  return bar;
}
