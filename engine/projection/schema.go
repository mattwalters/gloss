package projection

const schemaVersion = 2

var projectionTables = []string{
	"meta",
	"chain_tips",
	"code_tips",
	"ops",
	"objects",
	"unknown_ops",
	"reviews",
	"review_revisions",
	"approvals",
	"ci_statuses",
	"comments",
	"anchor_resolutions",
	"issues",
	"issue_assignees",
	"issue_labels",
	"issue_links",
	"projects",
	"project_issues",
	"cycles",
	"cycle_issues",
}

var tableQueries = map[string]string{
	"meta":               "SELECT * FROM meta ORDER BY key ASC",
	"chain_tips":         "SELECT * FROM chain_tips ORDER BY ref_name ASC",
	"code_tips":          "SELECT * FROM code_tips ORDER BY ref_name ASC",
	"ops":                "SELECT * FROM ops ORDER BY op_id ASC",
	"objects":            "SELECT * FROM objects ORDER BY object_id ASC",
	"unknown_ops":        "SELECT * FROM unknown_ops ORDER BY object_id ASC, op_index ASC",
	"reviews":            "SELECT * FROM reviews ORDER BY object_id ASC",
	"review_revisions":   "SELECT * FROM review_revisions ORDER BY review_object_id ASC, revision_index ASC",
	"approvals":          "SELECT * FROM approvals ORDER BY review_object_id ASC, subject ASC, revision ASC",
	"ci_statuses":        "SELECT * FROM ci_statuses ORDER BY review_object_id ASC, revision ASC, name ASC",
	"comments":           "SELECT * FROM comments ORDER BY object_id ASC",
	"anchor_resolutions": "SELECT * FROM anchor_resolutions ORDER BY comment_object_id ASC, target_commit ASC, side ASC",
	"issues":             "SELECT * FROM issues ORDER BY object_id ASC",
	"issue_assignees":    "SELECT * FROM issue_assignees ORDER BY issue_object_id ASC, assignee ASC",
	"issue_labels":       "SELECT * FROM issue_labels ORDER BY issue_object_id ASC, label ASC",
	"issue_links":        "SELECT * FROM issue_links ORDER BY issue_object_id ASC, target ASC",
	"projects":           "SELECT * FROM projects ORDER BY object_id ASC",
	"project_issues":     "SELECT * FROM project_issues ORDER BY project_object_id ASC, issue ASC",
	"cycles":             "SELECT * FROM cycles ORDER BY object_id ASC",
	"cycle_issues":       "SELECT * FROM cycle_issues ORDER BY cycle_object_id ASC, issue ASC",
}

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
    last_op_id TEXT NOT NULL,
    author_name TEXT NOT NULL,
    author_email TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_objects_author_email ON objects(author_email);
CREATE INDEX IF NOT EXISTS idx_objects_object_type ON objects(object_type);

CREATE TABLE IF NOT EXISTS unknown_ops (
    object_id TEXT NOT NULL,
    op_id TEXT NOT NULL,
    op_type TEXT NOT NULL,
    op_version INTEGER NOT NULL,
    op_index INTEGER NOT NULL DEFAULT 0,
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
CREATE INDEX IF NOT EXISTS idx_reviews_status ON reviews(status);

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
CREATE INDEX IF NOT EXISTS idx_comments_subject ON comments(subject_id);

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

CREATE TABLE IF NOT EXISTS issues (
    object_id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    state TEXT NOT NULL,
    reason TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_issues_state ON issues(state);

CREATE TABLE IF NOT EXISTS issue_assignees (
    issue_object_id TEXT NOT NULL,
    assignee TEXT NOT NULL,
    PRIMARY KEY (issue_object_id, assignee)
);
CREATE INDEX IF NOT EXISTS idx_issue_assignees_assignee ON issue_assignees(assignee);

CREATE TABLE IF NOT EXISTS issue_labels (
    issue_object_id TEXT NOT NULL,
    label TEXT NOT NULL,
    PRIMARY KEY (issue_object_id, label)
);
CREATE INDEX IF NOT EXISTS idx_issue_labels_label ON issue_labels(label);

CREATE TABLE IF NOT EXISTS issue_links (
    issue_object_id TEXT NOT NULL,
    target TEXT NOT NULL,
    target_type TEXT NOT NULL,
    relation TEXT NOT NULL,
    PRIMARY KEY (issue_object_id, target)
);

CREATE TABLE IF NOT EXISTS projects (
    object_id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    status TEXT NOT NULL,
    reason TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS project_issues (
    project_object_id TEXT NOT NULL,
    issue TEXT NOT NULL,
    PRIMARY KEY (project_object_id, issue)
);

CREATE TABLE IF NOT EXISTS cycles (
    object_id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    starts_at TEXT NOT NULL,
    ends_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS cycle_issues (
    cycle_object_id TEXT NOT NULL,
    issue TEXT NOT NULL,
    PRIMARY KEY (cycle_object_id, issue)
);
`
