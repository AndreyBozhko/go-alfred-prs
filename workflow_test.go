package main

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
	"time"

	aw "github.com/deanishe/awgo"
	kc "github.com/deanishe/awgo/keychain"
	"github.com/stretchr/testify/assert"
)

var testWf *GithubWorkflow

var kcErr = kc.ErrNotFound

func init() {
	log.SetOutput(io.Discard)

	testWf = &GithubWorkflow{
		Workflow:     aw.New(),
		cacheMaxAge:  5 * time.Second,
		allowUpdates: false,
		roleFilters:  []string{"author", "involves"},
		fetchReviews: false,
		gitApiUrl:    "",
	}
}

func TestFetchAndDisplay(t *testing.T) {
	// given
	url, teardown := setupFakeGitHub()
	defer teardown()

	testWf.gitApiUrl = url

	kc.ErrNotFound = nil // effectively disable using keychain
	defer func() {
		kc.ErrNotFound = kcErr
	}()

	// when
	assert.Nil(t, testWf.FetchPRs())
	assert.Equal(t, 0, len(testWf.Feedback.Items))

	assert.Nil(t, testWf.FetchPRStatus())
	assert.Equal(t, 0, len(testWf.Feedback.Items))

	assert.Nil(t, testWf.DisplayPRs(0))
	assert.Equal(t, 3, len(testWf.Feedback.Items))

	// then
	actual := make([]string, 3)
	for idx, itm := range testWf.Feedback.Items {
		bts, err := itm.MarshalJSON()
		assert.Nil(t, err)

		actual[idx] = string(bts)
	}

	assert.Equal(t, []string{
		`{"title":"Title 3","subtitle":"org/repo#89 by ccc, 11-Nov-2022 05:23","arg":"https://gh.com/org/repo/pull/89","valid":true}`,
		`{"title":"Title 2","subtitle":"org/repo#67 by bbb, 11-Nov-2021 05:23","arg":"https://gh.com/org/repo/pull/67","valid":true}`,
		`{"title":"Title 1 ✅","subtitle":"org/repo#78 by aaa, 11-Nov-2020 05:23","arg":"https://gh.com/org/repo/pull/78","valid":true}`,
	}, actual)
}

func setupFakeGitHub() (serverURL string, teardown func()) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v3/user", handleUser)
	mux.HandleFunc("/api/v3/search/issues", handleSearchIssues)
	for _, pr := range []string{"67", "78", "89"} {
		mux.HandleFunc("/api/v3/repos/org/repo/pulls/"+pr+"/reviews", handleReviews)
	}

	server := httptest.NewServer(mux)
	return server.URL, server.Close
}

func handleUser(w http.ResponseWriter, r *http.Request) {
	body := `{"login": "testuser"}`
	w.Write([]byte(body))
}

func handleSearchIssues(w http.ResponseWriter, r *http.Request) {
	body := `[]`
	q, err := url.QueryUnescape(r.URL.RawQuery[2:])
	if err != nil {
		w.Write([]byte(body))
		return
	}

	switch q {
	case "type:pr is:open author:testuser":
		body = `{"total_count": 1, "items": [
			{"id": 1, "number": 78, "title": "Title 1", "html_url": "https://gh.com/org/repo/pull/78", "updated_at": "2020-11-11T05:23:57Z", "user": {"login": "aaa"}}
		]}`
	case "type:pr is:open involves:testuser":
		body = `{"total_count": 3, "items": [
			{"id": 2, "number": 67, "title": "Title 2", "html_url": "https://gh.com/org/repo/pull/67", "updated_at": "2021-11-11T05:23:57Z", "user": {"login": "bbb"}},
			{"id": 1, "number": 78, "title": "Title 1", "html_url": "https://gh.com/org/repo/pull/78", "updated_at": "2020-11-11T05:23:57Z", "user": {"login": "aaa"}},
			{"id": 3, "number": 89, "title": "Title 3", "html_url": "https://gh.com/org/repo/pull/89", "updated_at": "2022-11-11T05:23:57Z", "user": {"login": "ccc"}}
		]}`
	}

	w.Write([]byte(body))
}

var reviewUrlPattern = regexp.MustCompile(`pulls/(\d+)/reviews`)

func handleReviews(w http.ResponseWriter, r *http.Request) {
	body := `[]`
	pr := reviewUrlPattern.FindStringSubmatch(r.URL.Path)[1]

	switch pr {
	case "67":
		body = `[]`
	case "78":
		body = `[{"id": 298364, "submitted_at": "2020-11-13T00:00:00Z", "state": "APPROVED", "user": {"login": "reviewer1"}}]`
	case "89":
		body = `[{"id": 390457, "submitted_at": "2022-11-13T00:00:00Z", "state": "COMMENTED", "user": {"login": "reviewer2"}}]`
	}

	w.Write([]byte(body))
}
