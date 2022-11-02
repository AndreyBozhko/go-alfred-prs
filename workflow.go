package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	aw "github.com/deanishe/awgo"
	kc "github.com/deanishe/awgo/keychain"
	"github.com/google/go-github/v48/github"
)

const (
	ghAuthTokenKey    = "gh-auth-token"
	ghBaseUrlKey      = "gh-base-url"
	ghUserInfoKey     = "gh-user-info"
	ghPullRequestsKey = "gh-pull-requests"
)

var (
	gitUrlPattern = regexp.MustCompile("^https://(.*)?.com$")
)

var (
	errMissingArgs     = errors.New("wrong number of arguments passed")
	errPatternMismatch = fmt.Errorf("url does not match pattern %s", gitUrlPattern)
	errUnknownCmd      = errors.New("unknown command")
)

type GithubWorkflow struct {
	aw.Workflow

	ctx context.Context
}

var wf *GithubWorkflow

func init() {
	wf = &GithubWorkflow{*aw.New(), context.Background()}
}

func (wf *GithubWorkflow) BaseUrl() string {
	if base, err := wf.Data.Load(ghBaseUrlKey); err == nil {
		return string(base)
	}
	return ""
}

func (wf *GithubWorkflow) SetBaseUrl(url string) error {
	if ok := gitUrlPattern.MatchString(url); !ok {
		return errPatternMismatch
	}
	return wf.Data.Store(ghBaseUrlKey, []byte(url))
}

func (wf *GithubWorkflow) SetToken(token string) error {
	return wf.Keychain.Set(ghAuthTokenKey, token)
}

func (wf *GithubWorkflow) Token() (string, error) {
	return wf.Keychain.Get(ghAuthTokenKey)
}

func (wf *GithubWorkflow) DisplayPRs() error {
	_, err := wf.Token()
	if err != nil {
		return err
	}

	var data []github.Issue
	if err = wf.Cache.LoadJSON(ghPullRequestsKey, &data); err != nil {
		return err
	}

	for _, pr := range data {

		var reviewState string
		var reviews []github.PullRequestReview

		if err = wf.Cache.LoadJSON(strconv.FormatInt(*pr.ID, 10), &reviews); err != nil {
			log.Printf("failed to load reviews for PR %d, error: %s", *pr.ID, err)
		} else {
			reviewState = constructDisplayState(reviews)
		}

		wf.NewItem(*pr.Title).
			Subtitle(fmt.Sprintf("%s%s#%d by %s, %s",
				reviewState,
				extractProject(*pr.HTMLURL),
				*pr.Number,
				*pr.User.Login,
				pr.UpdatedAt.Format("02-Jan-2006 15:04"))).
			Arg(*pr.HTMLURL).
			Valid(true)
	}
	wf.WarnEmpty("No pull requests to show :(", "")

	return nil
}

func (wf *GithubWorkflow) FetchPRStatus() error {
	token, err := wf.Token()
	if err != nil {
		return err
	}

	var data []github.Issue
	if err = wf.Cache.LoadJSON(ghPullRequestsKey, &data); err != nil {
		return err
	}

	client, err := newGithubClient(wf.ctx, wf.BaseUrl(), token)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(len(data))
	defer wg.Wait()

	// TODO FIXME invalidate cache
	for _, pr := range data {
		go func(p github.Issue) {
			defer wg.Done()

			project := extractProject(*p.HTMLURL)
			owner, repo, _ := strings.Cut(project, "/")

			uniqueKey := strconv.FormatInt(*p.ID, 10)

			var ignored []github.PullRequestReview
			err := wf.Cache.LoadOrStoreJSON(uniqueKey, time.Since(*p.UpdatedAt), func() (interface{}, error) {
				reviews, _, err := client.PullRequests.ListReviews(wf.ctx, owner, repo, *p.Number, nil)
				return reviews, err
			}, &ignored)

			if err != nil {
				panic(err)
			}
		}(pr)
	}

	return nil
}

func run() error {
	args := wf.Args()

	if len(args) == 0 {
		return errMissingArgs
	}

	switch args[0] {
	case "auth":
		if len(args) < 2 {
			return errMissingArgs
		}
		return wf.SetToken(args[1])
	case "base_url":
		if len(args) < 2 {
			return errMissingArgs
		}
		return wf.SetBaseUrl(args[1])
	case "display":
		return wf.DisplayPRs()
	case "update":
		return wf.FetchPRs()
	case "update_status":
		return wf.FetchPRStatus()
	default:
		return errUnknownCmd
	}
}

func (wf *GithubWorkflow) HandleError(err error) {
	if err == kc.ErrNotFound {
		wf.NewItem("No API key set").
			Subtitle("Please use ghpr-auth to set your GitHub personal token").
			Valid(false).
			Icon(aw.IconError)

		tokenUrl := wf.BaseUrl() + "/settings/tokens"
		wf.NewItem("Generate new token on GitHub").
			Subtitle(tokenUrl).
			Arg(tokenUrl).
			Valid(true).
			Icon(aw.IconWeb)
	} else {
		wf.FatalError(err)
	}
}

func main() {
	wf.Run(func() {
		if err := run(); err != nil {
			wf.HandleError(err)
		}
		wf.SendFeedback()
	})
}
