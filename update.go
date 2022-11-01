package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type userInfo struct {
	Login string `json:"login"`
}

type pullRequestInfo struct {
	HtmlUrl   string    `json:"html_url"`
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
	User      userInfo  `json:"user"`
}

type issuesInfo struct {
	Items []pullRequestInfo `json:"items"`
}

func (pr *pullRequestInfo) Project() string {
	if project, _, ok := strings.Cut(pr.HtmlUrl, "/pull"); !ok {
		return ""
	} else {
		return project
	}
}

func (client *GithubClient) fetchPullRequests(name, role string) (*issuesInfo, error) {
	query := url.QueryEscape(fmt.Sprintf("type:pr is:open %s:%s", role, name))
	url1 := "https://api.github.com/search/issues?q=%s"

	body, err := client.doRequest(fmt.Sprintf(url1, query))
	if err != nil {
		return nil, err
	}

	var issues issuesInfo
	if err = json.Unmarshal(body, &issues); err != nil {
		return nil, err
	}

	return &issues, nil
}

func (client *GithubClient) getUserName() ([]byte, error) {
	userUrl := "https://api.github.com/user"

	body, err := client.doRequest(userUrl)
	if err != nil {
		return nil, err
	}

	var user userInfo
	if err = json.Unmarshal(body, &user); err != nil {
		return nil, err
	}

	return []byte(user.Login), nil
}

func deduplicate(prs []pullRequestInfo) []pullRequestInfo {
	seen := make(map[string]bool)
	rslt := make([]pullRequestInfo, 0)
	for _, item := range prs {
		if _, ok := seen[item.HtmlUrl]; !ok {
			seen[item.HtmlUrl] = true
			rslt = append(rslt, item)
		}
	}
	return rslt
}

func (wf *GithubWorkflow) doUpdate() error {
	token, err := wf.Token()
	if err != nil {
		return err
	}

	client := GithubClient{token}

	name, err := wf.Cache.LoadOrStore(userNameKey, 6*time.Hour, func() ([]byte, error) {
		return client.getUserName()
	})
	if err != nil {
		return err
	}

	var prs []pullRequestInfo
	for _, role := range []string{"author", "review-requested", "mentions", "assignee"} {
		issues, err := client.fetchPullRequests(string(name), role)
		if err != nil {
			return err
		}
		prs = append(prs, issues.Items...)
	}

	if err = wf.Cache.StoreJSON(pullRequestsKey, deduplicate(prs)); err != nil {
		return err
	}

	return nil
}
