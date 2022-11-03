package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
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
	cmdAuth         = "auth"
	cmdBaseUrl      = "base_url"
	cmdDisplay      = "display"
	cmdUpdate       = "update"
	cmdUpdateStatus = "update_status"
)

const (
	ghAuthTokenKey    = "gh-auth-token"
	ghBaseUrlKey      = "gh-base-url"
	ghUserInfoKey     = "gh-user-info"
	ghPullRequestsKey = "gh-pull-requests"
)

const (
	rerunDelay = 3 * time.Second
)

var (
	gitUrlPattern = regexp.MustCompile("^https://(.*)?.com$")
)

var (
	errMissingArgs     = errors.New("wrong number of arguments passed")
	errPatternMismatch = errors.New("url does not match pattern " + gitUrlPattern.String())
	errShowNoResults   = errors.New("need to display zero item warning")
	errTaskRunning     = errors.New("task is already running")
	errTokenEmpty      = errors.New("token must not be empty")
	errUnknownCmd      = errors.New("unknown command")
	errUpdateNeeded    = errors.New("need to refresh pull requests cache")
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
	if token == "" {
		return errTokenEmpty
	}
	return wf.Keychain.Set(ghAuthTokenKey, token)
}

func (wf *GithubWorkflow) Token() (string, error) {
	return wf.Keychain.Get(ghAuthTokenKey)
}

func (wf *GithubWorkflow) LaunchBackgroundTask(task string, arg ...string) error {
	log.Printf("Launching task '%s' in background...", task)
	if wf.IsRunning(task) {
		return errTaskRunning
	}

	cmdArgs := append([]string{task}, arg...)
	return wf.RunInBackground(task, exec.Command(os.Args[0], cmdArgs...))
}

func (wf *GithubWorkflow) DisplayPRs(allowUpdates bool) error {
	_, err := wf.Token()
	if err != nil {
		return err
	}

	var data []github.Issue
	if err = wf.Cache.LoadJSON(ghPullRequestsKey, &data); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if allowUpdates {
				return errUpdateNeeded
			} else {
				return errShowNoResults
			}
		}
		return err
	}

	for _, pr := range data {

		var reviewState string
		var reviews []github.PullRequestReview

		uniqueKey := strconv.FormatInt(*pr.ID, 10)
		if err = wf.Cache.LoadJSON(uniqueKey, &reviews); err != nil {
			log.Printf("failed to load reviews for PR %d, error: %s", *pr.ID, err)
		} else {
			reviewState = constructDisplayState(reviews)
		}

		wf.NewItem(*pr.Title + reviewState).
			Subtitle(fmt.Sprintf("%s#%d by %s, %s",
				parseRepo(*pr.HTMLURL),
				*pr.Number,
				*pr.User.Login,
				pr.UpdatedAt.Format("02-Jan-2006 15:04"))).
			Arg(*pr.HTMLURL).
			Valid(true)
	}

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
	defer wg.Wait()

	// TODO FIXME invalidate cache
	wg.Add(len(data))
	for _, pr := range data {
		go func(p github.Issue) {
			defer wg.Done()

			project := parseRepo(*p.HTMLURL)
			owner, repo, _ := strings.Cut(project, "/")

			uniqueKey := strconv.FormatInt(*p.ID, 10)

			var ignored []github.PullRequestReview
			err := wf.Cache.LoadOrStoreJSON(
				uniqueKey,
				time.Since(*p.UpdatedAt),
				func() (interface{}, error) {
					reviews, _, err := client.PullRequests.ListReviews(wf.ctx, owner, repo, *p.Number, nil)
					return reviews, err
				},
				&ignored)

			if err != nil {
				panic(err)
			}
		}(pr)
	}

	return nil
}

func run() error {
	args := wf.Args()

	if len(args) < 1 {
		return errMissingArgs
	}
	if len(args) == 1 {
		args = append(args, "")
	}

	cmd, arg := args[0], args[1]

	switch cmd {
	case cmdAuth:
		return wf.SetToken(arg)
	case cmdBaseUrl:
		return wf.SetBaseUrl(arg)
	case cmdDisplay:
		allowUpdates := arg == "--allow-updates"
		return wf.DisplayPRs(allowUpdates)
	case cmdUpdate:
		return wf.FetchPRs()
	case cmdUpdateStatus:
		return wf.FetchPRStatus()
	default:
		return errUnknownCmd
	}
}

func (wf *GithubWorkflow) HandleMissingToken() {
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
}

func (wf *GithubWorkflow) HandleError(err error) {
	switch err {
	case kc.ErrNotFound:
		wf.HandleMissingToken()
	case errShowNoResults:
		wf.WarnEmpty("No pull requests to display :(", "")
	case errUpdateNeeded:
		if err1 := wf.LaunchBackgroundTask(cmdUpdate); err1 != nil {
			log.Println("failed to launch update task:", err1)
		}
		wf.NewItem("Loading...").Valid(false)
		wf.Feedback.Rerun(rerunDelay.Seconds())
	default:
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
