package projection

const schemaVersion = 1

const schemaSQL = `
CREATE TABLE IF NOT EXISTS meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS chain_tips (
    ref_name TEXT PRIMARY KEY,
    tip TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS code_tips (
    ref_name TEXT PRIMARY KEY,
    tip TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS ops (
    op_id TEXT PRIMARY KEY,
    object_id TEXT NOT NULL,
    object_type TEXT NOT NULL,
    op_type TEXT NOT NULL,
    op_version INTEGER NOT NULL,
    parents TEXT NOT NULL,
    author_name TEXT NOT NULL,
    author_email TEXT NOT NULL,
    author_time INTEGER NOT NULL,
    author_tz TEXT NOT NULL,
    committer_name TEXT NOT NULL,
    committer_email TEXT NOT NULL,
    committer_time INTEGER NOT NULL,
    committer_tz TEXT NOT NULL,
    message TEXT NOT NULL,
    signature TEXT,
    payload BLOB NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ops_object_id ON ops(object_id);

CREATE TABLE IF NOT EXISTS objects (
    object_id TEXT PRIMARY KEY,
    object_type TEXT NOT NULL,
    op_count INTEGER NOT NULL,
    last_op_id TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS unknown_ops (
    object_id TEXT NOT NULL,
    op_id TEXT NOT NULL,
    op_type TEXT NOT NULL,
    op_version INTEGER NOT NULL,
    PRIMARY KEY (object_id, op_id)
);
CREATE INDEX IF NOT EXISTS idx_unknown_ops_object_id ON unknown_ops(object_id);

CREATE TABLE IF NOT EXISTS reviews (
    object_id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    status TEXT NOT NULL,
    merge_commit TEXT NOT NULL,
    reason TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS review_revisions (
    review_object_id TEXT NOT NULL,
    revision_index INTEGER NOT NULL,
    base TEXT NOT NULL,
    head TEXT NOT NULL,
    PRIMARY KEY (review_object_id, revision_index)
);
CREATE INDEX IF NOT EXISTS idx_review_revisions_review_object_id ON review_revisions(review_object_id);

CREATE TABLE IF NOT EXISTS approvals (
    review_object_id TEXT NOT NULL,
    subject TEXT NOT NULL,
    revision TEXT NOT NULL,
    verdict TEXT NOT NULL,
    message TEXT NOT NULL,
    PRIMARY KEY (review_object_id, subject, revision)
);
CREATE INDEX IF NOT EXISTS idx_approvals_review_object_id ON approvals(review_object_id);

CREATE TABLE IF NOT EXISTS ci_statuses (
    review_object_id TEXT NOT NULL,
    revision TEXT NOT NULL,
    name TEXT NOT NULL,
    state TEXT NOT NULL,
    url TEXT NOT NULL,
    description TEXT NOT NULL,
    started_at TEXT NOT NULL,
    completed_at TEXT NOT NULL,
    external_id TEXT NOT NULL,
    PRIMARY KEY (review_object_id, revision, name)
);
CREATE INDEX IF NOT EXISTS idx_ci_statuses_review_object_id ON ci_statuses(review_object_id);

CREATE TABLE IF NOT EXISTS comments (
    object_id TEXT PRIMARY KEY,
    subject_type TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    text TEXT NOT NULL,
    in_reply_to TEXT NOT NULL,
    anchor TEXT NOT NULL,
    deleted INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS anchor_resolutions (
    comment_object_id TEXT NOT NULL,
    target_commit TEXT NOT NULL,
    side TEXT NOT NULL,
    outcome TEXT NOT NULL,
    match TEXT NOT NULL,
    path TEXT NOT NULL,
    start_line INTEGER NOT NULL,
    end_line INTEGER NOT NULL,
    reason TEXT NOT NULL,
    PRIMARY KEY (comment_object_id, target_commit, side)
);
CREATE INDEX IF NOT EXISTS idx_anchor_resolutions_comment ON anchor_resolutions(comment_object_id);
CREATE INDEX IF NOT EXISTS idx_anchor_resolutions_target ON anchor_resolutions(target_commit);
`
