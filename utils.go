package main

import (
	"context"
	"sort"
	"strings"

	"github.com/google/go-github/v48/github"
	"golang.org/x/oauth2"
)

// updateNeeded is an error that holds extra information such as number of remaining attempts.
type updateNeeded struct {
	message      string
	attemptsLeft int
}

func (e *updateNeeded) Error() string {
	return e.message
}

// parseRepoFromUrl extracts 'org/repo' substring from the HTML URL of a github issue.
func parseRepoFromUrl(htmlUrl string) string {
	project := htmlUrl

	project, _, _ = strings.Cut(project, "/pull")
	_, project, _ = strings.Cut(project, ".com/")

	return project
}

// deduplicateAndSort returns unique github issues from the slice, sorted by the update timestamp.
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

// parseReviewState summarizes the reviews of a pull request in a single string.
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

// newGithubClient creates a github client which uses
// provided url and API token to connect to GitHub.
func newGithubClient(ctx context.Context, url, token string) (*github.Client, error) {
	httpclient := oauth2.NewClient(ctx, oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	))

	if url == "" {
		return github.NewClient(httpclient), nil
	}
	return github.NewEnterpriseClient(url, url, httpclient)
}
