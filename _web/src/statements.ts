/**
 * Splits a worksheet buffer into the statements the emulator can run.
 *
 * The REST API takes one statement per request, so the console has to divide
 * the buffer itself. Splitting on ";" is not enough: a procedure body between
 * $$ … $$ is full of semicolons, and so are string literals and comments. This
 * scans with a small state machine instead, and keeps each statement's offsets
 * so the caller can tell which one the cursor is in.
 */

export interface Statement {
  /** The statement, trimmed, without its trailing semicolon. */
  text: string;
  /** Offsets into the original buffer, for locating the cursor. */
  start: number;
  end: number;
}

export function splitStatements(buffer: string): Statement[] {
  const statements: Statement[] = [];
  let start = 0;

  for (let i = 0; i < buffer.length; ) {
    const skipped = skipNonCode(buffer, i);
    if (skipped > i) {
      i = skipped;
      continue;
    }

    if (buffer[i] === ";") {
      push(statements, buffer, start, i);
      i++;
      start = i;
      continue;
    }

    i++;
  }

  push(statements, buffer, start, buffer.length);
  return statements;
}

/**
 * Returns the statement containing the given offset, or the one before it when
 * the cursor sits in the whitespace after a semicolon — which is where it ends
 * up right after typing one.
 */
export function statementAt(buffer: string, offset: number): Statement | null {
  const statements = splitStatements(buffer);
  if (statements.length === 0) {
    return null;
  }

  for (const statement of statements) {
    if (offset >= statement.start && offset <= statement.end) {
      return statement;
    }
  }

  // Past the end of the last statement.
  let previous: Statement | null = null;
  for (const statement of statements) {
    if (statement.end <= offset) {
      previous = statement;
    }
  }
  return previous ?? statements[0] ?? null;
}

function push(statements: Statement[], buffer: string, from: number, to: number): void {
  const raw = buffer.slice(from, to);
  const text = raw.trim();
  if (text === "") {
    return;
  }

  // Narrow the recorded span to the text itself, so trailing whitespace after
  // a semicolon belongs to no statement.
  const leading = raw.length - raw.trimStart().length;
  statements.push({ text, start: from + leading, end: from + leading + text.length });
}

/**
 * Consumes a region where a semicolon does not end a statement, returning the
 * index just past it. Returns i unchanged when there is nothing to skip.
 */
function skipNonCode(buffer: string, i: number): number {
  const char = buffer[i];

  if (char === "'" || char === '"') {
    return skipQuoted(buffer, i, char);
  }

  if (buffer.startsWith("$$", i)) {
    const close = buffer.indexOf("$$", i + 2);
    return close < 0 ? buffer.length : close + 2;
  }

  if (buffer.startsWith("--", i)) {
    const newline = buffer.indexOf("\n", i);
    return newline < 0 ? buffer.length : newline + 1;
  }

  if (buffer.startsWith("/*", i)) {
    return skipBlockComment(buffer, i);
  }

  return i;
}

function skipQuoted(buffer: string, start: number, quote: string): number {
  for (let i = start + 1; i < buffer.length; i++) {
    if (buffer[i] !== quote) {
      continue;
    }
    if (buffer[i + 1] === quote) {
      i++;
      continue;
    }
    return i + 1;
  }
  return buffer.length;
}

/** Block comments nest, as they do in Snowflake and DuckDB. */
function skipBlockComment(buffer: string, start: number): number {
  let depth = 0;
  for (let i = start; i + 1 < buffer.length; i++) {
    if (buffer[i] === "/" && buffer[i + 1] === "*") {
      depth++;
      i++;
    } else if (buffer[i] === "*" && buffer[i + 1] === "/") {
      depth--;
      i++;
      if (depth === 0) {
        return i + 1;
      }
    }
  }
  return buffer.length;
}
