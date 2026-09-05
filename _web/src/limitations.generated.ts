// Generated from README.md by scripts/generate-limitations.mjs.
// Do not edit: change the README's Limitations section instead.

/** What the emulator does not support, as documented in the README. */
export const LIMITATIONS: readonly string[] = [
  "Authentication/Authorization (skipped in dev mode)",
  "Distributed processing / Clustering",
  "Time Travel / Zero-Copy Cloning",
  "Tasks and Pipes",
  "External stages (S3, Azure, GCS)",
  "Stored procedures with JavaScript, Python, or Java",
  "Advanced Snowflake Scripting (LET, loops, nested exception scopes, qualified/quoted dynamic identifiers, and procedure overloading)",
  "Stream change tracking for UPDATE and DELETE",
  "Stream consumption semantics, retention, and stale-state handling",
  "User-defined functions",
];
