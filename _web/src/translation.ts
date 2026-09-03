import type { Rewrite, Translation } from "./api";
import { formatSQL } from "./sql-format";

/** Names the processors report themselves by, in words a reader recognises. */
const HANDLER_LABELS: Record<string, string> = {
  translator: "Translator",
  copy_processor: "COPY processor",
  merge_processor: "MERGE processor",
  procedure_processor: "Procedure interpreter",
  stream_processor: "Stream processor",
  task_processor: "Task processor",
  transaction: "Transaction control",
};

/**
 * Renders the statement beside the SQL DuckDB receives.
 *
 * Two transformations show up here, and the second is the one that surprises
 * people: Snowflake functions become their DuckDB equivalents, and unqualified
 * table names are resolved to the emulator's physical DATABASE.SCHEMA_TABLE.
 */
export function renderTranslation(translation: Translation): HTMLElement {
  const pane = document.createElement("div");
  pane.className = "translation";

  if (!translation.complete) {
    pane.append(
      partialNotice(
        HANDLER_LABELS[translation.handledBy] ?? translation.handledBy,
        translation.note ?? "",
      ),
    );
  }

  const columns = document.createElement("div");
  columns.className = "xcol";
  columns.append(
    // What the user wrote keeps their own line breaks; only the generated side
    // is broken up, since the translator emits it as a single line.
    sqlColumn("Snowflake SQL", "what you wrote", translation.statement),
    sqlColumn("DuckDB SQL", "what actually runs", formatSQL(translation.translated)),
  );
  pane.append(columns);

  if (translation.rewrites.length > 0) {
    pane.append(rewriteTable(translation.rewrites));
  }

  if (translation.complete && translation.statement.trim() === translation.translated.trim()) {
    pane.append(
      note("Nothing to translate — this statement reaches DuckDB unchanged."),
    );
  }

  return pane;
}

/**
 * Lists the substitutions rather than leaving them to be spotted. Two blocks of
 * SQL side by side show that something changed; this says what.
 */
function rewriteTable(rewrites: Rewrite[]): HTMLElement {
  const wrapper = document.createElement("div");
  wrapper.className = "rewrites";

  const table = document.createElement("table");

  const head = document.createElement("thead");
  const headRow = document.createElement("tr");
  for (const label of ["What you wrote", "What runs", "Why"]) {
    const th = document.createElement("th");
    th.textContent = label;
    headRow.append(th);
  }
  head.append(headRow);

  const body = document.createElement("tbody");
  for (const rewrite of rewrites) {
    const row = document.createElement("tr");
    row.append(cell(rewrite.from, "from"), cell(rewrite.to, "to"), cell(reason(rewrite.kind), "why"));
    body.append(row);
  }

  table.append(head, body);
  wrapper.append(table);
  return wrapper;
}

function cell(text: string, className: string): HTMLElement {
  const td = document.createElement("td");
  td.className = className;
  td.textContent = text;
  return td;
}

function reason(kind: string): string {
  return kind === "object"
    ? "Resolved against the worksheet's database and schema"
    : "DuckDB has no such function";
}

function sqlColumn(title: string, subtitle: string, sql: string): HTMLElement {
  const column = document.createElement("div");

  const head = document.createElement("div");
  head.className = "xhead";
  head.append(strong(title), muted(subtitle));

  const body = document.createElement("pre");
  body.textContent = sql;

  column.append(head, body);
  return column;
}

function partialNotice(handler: string, detail: string): HTMLElement {
  const notice = document.createElement("div");
  notice.className = "notice warn";

  const bar = document.createElement("div");
  bar.className = "bar";

  const content = document.createElement("div");
  const heading = document.createElement("h4");
  heading.textContent = `Handled by the ${handler} — this preview is partial`;
  content.append(heading);

  if (detail) {
    const paragraph = document.createElement("p");
    paragraph.textContent = detail;
    content.append(paragraph);
  }

  notice.append(bar, content);
  return notice;
}

function note(text: string): HTMLElement {
  const element = document.createElement("p");
  element.className = "xnote";
  element.textContent = text;
  return element;
}

function strong(text: string): HTMLElement {
  const element = document.createElement("b");
  element.textContent = text;
  return element;
}

function muted(text: string): HTMLElement {
  const element = document.createElement("span");
  element.className = "muted";
  element.textContent = text;
  return element;
}
