-- +goose Up
CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  key TEXT NOT NULL COLLATE NOCASE UNIQUE
    CHECK (length(key) BETWEEN 2 AND 8 AND key NOT GLOB '*[^A-Z0-9]*'
      AND substr(key, 1, 1) GLOB '[A-Z]'),
  name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 200),
  description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 100000),
  state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'archived')),
  revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
  next_ticket_number INTEGER NOT NULL DEFAULT 1 CHECK (next_ticket_number > 0),
  inserted_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE tickets (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  number INTEGER NOT NULL CHECK (number > 0),
  title TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 500),
  description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 100000),
  status TEXT NOT NULL DEFAULT 'triage'
    CHECK (status IN ('triage', 'backlog', 'ready', 'in_progress', 'done', 'canceled')),
  priority TEXT NOT NULL DEFAULT 'none'
    CHECK (priority IN ('none', 'low', 'medium', 'high', 'urgent')),
  assignee TEXT NOT NULL DEFAULT 'unassigned'
    CHECK (assignee IN ('unassigned', 'me', 'codex')),
  revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
  parent_ticket_id TEXT REFERENCES tickets(id) ON DELETE RESTRICT,
  inserted_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(project_id, number),
  CHECK (parent_ticket_id IS NULL OR parent_ticket_id <> id)
);

CREATE INDEX tickets_project_id_status_idx ON tickets(project_id, status);
CREATE INDEX tickets_project_id_assignee_idx ON tickets(project_id, assignee);
CREATE INDEX tickets_parent_ticket_id_idx ON tickets(parent_ticket_id);

CREATE TABLE labels (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  name TEXT NOT NULL COLLATE NOCASE
    CHECK (length(trim(name)) BETWEEN 1 AND 100),
  inserted_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(project_id, name)
);

CREATE TABLE ticket_labels (
  ticket_id TEXT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
  label_id TEXT NOT NULL REFERENCES labels(id) ON DELETE RESTRICT,
  PRIMARY KEY(ticket_id, label_id)
);

CREATE INDEX labels_project_id_name_idx ON labels(project_id, name);
CREATE INDEX ticket_labels_label_id_idx ON ticket_labels(label_id);

CREATE TABLE ticket_dependencies (
  blocker_ticket_id TEXT NOT NULL REFERENCES tickets(id) ON DELETE RESTRICT,
  blocked_ticket_id TEXT NOT NULL REFERENCES tickets(id) ON DELETE RESTRICT,
  inserted_at TEXT NOT NULL,
  PRIMARY KEY(blocker_ticket_id, blocked_ticket_id),
  CHECK (blocker_ticket_id <> blocked_ticket_id)
);

CREATE INDEX ticket_dependencies_blocked_ticket_id_idx
  ON ticket_dependencies(blocked_ticket_id);

CREATE TABLE comments (
  id TEXT PRIMARY KEY,
  ticket_id TEXT NOT NULL REFERENCES tickets(id) ON DELETE RESTRICT,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  body TEXT NOT NULL CHECK (length(trim(body)) BETWEEN 1 AND 100000),
  actor TEXT NOT NULL CHECK (actor IN ('me', 'codex', 'system')),
  inserted_at TEXT NOT NULL
);

CREATE INDEX comments_ticket_id_inserted_at_idx
  ON comments(ticket_id, inserted_at, id);

CREATE TABLE attachments (
  id TEXT PRIMARY KEY,
  ticket_id TEXT NOT NULL REFERENCES tickets(id) ON DELETE RESTRICT,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  original_filename TEXT NOT NULL,
  media_type TEXT NOT NULL,
  byte_size INTEGER NOT NULL CHECK (byte_size >= 0),
  sha256 TEXT NOT NULL,
  managed_path TEXT NOT NULL UNIQUE,
  actor TEXT NOT NULL CHECK (actor IN ('me', 'codex', 'system')),
  inserted_at TEXT NOT NULL
);

CREATE INDEX attachments_ticket_id_inserted_at_idx
  ON attachments(ticket_id, inserted_at, id);

CREATE TABLE activity_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_type TEXT NOT NULL,
  actor TEXT NOT NULL CHECK (actor IN ('me', 'codex', 'system')),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  ticket_id TEXT REFERENCES tickets(id) ON DELETE RESTRICT,
  payload TEXT NOT NULL DEFAULT '{}',
  inserted_at TEXT NOT NULL
);

CREATE INDEX activity_events_project_id_id_idx
  ON activity_events(project_id, id);
CREATE INDEX activity_events_ticket_id_id_idx
  ON activity_events(ticket_id, id);

-- +goose Down
DROP TABLE activity_events;
DROP TABLE attachments;
DROP TABLE comments;
DROP TABLE ticket_dependencies;
DROP TABLE ticket_labels;
DROP TABLE labels;
DROP TABLE tickets;
DROP TABLE projects;
