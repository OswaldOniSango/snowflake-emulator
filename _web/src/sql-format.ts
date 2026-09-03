/**
 * Breaks generated SQL into lines so it can be read.
 *
 * The translator emits a single line, which is fine for a database and
 * unreadable for a person. This is deliberately not a SQL formatter: it only
 * adds newlines, never changes spacing inside an expression, so the text stays
 * recognisably the SQL that runs.
 *
 * Only the generated column is formatted. What the user wrote keeps their own
 * line breaks.
 */

/** Clause keywords that start a new line. */
const CLAUSE_KEYWORDS = new Set([
  "FROM",
  "WHERE",
  "GROUP",
  "ORDER",
  "HAVING",
  "LIMIT",
  "OFFSET",
  "UNION",
  "INTERSECT",
  "EXCEPT",
  "JOIN",
  "LEFT",
  "RIGHT",
  "INNER",
  "OUTER",
  "FULL",
  "CROSS",
  "USING",
  "VALUES",
  "RETURNING",
]);

/** Words that already introduce a JOIN, so the JOIN itself must not break. */
const JOIN_MODIFIERS = new Set(["LEFT", "RIGHT", "INNER", "OUTER", "FULL", "CROSS"]);

interface Token {
  text: string;
  /** Parenthesis nesting, so nothing inside a function call is broken up. */
  depth: number;
  /** True for a word; false for a literal, a comment or punctuation. */
  isWord: boolean;
}

export function formatSQL(sql: string): string {
  const tokens = tokenize(sql);
  if (tokens.length === 0) {
    return sql;
  }

  let out = "";
  let previousWord = "";

  for (const token of tokens) {
    const upper = token.isWord ? token.text.toUpperCase() : "";

    if (token.depth === 0 && token.isWord && startsClause(upper, previousWord) && out !== "") {
      out = out.trimEnd() + "\n";
    }

    out += token.text;

    if (token.depth === 0 && token.text === ",") {
      out = out.trimEnd() + "\n";
    }

    if (token.isWord) {
      previousWord = upper;
    }
  }

  return collapseBlankLines(out).trim();
}

function startsClause(upper: string, previousWord: string): boolean {
  if (!CLAUSE_KEYWORDS.has(upper)) {
    return false;
  }
  // "LEFT JOIN" breaks once, at LEFT.
  if (upper === "JOIN" && JOIN_MODIFIERS.has(previousWord)) {
    return false;
  }
  // "GROUP BY" and "ORDER BY" break at the first word only.
  if (upper === "BY") {
    return false;
  }
  return true;
}

/**
 * Splits sql into words, literals, comments and punctuation, recording how
 * deeply nested each piece is. Literals and comments are kept whole so their
 * contents are never mistaken for keywords.
 */
function tokenize(sql: string): Token[] {
  const tokens: Token[] = [];
  let depth = 0;

  for (let i = 0; i < sql.length; ) {
    const char = sql[i] ?? "";

    if (char === "'" || char === '"') {
      const end = readQuoted(sql, i, char);
      tokens.push({ text: sql.slice(i, end), depth, isWord: false });
      i = end;
      continue;
    }

    if (sql.startsWith("--", i)) {
      const newline = sql.indexOf("\n", i);
      const end = newline < 0 ? sql.length : newline;
      tokens.push({ text: sql.slice(i, end), depth, isWord: false });
      i = end;
      continue;
    }

    if (sql.startsWith("/*", i)) {
      const close = sql.indexOf("*/", i + 2);
      const end = close < 0 ? sql.length : close + 2;
      tokens.push({ text: sql.slice(i, end), depth, isWord: false });
      i = end;
      continue;
    }

    if (isWordChar(char)) {
      let end = i;
      while (end < sql.length && isWordChar(sql[end] ?? "")) {
        end++;
      }
      tokens.push({ text: sql.slice(i, end), depth, isWord: true });
      i = end;
      continue;
    }

    if (char === "(") {
      depth++;
    } else if (char === ")") {
      depth = Math.max(0, depth - 1);
    }

    // A comma's depth is the level it separates at, not the level inside.
    tokens.push({ text: char, depth: char === "(" ? depth - 1 : depth, isWord: false });
    i++;
  }

  return tokens;
}

function readQuoted(sql: string, start: number, quote: string): number {
  for (let i = start + 1; i < sql.length; i++) {
    if (sql[i] !== quote) {
      continue;
    }
    if (sql[i + 1] === quote) {
      i++;
      continue;
    }
    return i + 1;
  }
  return sql.length;
}

function isWordChar(char: string): boolean {
  return /[A-Za-z0-9_$]/.test(char);
}

function collapseBlankLines(sql: string): string {
  // Generated SQL carries no indentation of its own, so trimming both ends is
  // safe and removes the space that followed a comma before the break.
  return sql
    .split("\n")
    .map((line) => line.trim())
    .filter((line, index, lines) => line !== "" || index === lines.length - 1)
    .join("\n");
}
