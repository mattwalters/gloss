package writ_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"

	writ "github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/codec/canonicaljson"
	"github.com/writtendev/writ/engine/resolve"
	"github.com/writtendev/writ/engine/state"
	"github.com/writtendev/writ/spec"
)

// --------------------------------------------------------------------------
// 1. Helpers & Conversions
// --------------------------------------------------------------------------

var interestingStrings = []string{
	"",
	"   ",
	"\t\n\r",
	"simple",
	"MixedCaseString",
	"  leading space",
	"trailing space  ",
	"  both spaces  ",
	"日本語",
	"Ünïcodé",
	"email:alice@example.com",
	"email:  ALICE@example.com  ",
	"user:octocat",
	"user:  OctoCat  ",
	// NFC vs NFD unicode pair for "café"
	"\u0063\u0061\u0066\u00e9",     // NFC
	"\u0063\u0061\u0066\u00e5\u0301", // NFD
	// Cherokee fixed-points
	"\u13a0",
	"\uab70",
}

func isValidOpSet(ops []codec.Op) bool {
	if len(ops) == 0 {
		return true
	}
	objID := ops[0].ObjectID
	objType := ops[0].ObjectType
	if objID == "" || objType == "" {
		return false
	}
	seen := make(map[string]bool, len(ops))
	for _, o := range ops {
		if o.ID == "" || seen[o.ID] || o.ObjectID != objID || o.ObjectType != objType || o.OpVersion < 1 {
			return false
		}
		seen[o.ID] = true
		if len(o.Body) > 0 {
			var bm map[string]any
			if err := json.Unmarshal(o.Body, &bm); err != nil {
				return false
			}
		}
	}
	return true
}

func toSpecOps(ops []codec.Op) []spec.MergeOp {
	mergeOps := make([]spec.MergeOp, 0, len(ops))
	for _, o := range ops {
		var bm map[string]any
		if len(o.Body) > 0 {
			_ = json.Unmarshal(o.Body, &bm)
		}
		if bm == nil {
			bm = make(map[string]any)
		}
		mergeOps = append(mergeOps, spec.MergeOp{
			ID:         o.ID,
			Parents:    o.Parents,
			Time:       o.Author.When.UTC().Unix(),
			ObjectID:   o.ObjectID,
			ObjectType: o.ObjectType,
			OpType:     o.OpType,
			OpVersion:  o.OpVersion,
			Author: spec.MergeAuthor{
				Name:  o.Author.Name,
				Email: o.Author.Email,
			},
			Body: bm,
		})
	}
	return mergeOps
}

func toSpecRules(rules []writ.Rule) []spec.FieldRule {
	specRules := make([]spec.FieldRule, 0, len(rules))
	for _, r := range rules {
		var norm *spec.NormalizeRule
		if r.Normalize != nil {
			norm = &spec.NormalizeRule{
				Value: r.Normalize.Value,
				Items: r.Normalize.Items,
				Key:   r.Normalize.Key,
			}
		}
		specRules = append(specRules, spec.FieldRule{
			OpType:    r.OpType,
			OpVersion: r.OpVersion,
			Field:     r.Field,
			Target:    r.Target,
			Strategy:  r.Strategy,
			Key:       r.Key,
			Lattice:   r.Lattice,
			Normalize: norm,
		})
	}
	return specRules
}

func toCanonicalJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	canon, err := canonicaljson.Marshal(b)
	if err != nil {
		t.Fatalf("canonicaljson.Marshal failed: %v", err)
	}
	return canon
}

func stringVal(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case json.RawMessage:
		var str string
		if err := json.Unmarshal(s, &str); err == nil {
			return str
		}
		return string(s)
	case []byte:
		var str string
		if err := json.Unmarshal(s, &str); err == nil {
			return str
		}
		return string(s)
	}
	return ""
}

func boolVal(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case json.RawMessage:
		return string(b) == "true"
	case []byte:
		return string(b) == "true"
	}
	return false
}

func boolPtrVal(v any) *bool {
	switch b := v.(type) {
	case bool:
		return &b
	case json.RawMessage:
		val := (string(b) == "true")
		return &val
	case []byte:
		val := (string(b) == "true")
		return &val
	}
	return nil
}

func stringSlice(raw any) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		res := make([]string, 0, len(v))
		for _, it := range v {
			if s, ok := it.(string); ok {
				res = append(res, s)
			}
		}
		return res
	}
	return nil
}

type keyedLWWItem struct {
	Key   []string
	Value any
}

func parseKeyedLWW(raw any) []keyedLWWItem {
	slice, ok := raw.([]any)
	if !ok {
		return nil
	}
	var res []keyedLWWItem
	for _, item := range slice {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		var key []string
		switch ks := m["key"].(type) {
		case []string:
			key = ks
		case []any:
			for _, k := range ks {
				if s, ok := k.(string); ok {
					key = append(key, s)
				}
			}
		}
		res = append(res, keyedLWWItem{
			Key:   key,
			Value: m["value"],
		})
	}
	return res
}

func makeKeyedMap(items []keyedLWWItem) map[string]string {
	m := make(map[string]string, len(items))
	for _, it := range items {
		if len(it.Key) >= 2 {
			m[it.Key[0]+":"+it.Key[1]] = stringVal(it.Value)
		} else if len(it.Key) == 1 {
			m[it.Key[0]] = stringVal(it.Value)
		}
	}
	return m
}

func toStateUnknownOps(uops []writ.UnknownOp) []state.UnknownOp {
	if len(uops) == 0 {
		return nil
	}
	res := make([]state.UnknownOp, len(uops))
	for i, u := range uops {
		res[i] = state.UnknownOp(u)
	}
	return res
}

func commentRules() []writ.Rule {
	return []writ.Rule{
		{OpType: "create", OpVersion: 1, Field: "subject", Strategy: "create-once"},
		{OpType: "create", OpVersion: 1, Field: "text", Strategy: "lww"},
		{OpType: "create", OpVersion: 1, Field: "in_reply_to", Strategy: "create-once"},
		{OpType: "create", OpVersion: 1, Field: "anchor", Strategy: "create-once"},
		{OpType: "edit", OpVersion: 1, Field: "text", Strategy: "lww"},
		{OpType: "delete", OpVersion: 1, Field: "deleted", Strategy: "tombstone"},
		{OpType: "resolve", OpVersion: 1, Field: "resolved", Strategy: "lww"},
		{OpType: "resolve", OpVersion: 1, Field: "resolved_by", Strategy: "lww", Normalize: &writ.NormalizeRule{Value: "person"}},
	}
}

// --------------------------------------------------------------------------
// 2. Declared Shape Projections
// --------------------------------------------------------------------------

func projectReview(generic writ.ObjectState, ops []codec.Op, mode string) state.Review {
	rev := state.Review{
		Title:       stringVal(generic.State["title"]),
		Description: stringVal(generic.State["description"]),
		Status:      stringVal(generic.State["status"]),
		MergeCommit: stringVal(generic.State["merge_commit"]),
		Reason:      stringVal(generic.State["reason"]),
		UnknownOps:  toStateUnknownOps(generic.UnknownOps),
	}

	bases := stringSlice(generic.State["base"])
	heads := stringSlice(generic.State["head"])
	n := len(bases)
	if len(heads) < n {
		n = len(heads)
	}
	if n > 0 {
		revs := make([]state.Revision, n)
		for i := 0; i < n; i++ {
			revs[i] = state.Revision{Base: bases[i], Head: heads[i]}
		}
		rev.Revisions = revs
	}

	verdictItems := parseKeyedLWW(generic.State["verdict"])
	messageItems := parseKeyedLWW(generic.State["message"])
	msgMap := make(map[string]string)
	for _, it := range messageItems {
		if len(it.Key) >= 2 {
			msgMap[it.Key[0]+":"+it.Key[1]] = stringVal(it.Value)
		}
	}
	var approvals []state.Approval
	for _, it := range verdictItems {
		if len(it.Key) >= 2 {
			subj := it.Key[0]
			revision := it.Key[1]
			verdict := stringVal(it.Value)
			if verdict != "" && verdict != "none" {
				msg := msgMap[subj+":"+revision]
				approvals = append(approvals, state.Approval{
					Subject:  subj,
					Revision: revision,
					Verdict:  verdict,
					Message:  msg,
				})
			}
		}
	}
	sort.Slice(approvals, func(i, j int) bool {
		if approvals[i].Subject != approvals[j].Subject {
			return approvals[i].Subject < approvals[j].Subject
		}
		return approvals[i].Revision < approvals[j].Revision
	})
	rev.Approvals = approvals

	stateItems := parseKeyedLWW(generic.State["state"])
	urlMap := makeKeyedMap(parseKeyedLWW(generic.State["url"]))
	descMap := makeKeyedMap(parseKeyedLWW(generic.State["ci_description"]))
	startMap := makeKeyedMap(parseKeyedLWW(generic.State["started_at"]))
	compMap := makeKeyedMap(parseKeyedLWW(generic.State["completed_at"]))
	extMap := makeKeyedMap(parseKeyedLWW(generic.State["external_id"]))
	var ciStatuses []state.CIStatus
	for _, it := range stateItems {
		if len(it.Key) >= 2 {
			revision := it.Key[0]
			name := it.Key[1]
			k := revision + ":" + name
			ciStatuses = append(ciStatuses, state.CIStatus{
				Revision:    revision,
				Name:        name,
				State:       stringVal(it.Value),
				URL:         urlMap[k],
				Description: descMap[k],
				StartedAt:   startMap[k],
				CompletedAt: compMap[k],
				ExternalID:  extMap[k],
			})
		}
	}
	sort.Slice(ciStatuses, func(i, j int) bool {
		if ciStatuses[i].Revision != ciStatuses[j].Revision {
			return ciStatuses[i].Revision < ciStatuses[j].Revision
		}
		return ciStatuses[i].Name < ciStatuses[j].Name
	})
	rev.CIStatuses = ciStatuses

	relItems := parseKeyedLWW(generic.State["relation"])
	targetTypeMap := make(map[string]string)
	for _, it := range parseKeyedLWW(generic.State["target_type"]) {
		if len(it.Key) >= 1 {
			targetTypeMap[it.Key[0]] = stringVal(it.Value)
		}
	}
	var links []state.Link
	for _, it := range relItems {
		if len(it.Key) >= 1 {
			target := it.Key[0]
			rel := stringVal(it.Value)
			if rel != "" && rel != "none" {
				links = append(links, state.Link{
					Target:     target,
					TargetType: targetTypeMap[target],
					Relation:   rel,
				})
			}
		}
	}
	sort.Slice(links, func(i, j int) bool {
		return links[i].Target < links[j].Target
	})
	rev.Links = links

	addItems := stringSlice(generic.State["add"])
	if mode == "assign" {
		rev.Assignees = addItems
	} else if mode == "label" {
		rev.Labels = addItems
	} else {
		hasAssign := false
		hasLabel := false
		for _, o := range ops {
			if o.OpType == "assign" {
				hasAssign = true
			}
			if o.OpType == "label" {
				hasLabel = true
			}
		}
		if hasAssign && !hasLabel {
			rev.Assignees = addItems
		} else if hasLabel && !hasAssign {
			rev.Labels = addItems
		}
	}

	return rev
}

func projectIssue(generic writ.ObjectState, ops []codec.Op, mode string) state.Issue {
	hasKnownOp := false
	for _, o := range ops {
		if o.ObjectType == "issue" && o.OpVersion == 1 {
			switch o.OpType {
			case "create", "update", "set-state", "assign", "label", "link":
				hasKnownOp = true
			}
		}
	}

	st := stringVal(generic.State["state"])
	if st == "" && hasKnownOp {
		st = "open"
	}

	iss := state.Issue{
		Title:       stringVal(generic.State["title"]),
		Description: stringVal(generic.State["description"]),
		State:       st,
		Reason:      stringVal(generic.State["reason"]),
		UnknownOps:  toStateUnknownOps(generic.UnknownOps),
	}

	relItems := parseKeyedLWW(generic.State["relation"])
	targetTypeMap := make(map[string]string)
	for _, it := range parseKeyedLWW(generic.State["target_type"]) {
		if len(it.Key) >= 1 {
			targetTypeMap[it.Key[0]] = stringVal(it.Value)
		}
	}
	var links []state.Link
	for _, it := range relItems {
		if len(it.Key) >= 1 {
			target := it.Key[0]
			rel := stringVal(it.Value)
			if rel != "" && rel != "none" {
				links = append(links, state.Link{
					Target:     target,
					TargetType: targetTypeMap[target],
					Relation:   rel,
				})
			}
		}
	}
	sort.Slice(links, func(i, j int) bool {
		return links[i].Target < links[j].Target
	})
	iss.Links = links

	addItems := stringSlice(generic.State["add"])
	if mode == "assign" {
		iss.Assignees = addItems
	} else if mode == "label" {
		iss.Labels = addItems
	} else {
		hasAssign := false
		hasLabel := false
		for _, o := range ops {
			if o.OpType == "assign" {
				hasAssign = true
			}
			if o.OpType == "label" {
				hasLabel = true
			}
		}
		if hasAssign && !hasLabel {
			iss.Assignees = addItems
		} else if hasLabel && !hasAssign {
			iss.Labels = addItems
		}
	}

	return iss
}

func projectComment(generic writ.ObjectState, _ []codec.Op) state.Comment {
	c := state.Comment{
		Text:       stringVal(generic.State["text"]),
		InReplyTo:  stringVal(generic.State["in_reply_to"]),
		Deleted:    boolVal(generic.State["deleted"]),
		Resolved:   boolPtrVal(generic.State["resolved"]),
		ResolvedBy: state.NormalizePerson(stringVal(generic.State["resolved_by"])),
		UnknownOps: toStateUnknownOps(generic.UnknownOps),
	}

	if raw, ok := generic.State["subject"].(json.RawMessage); ok && len(raw) > 0 && string(raw) != "null" {
		sub, err := state.ParseCommentSubject(raw)
		if err == nil {
			c.Subject = sub
		}
	} else if m, ok := generic.State["subject"].(map[string]any); ok {
		raw, _ := json.Marshal(m)
		sub, err := state.ParseCommentSubject(raw)
		if err == nil {
			c.Subject = sub
		}
	}

	if raw, ok := generic.State["anchor"].(json.RawMessage); ok && len(raw) > 0 && string(raw) != "null" {
		anc, err := resolve.ParseAnchor(raw)
		if err == nil {
			c.Anchor = &anc
		}
	} else if m, ok := generic.State["anchor"].(map[string]any); ok {
		raw, _ := json.Marshal(m)
		anc, err := resolve.ParseAnchor(raw)
		if err == nil {
			c.Anchor = &anc
		}
	}

	return c
}

func projectRepo(generic writ.ObjectState, _ []codec.Op) state.RepoEntry {
	return state.RepoEntry{
		RepoID:      generic.ObjectID,
		Slug:        stringVal(generic.State["slug"]),
		IsWorkspace: boolVal(generic.State["is_workspace"]),
		Remotes:     stringSlice(generic.State["remote"]),
		UnknownOps:  toStateUnknownOps(generic.UnknownOps),
	}
}

func projectProject(generic writ.ObjectState, _ []codec.Op) state.Project {
	return state.Project{
		Title:       stringVal(generic.State["title"]),
		Description: stringVal(generic.State["description"]),
		Status:      stringVal(generic.State["status"]),
		Reason:      stringVal(generic.State["reason"]),
		Issues:      stringSlice(generic.State["issue"]),
		UnknownOps:  toStateUnknownOps(generic.UnknownOps),
	}
}

func projectCycle(generic writ.ObjectState, _ []codec.Op) state.Cycle {
	return state.Cycle{
		Title:       stringVal(generic.State["title"]),
		Description: stringVal(generic.State["description"]),
		StartsAt:    stringVal(generic.State["starts_at"]),
		EndsAt:      stringVal(generic.State["ends_at"]),
		Issues:      stringSlice(generic.State["issue"]),
		UnknownOps:  toStateUnknownOps(generic.UnknownOps),
	}
}

// --------------------------------------------------------------------------
// 3. Three-Way Assertions
// --------------------------------------------------------------------------

func assertUnknownOpsParity(t *testing.T, writU []writ.UnknownOp, specU []spec.UnknownOp) {
	t.Helper()
	if len(writU) != len(specU) {
		t.Fatalf("unknown ops length mismatch: writ=%d, spec=%d", len(writU), len(specU))
	}
	for i, u := range writU {
		su := specU[i]
		if u.Commit != su.Commit || u.ObjectType != su.ObjectType || u.OpType != su.OpType || u.OpVersion != su.OpVersion {
			t.Fatalf("unknown op mismatch at %d: writ=%+v, spec=%+v", i, u, su)
		}
	}
}

func assertTypedUnknownOpsParity(t *testing.T, typedU []state.UnknownOp, writU []writ.UnknownOp) {
	t.Helper()
	if len(typedU) != len(writU) {
		t.Fatalf("typed unknown ops length mismatch: typed=%d, writ=%d", len(typedU), len(writU))
	}
	for i, u := range typedU {
		wu := writU[i]
		if u.Commit != wu.Commit || u.ObjectType != wu.ObjectType || u.OpType != wu.OpType || u.OpVersion != wu.OpVersion {
			t.Fatalf("typed unknown op mismatch at %d: typed=%+v, writ=%+v", i, u, wu)
		}
	}
}

func assertTotalOrderMatchesSpec(t *testing.T, ops []codec.Op, writRefs []writ.OpRef) {
	t.Helper()
	if len(ops) == 0 {
		return
	}
	orderOps := make([]spec.OrderOp, len(ops))
	for i, o := range ops {
		orderOps[i] = spec.OrderOp{
			ID:       o.ID,
			Parents:  o.Parents,
			Time:     o.Author.When.UTC().Unix(),
			ObjectID: o.ObjectID,
		}
	}
	specOrder, err := spec.TotalOrder(orderOps, ops[0].ObjectID)
	if err != nil {
		t.Fatalf("spec.TotalOrder failed: %v", err)
	}
	if len(writRefs) != len(specOrder) {
		t.Fatalf("total order length mismatch: writ=%d, spec=%d", len(writRefs), len(specOrder))
	}
	for i, ref := range writRefs {
		if ref.Commit != specOrder[i] {
			t.Fatalf("total order mismatch at %d: writ=%s, spec=%s", i, ref.Commit, specOrder[i])
		}
	}
}

func assertThreeWayFoldAbstract(t *testing.T, ops []codec.Op, rules []writ.Rule) {
	t.Helper()
	writRes, writErr := writ.Fold(ops, rules)
	mergeOps := toSpecOps(ops)
	specRules := toSpecRules(rules)
	specRes, specErr := spec.Fold(mergeOps, specRules)

	if (writErr != nil) != (specErr != nil) {
		t.Fatalf("error parity mismatch: writErr=%v, specErr=%v", writErr, specErr)
	}
	if writErr != nil {
		return
	}

	// 1. Total order
	assertTotalOrderMatchesSpec(t, ops, writRes.TotalOrder)

	// 2. Unknown ops
	assertUnknownOpsParity(t, writRes.UnknownOps, specRes.UnknownOps)

	// 3. State canonical JSON byte equality
	writJSON := toCanonicalJSON(t, writRes.State)
	specJSON := toCanonicalJSON(t, specRes.State)
	if !bytes.Equal(writJSON, specJSON) {
		t.Fatalf("canonical JSON mismatch between writ.Fold and spec.Fold:\n writ: %s\n spec: %s", string(writJSON), string(specJSON))
	}
}

func assertThreeWayFoldDomain(t *testing.T, objectType string, ops []codec.Op, rules []writ.Rule, mode string) {
	t.Helper()
	writRes, writErr := writ.Fold(ops, rules)
	mergeOps := toSpecOps(ops)
	specRules := toSpecRules(rules)
	specRes, specErr := spec.Fold(mergeOps, specRules)

	if (writErr != nil) != (specErr != nil) {
		t.Fatalf("error parity mismatch: writErr=%v, specErr=%v", writErr, specErr)
	}
	if writErr != nil {
		return
	}

	// 1. Total order
	assertTotalOrderMatchesSpec(t, ops, writRes.TotalOrder)

	// 2. Unknown ops between generic folds
	assertUnknownOpsParity(t, writRes.UnknownOps, specRes.UnknownOps)

	// 3. State canonical JSON byte equality between writ.Fold and spec.Fold
	writJSON := toCanonicalJSON(t, writRes.State)
	specJSON := toCanonicalJSON(t, specRes.State)
	if !bytes.Equal(writJSON, specJSON) {
		t.Fatalf("generic state canonical JSON mismatch:\n writ: %s\n spec: %s", string(writJSON), string(specJSON))
	}

	// 4. Domain object typed reducer comparison
	switch objectType {
	case "review":
		typed, err := writ.FoldReview(ops)
		if err != nil {
			t.Fatalf("FoldReview failed: %v", err)
		}
		projected := projectReview(writRes, ops, mode)
		projJSON := toCanonicalJSON(t, projected)
		typedJSON := toCanonicalJSON(t, typed)
		if !bytes.Equal(projJSON, typedJSON) {
			t.Fatalf("Review canonical JSON mismatch:\n proj:  %s\n typed: %s", string(projJSON), string(typedJSON))
		}
		assertTypedUnknownOpsParity(t, typed.UnknownOps, writRes.UnknownOps)

	case "issue":
		typed, err := writ.FoldIssue(ops)
		if err != nil {
			t.Fatalf("FoldIssue failed: %v", err)
		}
		projected := projectIssue(writRes, ops, mode)
		projJSON := toCanonicalJSON(t, projected)
		typedJSON := toCanonicalJSON(t, typed)
		if !bytes.Equal(projJSON, typedJSON) {
			t.Fatalf("Issue canonical JSON mismatch:\n proj:  %s\n typed: %s", string(projJSON), string(typedJSON))
		}
		assertTypedUnknownOpsParity(t, typed.UnknownOps, writRes.UnknownOps)

	case "comment":
		typed, err := writ.FoldComment(ops)
		if err != nil {
			t.Fatalf("FoldComment failed: %v", err)
		}
		projected := projectComment(writRes, ops)
		projJSON := toCanonicalJSON(t, projected)
		typedJSON := toCanonicalJSON(t, typed)
		if !bytes.Equal(projJSON, typedJSON) {
			t.Fatalf("Comment canonical JSON mismatch:\n proj:  %s\n typed: %s", string(projJSON), string(typedJSON))
		}
		assertTypedUnknownOpsParity(t, typed.UnknownOps, writRes.UnknownOps)

	case "repo":
		typed, err := state.FoldRepo(ops)
		if err != nil {
			t.Fatalf("FoldRepo failed: %v", err)
		}
		projected := projectRepo(writRes, ops)
		projJSON := toCanonicalJSON(t, projected)
		typedJSON := toCanonicalJSON(t, typed)
		if !bytes.Equal(projJSON, typedJSON) {
			t.Fatalf("RepoEntry canonical JSON mismatch:\n proj:  %s\n typed: %s", string(projJSON), string(typedJSON))
		}
		assertTypedUnknownOpsParity(t, typed.UnknownOps, writRes.UnknownOps)

	case "project":
		typed, err := writ.FoldProject(ops)
		if err != nil {
			t.Fatalf("FoldProject failed: %v", err)
		}
		projected := projectProject(writRes, ops)
		projJSON := toCanonicalJSON(t, projected)
		typedJSON := toCanonicalJSON(t, typed)
		if !bytes.Equal(projJSON, typedJSON) {
			t.Fatalf("Project canonical JSON mismatch:\n proj:  %s\n typed: %s", string(projJSON), string(typedJSON))
		}
		assertTypedUnknownOpsParity(t, typed.UnknownOps, writRes.UnknownOps)

	case "cycle":
		typed, err := writ.FoldCycle(ops)
		if err != nil {
			t.Fatalf("FoldCycle failed: %v", err)
		}
		projected := projectCycle(writRes, ops)
		projJSON := toCanonicalJSON(t, projected)
		typedJSON := toCanonicalJSON(t, typed)
		if !bytes.Equal(projJSON, typedJSON) {
			t.Fatalf("Cycle canonical JSON mismatch:\n proj:  %s\n typed: %s", string(projJSON), string(typedJSON))
		}
		assertTypedUnknownOpsParity(t, typed.UnknownOps, writRes.UnknownOps)
	}
}

// --------------------------------------------------------------------------
// 4. DAG & Operation Stream Generator
// --------------------------------------------------------------------------

func randomValue(rng *rand.Rand, depth int) any {
	if depth > 2 {
		return interestingStrings[rng.Intn(len(interestingStrings))]
	}
	switch rng.Intn(8) {
	case 0:
		return interestingStrings[rng.Intn(len(interestingStrings))]
	case 1:
		return rng.Intn(1000)
	case 2:
		return math.Round(rng.Float64()*1000) / 10
	case 3:
		return rng.Intn(2) == 1
	case 4:
		m := make(map[string]any)
		n := rng.Intn(3) + 1
		for i := 0; i < n; i++ {
			k := fmt.Sprintf("k%d", i)
			m[k] = randomValue(rng, depth+1)
		}
		return m
	case 5:
		n := rng.Intn(3)
		arr := make([]any, n)
		for i := 0; i < n; i++ {
			arr[i] = randomValue(rng, depth+1)
		}
		return arr
	case 6:
		return nil
	default:
		return "test-val"
	}
}

func generateDAGSkeleton(rng *rand.Rand, numOps int, objectID, objectType string) []codec.Op {
	numWriters := 1 + rng.Intn(5) // 1 to 5 writer chains
	writerTips := make([]string, numWriters)
	allOps := make([]codec.Op, 0, numOps)
	seenIDs := make(map[string]bool)

	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	forceIdenticalTime := rng.Intn(3) == 0
	commonTime := baseTime + 100

	for i := 0; i < numOps; i++ {
		var id string
		for {
			b := make([]byte, 20)
			rng.Read(b)
			id = hex.EncodeToString(b)
			if !seenIDs[id] {
				seenIDs[id] = true
				break
			}
		}

		w := rng.Intn(numWriters)
		var parents []string
		if writerTips[w] != "" {
			parents = append(parents, writerTips[w])
		}

		// 0 to 2 causal parents from earlier ops (acyclic)
		if len(allOps) > 0 && rng.Intn(3) > 0 {
			randIdx := rng.Intn(len(allOps))
			pID := allOps[randIdx].ID
			already := false
			for _, p := range parents {
				if p == pID {
					already = true
					break
				}
			}
			if !already {
				parents = append(parents, pID)
			}
		}

		writerTips[w] = id

		var opTime int64
		if forceIdenticalTime && rng.Intn(2) == 0 {
			opTime = commonTime // Simultaneous timestamp forcing alphabetical SHA tiebreak
		} else {
			opTime = baseTime + int64(rng.Intn(1000))
		}

		op := codec.Op{
			ID:      id,
			Parents: parents,
			Author: codec.Identity{
				Name:  fmt.Sprintf("Writer-%d", w),
				Email: fmt.Sprintf("w%d@example.test", w),
				When:  time.Unix(opTime, 0).UTC(),
			},
			Envelope: codec.Envelope{
				ObjectID:   objectID,
				ObjectType: objectType,
				OpVersion:  1,
			},
		}
		allOps = append(allOps, op)
	}

	return allOps
}

func generateAbstractSyntheticStream(rng *rand.Rand) ([]codec.Op, []writ.Rule) {
	numOps := 5 + rng.Intn(15)
	ops := generateDAGSkeleton(rng, numOps, "obj-synthetic", "synthetic")

	rules := []writ.Rule{
		{OpType: "op", OpVersion: 1, Field: "field_lww", Strategy: "lww"},
		{OpType: "op", OpVersion: 1, Field: "field_create_once", Strategy: "create-once"},
		{OpType: "op", OpVersion: 1, Field: "field_set_union", Strategy: "set-union"},
		{OpType: "op", OpVersion: 1, Field: "add", Strategy: "set-observed-remove"},
		{OpType: "op", OpVersion: 1, Field: "remove", Strategy: "set-observed-remove"},
		{OpType: "op", OpVersion: 1, Field: "field_append", Strategy: "append"},
		{OpType: "op", OpVersion: 1, Field: "deleted", Strategy: "tombstone"},
		{OpType: "op", OpVersion: 1, Field: "field_lattice", Strategy: "lattice", Lattice: []string{"draft", "review", "approved", "merged"}},
		{OpType: "op", OpVersion: 1, Field: "field_keyed", Strategy: "keyed-lww", Key: []string{"k1", "k2"}},
	}

	latticeVals := []string{"draft", "review", "approved", "merged", "unknown_lattice"}

	for i := range ops {
		// Forward-compatibility test: unknown op_type, future op_version, or unrecognized fields
		isUnknown := rng.Intn(10) == 0
		if isUnknown {
			ops[i].OpType = "future-type"
			ops[i].OpVersion = int64(2 + rng.Intn(5))
			body := map[string]any{
				"unrecognized_field": randomValue(rng, 1),
			}
			ops[i].Body, _ = json.Marshal(body)
			continue
		}

		ops[i].OpType = "op"
		body := make(map[string]any)

		// LWW
		if rng.Intn(2) == 0 {
			body["field_lww"] = randomValue(rng, 0)
		}
		// Create-once
		if rng.Intn(2) == 0 {
			body["field_create_once"] = interestingStrings[rng.Intn(len(interestingStrings))]
		}
		// Set-union (strings, empty strings, arrays of strings)
		if rng.Intn(2) == 0 {
			if rng.Intn(2) == 0 {
				body["field_set_union"] = interestingStrings[rng.Intn(len(interestingStrings))]
			} else {
				items := []string{
					interestingStrings[rng.Intn(len(interestingStrings))],
					interestingStrings[rng.Intn(len(interestingStrings))],
				}
				body["field_set_union"] = items
			}
		}
		// Set-observed-remove (add / remove, flat and nested shapes, causal vs concurrent)
		if rng.Intn(2) == 0 {
			item := fmt.Sprintf("item-%d", rng.Intn(5))
			if rng.Intn(2) == 0 {
				body["add"] = item
			} else {
				body["remove"] = item
			}
		}
		// Append
		if rng.Intn(2) == 0 {
			if rng.Intn(3) == 0 {
				body["field_append"] = []string{} // empty array append preserves []
			} else {
				body["field_append"] = interestingStrings[rng.Intn(len(interestingStrings))]
			}
		}
		// Tombstone
		if rng.Intn(4) == 0 {
			body["deleted"] = (rng.Intn(2) == 1)
		}
		// Lattice
		if rng.Intn(2) == 0 {
			body["field_lattice"] = latticeVals[rng.Intn(len(latticeVals))]
		}
		// Keyed-LWW
		if rng.Intn(2) == 0 {
			body["k1"] = fmt.Sprintf("group-%d", rng.Intn(3))
			body["k2"] = fmt.Sprintf("sub-%d", rng.Intn(3))
			body["field_keyed"] = interestingStrings[rng.Intn(len(interestingStrings))]
		}

		ops[i].Body, _ = json.Marshal(body)
	}

	return ops, rules
}

func generateReviewStream(rng *rand.Rand) ([]codec.Op, []writ.Rule, string) {
	numOps := 6 + rng.Intn(12)
	ops := generateDAGSkeleton(rng, numOps, "r-property", "review")
	rules := writ.ReviewRules()
	mode := "label"
	if rng.Intn(2) == 0 {
		mode = "assign"
	}

	// First op: create
	createBody := map[string]any{
		"title":       "Initial Property Review Title",
		"description": "Initial description",
	}
	ops[0].OpType = "create"
	ops[0].Body, _ = json.Marshal(createBody)

	for i := 1; i < len(ops); i++ {
		// Forward-compatibility test
		if rng.Intn(12) == 0 {
			ops[i].OpType = "custom-review-op"
			ops[i].OpVersion = 2
			ops[i].Body, _ = json.Marshal(map[string]any{"extra": "val"})
			continue
		}

		switch rng.Intn(7) {
		case 0:
			// update
			ops[i].OpType = "update"
			ops[i].Body, _ = json.Marshal(map[string]any{
				"title":       interestingStrings[rng.Intn(len(interestingStrings))],
				"description": interestingStrings[rng.Intn(len(interestingStrings))],
			})
		case 1:
			// set-status
			ops[i].OpType = "set-status"
			ops[i].Body, _ = json.Marshal(map[string]any{
				"status":       []string{"open", "merged", "closed"}[rng.Intn(3)],
				"merge_commit": "abcdef1234567890abcdef1234567890abcdef12",
				"reason":       interestingStrings[rng.Intn(len(interestingStrings))],
			})
		case 2:
			// revision
			ops[i].OpType = "revision"
			ops[i].Body, _ = json.Marshal(map[string]any{
				"base": fmt.Sprintf("base-%d", i),
				"head": fmt.Sprintf("head-%d", i),
			})
		case 3:
			// approval
			ops[i].OpType = "approval"
			subj := ""
			if rng.Intn(3) > 0 {
				subj = interestingStrings[rng.Intn(len(interestingStrings))]
			}
			verdict := []string{"approve", "request-changes", "none", ""}[rng.Intn(4)]
			b := map[string]any{
				"revision": "head-1",
				"verdict":  verdict,
				"message":  interestingStrings[rng.Intn(len(interestingStrings))],
			}
			if subj != "" {
				b["subject"] = subj
			}
			ops[i].Body, _ = json.Marshal(b)
		case 4:
			// ci-status
			ops[i].OpType = "ci-status"
			ops[i].Body, _ = json.Marshal(map[string]any{
				"revision":    "head-1",
				"name":        fmt.Sprintf("check-%d", rng.Intn(3)),
				"state":       []string{"pending", "success", "failure"}[rng.Intn(3)],
				"url":         "https://ci.example.com",
				"description": "ci run",
			})
		case 5:
			// link
			ops[i].OpType = "link"
			ops[i].Body, _ = json.Marshal(map[string]any{
				"target":      fmt.Sprintf("issue-%d", rng.Intn(3)),
				"target_type": "issue",
				"relation":    []string{"fixes", "relates-to", "none", ""}[rng.Intn(4)],
			})
		case 6:
			// label or assign (depending on mode)
			ops[i].OpType = mode
			item := fmt.Sprintf("tag-%d", rng.Intn(4))
			if mode == "assign" {
				item = fmt.Sprintf("email:user%d@example.com", rng.Intn(4))
			}
			b := make(map[string]any)
			if rng.Intn(2) == 0 {
				if rng.Intn(2) == 0 {
					b["add"] = item
				} else {
					b["add"] = []string{item, interestingStrings[rng.Intn(len(interestingStrings))]}
				}
			} else {
				if rng.Intn(2) == 0 {
					b["remove"] = item
				} else {
					b["remove"] = []string{item}
				}
			}
			ops[i].Body, _ = json.Marshal(b)
		}
	}

	return ops, rules, mode
}

func generateIssueStream(rng *rand.Rand) ([]codec.Op, []writ.Rule, string) {
	numOps := 5 + rng.Intn(10)
	ops := generateDAGSkeleton(rng, numOps, "iss-property", "issue")
	rules := writ.IssueRules()
	mode := "label"
	if rng.Intn(2) == 0 {
		mode = "assign"
	}

	ops[0].OpType = "create"
	ops[0].Body, _ = json.Marshal(map[string]any{
		"title":       "Issue Title",
		"description": "Issue Description",
	})

	for i := 1; i < len(ops); i++ {
		if rng.Intn(10) == 0 {
			ops[i].OpType = "unknown-issue-op"
			ops[i].OpVersion = 2
			ops[i].Body, _ = json.Marshal(map[string]any{"foo": "bar"})
			continue
		}

		switch rng.Intn(4) {
		case 0:
			ops[i].OpType = "update"
			ops[i].Body, _ = json.Marshal(map[string]any{
				"title":       interestingStrings[rng.Intn(len(interestingStrings))],
				"description": interestingStrings[rng.Intn(len(interestingStrings))],
			})
		case 1:
			ops[i].OpType = "set-state"
			ops[i].Body, _ = json.Marshal(map[string]any{
				"state":  []string{"open", "in-progress", "closed"}[rng.Intn(3)],
				"reason": interestingStrings[rng.Intn(len(interestingStrings))],
			})
		case 2:
			ops[i].OpType = "link"
			ops[i].Body, _ = json.Marshal(map[string]any{
				"target":      fmt.Sprintf("rev-%d", rng.Intn(3)),
				"target_type": "review",
				"relation":    []string{"fixed-by", "relates-to", "none"}[rng.Intn(3)],
			})
		case 3:
			ops[i].OpType = mode
			item := fmt.Sprintf("label-%d", rng.Intn(3))
			if mode == "assign" {
				item = fmt.Sprintf("email:dev%d@example.com", rng.Intn(3))
			}
			b := make(map[string]any)
			if rng.Intn(2) == 0 {
				b["add"] = []string{item, interestingStrings[rng.Intn(len(interestingStrings))]}
			} else {
				b["remove"] = []string{item}
			}
			ops[i].Body, _ = json.Marshal(b)
		}
	}

	return ops, rules, mode
}

func generateCommentStream(rng *rand.Rand) ([]codec.Op, []writ.Rule) {
	numOps := 4 + rng.Intn(8)
	ops := generateDAGSkeleton(rng, numOps, "c-property", "comment")
	rules := commentRules()

	ops[0].OpType = "create"
	ops[0].Body, _ = json.Marshal(map[string]any{
		"subject": map[string]any{
			"object_type": "review",
			"object_id":   "r-1",
		},
		"text": "Initial comment text",
		"anchor": map[string]any{
			"new": map[string]any{
				"commit": "1111111111111111111111111111111111111111",
				"path":   "main.go",
				"line":   10,
			},
		},
	})

	for i := 1; i < len(ops); i++ {
		switch rng.Intn(3) {
		case 0:
			// edit (exercising empty scalar write WRIT-125)
			ops[i].OpType = "edit"
			var txt string
			if rng.Intn(3) == 0 {
				txt = ""
			} else {
				txt = interestingStrings[rng.Intn(len(interestingStrings))]
			}
			ops[i].Body, _ = json.Marshal(map[string]any{"text": txt})
		case 1:
			// resolve (exercising whitespace-only actor WRIT-118)
			ops[i].OpType = "resolve"
			var actor string
			if rng.Intn(3) == 0 {
				actor = "   \t\n   "
			} else {
				actor = fmt.Sprintf("email:resolver%d@example.com", rng.Intn(3))
			}
			ops[i].Body, _ = json.Marshal(map[string]any{
				"resolved":    (rng.Intn(2) == 1),
				"resolved_by": actor,
			})
		case 2:
			// delete
			ops[i].OpType = "delete"
			ops[i].Body, _ = json.Marshal(map[string]any{"deleted": true})
		}
	}

	return ops, rules
}

func generateRepoStream(rng *rand.Rand) ([]codec.Op, []writ.Rule) {
	numOps := 4 + rng.Intn(6)
	ops := generateDAGSkeleton(rng, numOps, "repo-property", "repo")
	rules := state.RepoRules()

	ops[0].OpType = "create"
	ops[0].Body, _ = json.Marshal(map[string]any{
		"slug":         "writ",
		"is_workspace": (rng.Intn(2) == 1),
	})

	for i := 1; i < len(ops); i++ {
		if rng.Intn(2) == 0 {
			ops[i].OpType = "set-slug"
			ops[i].Body, _ = json.Marshal(map[string]any{
				"slug": fmt.Sprintf("repo-slug-%d", i),
			})
		} else {
			ops[i].OpType = "add-remote"
			ops[i].Body, _ = json.Marshal(map[string]any{
				"remote": []string{
					fmt.Sprintf("remote-%d", rng.Intn(4)),
					interestingStrings[rng.Intn(len(interestingStrings))],
				},
			})
		}
	}

	return ops, rules
}

func generateProjectStream(rng *rand.Rand) ([]codec.Op, []writ.Rule) {
	numOps := 4 + rng.Intn(6)
	ops := generateDAGSkeleton(rng, numOps, "proj-property", "project")
	rules := writ.ProjectRules()

	ops[0].OpType = "create"
	ops[0].Body, _ = json.Marshal(map[string]any{
		"title":       "Project v1",
		"description": "Project Desc",
	})

	for i := 1; i < len(ops); i++ {
		switch rng.Intn(3) {
		case 0:
			ops[i].OpType = "update"
			ops[i].Body, _ = json.Marshal(map[string]any{
				"title": interestingStrings[rng.Intn(len(interestingStrings))],
			})
		case 1:
			ops[i].OpType = "set-status"
			ops[i].Body, _ = json.Marshal(map[string]any{
				"status": []string{"planned", "in-progress", "completed"}[rng.Intn(3)],
				"reason": interestingStrings[rng.Intn(len(interestingStrings))],
			})
		case 2:
			if rng.Intn(2) == 0 {
				ops[i].OpType = "add-issue"
			} else {
				ops[i].OpType = "remove-issue"
			}
			ops[i].Body, _ = json.Marshal(map[string]any{
				"issue": fmt.Sprintf("issue-%d", rng.Intn(4)),
			})
		}
	}

	return ops, rules
}

func generateCycleStream(rng *rand.Rand) ([]codec.Op, []writ.Rule) {
	numOps := 4 + rng.Intn(6)
	ops := generateDAGSkeleton(rng, numOps, "cycle-property", "cycle")
	rules := writ.CycleRules()

	ops[0].OpType = "create"
	ops[0].Body, _ = json.Marshal(map[string]any{
		"title":       "Cycle 1",
		"description": "Cycle Desc",
		"starts_at":   "2026-09-01T00:00:00Z",
		"ends_at":     "2026-09-14T00:00:00Z",
	})

	for i := 1; i < len(ops); i++ {
		if rng.Intn(2) == 0 {
			ops[i].OpType = "add-issue"
		} else {
			ops[i].OpType = "remove-issue"
		}
		ops[i].Body, _ = json.Marshal(map[string]any{
			"issue": fmt.Sprintf("issue-%d", rng.Intn(4)),
		})
	}

	return ops, rules
}

// --------------------------------------------------------------------------
// 5. Seed Corpus: Historical Regression Vectors
// --------------------------------------------------------------------------

type FuzzCase struct {
	ObjectType string      `json:"object_type,omitempty"`
	Rules      []writ.Rule `json:"rules,omitempty"`
	Ops        []codec.Op  `json:"ops"`
	Mode       string      `json:"mode,omitempty"`
}

// WRIT-112: keyed-lww approval subject denormalized / case-folded subject key.
func regressionVectorWRIT112() FuzzCase {
	now := time.Unix(100, 0).UTC()
	ops := []codec.Op{
		{
			ID: "op-alice-app",
			Envelope: codec.Envelope{
				ObjectID:   "r-112",
				ObjectType: "review",
				OpType:     "approval",
				OpVersion:  1,
				Body:       json.RawMessage(`{"revision":"1111111111111111111111111111111111111111","subject":"Alice@Example.COM","verdict":"approve","message":"LGTM"}`),
			},
			Author: codec.Identity{Email: "alice@example.com", When: now},
		},
		{
			ID:      "op-alice-update",
			Parents: []string{"op-alice-app"},
			Envelope: codec.Envelope{
				ObjectID:   "r-112",
				ObjectType: "review",
				OpType:     "approval",
				OpVersion:  1,
				Body:       json.RawMessage(`{"revision":"1111111111111111111111111111111111111111","subject":"  alice@example.com  ","verdict":"approve","message":"Updated msg"}`),
			},
			Author: codec.Identity{Email: "alice@example.com", When: now.Add(60 * time.Second)},
		},
	}
	return FuzzCase{
		ObjectType: "review",
		Rules:      writ.ReviewRules(),
		Ops:        ops,
	}
}

// WRIT-116: set-observed-remove and set-union handling of empty and whitespace-only items.
func regressionVectorWRIT116() FuzzCase {
	now := time.Unix(100, 0).UTC()
	ops := []codec.Op{
		{
			ID: "op-label-add",
			Envelope: codec.Envelope{
				ObjectID:   "iss-116",
				ObjectType: "issue",
				OpType:     "label",
				OpVersion:  1,
				Body:       json.RawMessage(`{"add":["","   ","\t\n","bug","feature"]}`),
			},
			Author: codec.Identity{When: now},
		},
		{
			ID:      "op-label-rem",
			Parents: []string{"op-label-add"},
			Envelope: codec.Envelope{
				ObjectID:   "iss-116",
				ObjectType: "issue",
				OpType:     "label",
				OpVersion:  1,
				Body:       json.RawMessage(`{"remove":["","   ","feature"]}`),
			},
			Author: codec.Identity{When: now.Add(60 * time.Second)},
		},
	}
	return FuzzCase{
		ObjectType: "issue",
		Rules:      writ.IssueRules(),
		Ops:        ops,
		Mode:       "label",
	}
}

// WRIT-118: lww whitespace-only resolve actor normalized to "".
func regressionVectorWRIT118() FuzzCase {
	now := time.Unix(100, 0).UTC()
	ops := []codec.Op{
		{
			ID: "op-comm-create",
			Envelope: codec.Envelope{
				ObjectID:   "c-118",
				ObjectType: "comment",
				OpType:     "create",
				OpVersion:  1,
				Body:       json.RawMessage(`{"subject":{"object_type":"review","object_id":"r-1"},"text":"Initial text"}`),
			},
			Author: codec.Identity{When: now},
		},
		{
			ID:      "op-comm-resolve",
			Parents: []string{"op-comm-create"},
			Envelope: codec.Envelope{
				ObjectID:   "c-118",
				ObjectType: "comment",
				OpType:     "resolve",
				OpVersion:  1,
				Body:       json.RawMessage(`{"resolved":true,"resolved_by":"   \t\n   "}`),
			},
			Author: codec.Identity{When: now.Add(60 * time.Second)},
		},
	}
	return FuzzCase{
		ObjectType: "comment",
		Rules:      commentRules(),
		Ops:        ops,
	}
}

// WRIT-124: keyed-lww non-string key components quarantined into UnknownOps without leaking Go formatting verbs.
func regressionVectorWRIT124() FuzzCase {
	now := time.Unix(100, 0).UTC()
	ops := []codec.Op{
		{
			ID: "op-bad-key",
			Envelope: codec.Envelope{
				ObjectID:   "r-124",
				ObjectType: "review",
				OpType:     "approval",
				OpVersion:  1,
				Body:       json.RawMessage(`{"revision":"1111111111111111111111111111111111111111","subject":12345,"verdict":"approve"}`),
			},
			Author: codec.Identity{When: now},
		},
	}
	return FuzzCase{
		ObjectType: "review",
		Rules:      writ.ReviewRules(),
		Ops:        ops,
	}
}

// WRIT-125: omitempty empty-scalar retention in generic fold maps and omission in typed JSON.
func regressionVectorWRIT125() FuzzCase {
	now := time.Unix(100, 0).UTC()
	ops := []codec.Op{
		{
			ID: "op-c-create",
			Envelope: codec.Envelope{
				ObjectID:   "c-125",
				ObjectType: "comment",
				OpType:     "create",
				OpVersion:  1,
				Body:       json.RawMessage(`{"subject":{"object_type":"review","object_id":"r-1"},"text":"Initial text"}`),
			},
			Author: codec.Identity{When: now},
		},
		{
			ID:      "op-c-edit-empty",
			Parents: []string{"op-c-create"},
			Envelope: codec.Envelope{
				ObjectID:   "c-125",
				ObjectType: "comment",
				OpType:     "edit",
				OpVersion:  1,
				Body:       json.RawMessage(`{"text":""}`),
			},
			Author: codec.Identity{When: now.Add(60 * time.Second)},
		},
	}
	return FuzzCase{
		ObjectType: "comment",
		Rules:      commentRules(),
		Ops:        ops,
	}
}

// WRIT-126: Non-string and null elements in set-union and append triggering op-level rejection into UnknownOps, plus empty array append producing [].
func regressionVectorWRIT126() FuzzCase {
	now := time.Unix(100, 0).UTC()
	ops := []codec.Op{
		{
			ID: "op-repo-create",
			Envelope: codec.Envelope{
				ObjectID:   "repo-126",
				ObjectType: "repo",
				OpType:     "create",
				OpVersion:  1,
				Body:       json.RawMessage(`{"slug":"writ"}`),
			},
			Author: codec.Identity{When: now},
		},
		{
			ID:      "op-remote-bad-num",
			Parents: []string{"op-repo-create"},
			Envelope: codec.Envelope{
				ObjectID:   "repo-126",
				ObjectType: "repo",
				OpType:     "add-remote",
				OpVersion:  1,
				Body:       json.RawMessage(`{"remote":[12345]}`),
			},
			Author: codec.Identity{When: now.Add(10 * time.Second)},
		},
		{
			ID:      "op-remote-bad-null",
			Parents: []string{"op-remote-bad-num"},
			Envelope: codec.Envelope{
				ObjectID:   "repo-126",
				ObjectType: "repo",
				OpType:     "add-remote",
				OpVersion:  1,
				Body:       json.RawMessage(`{"remote":null}`),
			},
			Author: codec.Identity{When: now.Add(20 * time.Second)},
		},
	}
	return FuzzCase{
		ObjectType: "repo",
		Rules:      state.RepoRules(),
		Ops:        ops,
	}
}

// --------------------------------------------------------------------------
// 6. Test Suite & Property Tests
// --------------------------------------------------------------------------

func TestProperty_FoldThreeWay(t *testing.T) {
	seed := time.Now().UnixNano()
	rng := rand.New(rand.NewSource(seed))
	t.Logf("property test random seed: %d", seed)

	// Subtests for the 6 historical regression vectors
	t.Run("Regression_WRIT_112_ApprovalSubjectDenormalized", func(t *testing.T) {
		c := regressionVectorWRIT112()
		assertThreeWayFoldDomain(t, c.ObjectType, c.Ops, c.Rules, c.Mode)
	})

	t.Run("Regression_WRIT_116_EmptySetItems", func(t *testing.T) {
		c := regressionVectorWRIT116()
		assertThreeWayFoldDomain(t, c.ObjectType, c.Ops, c.Rules, c.Mode)
	})

	t.Run("Regression_WRIT_118_WhitespaceResolveActor", func(t *testing.T) {
		c := regressionVectorWRIT118()
		assertThreeWayFoldDomain(t, c.ObjectType, c.Ops, c.Rules, c.Mode)
	})

	t.Run("Regression_WRIT_124_KeyedLWWNonStringKeys", func(t *testing.T) {
		c := regressionVectorWRIT124()
		assertThreeWayFoldDomain(t, c.ObjectType, c.Ops, c.Rules, c.Mode)
	})

	t.Run("Regression_WRIT_125_OmitemptyEmptyScalars", func(t *testing.T) {
		c := regressionVectorWRIT125()
		assertThreeWayFoldDomain(t, c.ObjectType, c.Ops, c.Rules, c.Mode)
	})

	t.Run("Regression_WRIT_126_NonStringNullSetUnionAppend", func(t *testing.T) {
		c := regressionVectorWRIT126()
		assertThreeWayFoldDomain(t, c.ObjectType, c.Ops, c.Rules, c.Mode)
	})

	// 100 randomized property iterations over abstract strategies and domain object streams
	const iterations = 100
	for i := 0; i < iterations; i++ {
		// 1. Abstract synthetic strategies
		synthOps, synthRules := generateAbstractSyntheticStream(rng)
		assertThreeWayFoldAbstract(t, synthOps, synthRules)

		// 2. Domain object stream
		domainIdx := rng.Intn(6)
		switch domainIdx {
		case 0:
			ops, rules, mode := generateReviewStream(rng)
			assertThreeWayFoldDomain(t, "review", ops, rules, mode)
		case 1:
			ops, rules, mode := generateIssueStream(rng)
			assertThreeWayFoldDomain(t, "issue", ops, rules, mode)
		case 2:
			ops, rules := generateCommentStream(rng)
			assertThreeWayFoldDomain(t, "comment", ops, rules, "")
		case 3:
			ops, rules := generateRepoStream(rng)
			assertThreeWayFoldDomain(t, "repo", ops, rules, "")
		case 4:
			ops, rules := generateProjectStream(rng)
			assertThreeWayFoldDomain(t, "project", ops, rules, "")
		case 5:
			ops, rules := generateCycleStream(rng)
			assertThreeWayFoldDomain(t, "cycle", ops, rules, "")
		}
	}
}

// --------------------------------------------------------------------------
// 7. Go Native Fuzz Target
// --------------------------------------------------------------------------

func FuzzFoldThreeWay(f *testing.F) {
	// Seed with the 6 regression vectors
	seedVectors := []FuzzCase{
		regressionVectorWRIT112(),
		regressionVectorWRIT116(),
		regressionVectorWRIT118(),
		regressionVectorWRIT124(),
		regressionVectorWRIT125(),
		regressionVectorWRIT126(),
	}
	for _, vec := range seedVectors {
		if data, err := json.Marshal(vec); err == nil {
			f.Add(data)
		}
	}

	// Seed with generated cases for each domain
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 6; i++ {
		var fc FuzzCase
		switch i {
		case 0:
			ops, rules, mode := generateReviewStream(rng)
			fc = FuzzCase{ObjectType: "review", Rules: rules, Ops: ops, Mode: mode}
		case 1:
			ops, rules, mode := generateIssueStream(rng)
			fc = FuzzCase{ObjectType: "issue", Rules: rules, Ops: ops, Mode: mode}
		case 2:
			ops, rules := generateCommentStream(rng)
			fc = FuzzCase{ObjectType: "comment", Rules: rules, Ops: ops}
		case 3:
			ops, rules := generateRepoStream(rng)
			fc = FuzzCase{ObjectType: "repo", Rules: rules, Ops: ops}
		case 4:
			ops, rules := generateProjectStream(rng)
			fc = FuzzCase{ObjectType: "project", Rules: rules, Ops: ops}
		case 5:
			ops, rules := generateCycleStream(rng)
			fc = FuzzCase{ObjectType: "cycle", Rules: rules, Ops: ops}
		}
		if data, err := json.Marshal(fc); err == nil {
			f.Add(data)
		}
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var fc FuzzCase
		if err := json.Unmarshal(data, &fc); err == nil && len(fc.Ops) > 0 {
			if !isValidOpSet(fc.Ops) {
				return
			}
			if fc.ObjectType != "" && fc.Ops[0].ObjectType != fc.ObjectType {
				return
			}
			if fc.ObjectType != "" {
				var rules []writ.Rule
				switch fc.ObjectType {
				case "review":
					rules = writ.ReviewRules()
				case "issue":
					rules = writ.IssueRules()
				case "comment":
					rules = commentRules()
				case "repo":
					rules = state.RepoRules()
				case "project":
					rules = writ.ProjectRules()
				case "cycle":
					rules = writ.CycleRules()
				default:
					return
				}
				assertThreeWayFoldDomain(t, fc.ObjectType, fc.Ops, rules, fc.Mode)
			} else {
				if len(fc.Rules) == 0 {
					return
				}
				assertThreeWayFoldAbstract(t, fc.Ops, fc.Rules)
			}
			return
		}

		// If data is not valid FuzzCase JSON, use entropy to seed property generator
		if len(data) >= 8 {
			seed := int64(binary.BigEndian.Uint64(data[:8]))
			localRNG := rand.New(rand.NewSource(seed))
			choice := localRNG.Intn(7)
			if choice == 0 {
				synthOps, synthRules := generateAbstractSyntheticStream(localRNG)
				assertThreeWayFoldAbstract(t, synthOps, synthRules)
			} else {
				switch choice {
				case 1:
					ops, rules, mode := generateReviewStream(localRNG)
					assertThreeWayFoldDomain(t, "review", ops, rules, mode)
				case 2:
					ops, rules, mode := generateIssueStream(localRNG)
					assertThreeWayFoldDomain(t, "issue", ops, rules, mode)
				case 3:
					ops, rules := generateCommentStream(localRNG)
					assertThreeWayFoldDomain(t, "comment", ops, rules, "")
				case 4:
					ops, rules := generateRepoStream(localRNG)
					assertThreeWayFoldDomain(t, "repo", ops, rules, "")
				case 5:
					ops, rules := generateProjectStream(localRNG)
					assertThreeWayFoldDomain(t, "project", ops, rules, "")
				case 6:
					ops, rules := generateCycleStream(localRNG)
					assertThreeWayFoldDomain(t, "cycle", ops, rules, "")
				}
			}
		}
	})
}
