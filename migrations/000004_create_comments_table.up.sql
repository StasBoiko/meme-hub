CREATE TABLE IF NOT EXISTS comments (
	id SERIAL PRIMARY KEY,
	message_id INTEGER REFERENCES messages(id) ON DELETE CASCADE,
	user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
	body TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS comments_message_id_idx ON comments (message_id);
