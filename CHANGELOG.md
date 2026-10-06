# Changelog

## v0.5.0

- Isolate authenticated statement results, cancellation, and persisted history by user.
- Revalidate active roles and users before SQL execution, and prevent late browser responses from crossing login sessions.
- Add a shared SQL/CSV learning journey with expected results and cleanup instructions.
- Run the journey against the compiled console in Chromium, including upload, stream/procedure/task processing, dynamic table refresh, and role denials.
- Reload the database/schema picker when opened so newly created databases are available.
- Disable the role/warehouse picker until its initial options and click handler are ready.
- Require login tokens for REST statements; update REST/Docker examples and document remaining local-only limitations.

Release notes and upgrade instructions: [v0.5.0](docs/releases/v0.5.0.md).

## [v0.4.0](https://github.com/OswaldOniSango/snowflake-emulator/releases/tag/v0.4.0) - 2026-09-13

- Add persistent local users and roles, password authentication for `gosnowflake`, authenticated session context, and `USE ROLE`.
- Add `CURRENT_USER()` and `CURRENT_ROLE()`, plus the local demonstration administrator `ADMIN` with the `ACCOUNTADMIN` role.
- Add user/role administration, role grants and revocation, `SHOW USERS`, `SHOW ROLES`, and `SHOW GRANTS`.
- Add inherited privileges through the active role hierarchy and authorization checks for supported database, schema, table, warehouse, procedure, stream, and task operations.
- Persist warehouse definitions and configuration; add SQL lifecycle operations, automatic resume, size-based admission slots, and FIFO queuing. Compute statements require a warehouse.
- Add browser login/logout, identity management, identity-aware context selection, and warehouse-management improvements.
- Fix CTAS stream consumption, leading comments before `TRANSIENT` table statements, and database creation/login edge cases; improve catalog synchronization, session persistence, and history.

At this release, browser and REST statement requests still retained an anonymous local-study authorization bypass; authenticated REST statement enforcement follows in v0.5.0. Warehouses share one DuckDB engine, and authentication is not a production security boundary.

## [v0.3.0](https://github.com/OswaldOniSango/snowflake-emulator/releases/tag/v0.3.0) - 2026-09-05

- Add ordinary views with `CREATE [OR REPLACE] VIEW`, `SHOW VIEWS`, and `DROP VIEW`.
- Add dynamic tables backed by DuckDB materialization and manual `ALTER DYNAMIC TABLE ... REFRESH`.
- Persist dynamic-table definitions, source context, target lag, warehouse, and refresh timestamps.
- Make dynamic-table create, replace, refresh, and drop transactional, preserving previous data and metadata on failure; block ordinary table writes to their materialized results.
- Add SQL user-defined functions, `LET` declarations in SQL procedures, and Snowflake-style `FROM VALUES` with implicit column names.
- Display views and dynamic tables separately in the object explorer; update Docker, compatibility, and contribution documentation.

At this release, `TARGET_LAG` was metadata only: automatic/incremental refresh and dynamic-table scheduling were not implemented. Warehouses were still in memory, and `SHOW VIEWS` did not support the additional `LIKE`/`IN` variants or the complete Snowflake result shape.

## [v0.2.0](https://github.com/OswaldOniSango/snowflake-emulator/releases/tag/v0.2.0) - 2026-09-04

- Fix CTE resolution, including `WITH` queries inside SQL procedures.
- Improve session-scoped temporary-table creation and visibility across statements.
- Fix `MERGE` and `::type` casts inside procedure bodies.
- Add `SQLROWCOUNT` to track rows affected by the most recent statement in a procedure.
- Accept leading comments before `CREATE PROCEDURE`, `DROP PROCEDURE`, and `CALL`.
- Add a collapsible results panel with a preference retained across reloads, and highlight the statement currently executing during Run All.
- Make file-upload feedback dismissible, clear it on refresh, and automatically dismiss successful upload messages.

## [v0.1.0](https://github.com/OswaldOniSango/snowflake-emulator/releases/tag/v0.1.0) - 2026-09-03

- Publish the fork's first educational MVP for local experimentation with a subset of Snowflake concepts.
- Include a web SQL worksheet and object explorer; permanent, temporary, and transient tables; and SQL stored procedures.
- Include streams with offset tracking and basic tasks that can call procedures.
- Include named internal stages, CSV/JSON uploads through the console, `LIST @stage`, and `COPY INTO`.
- Include persistent query history and asynchronous statements.
- Publish Docker images for AMD64 and ARM64.

Versions v0.1.0 through v0.4.0 above summarize their linked published release notes; dates are publication dates and limitations describe those versions. This fork builds on [Naoki Kuroda's original project](https://github.com/nnnkkk7/snowflake-emulator). The upstream history below is preserved as originally recorded.

## [v0.0.9](https://github.com/nnnkkk7/snowflake-emulator/compare/v0.0.8...v0.0.9) - 2026-01-19
- chore: fix tagpr config by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/43

## [v0.0.8](https://github.com/nnnkkk7/snowflake-emulator/compare/v0.0.7...v0.0.8) - 2026-01-19
- refactor: update comments and test names for clarity and consistency by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/39
- docs: add Go gopher design attribution by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/41

## [v0.0.8](https://github.com/nnnkkk7/snowflake-emulator/compare/v0.0.7...v0.0.8) - 2026-01-06
- refactor: update comments and test names for clarity and consistency by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/39
- docs: add Go gopher design attribution by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/41

## [v0.0.7](https://github.com/nnnkkk7/snowflake-emulator/compare/v0.0.6...v0.0.7) - 2026-01-05
- docs: update README.md to enhance clarity and structure by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/36
- ci: fix release workflow by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/38

## [v0.0.6](https://github.com/nnnkkk7/snowflake-emulator/compare/v0.0.5...v0.0.6) - 2026-01-03
- chore: remove unnecessary example by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/30
- docs: add link by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/32
- deps: replace duckdb client by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/33

## [v0.0.5](https://github.com/nnnkkk7/snowflake-emulator/compare/v0.0.4...v0.0.5) - 2026-01-02
- docs: remove Snowflake SQL Functions Supported section by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/25

## [v0.0.4](https://github.com/nnnkkk7/snowflake-emulator/compare/v0.0.3...v0.0.4) - 2026-01-02
- docs: update README with new features and examples by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/20
- test: add comprehensive SQL operations tests for integration and REST API by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/22
- docs: update readme by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/24

## [v0.0.3](https://github.com/nnnkkk7/snowflake-emulator/compare/v0.0.2...v0.0.3) - 2026-01-02
- chore: refine GoReleaser configuration for native builds and clarify installation instructions by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/16
- chore: improve dockerfile for security and performance by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/18
- docs: remove architecture and layer design sections by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/19

## [v0.0.2](https://github.com/nnnkkk7/snowflake-emulator/compare/v0.0.1...v0.0.2) - 2025-12-31
- feat: add Renovate configuration for dependency management by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/4
- ci: remove unnecessary by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/6
- docs: update readme by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/7
- refactor: rename BindingValue to QueryBindingValue and update related handlers for consistency by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/8
- chore: add example code by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/9
- ci: add workflow for docker by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/10
- feat: add ALTER DATABASE and ALTER TABLE endpoints by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/11
- feat: implement MERGE INTO support with handler and SQL parsing by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/12
- refactor: replace COPY and MERGE handlers with processors by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/13
- docs: update readme by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/14
- chore: update GoReleaser and Docker configurations for docker artifact by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/15

## [v0.0.1](https://github.com/nnnkkk7/snowflake-emulator/commits/v0.0.1) - 2025-12-30
- feat: add GoReleaser and tagpr workflows for automated releases by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/2
- feat: Initial implementation of Snowflake Emulator by @nnnkkk7 in https://github.com/nnnkkk7/snowflake-emulator/pull/1
