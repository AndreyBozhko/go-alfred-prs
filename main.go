package main

import (
	"errors"
	"fmt"
        "sort"

	aw "github.com/deanishe/awgo"
	kc "github.com/deanishe/awgo/keychain"
)

const (
	ghAuthTokenKey  = "gh-auth-token"
	userNameKey     = "username"
	pullRequestsKey = "pull-requests"
)

type GithubWorkflow struct {
	aw.Workflow
}

func (wf *GithubWorkflow) Token() (string, error) {
	return wf.Keychain.Get(ghAuthTokenKey)
}

var wf *GithubWorkflow

var (
	missingArgsError = errors.New("wrong number of arguments passed")
	unknownCmdError  = errors.New("unknown command")
)

func init() {
	wf = &GithubWorkflow{}
}

func (wf *GithubWorkflow) doAuth(passwd string) error {
	return wf.Keychain.Set(ghAuthTokenKey, passwd)
}

func (wf *GithubWorkflow) doList() error {
	_, err := wf.Token()
	if err != nil {
		return err
	}

	var data []pullRequestInfo
	if err = wf.Cache.LoadJSON(pullRequestsKey, &data); err != nil {
		return err
	}
	sort.Slice(data, func(i, j int) bool {
		return data[i].UpdatedAt.After(data[j].UpdatedAt)
	})

	for _, pr := range data {
		wf.NewItem(pr.Title).
			Subtitle(fmt.Sprintf("%s by %s, %s",
				pr.Project(),
				pr.User.Login,
				pr.UpdatedAt.Format("02-Jan-2006 15:04"))).
			Arg(pr.HtmlUrl).
			Valid(true)
	}

	return nil
}

func run() error {
	args := wf.Args()

	if len(args) == 0 {
		return missingArgsError
	}

	switch args[0] {
	case "auth":
		if len(args) < 2 {
			return missingArgsError
		}
		return wf.doAuth(args[1])
	case "list":
		return wf.doList()
	case "update":
		return wf.doUpdate()
	default:
		return unknownCmdError
	}
}

func main() {
	wf.Run(func() {
		if err := run(); err != nil {
			if err == kc.ErrNotFound {
				wf.Feedback.Clear()
				wf.NewItem("No API key set").
					Subtitle("Please use ghpr-auth to set your GitHub personal token").
					Arg("https://github.com/settings/tokens/new").
					Valid(false).
					Icon(aw.IconWarning)
			} else {
				wf.FatalError(err)
			}
		}
		wf.SendFeedback()
	})
}
