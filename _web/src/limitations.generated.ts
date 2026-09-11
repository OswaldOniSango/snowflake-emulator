// Generated from README.md by scripts/generate-limitations.mjs.
// Do not edit: change the README's Limitations section instead.

/** What the emulator does not support, as documented in the README. */
export const LIMITATIONS: readonly string[] = [
  "Production authentication and object-level authorization — local gosnowflake sessions authenticate users and roles, but REST/UI requests remain anonymous and object privileges such as GRANT SELECT or GRANT USAGE are not implemented yet. Identity SQL manages users and role membership only; ownership transfer, secondary roles, and database roles are also outside the current subset.",
  "Distributed processing / Clustering",
  "Time Travel / Zero-Copy Cloning",
  "Task graphs, task dependencies, USING CRON schedules, and Pipes",
  "Automatic/incremental dynamic-table refresh and dynamic-table dependency graphs",
  "External stages (S3, Azure, GCS)",
  "Stored procedures and functions with JavaScript, Python, or Java",
  "Advanced Snowflake Scripting (loops, nested exception scopes, qualified/quoted dynamic identifiers, and procedure overloading)",
  "Advanced stream semantics beyond append-only INSERT tracking (UPDATE/DELETE, retention, and stale-state handling)",
  "User-defined table functions (UDTFs), and SQL functions with a procedural (multi-statement) body — a function's body is one expression, backed directly by a DuckDB MACRO",
];
