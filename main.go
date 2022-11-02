package main

import (
	"errors"
	"fmt"
	"regexp"

	aw "github.com/deanishe/awgo"
	kc "github.com/deanishe/awgo/keychain"
)

const (
	ghAuthTokenKey    = "gh-auth-token"
	ghBaseUrlKey      = "gh-base-url"
	ghUserInfoKey     = "gh-user-info"
	ghPullRequestsKey = "gh-pull-requests"
)

var (
	gitUrlPattern = regexp.MustCompile("^github(.*)?.com$")
)

var (
	errMissingArgs = errors.New("wrong number of arguments passed")
	errUnknownCmd  = errors.New("unknown command")
)

type GithubWorkflow struct {
	aw.Workflow
}

var wf *GithubWorkflow

func init() {
	wf = &GithubWorkflow{*aw.New()}
}

func (wf *GithubWorkflow) BaseUrl() string {
	if base, err := wf.Data.Load(ghBaseUrlKey); err == nil {
		return string(base)
	}
	return "github.com"
}

func (wf *GithubWorkflow) SetBaseUrl(url string) error {
	if ok := gitUrlPattern.MatchString(url); !ok {
		return fmt.Errorf("url does not match pattern %s", gitUrlPattern)
	}
	return wf.Data.Store(ghBaseUrlKey, []byte(url))
}

func (wf *GithubWorkflow) SetToken(passwd string) error {
	return wf.Keychain.Set(ghAuthTokenKey, passwd)
}

func (wf *GithubWorkflow) Token() (string, error) {
	return wf.Keychain.Get(ghAuthTokenKey)
}

func (wf *GithubWorkflow) DisplayPulls() error {
	_, err := wf.Token()
	if err != nil {
		return err
	}

	var data []pullRequestInfo
	if err = wf.Cache.LoadJSON(ghPullRequestsKey, &data); err != nil {
		return err
	}

	for _, pr := range data {
		wf.NewItem(pr.Title).
			Subtitle(fmt.Sprintf("%s#%d by %s, %s",
				pr.Project(),
				pr.Number,
				pr.User.Login,
				pr.UpdatedAt.Format("02-Jan-2006 15:04"))).
			Arg(pr.HtmlUrl).
			Valid(true)
	}
	wf.WarnEmpty("No PRs to display :(", "")

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
		return wf.DisplayPulls()
	case "update":
		return wf.FetchPulls()
	default:
		return errUnknownCmd
	}
}

func (wf *GithubWorkflow) HandleError(err error) {
	if err == kc.ErrNotFound {
		wf.Feedback.Clear()
		wf.NewItem("No API key set").
			Subtitle("Please use ghpr-auth to set your GitHub personal token").
			Arg(fmt.Sprintf("https://%s/settings/tokens/new", wf.BaseUrl())).
			Valid(true).
			Icon(aw.IconWarning)
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
