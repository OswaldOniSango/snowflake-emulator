<p align="center">
  <img src="assets/snowflake-emulator.png" alt="snowflake-emulator" width="360" />
</p>

The Go gopher was designed by the awesome [Renee French](https://reneefrench.blogspot.com/)

# Snowflake Emulator

A lightweight, open-source Snowflake emulator built with Go and DuckDB for
learning, local experimentation, and development without requiring a paid
Snowflake account.

[![CI](https://github.com/OswaldOniSango/snowflake-emulator/actions/workflows/ci.yaml/badge.svg)](https://github.com/OswaldOniSango/snowflake-emulator/actions/workflows/ci.yaml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Reference](https://pkg.go.dev/badge/github.com/nnnkkk7/snowflake-emulator.svg)](https://pkg.go.dev/github.com/nnnkkk7/snowflake-emulator)
[![Latest Release](https://img.shields.io/github/v/release/OswaldOniSango/snowflake-emulator)](https://github.com/OswaldOniSango/snowflake-emulator/releases/latest)
[![GitHub Stars](https://img.shields.io/github/stars/OswaldOniSango/snowflake-emulator?style=social)](https://github.com/OswaldOniSango/snowflake-emulator)

⭐ Like it? Give us a star!

## TL;DR

```bash
docker run -p 8080:8080 ghcr.io/oswaldonisango/snowflake-emulator:latest
```

```go
dsn := "user:pass@localhost:8080/TEST_DB/PUBLIC?account=test&protocol=http"
db, _ := sql.Open("snowflake", dsn)
rows, _ := db.Query("SELECT IFF(1>0,'yes','no')")  // Supported Snowflake syntax works locally
```

## Use Cases

- Study Snowflake concepts locally without a paid cloud account
- Practice with tables, procedures, streams, tasks, and internal stages from a browser
- Run integration tests locally without Snowflake credentials (Go via gosnowflake, or any language via REST API)
- Cheap & fast CI smoke tests for Snowflake SQL
- Validate Snowflake-ish SQL behavior before hitting real Snowflake

> **Note**: This is a dev/test emulator — no auth, no clustering, no external stages, no JS stored procedures. See [Limitations](#limitations) for details.

## Overview

Snowflake Emulator provides a learning-oriented subset of the
[Snowflake](https://www.snowflake.com/) SQL interface backed by DuckDB:

- **Local & CI workflows** - Run supported Snowflake-style SQL with no external dependencies
- **Snowflake-compatible access** - [`gosnowflake`](https://github.com/snowflakedb/gosnowflake) driver support and REST API v2
- **SQL execution** - Snowflake → DuckDB translation

## Installation

<details>
<summary><b>Platform Support</b></summary>

| Platform | Docker |
|----------|--------|
| Linux x86_64 (amd64) | ✅ |
| Linux ARM64 | ✅ |
| macOS x86_64 | ✅ |
| macOS ARM64 (Apple Silicon) | ✅ |
| Windows (WSL2) | ✅ |

> **Note**: Published Docker images support Linux AMD64 and ARM64. Docker
> Desktop runs those images on supported macOS and Windows hosts. Prebuilt
> standalone binaries are not currently published.

</details>

### Docker (Recommended)

Docker is the recommended installation method for all platforms.

```bash
# Pull the image
docker pull ghcr.io/oswaldonisango/snowflake-emulator:latest

# Run with in-memory database
docker run -p 8080:8080 ghcr.io/oswaldonisango/snowflake-emulator:latest

# Run with persistent storage
docker run -p 8080:8080 -v snowflake-data:/data \
  -e DB_PATH=/data/snowflake.db \
  -e STAGE_DIR=/data/stages \
  ghcr.io/oswaldonisango/snowflake-emulator:latest
```

Use `latest` for the newest stable image, or pin a version for reproducible
environments:

```bash
docker pull ghcr.io/oswaldonisango/snowflake-emulator:0.2.0
```

Published versions are listed in
[GitHub Releases](https://github.com/OswaldOniSango/snowflake-emulator/releases).

#### Build and Run Local Changes with Docker

From the repository root, build a local image:

```bash
docker build -t snowflake-emulator .
```

Confirm that the image exists:

```bash
docker images snowflake-emulator
```

Start the emulator:

```bash
docker run --rm \
  --name snowflake-emulator \
  -p 8080:8080 \
  snowflake-emulator
```

Open `http://localhost:8080` in a browser. Press `Ctrl+C` to stop the
container; `--rm` removes the stopped container automatically.

### Build from Source

Prerequisites:

- Go 1.24+
- GCC (for DuckDB CGO)

```bash
git clone https://github.com/OswaldOniSango/snowflake-emulator.git
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

| | |
|---|---|
| **Worksheets** | Tabbed SQL editor with syntax highlighting. `Cmd`/`Ctrl` + `Enter` runs the statement under the cursor, or the selection. Multiple statements in one buffer are split correctly — including procedure bodies between `$$`, which are full of semicolons. Worksheets, their names and their execution context are kept in the browser, and tabs can be dragged into any order. A running statement can be canceled, and results exported as CSV or JSON. |
| **Translated SQL** | Shows the DuckDB SQL a statement becomes, beside what you wrote, without running it. Statements handled by a processor (COPY, MERGE, procedures) say so rather than showing a partial translation as though it were the whole story. |
| **Object explorer** | Databases, schemas, tables, streams, procedures, tasks and stages. Clicking an object writes its name into the editor. |
| **Warehouses** | Create, resume, suspend and drop. Compute is emulated: a suspended warehouse changes what the API reports, not where statements run. |
| **History** | Recent statements with their status, duration and handle. Click one to reopen it in a new worksheet. Statements are kept for seven days, and survive a restart when the emulator is run against a database file (`DB_PATH`); with the default in-memory database they go when the process does. |

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

# Ask for more rows than the default cap
curl -X POST http://localhost:8080/api/v2/statements \
  -H "Content-Type: application/json" \
  -d '{"statement": "SELECT * FROM orders", "rowLimit": 50000}'

# Create a database
curl -X POST http://localhost:8080/api/v2/databases \
  -H "Content-Type: application/json" \
  -d '{"name": "MY_DB"}'

# List warehouses
curl http://localhost:8080/api/v2/warehouses
```

The `database` and `schema` fields provide the execution context used to resolve
unqualified and schema-qualified object names. Table references may use
`table`, `schema.table`, or `database.schema.table`; explicit namespaces are
validated against the emulator catalog before execution. For example, the
following statement resolves `users_stream` inside `LEARNING_DB.PUBLIC`:

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
values. `SQLROWCOUNT` holds the row count of the most recent DML or `MERGE`,
readable from the start of the procedure (it starts at `0`); a top-level
`EXCEPTION WHEN OTHER THEN` handler can additionally inspect the
emulator-provided `SQLCODE`, `SQLSTATE`, and `SQLERRM` diagnostic variables,
and `SQLROWCOUNT` is not reset by the exception, so the handler still sees
what the last successful statement affected. Inside a plain SQL statement a
variable is written `:name`; a bare `name` is only substituted in a `RETURN`
expression or the right-hand side of an assignment. SQL executed inside a
procedure uses the procedure call's database and schema context. Dynamic
identifiers currently support simple unquoted object names; qualified/quoted
names remain limited. During a `CALL`, temporary tables use a single pinned
DuckDB connection, remain isolated from concurrent calls, and are cleaned up
when the invocation finishes.

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
| `/api/v2/statements` | POST | Submit SQL statement (`rowLimit` caps returned rows; `resultSetMetaData.numRows` always reports the true total). Add `"async": true`, or `?async=true`, to get a handle back at once and poll the status URL — a statement submitted that way can be canceled while it runs |
| `/api/v2/statements` | GET | Recent statement history (`?limit=N`) |
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
| `/api/v2/databases/{db}/schemas/{schema}/stages` | GET, POST | List/Create named internal stages |
| `/api/v2/databases/{db}/schemas/{schema}/stages/{stage}` | DELETE | Drop an internal stage and its files |
| `/api/v2/databases/{db}/schemas/{schema}/stages/{stage}/files` | GET, POST | List files or upload one multipart `file` (maximum 64 MiB) |
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
| **Query** | `WITH ... SELECT` | Common table expressions, chained and referencing one another, ahead of any statement type |
| **DML** | `INSERT`, `UPDATE`, `DELETE` | Data manipulation with rows affected count |
| **DDL** | `CREATE TABLE`, `DROP TABLE`, `ALTER TABLE` | Schema management |
| **DDL** | `CREATE TABLE ... AS <query>`, including a `WITH` clause | Function translation runs on the query body, not just a bare `SELECT` |
| **DDL** | `CREATE [OR REPLACE] TEMPORARY TABLE ... AS <query>` | A true DuckDB TEMP table, visible to every statement in the session |
| **DDL** | `CREATE DATABASE`, `DROP DATABASE` | Database management |
| **DDL** | `CREATE SCHEMA`, `DROP SCHEMA` | Schema namespace management |
| **DDL** | `CREATE [OR REPLACE] STAGE`, `DROP STAGE` | Named internal stages |
| **Transaction** | `BEGIN`, `COMMIT`, `ROLLBACK` | Transaction control |
| **Data Loading** | `LIST @stage`, `COPY INTO` | Upload and load CSV or JSON files from named internal stages |
| **Upsert** | `MERGE INTO` | Conditional insert/update/delete operations |
| **Procedures** | `CREATE [OR REPLACE] PROCEDURE`, `CALL`, `SHOW PROCEDURES`, `DROP PROCEDURE` | `LANGUAGE SQL` procedures with variables, assignments, dynamic `IDENTIFIER`, `CASE`, `IF/ELSE`, top-level `EXCEPTION`, and `RETURN` |
| **Streams** | `CREATE [OR REPLACE] STREAM`, `SHOW STREAMS`, `DROP STREAM`, `SELECT FROM stream` | Append-only insert tracking |
| **Tasks** | `CREATE [OR REPLACE] TASK`, `ALTER TASK ... RESUME/SUSPEND`, `EXECUTE TASK`, `SHOW TASKS`, `DROP TASK` | Manual execution and automatic second/minute/hour interval schedules; task bodies may execute SQL or call a procedure |

**Parameter Binding**: Supports positional placeholder substitution (`:1`, `:2`, `?`).

Schemas and persistent/transient tables created through SQL are synchronized
with the emulator catalog, so they are visible through the REST object explorer.
Temporary tables remain connection-scoped and are not stored in the global
catalog.

</details>

## Loading a CSV from an Internal Stage

Create a destination table and a named internal stage in the worksheet's
database and schema:

```sql
CREATE TABLE users (id INTEGER, name VARCHAR);
CREATE STAGE users_stage;
```

Refresh the object explorer, expand **Stages**, and use the upload arrow next to
`USERS_STAGE` to choose a local CSV file. The same upload is available through
the REST API:

```bash
curl -X POST \
  -F "file=@users.csv" \
  http://localhost:8080/api/v2/databases/TEST_DB/schemas/PUBLIC/stages/USERS_STAGE/files
```

Inspect and load the file:

```sql
LIST @users_stage;

COPY INTO users
FROM @users_stage
FILE_FORMAT = (
  TYPE = CSV
  SKIP_HEADER = 1
);

SELECT * FROM users;
```

This is a learning-oriented subset of Snowflake named internal stages. Upload
uses the emulator's HTTP/UI interface rather than Snowflake's `PUT` command.
External stages, named file-format objects, compressed files, and Snowflake load
history are not implemented. Running the same `COPY INTO` again loads the file
again unless the first command used `PURGE = TRUE`.

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
- Task graphs, task dependencies, `USING CRON` schedules, and Pipes
- External stages (S3, Azure, GCS)
- Stored procedures with JavaScript, Python, or Java
- Advanced Snowflake Scripting (`LET`, loops, nested exception scopes, qualified/quoted dynamic identifiers, and procedure overloading)
- Advanced stream semantics beyond append-only `INSERT` tracking (`UPDATE`/`DELETE`, retention, and stale-state handling)
- User-defined functions
- Snowflake's `FROM VALUES (...)` table literal with implicit `column1`/`column2` naming, which is not valid DuckDB syntax at all — `FROM (VALUES (1, 'Alice'), (2, 'Bob')) AS t(column1, column2)` reaches the same result unmodified

## Contributing

Contributions are welcome. Check the
[open issues](https://github.com/OswaldOniSango/snowflake-emulator/issues), fork
the repository, create a focused feature branch, add tests, and open a pull
request against `dev`. You do not need to know every part of Go or Snowflake to
start: the project is intended to be learned and improved incrementally.

## Authors and maintainers

- [Naoki Kuroda](https://github.com/nnnkkk7) — Original author of [snowflake-emulator](https://github.com/nnnkkk7/snowflake-emulator).
- [Oswaldo Hernández](https://github.com/OswaldOniSango) — Maintainer of this [extended fork](https://github.com/OswaldOniSango/snowflake-emulator) and
  contributor to its Snowflake learning features.

This fork builds on Naoki Kuroda's original project and extends it as a local
environment for studying Snowflake concepts.

## License

[MIT](LICENSE)
