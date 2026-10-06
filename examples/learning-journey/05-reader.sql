-- Sign out and sign in as JOURNEY_ALICE / learn-alice.
-- Select JOURNEY_WH and JOURNEY_DB / PUBLIC.
SELECT CURRENT_USER() AS username, CURRENT_ROLE() AS active_role;
SELECT id, name, region FROM source_users ORDER BY id;
