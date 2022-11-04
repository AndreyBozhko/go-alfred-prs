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
	cmdUrlChoices   = "url_choices"
)

const (
	ghAuthTokenKey    = "gh-auth-token"
	ghBaseUrlKey      = "gh-base-url"
	ghUserInfoKey     = "gh-user-info"
	ghPullRequestsKey = "gh-pull-requests"
)

const (
	ghRetryAttemptKey = "GH_ATTEMPTS_LEFT"
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
)

type GithubWorkflow struct {
	aw.Workflow

	ctx context.Context
}

var workflow *GithubWorkflow

func init() {
	workflow = &GithubWorkflow{*aw.New(), context.Background()}
}

func (wf *GithubWorkflow) GetBaseUrl() string {
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

func (wf *GithubWorkflow) GetToken() (string, error) {
	return wf.Keychain.Get(ghAuthTokenKey)
}

func (wf *GithubWorkflow) SetToken(token string) error {
	if token == "" {
		return errTokenEmpty
	}
	return wf.Keychain.Set(ghAuthTokenKey, token)
}

func (wf *GithubWorkflow) LaunchBackgroundTask(task string, arg ...string) error {
	log.Printf("Launching task '%s' in background...", task)
	if wf.IsRunning(task) {
		return errTaskRunning
	}

	cmdArgs := append([]string{task}, arg...)
	return wf.RunInBackground(task, exec.Command(os.Args[0], cmdArgs...))
}

func (wf *GithubWorkflow) DisplayPRs(attemptsLeft int) error {
	_, err := wf.GetToken()
	if err != nil {
		return err
	}

	if wf.Cache.Expired(ghPullRequestsKey, time.Hour) {
		return &updateNeeded{
			"could not load pull requests - try running ghpr-update manually",
			attemptsLeft - 1,
		}
	}

	var prs []github.Issue
	if err = wf.Cache.LoadJSON(ghPullRequestsKey, &prs); err != nil {
		return err
	}

	if len(prs) == 0 {
		return errShowNoResults
	}

	zone, _ := time.LoadLocation("Local")

	for _, pr := range prs {

		var reviewState string
		var reviews []github.PullRequestReview

		uniqueKey := strconv.FormatInt(*pr.ID, 10)
		if err = wf.Cache.LoadJSON(uniqueKey, &reviews); err != nil {
			log.Printf("failed to load reviews for PR %d, error: %s", *pr.ID, err)
		} else {
			reviewState = parseReviewState(reviews)
		}

		wf.NewItem(*pr.Title + reviewState).
			Subtitle(fmt.Sprintf("%s#%d by %s, %s",
				parseRepoFromUrl(*pr.HTMLURL),
				*pr.Number,
				*pr.User.Login,
				pr.UpdatedAt.In(zone).Format("02-Jan-2006 15:04"))).
			Arg(*pr.HTMLURL).
			Valid(true)
	}

	return nil
}

func (wf *GithubWorkflow) FetchPRs() error {
	token, err := wf.GetToken()
	if err != nil {
		return err
	}

	client, err := newGithubClient(wf.ctx, wf.GetBaseUrl(), token)
	if err != nil {
		return err
	}

	var user github.User
	err = wf.Cache.LoadOrStoreJSON(
		ghUserInfoKey,
		time.Hour,
		func() (interface{}, error) {
			u, _, err := client.Users.Get(wf.ctx, "")
			return u, err
		},
		&user)
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

	defer func() {
		if err := wf.LaunchBackgroundTask(cmdUpdateStatus); err != nil {
			log.Println("failed to launch update task:", err)
		}
	}()

	return wf.Cache.StoreJSON(ghPullRequestsKey, deduplicateAndSort(prs))
}

func (wf *GithubWorkflow) FetchPRStatus() error {
	token, err := wf.GetToken()
	if err != nil {
		return err
	}

	var prs []github.Issue
	if err = wf.Cache.LoadJSON(ghPullRequestsKey, &prs); err != nil {
		return err
	}

	client, err := newGithubClient(wf.ctx, wf.GetBaseUrl(), token)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	// TODO FIXME invalidate cache
	wg.Add(len(prs))
	for _, pr := range prs {
		go func(p github.Issue) {
			defer wg.Done()

			project := parseRepoFromUrl(*p.HTMLURL)
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

func (wf *GithubWorkflow) DisplayUrlChoices(url string) error {
	if url != "" {
		wf.NewItem("[Custom]: " + url).
			Subtitle("Set URL as https://" + url).
			Arg("https://" + url)
	}

	wf.NewItem("[Default]: github.com").
		Subtitle("Set URL as https://github.com").
		Arg("")

	return nil
}

func run() error {
	args := workflow.Args()

	if len(args) < 1 {
		return errMissingArgs
	}
	if len(args) == 1 {
		args = append(args, "")
	}

	cmd, arg := args[0], args[1]

	switch cmd {
	case cmdAuth:
		return workflow.SetToken(arg)
	case cmdBaseUrl:
		return workflow.SetBaseUrl(arg)
	case cmdDisplay:
		attempt, _ := strconv.Atoi(arg)
		return workflow.DisplayPRs(attempt)
	case cmdUpdate:
		return workflow.FetchPRs()
	case cmdUpdateStatus:
		return workflow.FetchPRStatus()
	case cmdUrlChoices:
		return workflow.DisplayUrlChoices(arg)
	default:
		return errUnknownCmd
	}
}

func (wf *GithubWorkflow) HandleMissingToken() {
	wf.NewItem("No API key set").
		Subtitle("Please use ghpr-auth to set your GitHub personal token").
		Valid(false).
		Icon(aw.IconError)

	tokenUrl := wf.GetBaseUrl() + "/settings/tokens"
	wf.NewItem("Generate new token on GitHub").
		Subtitle(tokenUrl).
		Arg(tokenUrl).
		Valid(true).
		Icon(aw.IconWeb)
}

func (wf *GithubWorkflow) HandleUpdateNeeded(upd *updateNeeded) {
	if upd.attemptsLeft <= 0 {
		wf.FatalError(upd)
	}

	wf.NewItem("Loading...").
		Subtitle(fmt.Sprintf("will retry a few times - %d attempt(s) left", upd.attemptsLeft)).
		Valid(false)

	wf.Rerun(rerunDelay.Seconds())
	wf.Var(ghRetryAttemptKey, strconv.Itoa(upd.attemptsLeft))

	if err := wf.LaunchBackgroundTask(cmdUpdate); err != nil {
		log.Println("failed to launch update task:", err)
	}
}

func (wf *GithubWorkflow) HandleError(e error) {
	if upd, ok := e.(*updateNeeded); ok {
		wf.HandleUpdateNeeded(upd)
		return
	}

	switch e {
	case kc.ErrNotFound:
		wf.HandleMissingToken()
	case errShowNoResults:
		wf.WarnEmpty("No pull requests to display :(", "")
	default:
		wf.FatalError(e)
	}
}

func main() {
	workflow.Run(func() {
		if err := run(); err != nil {
			workflow.HandleError(err)
		}
		workflow.SendFeedback()
	})
}
