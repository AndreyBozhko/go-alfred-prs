package main

import (
	"context"
	"sort"
	"strings"

	"github.com/google/go-github/v48/github"
	"golang.org/x/oauth2"
)

func parseRepoFromUrl(htmlUrl string) string {
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

func parseReviewState(reviews []github.PullRequestReview) string {
	seen := make(map[string]github.PullRequestReview)
	for _, item := range reviews {
		v := seen[*item.User.Login]
		if item.GetSubmittedAt().After(v.GetSubmittedAt()) {
			seen[*item.User.Login] = item
		}
	}

	var rslt string

	for _, v := range seen {
		switch *v.State {
		case "APPROVED":
			rslt += "✅"
		case "CHANGES_REQUESTED":
			rslt += "❌"
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
