// Generated from README.md by scripts/generate-limitations.mjs.
// Do not edit: change the README's Limitations section instead.

/** What the emulator does not support, as documented in the README. */
export const LIMITATIONS: readonly string[] = [
  "Production authentication and object-level authorization — local gosnowflake sessions authenticate users and roles, while REST/UI requests remain anonymous. Authenticated sessions enforce warehouse USAGE and OPERATE; namespace USAGE on databases and schemas; CREATE TABLE on schemas; and SELECT, INSERT, UPDATE, and DELETE on tables. Grants are inherited through the active role hierarchy and are checked before warehouse admission. Ownership transfer, secondary roles, future grants, stage privileges, row policies, and database roles remain outside the current subset.",
  "Procedures use caller-rights rather than Snowflake's full configurable caller/owner-rights model. COPY INTO and streams enforce warehouse compute authorization, but stage and table object privileges are not implemented.",
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
