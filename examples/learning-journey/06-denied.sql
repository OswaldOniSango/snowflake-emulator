-- JOURNEY_ALICE: run EACH statement separately. Both MUST fail.
-- Run all intentionally stops at the first failure.
INSERT INTO source_users VALUES (99, 'Not allowed', 'US');
USE ROLE JOURNEY_WRITER;
