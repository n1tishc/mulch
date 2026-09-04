PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  parent_id TEXT,
  fork_seq INTEGER,
  label TEXT,
  task TEXT NOT NULL,
  model TEXT NOT NULL,
  context_window INTEGER NOT NULL DEFAULT 0,
  workdir TEXT NOT NULL,
  created_at TEXT NOT NULL,
  ended_at TEXT,
  status TEXT NOT NULL DEFAULT 'running'
);

CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  seq INTEGER NOT NULL,
  turn INTEGER NOT NULL,
  type TEXT NOT NULL,
  payload TEXT NOT NULL,
  tokens INTEGER,
  visible INTEGER NOT NULL DEFAULT 1,
  image_ref TEXT,
  created_at TEXT NOT NULL,
  UNIQUE(session_id, seq)
);
CREATE INDEX IF NOT EXISTS idx_events_session_seq ON events(session_id, seq);
CREATE INDEX IF NOT EXISTS idx_events_session_type ON events(session_id, type);

CREATE TABLE IF NOT EXISTS embeddings (
  model TEXT NOT NULL,
  hash TEXT NOT NULL,
  vector BLOB NOT NULL,
  PRIMARY KEY(model, hash)
);
