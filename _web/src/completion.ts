import type { CompletionContext, CompletionResult, Completion } from "@codemirror/autocomplete";
import type { SchemaObject } from "./api";
import type { Catalog } from "./catalog";

/**
 * Completion for the SQL editor.
 *
 * Suggests the objects of the worksheet's namespace, plus the Snowflake
 * functions the emulator translates and the statements it understands. Column
 * names are deliberately absent: they need a DESCRIBE per table, which is a
 * decision of its own once object completion has been used in anger.
 */

/** How each kind of object is presented in the list. */
const KINDS: Record<string, { icon: string; label: string }> = {
  table: { icon: "class", label: "table" },
  stream: { icon: "class", label: "stream" },
  procedure: { icon: "function", label: "procedure" },
  task: { icon: "variable", label: "task" },
  stage: { icon: "variable", label: "stage" },
};

/**
 * Functions the translator rewrites on the way to DuckDB. Suggesting these
 * says which Snowflake spellings the emulator actually understands, which is
 * the question the console exists to answer.
 */
const FUNCTIONS = [
  "ARRAY_AGG", "DATEADD", "DATEDIFF", "DATE_TRUNC", "FLATTEN", "IFF", "IFNULL",
  "LISTAGG", "NVL", "NVL2", "OBJECT_CONSTRUCT", "PARSE_JSON", "TO_VARIANT",
  "TRY_CAST", "ZEROIFNULL",
];

const KEYWORDS = [
  "SELECT", "FROM", "WHERE", "GROUP BY", "ORDER BY", "HAVING", "LIMIT", "QUALIFY",
  "INSERT INTO", "UPDATE", "DELETE FROM", "MERGE INTO", "WHEN MATCHED", "WHEN NOT MATCHED",
  "CREATE TABLE", "CREATE OR REPLACE TABLE", "CREATE STREAM", "CREATE TASK",
  "CREATE PROCEDURE", "DROP TABLE", "ALTER TABLE", "COPY INTO", "CALL",
  "DESCRIBE TABLE", "SHOW STREAMS", "SHOW TASKS", "SHOW PROCEDURES",
];

/**
 * Builds the list offered for a partly typed word.
 *
 * Catalog objects are boosted above the fixed vocabulary: a table name is what
 * someone reaches for completion to spell, whereas they already know SELECT.
 * Matching is case-insensitive so a lowercase prefix still finds the uppercase
 * names the emulator stores.
 */
export function completionsFor(word: string, objects: SchemaObject[]): Completion[] {
  const prefix = word.toUpperCase();
  const starts = (candidate: string) => candidate.toUpperCase().startsWith(prefix);

  const fromCatalog = objects.filter((object) => starts(object.name)).map((object) => {
    const kind = KINDS[object.kind] ?? { icon: "variable", label: object.kind };
    return {
      label: object.name,
      type: kind.icon,
      detail: object.detail ? `${kind.label} · ${object.detail}` : kind.label,
      boost: 2,
    };
  });

  const functions = FUNCTIONS.filter(starts).map((name) => ({
    label: name,
    type: "function",
    detail: "translated for DuckDB",
    boost: 1,
  }));

  const keywords = KEYWORDS.filter(starts).map((name) => ({ label: name, type: "keyword" }));

  return [...fromCatalog, ...functions, ...keywords];
}

/** Adapts the list above to the shape CodeMirror's autocompletion expects. */
export function createCompletionSource(catalog: Catalog) {
  return (context: CompletionContext): CompletionResult | null => {
    const word = context.matchBefore(/[\w$]*/);
    if (!word) {
      return null;
    }
    // Completing an empty word unasked would open the list on every keystroke,
    // including after a space. Ctrl-Space still forces it.
    if (word.from === word.to && !context.explicit) {
      return null;
    }

    return {
      from: word.from,
      options: completionsFor(word.text, catalog.objects()),
      validFor: /^[\w$]*$/,
    };
  };
}
