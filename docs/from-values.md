# FROM VALUES

Authenticated queries accept inline Snowflake VALUES rows and implicit columnN names.

```sql
SELECT column1 AS id, column2 AS name, column3 AS region
FROM VALUES (1, 'Alice', 'NORTH'), (2, 'Bob', 'SOUTH'), (3, 'Carla', 'NORTH')
ORDER BY column1;
```

Strings, escaped quotes and commas inside rows are data, not table names.
The same form works inside a subquery. Joins to real tables still require the
corresponding SELECT privileges. This does not add a complete Snowflake SQL parser.
