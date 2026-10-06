-- Sign out and sign in as JOURNEY_BOB / learn-bob.
-- Select JOURNEY_WH and JOURNEY_DB / PUBLIC.
SELECT CURRENT_USER() AS username, CURRENT_ROLE() AS active_role;
INSERT INTO source_users VALUES (5, 'Test writer', 'EU');
UPDATE source_users SET name = 'Updated writer' WHERE id = 5;
SELECT name FROM source_users WHERE id = 5;
DELETE FROM source_users WHERE id = 5;
SELECT COUNT(*) AS source_rows FROM source_users;
