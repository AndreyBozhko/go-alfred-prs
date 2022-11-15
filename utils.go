package main

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/google/go-github/v48/github"
	"golang.org/x/oauth2"
)

var (
	ghHtmlUrlPattern = regexp.MustCompile(`^https://[a-z.]+.com/([a-zA-Z0-9/_\-]+)/pull/\d+$`)

	availableRoles    = []string{"assignee", "author", "commenter", "involves", "mentions", "review-requested", "reviewed-by"}
	singleRolePattern = regexp.MustCompile(`^(([+-])(` + strings.Join(availableRoles, "|") + `))$`)
)

// parseRepoFromUrl extracts 'org/repo' substring from the HTML URL of a GitHub issue.
func parseRepoFromUrl(htmlUrl string) string {
	match := ghHtmlUrlPattern.FindStringSubmatch(htmlUrl)
	if match != nil {
		return match[1]
	}
	return ""
}

// parseRoleFilters analyzes configuration strings
// and extracts roles that are enabled.
func parseRoleFilters(roles []string) ([]string, error) {
	result := make([]string, 0)

	seen := make(map[string]string)
	for _, roleString := range roles {
		matches := singleRolePattern.FindAllStringSubmatch(roleString, -1)
		if len(matches) != 1 {
			return nil, &alfredError{
				"invalid role: " + roleString,
				"expected one of: " + strings.Join(availableRoles, ","),
			}
		}

		flag, role := matches[0][2], matches[0][3]
		seen[role] = flag
	}

	for role, flag := range seen {
		if flag == "+" {
			result = append(result, role)
		}
	}

	return result, nil
}

// deduplicateAndSort returns unique GitHub issues from the slice, sorted by the update timestamp.
func deduplicateAndSort(prs []*github.Issue) []*github.Issue {
	result := make([]*github.Issue, 0)

	seen := make(map[int64]bool)
	for _, item := range prs {
		if _, ok := seen[*item.ID]; !ok {
			seen[*item.ID] = true
			result = append(result, item)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(*result[j].UpdatedAt)
	})

	return result
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

	var result string

	mapping := map[string]string{
		"APPROVED":          "✅",
		"CHANGES_REQUESTED": "❌",
	}

	for _, v := range seen {
		result += mapping[*v.State]
	}

	return result
}

// newGithubClient creates a GitHub client which uses
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
