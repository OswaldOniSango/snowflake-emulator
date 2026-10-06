-- Upload users.csv to USERS_STAGE before running this file. Load it only once.
LIST @users_stage;
COPY INTO source_users FROM @users_stage FILE_FORMAT = (TYPE = CSV SKIP_HEADER = 1);
SELECT id, name, region FROM source_users ORDER BY id;
SELECT COUNT(*) AS pending_rows FROM users_stream;
-- A SELECT does not consume the stream: this is still 3.
SELECT COUNT(*) AS pending_rows FROM users_stream;
SELECT COUNT(*) AS visible_rows FROM users_view;
