# SQL session variables

In an authenticated worksheet or driver session:

```sql
SET siteid = 35;
SELECT $siteid;
SET siteid = $siteid + 1;
SELECT * FROM VALUES (35), (36) WHERE column1 = $siteid;
UNSET siteid;
```

`SET name = expression` supports one scalar value (numeric, string, Boolean, date,
time, timestamp or NULL), including a scalar subquery. Names are case-insensitive;
variables are isolated per login session, shared by REST/driver requests for that
session, and survive token renewal. They are memory-only and reset on server
restart; logout/expiry discards the session. Values over 16 KB, compound data types,
multi-variable SET/UNSET, `SHOW VARIABLES`, `IDENTIFIER($var)` and connection-string
initialization are not implemented. Positional bindings remain separate.

Reference: [Snowflake session variables](https://docs.snowflake.com/en/sql-reference/session-variables).
