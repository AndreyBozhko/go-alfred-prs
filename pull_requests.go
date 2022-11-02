package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/google/go-github/v48/github"
	"golang.org/x/oauth2"
)

func extractProject(htmlUrl string) string {
	project := htmlUrl

	project, _, _ = strings.Cut(project, "/pull")
	_, project, _ = strings.Cut(project, ".com/")

	return project
}

func deduplicateAndSort(prs []*github.Issue) []*github.Issue {
	var rslt []*github.Issue

	seen := make(map[string]bool)
	for _, item := range prs {
		if _, ok := seen[*item.HTMLURL]; !ok {
			seen[*item.HTMLURL] = true
			rslt = append(rslt, item)
		}
	}
	sort.Slice(rslt, func(i, j int) bool {
		return rslt[i].UpdatedAt.After(*rslt[j].UpdatedAt)
	})

	return rslt
}

func constructDisplayState(status []github.PullRequestReview) string {
	var rslt string
	seen := make(map[string]github.PullRequestReview)
	for _, item := range status {
		if v, ok := seen[*item.User.Login]; !ok || item.SubmittedAt.After(*v.SubmittedAt) {
			seen[*item.User.Login] = item
		}
	}

	for _, v := range seen {
		if strings.ToLower(*v.State) == "approved" {
			rslt += "✅"
		}
	}

	if rslt == "" {
		return ""
	}
	return " " + rslt
}

func newGithubClient(ctx context.Context, url, token string) (*github.Client, error) {
	httpclient := oauth2.NewClient(ctx, oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	))

	if url == "" {
		return github.NewClient(httpclient), nil
	}
	return github.NewEnterpriseClient(url, url, httpclient)
}

func (wf *GithubWorkflow) FetchPRs() error {
	token, err := wf.Token()
	if err != nil {
		return err
	}

	client, err := newGithubClient(wf.ctx, wf.BaseUrl(), token)
	if err != nil {
		return err
	}

	var user github.User
	err = wf.Cache.LoadOrStoreJSON(ghUserInfoKey, time.Hour, func() (interface{}, error) {
		u, _, err := client.Users.Get(wf.ctx, "")
		return u, err
	}, &user)
	if err != nil {
		return err
	}

	var prs []*github.Issue
	for _, role := range []string{"author", "review-requested", "mentions", "assignee"} {
		query := fmt.Sprintf("type:pr is:open %s:%s", role, *user.Login)
		issues, _, err := client.Search.Issues(wf.ctx, query, nil)
		if err != nil {
			return err
		}
		prs = append(prs, issues.Issues...)
	}

	prs = deduplicateAndSort(prs)

	if !wf.IsRunning("update_status") {
		err = wf.RunInBackground("update_status", exec.Command(os.Args[0], "update_status"))
		log.Println("Failed to run background task 'update_status':", err)
	}

	return wf.Cache.StoreJSON(ghPullRequestsKey, prs)
}
