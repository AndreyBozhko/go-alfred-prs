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
	"github.com/google/go-github/v48/github"
)

// Commands that the workflow can run.
const (
	cmdAuth         = "auth"
	cmdDisplay      = "display"
	cmdUpdate       = "update"
	cmdUpdateStatus = "update_status"
)

// Cache keys used by the workflow.
const (
	wfAuthTokenKey    = "gh-auth-token"
	wfBaseUrlKey      = "gh-base-url"
	wfUserInfoKey     = "gh-user-info"
	wfPullRequestsKey = "gh-pull-requests"
)

// Variables that can be set in the workflow feedback.
const (
	fbAttemptsLeftKey  = "GH_ATTEMPTS_LEFT"
	fbErrorOccurredKey = "GH_ERROR_OCCURRED"
)

// Environment variables used by the workflow.
var (
	envVars = struct {
		cacheMaxAge string
		gitBaseUrl  string
		roleFilters string
		showReviews string
	}{
		os.Getenv("CACHE_AGE_SECONDS"),
		os.Getenv("GIT_BASE_URL"),
		os.Getenv("QUERY_BY_ROLES"),
		os.Getenv("SHOW_REVIEWS"),
	}
)

// Common time and duration parameters used by the workflow.
const (
	cacheMaxAgeDefault = 600 * time.Second
	rerunDelayDefault  = 3 * time.Second
)

// Common regex patterns used by the workflow.
var (
	gitUrlPattern      = regexp.MustCompile(`^(https://)?(api.)?[a-z.]+\.com$`)
	roleFiltersPattern = regexp.MustCompile(`^([+-](assignee|author|involves|mentions|review-requested),?)*$`)
	singleRolePattern  = regexp.MustCompile(`(([+-])(assignee|author|involves|mentions|review-requested))`)
)

// Common workflow errors.
var (
	errMissingUrl  = errors.New("github url is not set")
	errTaskRunning = errors.New("task is already running")
	errTokenEmpty  = errors.New("token must not be empty")
	errUnknownCmd  = errors.New("unknown command")
)

// GithubWorkflow is a wrapper around aw.Workflow.
type GithubWorkflow struct {
	*aw.Workflow

	cacheMaxAge  time.Duration
	roleFilters  []string
	fetchReviews bool
	gitApiUrl    string
}

// getCacheMaxAge returns the max age configuration for the workflow cache.
func getCacheMaxAge() time.Duration {
	if age, err := strconv.Atoi(envVars.cacheMaxAge); err == nil {
		return time.Duration(age) * time.Second
	}

	return cacheMaxAgeDefault
}

// configureRoleFilters returns user roles which will be used to search for open pull requests.
func (wf *GithubWorkflow) configureRoleFilters() error {
	input := envVars.roleFilters

	if ok := roleFiltersPattern.MatchString(input); !ok {
		return &alfredError{"invalid config: " + input, "expected something like +author,+review-requested"}
	}

	wf.roleFilters = parseRoleFilters(input)

	return nil
}

// getShowReviews returns flag that enables or disables showing PR reviews.
func getShowReviews() bool {
	return strings.ToLower(envVars.showReviews) == "true"
}

// configureBaseUrl parses git url from an environment variable,
// updates the workflow, and invalidates workflow cache if needed.
func (wf *GithubWorkflow) configureBaseUrl() error {
	u := envVars.gitBaseUrl
	if u == "" {
		return errMissingUrl
	}

	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "api.")

	if u != "" {
		u = "https://api." + u
	}

	if !gitUrlPattern.MatchString(u) {
		return &alfredError{"invalid github url: " + envVars.gitBaseUrl, "expected something like github.com"}
	}

	wf.gitApiUrl = u

	// remove previously cached username and PRs
	// if current git url does not match cached url
	prevUrl, err := wf.Cache.Load(wfBaseUrlKey)
	if err == nil && string(prevUrl) == u {
		return nil
	}

	if err = wf.ClearCache(); err != nil {
		return err
	}

	if err = wf.Cache.Store(wfBaseUrlKey, []byte(u)); err != nil {
		return err
	}

	return nil
}

// GetBaseWebUrl retrieves web URL of the GitHub instance from workflow data.
func (wf *GithubWorkflow) GetBaseWebUrl() string {
	return strings.ReplaceAll(wf.gitApiUrl, "https://api.", "https://")
}

// GetToken retrieves the API token from user's keychain.
func (wf *GithubWorkflow) GetToken() (string, error) {
	return wf.Keychain.Get(wfAuthTokenKey)
}

// SetToken saves the API token in user's keychain, and invalidates workflow cache.
func (wf *GithubWorkflow) SetToken(token string) error {
	if token == "" {
		return errTokenEmpty
	}

	// remove previously cached username and PRs
	if err := wf.ClearCache(); err != nil {
		return err
	}

	return wf.Keychain.Set(wfAuthTokenKey, token)
}

// DisplayPRs sends the list of pull requests to Alfred as feedback items.
func (wf *GithubWorkflow) DisplayPRs(attemptsLeft int) error {
	_, err := wf.GetToken()
	if err != nil {
		return err
	}

	var prs []github.Issue
	if err = wf.Cache.LoadJSON(wfPullRequestsKey, &prs); err != nil {
		log.Println(err)
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

		wf.NewItem(strings.TrimSpace(*pr.Title + " " + reviewState)).
			Subtitle(fmt.Sprintf("%s#%d by %s, %s",
				parseRepoFromUrl(*pr.HTMLURL),
				*pr.Number,
				*pr.User.Login,
				pr.UpdatedAt.In(zone).Format("02-Jan-2006 15:04"))).
			Arg(*pr.HTMLURL).
			Valid(true)
	}

	if wf.Cache.Expired(wfPullRequestsKey, wf.cacheMaxAge) {
		return &retryable{
			"Could not load pull requests :(",
			"try running ghpr-update manually",
			attemptsLeft - 1,
		}
	}

	// fallback in case cache exists, but prs is empty
	wf.InfoEmpty("No pull requests were found :(", "")

	return nil
}

// FetchPRs searches GitHub for any pull requests that satisfy the user query,
// and caches the metadata and review status for each PR.
func (wf *GithubWorkflow) FetchPRs() error {
	ctx := context.Background()

	token, err := wf.GetToken()
	if err != nil {
		return err
	}

	client, err := newGithubClient(ctx, wf.gitApiUrl, token)
	if err != nil {
		return err
	}

	var user github.User
	err = wf.Cache.LoadOrStoreJSON(
		wfUserInfoKey,
		0,
		func() (interface{}, error) {
			u, _, err := client.Users.Get(ctx, "")
			return u, err
		},
		&user)
	if err != nil {
		return err
	}

	var prs []*github.Issue
	for _, role := range wf.roleFilters {
		query := fmt.Sprintf("type:pr is:open %s:%s", role, *user.Login)
		issues, _, err := client.Search.Issues(ctx, query, nil)
		if err != nil {
			return err
		}
		prs = append(prs, issues.Issues...)
	}

	if wf.fetchReviews {
		defer func() {
			if err := wf.LaunchBackgroundTask(cmdUpdateStatus); err != nil {
				log.Println("failed to launch update task:", err)
			}
		}()
	}

	return wf.Cache.StoreJSON(wfPullRequestsKey, deduplicateAndSort(prs))
}

// FetchPRStatus gets the review status of pull requests from GitHub.
func (wf *GithubWorkflow) FetchPRStatus() error {
	ctx := context.Background()

	token, err := wf.GetToken()
	if err != nil {
		return err
	}

	var prs []github.Issue
	if err = wf.Cache.LoadJSON(wfPullRequestsKey, &prs); err != nil {
		return err
	}

	client, err := newGithubClient(ctx, wf.gitApiUrl, token)
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
					reviews, _, err := client.PullRequests.ListReviews(ctx, owner, repo, *p.Number, nil)
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

// LaunchBackgroundTask starts a workflow task in the background (if it is not running already).
func (wf *GithubWorkflow) LaunchBackgroundTask(task string, arg ...string) error {
	log.Printf("Launching task '%s' in background...", task)
	if wf.IsRunning(task) {
		return errTaskRunning
	}

	cmdArgs := append([]string{task}, arg...)
	return wf.RunInBackground(task, exec.Command(os.Args[0], cmdArgs...))
}

// LaunchUpdateTask retries 'update' task, if allowed by the attempt limit.
func (wf *GithubWorkflow) LaunchUpdateTask(attemptsLeft int) {
	wf.NewItem("Fetching pull requests from GitHub...").
		Subtitle(fmt.Sprintf("will retry a few times - %d attempt(s) left", attemptsLeft)).
		Icon(aw.IconSync).
		Valid(false)

	wf.Rerun(rerunDelayDefault.Seconds())
	wf.Var(fbAttemptsLeftKey, strconv.Itoa(attemptsLeft))

	if err := wf.LaunchBackgroundTask(cmdUpdate); err != nil {
		log.Println("failed to launch update task:", err)
	}
}

var workflow *GithubWorkflow

const help = `
Alfred workflow for GitHub pull requests

Usage:
	go-ghpr [command]

Available Commands:
	auth          set API token
	display       display pull requests
	update        update pull requests
	update_status update review status of pull requests
`

// init displays the help message if no command line arguments were passed,
// passed and terminates the program. Otherwise, the package initialization
// continues in the second init func.
func init() {
	if len(os.Args) < 2 {
		println(strings.TrimSpace(help))
		os.Exit(0)
	}
}

// init creates the default workflow.
func init() {
	if _, err := os.Stat(aw.IconWarning.Value); err != nil {
		// substitute icon if it doesn't exist
		aw.IconWarning = aw.IconError
	}

	workflow = &GithubWorkflow{
		Workflow:     aw.New(),
		cacheMaxAge:  getCacheMaxAge(),
		roleFilters:  []string{},
		fetchReviews: getShowReviews(),
		gitApiUrl:    "",
	}
}

// run executes the workflow logic. It delegates to concrete
// workflow methods, based on parsed command line arguments.
func run() error {
	args := workflow.Args()

	if len(args) == 1 {
		args = append(args, "")
	}

	cmd, arg := args[0], args[1]

	if err := workflow.configureBaseUrl(); err != nil {
		return err
	}
	if err := workflow.configureRoleFilters(); err != nil {
		return err
	}

	switch cmd {
	case cmdAuth:
		return workflow.SetToken(arg)
	case cmdDisplay:
		attempt, _ := strconv.Atoi(arg)
		return workflow.DisplayPRs(attempt)
	case cmdUpdate:
		return workflow.FetchPRs()
	case cmdUpdateStatus:
		return workflow.FetchPRStatus()
	default:
		return errUnknownCmd
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
