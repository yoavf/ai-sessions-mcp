-- Session cache with metadata
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    project_path TEXT NOT NULL,
    file_path TEXT NOT NULL,
    first_message TEXT,
    summary TEXT,
    timestamp INTEGER NOT NULL,
    last_indexed INTEGER NOT NULL,
    file_mtime INTEGER NOT NULL,  -- Track file modification time
    doc_length INTEGER DEFAULT 0,  -- Total tokens for BM25
    content TEXT                    -- Full session content for snippet extraction
);

CREATE INDEX IF NOT EXISTS idx_sessions_source ON sessions(source);
CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_path);
CREATE INDEX IF NOT EXISTS idx_sessions_timestamp ON sessions(timestamp DESC);

-- Inverted index for fast keyword lookup
CREATE TABLE IF NOT EXISTS term_index (
    term TEXT NOT NULL,
    session_id TEXT NOT NULL,
    term_frequency INTEGER NOT NULL,
    PRIMARY KEY (term, session_id),
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_term_index_term ON term_index(term);

-- Global statistics for BM25
CREATE TABLE IF NOT EXISTS search_stats (
    key TEXT PRIMARY KEY,
    value REAL NOT NULL
);

-- Insert default stats
INSERT OR IGNORE INTO search_stats (key, value) VALUES ('total_docs', 0);
INSERT OR IGNORE INTO search_stats (key, value) VALUES ('avg_doc_length', 0);
