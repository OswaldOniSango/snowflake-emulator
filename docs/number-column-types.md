# NUMBER column declarations

`NUMBER` is translated only in CREATE TABLE column type declarations, with defaults
`NUMBER(38,0)`. Precision is 1–38; scale is 0–min(precision,37). DuckDB DECIMAL storage,
rounding, overflow and arithmetic apply; this does not promise complete Snowflake
numeric semantics or support every NUMBER cast spelling. Column names, strings,
comments and expressions containing NUMBER are left intact. See the Snowflake
reference for [numeric types](https://docs.snowflake.com/en/sql-reference/data-types-numeric).

