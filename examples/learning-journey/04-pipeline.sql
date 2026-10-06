CREATE PROCEDURE process_users()
RETURNS VARCHAR
LANGUAGE SQL
AS $$
BEGIN
    INSERT INTO processed_users SELECT id, name, region FROM users_stream;
    INSERT INTO procedure_log VALUES ('processed');
    RETURN 'processed';
END;
$$;

CALL process_users();
SELECT COUNT(*) AS processed_rows FROM processed_users;
SELECT COUNT(*) AS pending_rows FROM users_stream;

-- Keep it suspended: EXECUTE TASK is deterministic and does not wait for a timer.
CREATE TASK process_users_task WAREHOUSE = JOURNEY_WH SCHEDULE = '1 HOUR' AS CALL process_users();

CREATE DYNAMIC TABLE users_by_region
TARGET_LAG = '1 MINUTE'
WAREHOUSE = JOURNEY_WH
AS SELECT region, COUNT(*) AS users FROM processed_users GROUP BY region;
SELECT region, users FROM users_by_region ORDER BY region;

INSERT INTO source_users VALUES (4, 'Barbara', 'US');
SELECT COUNT(*) AS pending_rows FROM users_stream;
EXECUTE TASK process_users_task;
SELECT COUNT(*) AS processed_rows FROM processed_users;
SELECT COUNT(*) AS pending_rows FROM users_stream;
-- TARGET_LAG is metadata in this MVP. Materialized rows still total 3 here.
SELECT SUM(users) AS materialized_users FROM users_by_region;
ALTER DYNAMIC TABLE users_by_region REFRESH;
SELECT region, users FROM users_by_region ORDER BY region;
SELECT COUNT(*) AS procedure_calls FROM procedure_log;
-- Executing again sees no changes; processed_rows remains 4.
EXECUTE TASK process_users_task;
SELECT COUNT(*) AS processed_rows FROM processed_users;
