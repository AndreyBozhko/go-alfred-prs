package main

import (
	"sort"
	"testing"
	"time"

	"github.com/google/go-github/v48/github"
	"github.com/stretchr/testify/assert"
)

func TestParseRepoFromUrl(t *testing.T) {
	data := []struct {
		url, repo string
	}{
		{"https://github.com/deanishe/awgo/pull/77", "deanishe/awgo"},
		{"https://github.com/renuo/alfred-pr-workflow/pull/1", "renuo/alfred-pr-workflow"},
		{"https://github.com/chokkan/simstring/pull/", ""},
	}

	for _, testcase := range data {
		assert.Equal(t, testcase.repo, parseRepoFromUrl(testcase.url))
	}
}

func TestParseRoleFilters(t *testing.T) {
	data := []struct {
		input    []string
		expected []string
	}{
		{
			[]string{"+author"},
			[]string{"author"},
		},
		{
			[]string{"-author"},
			[]string{},
		},
		{
			[]string{"+assignee", "+author", "+involves", "+mentions", "+review-requested"},
			[]string{"assignee", "author", "involves", "mentions", "review-requested"},
		},
		{
			[]string{"+author", "+assignee", "-involves", "+mentions", "-review-requested"},
			[]string{"assignee", "author", "mentions"},
		},
		{
			[]string{"+author", "+author", "-author", "+mentions"},
			[]string{"mentions"},
		},
		{
			[]string{"-author", "-assignee", "+involves", "+assignee"},
			[]string{"assignee", "involves"},
		},
	}

	for _, testcase := range data {
		actual, err := parseRoleFilters(testcase.input)
		assert.Nil(t, err)

		sort.Strings(actual)

		assert.Equal(t, testcase.expected, actual)
	}
}

func TestParseRoleFiltersError(t *testing.T) {
	data := []struct {
		input []string
	}{
		{
			[]string{"+not-author"},
		},
		{
			[]string{"*author"},
		},
		{
			[]string{"+assignee", "+author", "+involves", "+mentions", "+review-request"},
		},
		{
			[]string{"+author", "+assignee", "/involves", "+mentions", "-review-requested"},
		},
		{
			[]string{"+author+author", "-author", "+mentions"},
		},
		{
			[]string{"-author", "-assignee,+involves", "+assignee"},
		},
	}

	for _, testcase := range data {
		_, err := parseRoleFilters(testcase.input)
		assert.Error(t, err)
	}
}

func TestDeduplicateAndSort(t *testing.T) {

	issue := func(id int64, upd time.Time) *github.Issue {
		return &github.Issue{ID: &id, UpdatedAt: &upd}
	}

	issues := []*github.Issue{
		issue(1, time.UnixMilli(1000)),
		issue(2, time.UnixMilli(5000)),
		issue(3, time.UnixMilli(3000)),
		issue(2, time.UnixMilli(5000)),
		issue(4, time.UnixMilli(2000)),
		issue(9, time.UnixMilli(3000)),
		issue(9, time.UnixMilli(3000)),
	}

	expected := []*github.Issue{
		issue(2, time.UnixMilli(5000)),
		issue(3, time.UnixMilli(3000)),
		issue(9, time.UnixMilli(3000)),
		issue(4, time.UnixMilli(2000)),
		issue(1, time.UnixMilli(1000)),
	}

	actual := deduplicateAndSort(issues)

	assert.Equal(t, expected, actual)

	assert.True(t, sort.SliceIsSorted(actual, func(i, j int) bool {
		return actual[i].UpdatedAt.After(*actual[j].UpdatedAt)
	}))
}

func TestParseReviewState(t *testing.T) {
	review := func(upd time.Time, user, state string) *github.PullRequestReview {
		return &github.PullRequestReview{
			User:        &github.User{Login: &user},
			State:       &state,
			SubmittedAt: &upd,
		}
	}

	data := []struct {
		expected string
		reviews  []*github.PullRequestReview
	}{
		{
			"",
			[]*github.PullRequestReview{
				review(time.UnixMilli(1000), "user1", "COMMENTED"),
				review(time.UnixMilli(2000), "user2", "COMMENTED"),
			},
		},
		{
			"",
			[]*github.PullRequestReview{
				review(time.UnixMilli(1000), "user1", "APPROVED"),
				review(time.UnixMilli(2000), "user1", "DISMISSED"),
			},
		},
		{
			"✅",
			[]*github.PullRequestReview{
				review(time.UnixMilli(1000), "user1", "COMMENTED"),
				review(time.UnixMilli(2000), "user2", "APPROVED"),
			},
		},
		{
			"✅✅",
			[]*github.PullRequestReview{
				review(time.UnixMilli(1000), "user1", "APPROVED"),
				review(time.UnixMilli(2000), "user1", "COMMENTED"),
				review(time.UnixMilli(3000), "user2", "APPROVED"),
			},
		},
		{
			"✅❌",
			[]*github.PullRequestReview{
				review(time.UnixMilli(1000), "user1", "APPROVED"),
				review(time.UnixMilli(2000), "user1", "DISMISSED"),
				review(time.UnixMilli(2000), "user2", "CHANGES_REQUESTED"),
				review(time.UnixMilli(3000), "user1", "APPROVED"),
			},
		},
		{
			"❌",
			[]*github.PullRequestReview{
				review(time.UnixMilli(1000), "user1", "APPROVED"),
				review(time.UnixMilli(2000), "user1", "COMMENTED"),
				review(time.UnixMilli(3000), "user1", "CHANGES_REQUESTED"),
			},
		},
	}

	for _, testcase := range data {
		actual := parseReviewState(testcase.reviews)
		assert.Equal(t, testcase.expected, sorted(actual))
	}
}

func sorted(input string) string {
	result := []rune(input)
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return string(result)
}
