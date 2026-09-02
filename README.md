<p align="center">
  <img src="assets/snowflake-emulator.png" alt="snowflake-emulator" width="360" />
</p>

The Go gopher was designed by the awesome [Renee French](https://reneefrench.blogspot.com/)

# Snowflake Emulator

A lightweight, open-source Snowflake emulator built with Go and DuckDB, designed for local development and testing.

[![CI](https://github.com/OswaldOniSango/snowflake-emulator/actions/workflows/ci.yaml/badge.svg)](https://github.com/OswaldOniSango/snowflake-emulator/actions/workflows/ci.yaml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Reference](https://pkg.go.dev/badge/github.com/nnnkkk7/snowflake-emulator.svg)](https://pkg.go.dev/github.com/nnnkkk7/snowflake-emulator)
[![GitHub Stars](https://img.shields.io/github/stars/nnnkkk7/snowflake-emulator?style=social)](https://github.com/nnnkkk7/snowflake-emulator)

⭐ Like it? Give us a star!

## TL;DR

```bash
docker run -p 8080:8080 ghcr.io/nnnkkk7/snowflake-emulator:latest
```

```go
dsn := "user:pass@localhost:8080/TEST_DB/PUBLIC?account=test&protocol=http"
db, _ := sql.Open("snowflake", dsn)
rows, _ := db.Query("SELECT IFF(1>0,'yes','no')")  // Snowflake SQL works!
```

## Use Cases

- Run integration tests locally without Snowflake credentials (Go via gosnowflake, or any language via REST API)
- Cheap & fast CI smoke tests for Snowflake SQL
- Validate Snowflake-ish SQL behavior before hitting real Snowflake

> **Note**: This is a dev/test emulator — no auth, no clustering, no external stages, no JS stored procedures. See [Limitations](#limitations) for details.

## Overview

Snowflake Emulator provides a [Snowflake](https://www.snowflake.com/)-compatible SQL interface backed by DuckDB for local development and testing:

- **Local & CI workflows** - Run Snowflake-compatible SQL with no external dependencies
- **Snowflake-compatible access** - [`gosnowflake`](https://github.com/snowflakedb/gosnowflake) driver support and REST API v2
- **SQL execution** - Snowflake → DuckDB translation

## Installation

<details>
<summary><b>Platform Support</b></summary>

| Platform | Docker | Binary |
|----------|--------|--------|
| Linux x86_64 (amd64) | ✅ | ✅ |
| Linux ARM64 | ✅ | - |
| macOS x86_64 | ✅ | - |
| macOS ARM64 (Apple Silicon) | ✅ | - |
| Windows (WSL2) | ✅ | - |

> **Note**: Binary releases are only available for Linux x86_64. This is due to DuckDB requiring CGO, which makes cross-compilation complex. For all other platforms, Docker is recommended.

</details>

### Docker (Recommended)

Docker is the recommended installation method for all platforms.

```bash
# Pull the image
docker pull ghcr.io/nnnkkk7/snowflake-emulator:latest

# Run with in-memory database
docker run -p 8080:8080 ghcr.io/nnnkkk7/snowflake-emulator:latest

# Run with persistent storage
docker run -p 8080:8080 -v snowflake-data:/data \
  -e DB_PATH=/data/snowflake.db \
  ghcr.io/nnnkkk7/snowflake-emulator:latest
```

### Build from Source (Linux x86_64)

Prerequisites:

- Go 1.24+
- GCC (for DuckDB CGO)

```bash
git clone https://github.com/nnnkkk7/snowflake-emulator.git
cd snowflake-emulator
CGO_ENABLED=1 go build -o snowflake-emulator ./cmd/server
```

### Run the Server

```bash
# In-memory mode (default)
./snowflake-emulator

# With persistent storage
DB_PATH=/path/to/database.db ./snowflake-emulator

# Custom port
PORT=9090 ./snowflake-emulator
```

## Web Console

The emulator serves a browser console at `http://localhost:8080/`. It is a static
bundle compiled into the binary, so Docker images and releases need no extra
assets and no separate process.

> **Note**: The console is an original interface for this emulator. It is not
> affiliated with or endorsed by Snowflake Inc.

<details>
<summary><b>Developing the console</b></summary>

The frontend is a TypeScript + Vite project in `_web/`. The leading underscore is
deliberate: it hides `node_modules` from the Go toolchain, which would otherwise
compile Go files that some npm packages ship. Vite writes its output to
`server/ui/dist`, which `server/ui` embeds with `go:embed`.

```bash
# Install dependencies (Node version comes from _web/.nvmrc)
make ui-install

# Hot-reloading dev server on :5173, proxying /api and /health to :8080
make ui-dev

# Produce the bundle the Go binary embeds
make ui-build
```

Run `make ui-dev` alongside `make run` for frontend work: the Vite proxy forwards
API calls to the emulator, so development is same-origin too and needs no CORS.

`make build` builds the console before compiling. For a Node-free Go build, the
placeholder in `server/ui/dist` keeps `go:embed` satisfied:

```bash
CGO_ENABLED=1 go build -o snowflake-emulator ./cmd/server
```

That binary starts normally and serves the full REST API; only the console
answers with a build hint until you run `make ui-build`.

| Target | Description |
|--------|-------------|
| `make ui-install` | Install frontend dependencies |
| `make ui-dev` | Vite dev server with hot reload |
| `make ui-build` | Build the bundle into `server/ui/dist` |
| `make ui-test` | Frontend unit tests |
| `make ui-lint` | Frontend linting |
| `make ui-typecheck` | Frontend type checking |

</details>

### Using with gosnowflake Driver

```go
package main

import (
    "database/sql"
    "fmt"
    "log"

    _ "github.com/snowflakedb/gosnowflake"
)

func main() {
    // Connect to local emulator
    dsn := "user:pass@localhost:8080/TEST_DB/PUBLIC?account=test&protocol=http"
    db, err := sql.Open("snowflake", dsn)
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // Execute Snowflake SQL (automatically translated)
    rows, err := db.Query(`
        SELECT
            name,
            IFF(score >= 90, 'A', 'B') AS grade,
            NVL(email, 'no-email') AS email
        FROM users
    `)
    if err != nil {
        log.Fatal(err)
    }
    defer rows.Close()

    for rows.Next() {
        var name, grade, email string
        rows.Scan(&name, &grade, &email)
        fmt.Printf("%s: %s (%s)\n", name, grade, email)
    }
}
```

### Using REST API v2

```bash
# Submit a SQL statement
curl -X POST http://localhost:8080/api/v2/statements \
  -H "Content-Type: application/json" \
  -d '{
    "statement": "SELECT IFF(1 > 0, '\''yes'\'', '\''no'\'')",
    "database": "TEST_DB",
    "schema": "PUBLIC"
  }'

# Get statement result
curl http://localhost:8080/api/v2/statements/{handle}

# Create a database
curl -X POST http://localhost:8080/api/v2/databases \
  -H "Content-Type: application/json" \
  -d '{"name": "MY_DB"}'

# List warehouses
curl http://localhost:8080/api/v2/warehouses
```

The `database` and `schema` fields provide the execution context used to resolve
unqualified object names. For example, the following statements resolve
`users` and `users_stream` inside `LEARNING_DB.PUBLIC`:

```json
{
  "statement": "SELECT * FROM users_stream",
  "database": "LEARNING_DB",
  "schema": "PUBLIC"
}
```

### SQL Stored Procedures

The emulator supports a small `LANGUAGE SQL` stored procedure subset:

```sql
CREATE PROCEDURE LEARNING_DB.PUBLIC.GREET(NAME VARCHAR)
RETURNS VARCHAR
LANGUAGE SQL
AS $$
BEGIN
    RETURN CONCAT('Hello, ', :NAME);
END
$$;

CALL LEARNING_DB.PUBLIC.GREET('Snowflake');
SHOW PROCEDURES;
DROP PROCEDURE LEARNING_DB.PUBLIC.GREET;
```

Procedure definitions are persisted in the emulator catalog when `DB_PATH` is
configured. Procedure bodies support multiple SQL statements, `DECLARE`
variables with `DEFAULT` values, scalar assignments with `:=`, searched
execution through `CASE`, conditional branches with `IF/ELSE`, variable
bindings, dynamic object names through `IDENTIFIER(:variable)`, and `RETURN`
values. A top-level `EXCEPTION WHEN OTHER THEN` handler can inspect the
emulator-provided `SQLCODE`, `SQLSTATE`, and `SQLERRM` diagnostic variables. SQL
executed inside a procedure uses the procedure call's database and schema
context. Dynamic identifiers currently support simple unquoted object names;
qualified/quoted names remain limited. During a `CALL`, temporary tables use a
single pinned DuckDB connection, remain isolated from concurrent calls, and are
cleaned up when the invocation finishes.

### Append-Only Streams

Streams expose rows inserted after the stream was created:

```sql
CREATE TABLE users (
    id INTEGER,
    name VARCHAR
);

CREATE STREAM users_stream
ON TABLE users
APPEND_ONLY = TRUE;

INSERT INTO users VALUES (1, 'Oswaldo');

SELECT * FROM users_stream;
```

The result includes `METADATA$ACTION`, `METADATA$ISUPDATE`, and
`METADATA$ROW_ID`. Short object names require a database and schema execution
context, supplied by the REST request or the `gosnowflake` session.

A plain `SELECT` does not consume a stream. A successful DML statement that
reads from it advances its offset, so the same changes are not returned again:

```sql
INSERT INTO processed_users
SELECT id, name FROM users_stream;
```

### Tasks

Tasks store SQL to be executed in a database/schema context. They are created
in `SUSPENDED` state and can currently be run manually:

```sql
CREATE TASK process_users
  WAREHOUSE = LEARNING_WH
  SCHEDULE = '1 MINUTE'
AS
  INSERT INTO processed_users
  SELECT id, name FROM users_stream;

ALTER TASK process_users RESUME;
EXECUTE TASK process_users;
SHOW TASKS;
ALTER TASK process_users SUSPEND;
DROP TASK process_users;
```

A task body can also call a SQL stored procedure. Short procedure names resolve
inside the task's database and schema:

```sql
CREATE TASK greet_task
  WAREHOUSE = LEARNING_WH
  SCHEDULE = '5 MINUTES'
AS
  CALL greet('Oswaldo');
```

Tasks in `STARTED` state run automatically. The scheduler currently supports
second, minute, and hour intervals, such as `1 SECOND`, `5 MINUTES`, or
`2 HOURS`. `USING CRON` schedules are not supported yet. `EXECUTE TASK` remains
available for immediate manual execution, including while a task is suspended.

## Next Steps

| Example | Description |
|---------|-------------|
| [`gosnowflake/`](example/gosnowflake/) | Go driver example |
| [`restapi/`](example/restapi/) | REST API v2 example |
| [`docker/`](example/docker/) | Docker container example |

```bash
# Start the emulator
go run ./cmd/server

# In another terminal, run an example
go run ./example/gosnowflake
```

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP server port |
| `DB_PATH` | `:memory:` | DuckDB database path (empty for in-memory) |
| `STAGE_DIR` | `./stages` | Directory for internal stage files |

## API Endpoints

### gosnowflake Protocol

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/session/v1/login-request` | POST | Session login |
| `/session/token-request` | POST | Token refresh |
| `/session/heartbeat` | POST | Keep-alive |
| `/session/renew` | POST | Renew session |
| `/session/logout` | POST | Logout |
| `/session/use` | POST | USE DATABASE/SCHEMA |
| `/queries/v1/query-request` | POST | Execute SQL query |
| `/queries/v1/abort-request` | POST | Cancel query |

### REST API v2

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v2/statements` | POST | Submit SQL statement |
| `/api/v2/statements/{handle}` | GET | Get statement status/result |
| `/api/v2/statements/{handle}/cancel` | POST | Cancel statement |
| `/api/v2/translate` | POST | Show the DuckDB SQL a statement translates to, without running it |
| `/api/v2/databases` | GET, POST | List/Create databases |
| `/api/v2/databases/{db}` | GET, PUT, DELETE | Get/Alter/Drop database |
| `/api/v2/databases/{db}/schemas` | GET, POST | List/Create schemas |
| `/api/v2/databases/{db}/schemas/{schema}` | GET, DELETE | Get/Drop schema |
| `/api/v2/databases/{db}/schemas/{schema}/objects` | GET | List everything a schema contains (tables, streams, procedures, tasks, stages) |
| `/api/v2/databases/{db}/schemas/{schema}/tables` | GET, POST | List/Create tables |
| `/api/v2/databases/{db}/schemas/{schema}/tables/{table}` | GET, PUT, DELETE | Get/Alter/Drop table |
| `/api/v2/warehouses` | GET, POST | List/Create warehouses |
| `/api/v2/warehouses/{wh}` | GET, DELETE | Get/Drop warehouse |
| `/api/v2/warehouses/{wh}:resume` | POST | Resume warehouse |
| `/api/v2/warehouses/{wh}:suspend` | POST | Suspend warehouse |
| `/health` | GET | Health check |

## Compatibility

<details>
<summary><b>Supported SQL Operations</b></summary>

The emulator supports standard SQL operations with automatic Snowflake-to-DuckDB translation:

| Category | Operations | Description |
|----------|------------|-------------|
| **Query** | `SELECT`, `SHOW`, `DESCRIBE`, `EXPLAIN` | Read operations with full result set support |
| **DML** | `INSERT`, `UPDATE`, `DELETE` | Data manipulation with rows affected count |
| **DDL** | `CREATE TABLE`, `DROP TABLE`, `ALTER TABLE` | Schema management |
| **DDL** | `CREATE DATABASE`, `DROP DATABASE` | Database management |
| **DDL** | `CREATE SCHEMA`, `DROP SCHEMA` | Schema namespace management |
| **Transaction** | `BEGIN`, `COMMIT`, `ROLLBACK` | Transaction control |
| **Data Loading** | `COPY INTO` | Bulk data loading from internal stages (CSV, JSON) |
| **Upsert** | `MERGE INTO` | Conditional insert/update/delete operations |
| **Procedures** | `CREATE [OR REPLACE] PROCEDURE`, `CALL`, `SHOW PROCEDURES`, `DROP PROCEDURE` | `LANGUAGE SQL` procedures with variables, assignments, dynamic `IDENTIFIER`, `CASE`, `IF/ELSE`, top-level `EXCEPTION`, and `RETURN` |
| **Streams** | `CREATE [OR REPLACE] STREAM`, `SHOW STREAMS`, `DROP STREAM`, `SELECT FROM stream` | Append-only insert tracking |

**Parameter Binding**: Supports positional placeholder substitution (`:1`, `:2`, `?`).

</details>

<details>
<summary><b>Supported SQL Functions</b></summary>

| Snowflake | DuckDB | Description |
|-----------|--------|-------------|
| `IFF(cond, t, f)` | `IF(cond, t, f)` | Conditional expression |
| `NVL(a, b)` | `COALESCE(a, b)` | Null value substitution |
| `NVL2(a, b, c)` | `IF(a IS NOT NULL, b, c)` | Null conditional |
| `IFNULL(a, b)` | `COALESCE(a, b)` | Null value substitution |
| `DATEADD(part, n, date)` | `date + INTERVAL n part` | Date arithmetic |
| `DATEDIFF(part, start, end)` | `DATE_DIFF('part', start, end)` | Date difference |
| `TO_VARIANT(x)` | `CAST(x AS JSON)` | Convert to variant |
| `PARSE_JSON(str)` | `CAST(str AS JSON)` | Parse JSON string |
| `OBJECT_CONSTRUCT(...)` | `json_object(...)` | Build JSON object |
| `LISTAGG(col, sep)` | `STRING_AGG(col, sep)` | String aggregation |
| `FLATTEN(...)` | `UNNEST(...)` | Array expansion |

</details>

<details>
<summary><b>Supported Data Types</b></summary>

| Snowflake Type | DuckDB Type |
|----------------|-------------|
| NUMBER, NUMERIC, DECIMAL | DOUBLE / DECIMAL(p,s) |
| INTEGER, BIGINT, SMALLINT, TINYINT | INTEGER / BIGINT |
| FLOAT, DOUBLE, REAL | DOUBLE |
| VARCHAR, STRING, TEXT, CHAR | VARCHAR |
| BOOLEAN | BOOLEAN |
| DATE | DATE |
| TIME | TIME |
| TIMESTAMP, TIMESTAMP_NTZ | TIMESTAMP |
| TIMESTAMP_LTZ, TIMESTAMP_TZ | TIMESTAMPTZ |
| VARIANT, OBJECT | JSON |
| ARRAY | JSON |
| BINARY, VARBINARY | BLOB |
| GEOGRAPHY, GEOMETRY | VARCHAR (WKT) |

</details>

## Limitations

This emulator is designed for development and testing. The following features
are not supported or have limited support:

- Authentication/Authorization (skipped in dev mode)
- Distributed processing / Clustering
- Time Travel / Zero-Copy Cloning
- Tasks and Pipes
- External stages (S3, Azure, GCS)
- Stored procedures with JavaScript, Python, or Java
- Advanced Snowflake Scripting (`LET`, loops, nested exception scopes, qualified/quoted dynamic identifiers, and procedure overloading)
- Stream change tracking for `UPDATE` and `DELETE`
- Stream consumption semantics, retention, and stale-state handling
- User-defined functions

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## Authors and maintainers

- [Naoki Kuroda](https://github.com/nnnkkk7) — Original author of [snowflake-emulator](https://github.com/nnnkkk7/snowflake-emulator).
- [Oswaldo Hernández](https://github.com/OswaldOniSango) — Maintainer of this [extended fork](https://github.com/OswaldOniSango/snowflake-emulator) and
  contributor to its Snowflake learning features.

This fork builds on Naoki Kuroda's original project and extends it as a local
environment for studying Snowflake concepts.

## License

[MIT](LICENSE)
