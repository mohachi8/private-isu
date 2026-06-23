-- Indexes added for performance. Idempotent-ish: run once on a fresh DB.
-- These are NOT recreated by GET /initialize (it only deletes rows), so they
-- persist across benchmark runs once applied.

-- comments are looked up by post_id (count + latest 3, ordered by created_at)
ALTER TABLE comments ADD INDEX idx_post_id_created_at (post_id, created_at);
-- comments counted by user_id on the user page
ALTER TABLE comments ADD INDEX idx_user_id (user_id);
-- posts listed per user, ordered by created_at
ALTER TABLE posts ADD INDEX idx_user_id_created_at (user_id, created_at);
-- index page orders all posts by created_at
ALTER TABLE posts ADD INDEX idx_created_at (created_at);
