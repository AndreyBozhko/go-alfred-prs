package main

import (
	"context"
	"regexp"
	"sort"

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

var (
	ghHtmlUrlPattern = regexp.MustCompile(`^https://[a-z.]+.com/(.+)/pull/\d+$`)
)

// parseRepoFromUrl extracts 'org/repo' substring from the HTML URL of a github issue.
func parseRepoFromUrl(htmlUrl string) string {
	match := ghHtmlUrlPattern.FindStringSubmatch(htmlUrl)
	if match != nil {
		return match[1]
	}
	return ""
}

// deduplicateAndSort returns unique github issues from the slice, sorted by the update timestamp.
func deduplicateAndSort(prs []*github.Issue) []*github.Issue {
	var rslt []*github.Issue

	seen := make(map[int64]bool)
	for _, item := range prs {
		if _, ok := seen[*item.ID]; !ok {
			seen[*item.ID] = true
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
		if *item.State == "COMMENTED" {
			continue
		}

		v := seen[*item.User.Login]
		if item.GetSubmittedAt().After(v.GetSubmittedAt()) {
			seen[*item.User.Login] = item
		}
	}

	var rslt string

	mapping := map[string]string{
		"APPROVED":          "✅",
		"CHANGES_REQUESTED": "❌",
	}

	for _, v := range seen {
		rslt += mapping[*v.State]
	}

	return rslt
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
