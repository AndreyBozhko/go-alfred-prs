package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
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

func newGithubClient(ctx context.Context, url, token string) (*github.Client, error) {
	httpclient := oauth2.NewClient(ctx, oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	))

	if url == "" {
		return github.NewClient(httpclient), nil
	}
	return github.NewEnterpriseClient(url, url, httpclient)
}

func (wf *GithubWorkflow) FetchPulls() error {
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

	// TODO FIXME invalidate cache
	// TODO FIXME execute concurrently or run in background
	for _, pr := range prs {
		project := extractProject(*pr.HTMLURL)
		owner, repo, _ := strings.Cut(project, "/")

		uniqueKey := strconv.FormatInt(*pr.ID, 10)

		var ignored []github.PullRequestReview
		err := wf.Cache.LoadOrStoreJSON(uniqueKey, time.Since(*pr.UpdatedAt), func() (interface{}, error) {
			reviews, _, err := client.PullRequests.ListReviews(wf.ctx, owner, repo, *pr.Number, nil)
			return reviews, err
		}, &ignored)

		if err != nil {
			return err
		}
	}

	return wf.Cache.StoreJSON(ghPullRequestsKey, prs)
}
