package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	aw "github.com/deanishe/awgo"
	"github.com/deanishe/awgo/update"
	"github.com/google/go-github/v48/github"
	"go.deanishe.net/env"
	"golang.org/x/sync/errgroup"
)

// Workflow flags and arguments.
var (
	attempt           int
	maxAttempts       int
	cmdAuth           bool
	cmdCheck          bool
	cmdDisplay        bool
	cmdUpdatePRs      bool
	cmdUpdatePRStatus bool
	query             string
)

// Cache keys used by the workflow.
const (
	wfAuthTokenKey    = "gh-auth-token"
	wfUserInfoKey     = "gh-user-info"
	wfPullRequestsKey = "gh-pull-requests"
)

// Variables that can be set in the workflow feedback.
const (
	fbCurrentAttemptKey = "GH_CURRENT_ATTEMPT"
	fbErrorOccurredKey  = "GH_ERROR_OCCURRED"
)

// workflowConfig holds environment variables used by the workflow.
type workflowConfig struct {
	AllowUpdates bool          `env:"CHECK_FOR_UPDATES"`
	CacheMaxAge  time.Duration `env:"CACHE_MAX_AGE"`
	FetchReviews bool          `env:"SHOW_REVIEWS"`
	GitApiUrl    string        `env:"GIT_BASE_URL"`
	RoleFilters  []string      `env:"QUERY_BY_ROLES"`
}

// Common time and duration parameters used by the workflow.
const (
	rerunDelayDefault = 3 * time.Second
)

// Common regex patterns used by the workflow.
var (
	gitUrlPattern = regexp.MustCompile(`^(https://)?(api.)?[a-z.]+\.com$`)
)

// Common workflow errors.
var (
	errMissingUrl = errors.New("github url is not set")
	errTokenEmpty = errors.New("token must not be empty")
)

// GithubWorkflow is a wrapper around aw.Workflow.
type GithubWorkflow struct {
	// Alfred workflow API
	*aw.Workflow
	// additional configs
	*workflowConfig
}

// validateRoleFilters parses user roles which will be used to search for open pull requests.
func (wf *GithubWorkflow) validateRoleFilters() error {
	filters, err := parseRoleFilters(wf.RoleFilters)
	if err != nil {
		return err
	}

	wf.RoleFilters = filters
	return nil
}

// validateBaseUrl parses git url from an environment variable,
// updates the workflow, and invalidates workflow cache if needed.
func (wf *GithubWorkflow) validateBaseUrl() error {
	u := wf.GitApiUrl
	if u == "" {
		return errMissingUrl
	}

	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "api.")

	if u != "" {
		u = "https://api." + u
	}

	if !gitUrlPattern.MatchString(u) {
		return &alfredError{"invalid github url: " + wf.GitApiUrl, "expected something like github.com"}
	}

	wf.GitApiUrl = u

	// remove previously cached user info and PRs
	// if current git url does not match cached url
	var user github.User
	err := wf.Cache.LoadJSON(wfUserInfoKey, &user)
	if err == nil && !strings.HasPrefix(*user.HTMLURL, wf.GetBaseWebUrl()) {
		return wf.ClearCache()
	}

	return nil
}

// GetBaseWebUrl retrieves web URL of the GitHub instance from workflow data.
func (wf *GithubWorkflow) GetBaseWebUrl() string {
	return strings.ReplaceAll(wf.GitApiUrl, "https://api.", "https://")
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
func (wf *GithubWorkflow) DisplayPRs(currentAttempt int) error {
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
		var reviews []*github.PullRequestReview

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

	if wf.Cache.Expired(wfPullRequestsKey, wf.CacheMaxAge) {
		return &retryable{
			"Could not load pull requests :(",
			"try running ghpr-update manually",
			currentAttempt,
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

	client, err := newGithubClient(ctx, wf.GitApiUrl, token)
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

	wg, ctx := errgroup.WithContext(ctx)
	results := make([]*github.IssuesSearchResult, len(wf.RoleFilters))
	for i, role := range wf.RoleFilters {
		i, role := i, role
		wg.Go(func() error {
			query := fmt.Sprintf("type:pr is:open %s:%s", role, *user.Login)
			issues, _, err := client.Search.Issues(ctx, query, nil)
			if err != nil {
				return err
			}
			results[i] = issues
			return nil
		})
	}

	if err = wg.Wait(); err != nil {
		return err
	}

	var prs []*github.Issue
	for _, issues := range results {
		prs = append(prs, issues.Issues...)
	}

	if wf.FetchReviews {
		defer func() {
			if err := wf.LaunchBackgroundTask("--update_status"); err != nil {
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

	var prs []*github.Issue
	if err = wf.Cache.LoadJSON(wfPullRequestsKey, &prs); err != nil {
		return err
	}

	client, err := newGithubClient(ctx, wf.GitApiUrl, token)
	if err != nil {
		return err
	}

	wg, ctx := errgroup.WithContext(ctx)

	// TODO FIXME invalidate cache
	for _, pr := range prs {
		pr := pr
		wg.Go(func() error {
			project := parseRepoFromUrl(*pr.HTMLURL)
			owner, repo, _ := strings.Cut(project, "/")

			uniqueKey := strconv.FormatInt(*pr.ID, 10)

			var ignored []github.PullRequestReview
			return wf.Cache.LoadOrStoreJSON(
				uniqueKey,
				time.Since(*pr.UpdatedAt),
				func() (interface{}, error) {
					reviews, _, err := client.PullRequests.ListReviews(ctx, owner, repo, *pr.Number, nil)
					return reviews, err
				},
				&ignored)
		})
	}

	return wg.Wait()
}

// LaunchBackgroundTask starts a workflow task in the background (if it is not running already).
func (wf *GithubWorkflow) LaunchBackgroundTask(task string, arg ...string) error {
	log.Printf("Launching task '%s' in background...", task)
	cmdArgs := append([]string{task}, arg...)
	return wf.RunInBackground(task, exec.Command(os.Args[0], cmdArgs...))
}

// LaunchUpdateTask retries 'update' task, if allowed by the attempt limit.
func (wf *GithubWorkflow) LaunchUpdateTask(currentAttempt int) {
	subtitle := ""
	if currentAttempt > 0 {
		subtitle = fmt.Sprintf("something went wrong - retrying (attempt #%d)...", currentAttempt)
	}

	wf.NewItem("Fetching pull requests from GitHub...").
		Subtitle(subtitle).
		Icon(aw.IconSync).
		Valid(false)

	wf.Rerun(rerunDelayDefault.Seconds())
	wf.Var(fbCurrentAttemptKey, strconv.Itoa(currentAttempt+1))

	if err := wf.LaunchBackgroundTask("--update"); err != nil {
		log.Println("failed to launch update task:", err)
	}
}

// ShowNewVersions pulls workflow release info from GitHub
// and caches it locally, if the check is due.
func (wf *GithubWorkflow) ShowNewVersions(shouldDisplayPrompt bool) error {
	if wf.UpdateCheckDue() {
		if err := wf.LaunchBackgroundTask("--check"); err != nil {
			return err
		}
	}

	if shouldDisplayPrompt && wf.UpdateAvailable() {
		wf.NewItem("Update available!").
			Subtitle("press to install").
			Arg("workflow:update").
			Valid(true).
			Icon(aw.IconWeb)
	}

	return nil
}

var workflow *GithubWorkflow

// init defines command-line flags
func init() {
	flag.BoolVar(&cmdAuth, "auth", false, "set API token")
	flag.BoolVar(&cmdCheck, "check", false, "check for workflow updates")
	flag.BoolVar(&cmdDisplay, "display", false, "display pull requests")
	flag.BoolVar(&cmdUpdatePRs, "update", false, "update pull requests cache")
	flag.BoolVar(&cmdUpdatePRStatus, "update_status", false, "update PR status cache")
	flag.IntVar(&attempt, "attempt", 0, "indicate # of attempts so far")
	flag.IntVar(&maxAttempts, "max_attempts", 0, "indicate # of allowed attempts")
	flag.StringVar(&query, "query", "", "command input")
}

// init creates and configures the workflow
func init() {
	if _, err := os.Stat(aw.IconWarning.Value); err != nil {
		// substitute icon if it doesn't exist
		aw.IconWarning = aw.IconError
	}

	workflow = &GithubWorkflow{
		Workflow:       aw.New(update.GitHub("AndreyBozhko/go-alfred-prs")),
		workflowConfig: &workflowConfig{},
	}
}

// run executes the workflow logic. It delegates to concrete
// workflow methods, based on parsed command line arguments.
func run() error {
	// parse args and handle magic commands
	workflow.Args()
	flag.Parse()

	if err := env.Bind(workflow.workflowConfig); err != nil {
		return &alfredError{"cannot parse environment variables", err.Error()}
	}

	// load remaining workflow configurations
	if err := workflow.validateBaseUrl(); err != nil {
		return err
	}
	if err := workflow.validateRoleFilters(); err != nil {
		return err
	}

	// workflow logic
	if cmdAuth {
		return workflow.SetToken(query)
	}
	if cmdCheck {
		return workflow.CheckForUpdate()
	}
	if cmdDisplay {
		// handle updates
		if workflow.AllowUpdates {
			shouldDisplayPrompt := query == ""
			if err := workflow.ShowNewVersions(shouldDisplayPrompt); err != nil {
				return err
			}
		}
		return workflow.DisplayPRs(attempt)
	}
	if cmdUpdatePRs {
		return workflow.FetchPRs()
	}
	if cmdUpdatePRStatus {
		return workflow.FetchPRStatus()
	}

	// fallback
	println("Alfred workflow for GitHub pull requests\n")
	flag.Usage()

	return nil
}

func main() {
	workflow.Run(func() {
		if err := run(); err != nil {
			workflow.HandleError(err)
		}
		workflow.SendFeedback()
	})
}
