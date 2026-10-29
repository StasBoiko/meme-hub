CREATE TABLE IF NOT EXISTS message_votes (
	message_id INTEGER REFERENCES messages(id) ON DELETE CASCADE,
	user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
	value SMALLINT NOT NULL CHECK (value IN (-1, 1)),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (message_id, user_id)
);
