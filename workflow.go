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

// Commands that the workflow can run.
const (
	cmdAuth         = "auth"
	cmdBaseUrl      = "base_url"
	cmdDisplay      = "display"
	cmdUpdate       = "update"
	cmdUpdateStatus = "update_status"
	cmdUrlChoices   = "url_choices"
)

// Cache keys used by the workflow.
const (
	ghAuthTokenKey    = "gh-auth-token"
	ghBaseUrlKey      = "gh-base-url"
	ghUserInfoKey     = "gh-user-info"
	ghPullRequestsKey = "gh-pull-requests"
)

// Environment variables used by the workflow.
const (
	ghErrOccurredEnvVar  = "GH_ERR_OCCURRED"
	ghRetryAttemptEnvVar = "GH_ATTEMPTS_LEFT"
)

// Common time and duration parameters used by the workflow.
const (
	rerunDelay = 3 * time.Second
)

// Common regex patterns used by the workflow.
var (
	gitUrlPattern = regexp.MustCompile("^https://api.[a-z.]+.com$")
)

// Common workflow errors.
var (
	errMissingArgs     = errors.New("wrong number of arguments passed")
	errPatternMismatch = errors.New("url does not match pattern " + gitUrlPattern.String())
	errShowNoResults   = errors.New("need to display zero item warning")
	errTaskRunning     = errors.New("task is already running")
	errTokenEmpty      = errors.New("token must not be empty")
	errUnknownCmd      = errors.New("unknown command")
)

// Wrapper around aw.Workflow.
type GithubWorkflow struct {
	aw.Workflow

	ctx context.Context
}

var workflow *GithubWorkflow

// init creates the default workflow.
func init() {
	workflow = &GithubWorkflow{*aw.New(), context.Background()}
}

// GetBaseApiUrl retrieves API URL of the GitHub instance from workflow data.
func (wf *GithubWorkflow) GetBaseApiUrl() string {
	if base, err := wf.Data.Load(ghBaseUrlKey); err == nil {
		return string(base)
	}
	return ""
}

// GetBaseWebUrl retrieves web URL of the GitHub instance from workflow data.
func (wf *GithubWorkflow) GetBaseWebUrl() string {
	return strings.ReplaceAll(wf.GetBaseApiUrl(), "https://api.", "https://")
}

// SetBaseUrl stores URL of the GitHub instance as workflow data.
func (wf *GithubWorkflow) SetBaseUrl(url string) error {
	if ok := gitUrlPattern.MatchString(url); !ok {
		return errPatternMismatch
	}
	return wf.Data.Store(ghBaseUrlKey, []byte(url))
}

// GetToken retrieves the API token from user's keychain.
func (wf *GithubWorkflow) GetToken() (string, error) {
	return wf.Keychain.Get(ghAuthTokenKey)
}

// SetToken sets the API token in user's keychain, and invalidates cache with github user login.
func (wf *GithubWorkflow) SetToken(token string) error {
	if token == "" {
		return errTokenEmpty
	}

	// remove previously cached login and PRs
	for _, itm := range []string{ghUserInfoKey, ghPullRequestsKey} {
		if err := wf.Cache.Store(itm, nil); err != nil {
			log.Println(err)
		}
	}

	return wf.Keychain.Set(ghAuthTokenKey, token)
}

// LaunchBackgroundTask starts a workflow task in the background (if it is not running already).
func (wf *GithubWorkflow) LaunchBackgroundTask(task string, arg ...string) error {
	log.Printf("Launching task '%s' in background...", task)
	if wf.IsRunning(task) {
		return errTaskRunning
	}

	cmdArgs := append([]string{task}, arg...)
	return wf.RunInBackground(task, exec.Command(os.Args[0], cmdArgs...))
}

// DisplayPRs sends the list of pull requests to Alfred as feedback items.
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

// FetchPRs searches GitHub for any pull requests that satisfy the user query,
// and caches the metadata and review status for each PR.
func (wf *GithubWorkflow) FetchPRs() error {
	token, err := wf.GetToken()
	if err != nil {
		return err
	}

	client, err := newGithubClient(wf.ctx, wf.GetBaseApiUrl(), token)
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

// FetchPRStatus gets the review status of pull requests from GitHub.
func (wf *GithubWorkflow) FetchPRStatus() error {
	token, err := wf.GetToken()
	if err != nil {
		return err
	}

	var prs []github.Issue
	if err = wf.Cache.LoadJSON(ghPullRequestsKey, &prs); err != nil {
		return err
	}

	client, err := newGithubClient(wf.ctx, wf.GetBaseApiUrl(), token)
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

// DisplayUrlChoices sends the GitHub URL options to Alfred as feedback items.
func (wf *GithubWorkflow) DisplayUrlChoices(url string) error {
	u := url

	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "api.")

	if u != "" {
		u = "https://api." + u

		if gitUrlPattern.MatchString(u) {
			wf.NewItem(url).
				Subtitle("Set URL as " + u).
				Arg(u).
				Valid(true)
		} else {
			wf.NewItem(url).
				Subtitle(u + " does not match pattern " + gitUrlPattern.String()).
				Icon(aw.IconError).
				Valid(false)
		}
	}

	wf.NewItem("(Default) github.com").
		Subtitle("Set URL as https://api.github.com").
		Arg("https://api.github.com").
		Valid(true)

	return nil
}

// run executes the workflow logic. It delegates to
// concrete workflow methods, based on parsed command line arguments.
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

// HandleMissingToken indicates to user that the API token is not set.
func (wf *GithubWorkflow) HandleMissingToken() {
	wf.NewItem("No API key set").
		Subtitle("Please use ghpr-auth to set your GitHub personal token").
		Valid(false).
		Icon(aw.IconError)

	tokenUrl := wf.GetBaseWebUrl() + "/settings/tokens/new"
	wf.NewItem("Generate new token on GitHub").
		Subtitle(tokenUrl).
		Arg(tokenUrl + "?description=go-ghpr&scopes=repo").
		Valid(true).
		Icon(aw.IconWeb)
}

// HandleUpdateNeeded retries 'update' task, if allowed by the attempt limit.
func (wf *GithubWorkflow) HandleUpdateNeeded(upd *updateNeeded) {
	if upd.attemptsLeft <= 0 {
		wf.FatalError(upd)
	}

	wf.NewItem("Loading...").
		Subtitle(fmt.Sprintf("will retry a few times - %d attempt(s) left", upd.attemptsLeft)).
		Valid(false)

	wf.Rerun(rerunDelay.Seconds())
	wf.Var(ghRetryAttemptEnvVar, strconv.Itoa(upd.attemptsLeft))

	if err := wf.LaunchBackgroundTask(cmdUpdate); err != nil {
		log.Println("failed to launch update task:", err)
	}
}

// HandleError converts workflow errors to Alfred feedback items.
func (wf *GithubWorkflow) HandleError(e error) {
	if upd, ok := e.(*updateNeeded); ok {
		wf.HandleUpdateNeeded(upd)
		return
	}

	wf.Var(ghErrOccurredEnvVar, "true")

	switch e {
	case kc.ErrNotFound:
		wf.HandleMissingToken()
	case errShowNoResults:
		wf.WarnEmpty("No pull requests were found :(", "")
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
