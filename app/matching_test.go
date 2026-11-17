package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type matchingTransport func(*http.Request) (*http.Response, error)

func (f matchingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func matchingReply(output, finish string) *http.Response {
	body, _ := json.Marshal(map[string]any{"candidates": []any{map[string]any{
		"finishReason": finish, "content": map[string]any{"parts": []any{map[string]any{"text": output}}},
	}}})
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}
}

func matchingFixture(t *testing.T, matcher *geminiMatcher) (*client, *app) {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "matching.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a, err := newApp(db)
	if err != nil {
		t.Fatal(err)
	}
	a.matcher = matcher
	srv := httptest.NewServer(a.routes())
	t.Cleanup(srv.Close)
	c := newClient(t, srv)
	c.signup("matching_student", "matching_student@sfsu.edu")
	exec(t, a.store, `UPDATE Users SET Bio = 'I study databases'`)
	return c, a
}

func matchingGroupsFixture(t *testing.T, a *app) {
	t.Helper()
	exec(t, a.store, `INSERT INTO StudyGroups (Id, Name, ShortDescription, Mode, SchoolDomain, MeetingTime, InviteCode) VALUES
		(1, 'SQL practice', 'Practice CSC 675 queries', 'OPEN', '', 'Thursdays 4–5 pm America/Los_Angeles', 'never-send-this-invite'),
		(2, 'SQL late night', 'CSC 675 queries', 'OPEN', '', 'Thursdays 8–9 pm America/Los_Angeles', NULL),
		(3, 'Database circle', 'Discuss databases', 'OPEN', '', '', NULL),
		(4, 'private invite secret', 'hidden description', 'INVITE', '', 'Fridays', 'private-code'),
		(5, 'other school secret', 'hidden description', 'SCHOOL', 'other.edu', 'Fridays', NULL),
		(6, 'botany only secret', 'unjoinable description', 'INTEREST', '', 'Fridays', NULL),
		(7, 'already joined secret', 'joined description', 'OPEN', '', 'Wednesdays 1–2 pm America/Los_Angeles', NULL),
		(8, 'SFSU SQL', 'CSC 675 study', 'SCHOOL', 'sfsu.edu', 'Thursdays 4 pm', NULL),
		(9, 'Account SQL', 'CSC 675 study', 'ACCOUNT', '', 'Thursdays 4 pm', NULL)`)
	exec(t, a.store, `INSERT INTO Interests (Id, Name) VALUES (1, 'Databases'), (2, 'Botany')`)
	exec(t, a.store, `INSERT INTO UserInterests VALUES (1, 1)`)
	exec(t, a.store, `INSERT INTO StudyGroupInterests VALUES (1, 1), (6, 2)`)
	exec(t, a.store, `INSERT INTO StudyGroupEnrollment VALUES (1, 7, 'USER')`)
}

func TestMatchingRelatedCoursesAndCreateAction(t *testing.T) {
	for _, output := range []string{`{"matches":[]}`, `{"matches":[{"group_id":1,"score":80,"reason":"Related database topic, but CSC 675 differs from your course.","schedule_fit":"unknown"}]}`} {
		t.Run(output, func(t *testing.T) {
			m := newGeminiMatcher("test-secret", "")
			m.client.Transport = matchingTransport(func(*http.Request) (*http.Response, error) { return matchingReply(output, "STOP"), nil })
			c, a := matchingFixture(t, m)
			matchingGroupsFixture(t, a)
			code, body, _ := c.post("/matches", url.Values{"courses": {"CSC 667, CSC 415"}, "availability": {"Thursday 1pm-3pm"}})
			if code != 200 || !strings.Contains(body, "No matching group for these details yet") || !strings.Contains(body, "Review &amp; create this group") || !strings.Contains(body, `name="availability" value="Thursday 1pm-3pm"`) || !strings.Contains(body, "CSC 667 study group") {
				t.Fatal("no-match results must offer a draft using the submitted details")
			}
			if strings.Contains(output, `"group_id"`) {
				start := strings.Index(body, "Related groups to explore")
				link := strings.Index(body, `href="/groups/1"`)
				if start < 0 || link < start || !strings.Contains(body, "do not list your requested course codes") {
					t.Fatal("different course must be clearly separated from primary recommendations")
				}
			}
		})
	}
}

func TestCourseCodeBoundaries(t *testing.T) {
	g := StudyGroup{Name: "CSC 667 web development", ShortDescription: "Not an operating systems course"}
	for input, want := range map[string]bool{"csc667": true, "CSC-667, CSC 415": true, "CSC 66": false, "CSC 6670": false, "CSC 415": false} {
		if got := sharesCourse(g, courseCodes(input)); got != want {
			t.Errorf("course match %q = %v, want %v", input, got, want)
		}
	}
}

func TestMatchingHTTPFlow(t *testing.T) {
	var calls int
	m := newGeminiMatcher("test-server-secret", "")
	m.client.Transport = matchingTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.String() != "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.5-flash-lite:generateContent" || r.Header.Get("x-goog-api-key") != "test-server-secret" || r.URL.RawQuery != "" {
			t.Errorf("incorrect provider endpoint, method or authentication")
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) > maxMatchingRequest {
			t.Errorf("request exceeds byte cap: %d", len(raw))
		}
		for _, forbidden := range []string{"test-server-secret", "matching_student", "@sfsu.edu", "never-send-this-invite", "private-code", "private invite secret", "other school secret", "botany only secret", "already joined secret", "joined description"} {
			if strings.Contains(string(raw), forbidden) {
				t.Errorf("provider received private or ineligible data: %s", forbidden)
			}
		}
		var request struct {
			SystemInstruction struct{ Parts []struct{ Text string } }
			Contents          []struct{ Parts []struct{ Text string } }
			GenerationConfig  struct {
				ResponseMimeType   string
				ResponseJsonSchema json.RawMessage
				MaxOutputTokens    int
				ThinkingConfig     struct{ ThinkingLevel string }
			}
			Tools json.RawMessage
		}
		if err := json.Unmarshal(raw, &request); err != nil {
			t.Fatal(err)
		}
		cfg := request.GenerationConfig
		if cfg.ResponseMimeType != "application/json" || len(cfg.ResponseJsonSchema) == 0 || cfg.MaxOutputTokens != 2048 || cfg.ThinkingConfig.ThinkingLevel != "LOW" || len(request.Tools) != 0 || request.SystemInstruction.Parts[0].Text != matchingPrompt {
			t.Error("incorrect model configuration or missing system prompt")
		}
		var data struct {
			matchingProfile
			Groups []matchingCandidate
		}
		if err := json.Unmarshal([]byte(request.Contents[0].Parts[0].Text), &data); err != nil {
			t.Fatal(err)
		}
		if data.Bio != "I study databases" || data.Courses != "CSC 675" || data.Availability != "Thursdays 3–6 pm America/Los_Angeles" || data.Preferences != "quiet study" || len(data.Interests) != 1 || data.Interests[0] != "Databases" || len(data.BusyTimes) != 1 || len(data.Groups) != 5 {
			t.Errorf("saved profile, form details, commitments or candidate set missing: %+v", data)
		}
		return matchingReply(`{"matches":[{"group_id":1,"score":95,"reason":"SQL fits your course. <script>alert(1)</script>","schedule_fit":"compatible"},{"group_id":2,"score":90,"reason":"Too late.","schedule_fit":"conflict"},{"group_id":3,"score":80,"reason":"Shared database interests.","schedule_fit":"unknown"}]}`, "STOP"), nil
	})
	c, a := matchingFixture(t, m)
	matchingGroupsFixture(t, a)
	code, body, _ := c.get("/matches")
	if code != 200 || !strings.Contains(body, "Find my matches") || !strings.Contains(body, "Google Gemini") || calls != 0 {
		t.Fatalf("GET should show form without inference: %d, calls=%d", code, calls)
	}
	code, body, _ = c.post("/matches", url.Values{"courses": {"CSC 675"}, "availability": {"Thursdays 3–6 pm America/Los_Angeles"}, "preferences": {"quiet study"}})
	if code != 200 || calls != 1 || !strings.Contains(body, `href="/groups/1"`) || !strings.Contains(body, `href="/groups/3"`) || strings.Contains(body, `href="/groups/2"`) || !strings.Contains(body, "Schedule needs confirmation") || !strings.Contains(body, "Compared 5 of 5") || strings.Contains(body, "<script>alert(1)") || !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("ranked results, escaping or schedule filtering failed: %d %s", code, body)
	}
	for _, secret := range []string{"test-server-secret", "private invite secret", "other school secret", "botany only secret", "already joined secret"} {
		if strings.Contains(body, secret) {
			t.Errorf("private data rendered: %s", secret)
		}
	}
}

func TestMatchingAuthenticationAndCSRF(t *testing.T) {
	m := newGeminiMatcher("test-secret", "")
	m.client.Transport = matchingTransport(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected provider call")
		return matchingReply(`{"matches":[]}`, "STOP"), nil
	})
	c, _ := matchingFixture(t, m)
	guest := newClient(t, &httptest.Server{URL: c.base})
	for _, method := range []string{"GET", "POST"} {
		req, _ := http.NewRequest(method, c.base+"/matches", nil)
		code, _, next := guest.do(req)
		if code != 303 || next != "/signin?next=%2Fmatches" {
			t.Errorf("guest %s: %d %s", method, code, next)
		}
	}
	req, _ := http.NewRequest("POST", c.base+"/matches", strings.NewReader("courses=SQL"))
	req.Header.Set("Origin", "https://evil.example")
	if code, _, _ := c.do(req); code != 403 {
		t.Errorf("cross-site post: %d", code)
	}
	req, _ = http.NewRequest("POST", c.base+"/matches", strings.NewReader("courses=%zz"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if code, _, _ := c.do(req); code != 400 {
		t.Errorf("malformed form: %d", code)
	}
	req, _ = http.NewRequest("POST", c.base+"/matches", strings.NewReader("courses="+strings.Repeat("x", 1<<20)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if code, _, _ := c.do(req); code != 400 {
		t.Errorf("oversized form: %d", code)
	}
	res, err := c.http.Get(c.base + "/matches")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Error("personalized page must not be cached")
	}
}

func TestMatchingEmptyDisabledAndValidation(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			var m *geminiMatcher
			if enabled {
				m = newGeminiMatcher("test-secret", "")
				m.client.Transport = matchingTransport(func(*http.Request) (*http.Response, error) {
					t.Error("empty or invalid form should not call provider")
					return nil, errors.New("unexpected")
				})
			}
			c, _ := matchingFixture(t, m)
			code, body, _ := c.post("/matches", url.Values{})
			if enabled && (code != 200 || !strings.Contains(body, "No matching group for these details yet")) {
				t.Errorf("empty groups: %d", code)
			}
			if !enabled && (code != 503 || !strings.Contains(body, "hasn't been enabled")) {
				t.Errorf("disabled matching: %d", code)
			}
			for field, n := range map[string]int{"courses": 201, "availability": 301, "preferences": 301} {
				code, body, _ = c.post("/matches", url.Values{field: {strings.Repeat("界", n)}})
				if code != 422 || !strings.Contains(body, strings.Repeat("界", n)) {
					t.Errorf("%s length validation or value preservation failed: %d", field, code)
				}
			}
		})
	}
	if newGeminiMatcher(" \n", "") != nil {
		t.Error("empty key should disable matching")
	}
	if newGeminiMatcher("key", "custom-model").model != "custom-model" {
		t.Error("model override ignored")
	}
}

func TestMatchingProviderFailures(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500, 302} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			m := newGeminiMatcher("test-server-secret", "")
			calls := 0
			m.client.Transport = matchingTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://other.example/"}}, Body: io.NopCloser(strings.NewReader("test-server-secret private provider body"))}, nil
			})
			c, a := matchingFixture(t, m)
			matchingGroupsFixture(t, a)
			code, body, _ := c.post("/matches", url.Values{"courses": {"CSC 675"}})
			want := 503
			if status == 429 {
				want = 429
			}
			if code != want || calls != 1 || strings.Contains(body, "test-server-secret") || strings.Contains(body, "private provider body") || !strings.Contains(body, `value="CSC 675"`) {
				t.Errorf("provider failure not contained: code=%d, calls=%d", code, calls)
			}
		})
	}
}

func TestMatchingRejectsInvalidModelOutput(t *testing.T) {
	valid := `{"group_id":1,"score":80,"reason":"Good fit.","schedule_fit":"compatible"}`
	for name, output := range map[string]string{
		"malformed": "{", "missing matches": "{}", "null matches": `{"matches":null}`,
		"unknown field": `{"matches":[],"other":true}`, "trailing json": `{"matches":[]} {}`,
		"unknown id":    `{"matches":[` + strings.Replace(valid, `"group_id":1`, `"group_id":999`, 1) + `]}`,
		"duplicate id":  `{"matches":[` + valid + `,` + valid + `]}`,
		"missing score": `{"matches":[` + strings.Replace(valid, `"score":80,`, "", 1) + `]}`,
		"bad score":     `{"matches":[` + strings.Replace(valid, `"score":80`, `"score":101`, 1) + `]}`,
		"empty reason":  `{"matches":[` + strings.Replace(valid, "Good fit.", "", 1) + `]}`,
		"long reason":   `{"matches":[` + strings.Replace(valid, "Good fit.", strings.Repeat("a", 401), 1) + `]}`,
		"bad schedule":  `{"matches":[` + strings.Replace(valid, "compatible", "definitely", 1) + `]}`,
		"too many":      `{"matches":[` + strings.TrimSuffix(strings.Repeat(valid+",", 6), ",") + `]}`,
	} {
		t.Run(name, func(t *testing.T) {
			m := newGeminiMatcher("key", "")
			m.client.Transport = matchingTransport(func(*http.Request) (*http.Response, error) { return matchingReply(output, "STOP"), nil })
			_, _, err := m.rank(context.Background(), matchingProfile{}, []StudyGroup{{ID: 1}})
			if !errors.Is(err, errMatchingResponse) {
				t.Errorf("accepted invalid output: %v", err)
			}
		})
	}
	for _, finish := range []string{"MAX_TOKENS", "SAFETY", ""} {
		m := newGeminiMatcher("key", "")
		m.client.Transport = matchingTransport(func(*http.Request) (*http.Response, error) { return matchingReply(`{"matches":[]}`, finish), nil })
		if _, _, err := m.rank(context.Background(), matchingProfile{}, []StudyGroup{{ID: 1}}); !errors.Is(err, errMatchingResponse) {
			t.Errorf("accepted incomplete response %s", finish)
		}
	}
	for _, raw := range []string{`{"candidates":[]}`, strings.Repeat(" ", (64<<10)+1)} {
		m := newGeminiMatcher("key", "")
		m.client.Transport = matchingTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))}, nil
		})
		if _, _, err := m.rank(context.Background(), matchingProfile{}, []StudyGroup{{ID: 1}}); !errors.Is(err, errMatchingResponse) {
			t.Error("accepted empty/oversized response")
		}
	}
}

func TestMatchingBudgetsAndCancellation(t *testing.T) {
	now := time.Now()
	m := newGeminiMatcher("key", "")
	for range matchingRPM {
		if err := m.acquire(now); err != nil {
			t.Fatal(err)
		}
		m.release()
	}
	if !errors.Is(m.acquire(now), errMatchingLimited) {
		t.Error("RPM not enforced")
	}
	if err := m.acquire(now.Add(time.Minute)); err != nil {
		t.Errorf("minute boundary: %v", err)
	}
	m.release()
	if matchingRPM*matchingTokenReservation > 250000 || maxMatchingRequest > matchingTokenReservation {
		t.Fatal("input token reservation exceeds stated TPM")
	}
	m.requests = make([]time.Time, matchingRPD)
	for i := range m.requests {
		m.requests[i] = now.Add(-time.Hour)
	}
	if !errors.Is(m.acquire(now), errMatchingLimited) {
		t.Error("daily limit not enforced")
	}
	if err := m.acquire(now.Add(24 * time.Hour)); err != nil {
		t.Errorf("rolling daily boundary: %v", err)
	}
	m.release()
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if m.acquire(now.Add(24*time.Hour)) == nil {
				admitted.Add(1)
			}
		})
	}
	wg.Wait()
	if admitted.Load() != 1 {
		t.Errorf("in-flight call limit: %d", admitted.Load())
	}
	m.release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := m.rank(ctx, matchingProfile{}, []StudyGroup{{ID: 1}}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancellation not honored: %v", err)
	}
}

func TestMatchingRequestBoundAndUnknownSchedule(t *testing.T) {
	m := newGeminiMatcher("key", "")
	calls := 0
	m.client.Transport = matchingTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > maxMatchingRequest {
			t.Errorf("oversized provider request: %d", len(raw))
		}
		return matchingReply(`{"matches":[{"group_id":1,"score":80,"reason":"Relevant.","schedule_fit":"compatible"}]}`, "STOP"), nil
	})
	groups := make([]StudyGroup, 100)
	for i := range groups {
		groups[i] = StudyGroup{ID: int64(i + 1), Name: "SQL", MeetingTime: "Thursday"}
	}
	matches, n, err := m.rank(context.Background(), matchingProfile{}, groups)
	if err != nil || n > 30 || len(matches) != 1 || matches[0].ScheduleFit != "unknown" {
		t.Fatalf("candidate cap or unknown availability failed: %d %v %+v", n, err, matches)
	}
	if _, _, err := m.rank(context.Background(), matchingProfile{Bio: strings.Repeat("界", 5000)}, groups); !errors.Is(err, errMatchingTooLarge) || calls != 1 {
		t.Errorf("oversize profile not rejected before inference: %v calls=%d", err, calls)
	}
}

func TestMatchingRefreshesVisibility(t *testing.T) {
	m := newGeminiMatcher("key", "")
	c, a := matchingFixture(t, m)
	matchingGroupsFixture(t, a)
	m.client.Transport = matchingTransport(func(*http.Request) (*http.Response, error) {
		exec(t, a.store, `UPDATE StudyGroups SET Mode='INVITE' WHERE Id=1`)
		return matchingReply(`{"matches":[{"group_id":1,"score":95,"reason":"SQL practice suits you.","schedule_fit":"unknown"}]}`, "STOP"), nil
	})
	code, body, _ := c.post("/matches", url.Values{})
	if code != 200 || strings.Contains(body, `href="/groups/1"`) || strings.Contains(body, "SQL practice suits you") {
		t.Errorf("group hidden during inference was rendered: %d", code)
	}
}

func TestMatchingRefreshesViewer(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(strconv.FormatBool(revoke), func(t *testing.T) {
			m := newGeminiMatcher("key", "")
			c, a := matchingFixture(t, m)
			matchingGroupsFixture(t, a)
			m.client.Transport = matchingTransport(func(*http.Request) (*http.Response, error) {
				if revoke {
					exec(t, a.store, `DELETE FROM Sessions`)
				} else {
					exec(t, a.store, `UPDATE Users SET Email='changed@other.edu' WHERE Id=1`)
				}
				return matchingReply(`{"matches":[{"group_id":8,"score":95,"reason":"School SQL is relevant.","schedule_fit":"unknown"}]}`, "STOP"), nil
			})
			code, body, next := c.post("/matches", url.Values{})
			if revoke {
				if code != 303 || next != "/signin?next=%2Fmatches" {
					t.Errorf("revoked session still received results: %d %s", code, next)
				}
			} else if code != 200 || strings.Contains(body, `href="/groups/8"`) || strings.Contains(body, "School SQL is relevant.") {
				t.Errorf("changed school email retained access: %d", code)
			}
		})
	}
}

func TestMatchingTimeout(t *testing.T) {
	m := newGeminiMatcher("key", "")
	m.client.Timeout = 5 * time.Millisecond
	m.client.Transport = matchingTransport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	if _, _, err := m.rank(context.Background(), matchingProfile{}, []StudyGroup{{ID: 1}}); !errors.Is(err, errMatchingUnavailable) {
		t.Errorf("timeout not contained: %v", err)
	}
	if err := m.acquire(time.Now()); err != nil {
		t.Errorf("timeout retained in-flight lock: %v", err)
	}
	m.release()
}

func TestMatchingSkipsOversizeCandidate(t *testing.T) {
	form := url.Values{"name": {strings.Repeat("<", 80)}, "short": {strings.Repeat("<", 140)}, "long": {strings.Repeat("<", 2000)}, "place": {strings.Repeat("<", 120)}, "time": {strings.Repeat("<", 80)}, "mode": {"OPEN"}}
	for i := 0; i < 8; i++ {
		form.Add("topic", strings.Repeat("<", 38)+fmt.Sprintf("%02d", i))
	}
	r := httptest.NewRequest("POST", "/groups/new", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	large, topics, problem := readGroupForm(r, &User{ID: 1}, StudyGroup{ID: 1})
	if problem != "" {
		t.Fatal("probe group is invalid: " + problem)
	}
	for i, s := range topics {
		large.Interests = append(large.Interests, Interest{ID: int64(i + 1), Name: s})
	}
	profile := matchingProfile{Bio: strings.Repeat("b", 500), Courses: strings.Repeat("c", 200), Availability: strings.Repeat("a", 300), Preferences: strings.Repeat("p", 300)}
	for i := 0; i < 50; i++ {
		profile.Interests = append(profile.Interests, fmt.Sprintf("interest%02d%030d", i, i))
	}
	small := StudyGroup{ID: 2, Name: "SQL study", ShortDescription: "Practice SQL", MeetingTime: "Thursday"}
	bigBody, _ := matchingBody(profile, []matchingCandidate{{large.ID, large.Name, (large.ShortDescription + " " + large.LongDescription)[:500], topicNames(large), large.MeetingTime, large.MeetingPlace}})
	smallBody, _ := matchingBody(profile, []matchingCandidate{{ID: 2, Name: small.Name, Description: small.ShortDescription + " ", MeetingTime: small.MeetingTime}})
	t.Logf("valid group; first candidate request=%d bytes, later candidate request=%d bytes, cap=%d", len(bigBody), len(smallBody), maxMatchingRequest)
	if len(bigBody) <= maxMatchingRequest || len(smallBody) > maxMatchingRequest {
		t.Fatal("probe does not straddle request cap")
	}
	m := newGeminiMatcher("test-only-key", "")
	m.client.Transport = matchingTransport(func(r *http.Request) (*http.Response, error) {
		return matchingReply(`{"matches":[{"group_id":2,"score":90,"reason":"SQL is relevant.","schedule_fit":"unknown"}]}`, "STOP"), nil
	})
	matches, n, err := m.rank(context.Background(), profile, []StudyGroup{large, small})
	if err != nil || n != 1 || len(matches) != 1 || matches[0].ID != small.ID {
		t.Fatalf("a valid oversize group blocks later fitting candidate: compared=%d error=%v", n, err)
	}
	m.client.Transport = matchingTransport(func(*http.Request) (*http.Response, error) {
		return matchingReply(`{"matches":[{"group_id":1,"score":90,"reason":"Not actually sent.","schedule_fit":"unknown"}]}`, "STOP"), nil
	})
	if _, _, err := m.rank(context.Background(), profile, []StudyGroup{large, small}); !errors.Is(err, errMatchingResponse) {
		t.Error("accepted a skipped candidate ID")
	}
}
