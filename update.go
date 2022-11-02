package main

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

type userInfo struct {
	Login string `json:"login"`
}

type pullRequestInfo struct {
	Id        int       `json:"id"`
	Number    int       `json:"number"`
	HtmlUrl   string    `json:"html_url"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
	User      userInfo  `json:"user"`
}

type issuesInfo struct {
	Items []pullRequestInfo `json:"items"`
}

func (issues issuesInfo) Len() int {
	return len(issues.Items)
}

func (issues issuesInfo) Less(i, j int) bool {
	return issues.Items[i].UpdatedAt.After(issues.Items[j].UpdatedAt)
}

func (issues issuesInfo) Swap(i, j int) {
	issues.Items[i], issues.Items[j] = issues.Items[j], issues.Items[i]
}

func (pr *pullRequestInfo) Project() string {
	project := pr.HtmlUrl

	project, _, _ = strings.Cut(project, "/pull")
	_, project, _ = strings.Cut(project, ".com/")

	return project
}

func (client *GithubClient) fetchPullRequests(name, role string) (*issuesInfo, error) {
	query := url.QueryEscape(fmt.Sprintf("type:pr is:open %s:%s", role, name))
	resource := fmt.Sprintf("https://api.%s/search/issues?q=%s", client.baseUrl, query)

	var issues issuesInfo
	err := client.fetchResourceAsJson(resource, &issues)
	if err != nil {
		return nil, err
	}

	return &issues, nil
}

func (client *GithubClient) fetchUser() (*userInfo, error) {
	userUrl := fmt.Sprintf("https://api.%s/user", client.baseUrl)

	var user userInfo
	err := client.fetchResourceAsJson(userUrl, &user)
	if err != nil {
		return nil, err
	}

	return &user, nil
}

func deduplicateAndSort(prs []pullRequestInfo) []pullRequestInfo {
	var i issuesInfo

	seen := make(map[string]bool)
	for _, item := range prs {
		if _, ok := seen[item.HtmlUrl]; !ok {
			seen[item.HtmlUrl] = true
			i.Items = append(i.Items, item)
		}
	}
	sort.Sort(i)

	return i.Items
}

func (wf *GithubWorkflow) FetchPulls() error {
	token, err := wf.Token()
	if err != nil {
		return err
	}

	client := GithubClient{"github.com", token}

	var user userInfo
	err = wf.Cache.LoadOrStoreJSON(userNameKey, time.Hour, func() (interface{}, error) {
		return client.fetchUser()
	}, &user)
	if err != nil {
		return err
	}

	var prs []pullRequestInfo
	for _, role := range []string{"author", "review-requested", "mentions", "assignee"} {
		issues, err := client.fetchPullRequests(user.Login, role)
		if err != nil {
			return err
		}
		prs = append(prs, issues.Items...)
	}

	return wf.Cache.StoreJSON(pullRequestsKey, deduplicateAndSort(prs))
}
